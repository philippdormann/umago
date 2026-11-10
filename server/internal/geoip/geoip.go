// Package geoip adds optional IP-to-location resolution on top of the
// time-zone mapping in enrich. It stays completely off unless the mode is
// country or city (the default is off: no downloads, no extra memory).
//
// The MMDB file itself lives in the database as a blob so deployments need
// no extra volume and updates happen from the dashboard; at load it is
// spilled to a temp file and memory-mapped, so RSS barely moves. As an
// escape hatch UMAGO_GEOIP_PATH can point at an .mmdb file on disk, which
// then wins over the stored blob (DB-IP and MaxMind files both work, since
// the reader only needs the MMDB format).
package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// Modes: how much location to resolve. Off is the default.
const (
	ModeOff     = "off"
	ModeCountry = "country"
	ModeCity    = "city"
)

// Sources for the MMDB file.
const (
	SourceDBIP    = "dbip"    // DB-IP Lite, free download, no key, CC BY 4.0
	SourceMaxMind = "maxmind" // GeoLite2, needs a free license key
)

// rowID is the single row in the geoip table.
const rowID = "geoip"

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid geoip setting")

// Config is the dashboard-facing view. The key and the blob are never
// exposed; only whether a key is set and whether data is stored.
type Config struct {
	Mode          string `json:"mode"`
	ModeFromEnv   bool   `json:"mode_from_env"`
	Source        string `json:"source"`
	SourceFromEnv bool   `json:"source_from_env"`
	HasMaxMindKey bool   `json:"has_maxmind_key"`
	KeyFromEnv    bool   `json:"key_from_env"`
	Edition       string `json:"edition"`
	BuildDate     string `json:"build_date"`
	SizeBytes     int64  `json:"size_bytes"`
	HasData       bool   `json:"has_data"`
	SyncError     string `json:"sync_error"`
	SyncedAt      string `json:"synced_at"`
	// FilePath is set when UMAGO_GEOIP_PATH overrides the stored blob.
	FilePath string `json:"file_path"`
}

// Patch is a partial update: nil means "leave alone". An empty (non-nil)
// MaxMindKey clears the stored key.
type Patch struct {
	Mode       *string `json:"mode"`
	Source     *string `json:"source"`
	MaxMindKey *string `json:"maxmind_key"`
}

// Store persists the config and blob and serves lookups from a
// memory-mapped reader.
type Store struct {
	db  *database.DB
	log *slog.Logger

	// FilePath, when set (UMAGO_GEOIP_PATH), wins over the stored blob.
	FilePath string
	// Mode/source pins from the environment (UMAGO_GEOIP_MODE/SOURCE).
	Mode      string
	ModeSet   bool
	Source    string
	SourceSet bool
	// EnvKey (UMAGO_MAXMIND_LICENSE_KEY) wins over the stored key.
	EnvKey string

	// DownloadBase overrides the download hosts (tests).
	DownloadBaseDBIP    string
	DownloadBaseMaxMind string
	HTTP                *http.Client

	mu     sync.RWMutex
	reader *geoip2.Reader
	tmp    string // spilled blob, removed on replace
	now    func() time.Time
}

// New returns a Store. Call EnsureLoaded once at startup.
func New(db *database.DB, log *slog.Logger) *Store {
	return &Store{
		db: db, log: log,
		DownloadBaseDBIP:    "https://download.db-ip.com",
		DownloadBaseMaxMind: "https://download.maxmind.com",
		HTTP:                &http.Client{Timeout: 5 * time.Minute},
		now:                 time.Now,
	}
}

// row mirrors the geoip table.
type row struct {
	mode, source, key, edition string
	buildEpoch                 int64
	sha                        string
	size                       int64
	syncErr, syncedAt          string
	hasData                    bool
}

func (s *Store) readRow(ctx context.Context) (row, error) {
	var r row
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT mode, source, maxmind_key, edition, build_epoch, sha256, size_bytes, sync_error, synced_at, data FROM geoip WHERE id = ?`, rowID).
		Scan(&r.mode, &r.source, &r.key, &r.edition, &r.buildEpoch, &r.sha, &r.size, &r.syncErr, &r.syncedAt, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return row{mode: ModeOff, source: SourceDBIP}, nil
	}
	if err != nil {
		return row{}, err
	}
	r.hasData = len(data) > 0
	return r, nil
}

func validMode(m string) bool { return m == ModeOff || m == ModeCountry || m == ModeCity }

func validSource(s string) bool { return s == SourceDBIP || s == SourceMaxMind }

// Config returns the effective config: environment pins win over stored values.
func (s *Store) Config(ctx context.Context) (Config, error) {
	r, err := s.readRow(ctx)
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Mode: r.mode, Source: r.source,
		HasMaxMindKey: r.key != "",
		Edition:       r.edition, SizeBytes: r.size,
		HasData: r.hasData, SyncError: r.syncErr, SyncedAt: r.syncedAt,
		FilePath: s.FilePath,
	}
	if r.buildEpoch > 0 {
		c.BuildDate = time.Unix(r.buildEpoch, 0).UTC().Format("2006-01-02")
	}
	if s.ModeSet {
		c.Mode, c.ModeFromEnv = s.Mode, true
	}
	if s.SourceSet {
		c.Source, c.SourceFromEnv = s.Source, true
	}
	if s.EnvKey != "" {
		c.HasMaxMindKey, c.KeyFromEnv = true, true
	}
	return c, nil
}

// key returns the effective MaxMind key: environment wins.
func (s *Store) key(ctx context.Context) (string, error) {
	if s.EnvKey != "" {
		return s.EnvKey, nil
	}
	r, err := s.readRow(ctx)
	if err != nil {
		return "", err
	}
	return r.key, nil
}

// effectiveMode returns the mode honouring the environment pin.
func (s *Store) effectiveMode(ctx context.Context) string {
	if s.ModeSet {
		return s.Mode
	}
	r, err := s.readRow(ctx)
	if err != nil {
		return ModeOff
	}
	return r.mode
}

// Apply validates and saves a patch, then reloads the reader.
func (s *Store) Apply(ctx context.Context, p Patch) (Config, error) {
	r, err := s.readRow(ctx)
	if err != nil {
		return Config{}, err
	}
	if p.Mode != nil {
		if s.ModeSet {
			return Config{}, fmt.Errorf("%w: UMAGO_GEOIP_MODE is set in the environment and overrides this", ErrInvalid)
		}
		m := strings.ToLower(strings.TrimSpace(*p.Mode))
		if !validMode(m) {
			return Config{}, fmt.Errorf("%w: mode must be off, country or city", ErrInvalid)
		}
		r.mode = m
	}
	if p.Source != nil {
		if s.SourceSet {
			return Config{}, fmt.Errorf("%w: UMAGO_GEOIP_SOURCE is set in the environment and overrides this", ErrInvalid)
		}
		src := strings.ToLower(strings.TrimSpace(*p.Source))
		if !validSource(src) {
			return Config{}, fmt.Errorf("%w: source must be dbip or maxmind", ErrInvalid)
		}
		r.source = src
	}
	if p.MaxMindKey != nil {
		if s.EnvKey != "" {
			return Config{}, fmt.Errorf("%w: UMAGO_MAXMIND_LICENSE_KEY is set in the environment and overrides this", ErrInvalid)
		}
		k := strings.TrimSpace(*p.MaxMindKey)
		if len(k) > 200 {
			return Config{}, fmt.Errorf("%w: license key must be 200 characters or fewer", ErrInvalid)
		}
		r.key = k
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO geoip (id, mode, source, maxmind_key, updated_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET mode = excluded.mode, source = excluded.source, maxmind_key = excluded.maxmind_key, updated_at = excluded.updated_at`,
		rowID, r.mode, r.source, r.key, ids.Now()); err != nil {
		return Config{}, err
	}
	if err := s.reload(ctx); err != nil {
		s.log.Warn("geoip.reload_failed", "error", err.Error())
	}
	return s.Config(ctx)
}

// EnsureLoaded opens the file-path reader or spills the stored blob; a
// missing or broken database just leaves lookups disabled.
func (s *Store) EnsureLoaded(ctx context.Context) {
	if err := s.reload(ctx); err != nil {
		s.log.Warn("geoip.unavailable", "error", err.Error())
	}
}

// reload (re)opens the reader from the file path or the stored blob.
func (s *Store) reload(ctx context.Context) error {
	if s.FilePath != "" {
		return s.openFile(s.FilePath)
	}
	var data []byte
	if err := s.db.QueryRowContext(ctx, `SELECT data FROM geoip WHERE id = ?`, rowID).Scan(&data); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if len(data) == 0 {
		s.setReader(nil)
		return nil
	}
	f, err := os.CreateTemp("", "umago-geoip-*.mmdb")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	// The blob is fully on disk now; drop the heap copy before mapping.
	data = nil
	if err := s.openFile(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	s.mu.Lock()
	old := s.tmp
	s.tmp = tmp
	s.mu.Unlock()
	if old != "" {
		// Safe to unlink while mapped; the mapping survives until the
		// process exits and old readers are never closed mid-flight.
		os.Remove(old)
	}
	return nil
}

// openFile memory-maps path and swaps the reader.
func (s *Store) openFile(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if fi.Size() > 300<<20 {
		return fmt.Errorf("%w: file is larger than 300 MB", ErrInvalid)
	}
	rd, err := geoip2.Open(path)
	if err != nil {
		return fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	s.setReader(rd)
	s.log.Info("geoip.loaded", "edition", rd.Metadata().DatabaseType, "path", path)
	return nil
}

func (s *Store) setReader(rd *geoip2.Reader) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reader = rd
}

// Enabled reports whether lookups should run at all.
func (s *Store) Enabled(ctx context.Context) bool {
	return s.effectiveMode(ctx) != ModeOff && s.hasReader()
}

func (s *Store) hasReader() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reader != nil
}

// Lookup resolves an IP string to ISO country code and city name. The second
// return is "" unless the mode is city and the database knows a city.
// ok is false when disabled, unparsable, private, or unknown.
func (s *Store) Lookup(ctx context.Context, ipStr string) (country, city string, ok bool) {
	mode := s.effectiveMode(ctx)
	if mode == ModeOff {
		return "", "", false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(ipStr))
	if err != nil || !ip.IsValid() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return "", "", false
	}
	s.mu.RLock()
	rd := s.reader
	s.mu.RUnlock()
	if rd == nil {
		return "", "", false
	}
	addr := net.ParseIP(ip.String())
	if mode == ModeCity {
		if rec, err := rd.City(addr); err == nil {
			country = strings.ToUpper(strings.TrimSpace(rec.Country.IsoCode))
			city = cityName(rec.City.Names)
			if country != "" {
				return country, city, true
			}
		}
		// A country-edition file (or a gap in a city file): fall through
		// to the country lookup rather than giving up.
	}
	rec, err := rd.Country(addr)
	if err != nil {
		return "", "", false
	}
	country = strings.ToUpper(strings.TrimSpace(rec.Country.IsoCode))
	if len(country) != 2 {
		return "", "", false
	}
	return country, "", true
}

// cityName prefers English, then any available name.
func cityName(names map[string]string) string {
	if n := strings.TrimSpace(names["en"]); n != "" {
		return n
	}
	for _, n := range names {
		if strings.TrimSpace(n) != "" {
			return n
		}
	}
	return ""
}

// Validate checks raw .mmdb bytes and returns its database type.
func Validate(data []byte) (string, error) {
	if len(data) == 0 || len(data) > 300<<20 {
		return "", fmt.Errorf("%w: file must be 1 byte-300 MB", ErrInvalid)
	}
	f, err := os.CreateTemp("", "umago-geoip-check-*.mmdb")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return "", err
	}
	f.Close()
	rd, err := geoip2.Open(tmp)
	if err != nil {
		return "", fmt.Errorf("%w: not a valid .mmdb file", ErrInvalid)
	}
	defer rd.Close()
	t := rd.Metadata().DatabaseType
	if !strings.Contains(strings.ToLower(t), "city") && !strings.Contains(strings.ToLower(t), "country") {
		return "", fmt.Errorf("%w: database type %q is neither city nor country", ErrInvalid, t)
	}
	return t, nil
}

// StoreData validates and saves a blob, then reloads the reader.
func (s *Store) StoreData(ctx context.Context, data []byte, edition string, build time.Time) error {
	sum := sha256.Sum256(data)
	_, err := s.db.ExecContext(ctx, `INSERT INTO geoip (id, edition, build_epoch, sha256, size_bytes, synced_at, sync_error, updated_at, data)
		VALUES (?, ?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(id) DO UPDATE SET edition = excluded.edition, build_epoch = excluded.build_epoch,
		sha256 = excluded.sha256, size_bytes = excluded.size_bytes, synced_at = excluded.synced_at,
		sync_error = '', updated_at = excluded.updated_at, data = excluded.data`,
		rowID, edition, build.Unix(), hex.EncodeToString(sum[:]), len(data), ids.Now(), ids.Now(), data)
	if err != nil {
		return err
	}
	return s.reload(ctx)
}

// ClearData drops the stored blob (settings stay) and unloads the reader,
// unless a file path is configured.
func (s *Store) ClearData(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE geoip SET edition = '', build_epoch = 0, sha256 = '', size_bytes = 0, sync_error = '', synced_at = '', updated_at = ?, data = NULL WHERE id = ?`, ids.Now(), rowID); err != nil {
		return err
	}
	return s.reload(ctx)
}

func (s *Store) failSync(ctx context.Context, msg string) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO geoip (id, sync_error, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET sync_error = excluded.sync_error, updated_at = excluded.updated_at`, rowID, msg, ids.Now())
}

// editionFor picks the download edition for a mode.
func editionFor(source, mode string) string {
	if mode == ModeCity {
		if source == SourceMaxMind {
			return "GeoLite2-City"
		}
		return "city"
	}
	if source == SourceMaxMind {
		return "GeoLite2-Country"
	}
	return "country"
}

// Refresh downloads the database for the configured source/mode and stores
// it. It keeps the previous blob when the download fails.
func (s *Store) Refresh(ctx context.Context) error {
	r, err := s.readRow(ctx)
	if err != nil {
		return err
	}
	mode := r.mode
	if s.ModeSet {
		mode = s.Mode
	}
	source := r.source
	if s.SourceSet {
		source = s.Source
	}
	if mode == ModeOff {
		return fmt.Errorf("%w: turn the mode to country or city first", ErrInvalid)
	}
	var data []byte
	var build time.Time
	if source == SourceMaxMind {
		key, err := s.key(ctx)
		if err != nil {
			return err
		}
		if key == "" {
			return fmt.Errorf("%w: a MaxMind license key is required for the maxmind source", ErrInvalid)
		}
		data, _, build, err = s.downloadMaxMind(ctx, key, editionFor(source, mode))
	} else {
		data, _, build, err = s.downloadDBIP(ctx, editionFor(source, mode))
	}
	if err != nil {
		s.failSync(ctx, err.Error())
		return err
	}
	typ, err := Validate(data)
	if err != nil {
		s.failSync(ctx, err.Error())
		return err
	}
	if err := s.StoreData(ctx, data, typ, build); err != nil {
		s.failSync(ctx, err.Error())
		return err
	}
	s.log.Info("geoip.refreshed", "edition", typ, "bytes", len(data))
	return nil
}

// downloadDBIP fetches a DB-IP Lite file (gzipped .mmdb). Files are cut
// monthly, so it tries the current month, then falls back two months.
func (s *Store) downloadDBIP(ctx context.Context, which string) ([]byte, string, time.Time, error) {
	name := "dbip-country-lite"
	if which == "city" {
		name = "dbip-city-lite"
	}
	var last error
	for _, m := range []time.Time{s.now().UTC(), s.now().UTC().AddDate(0, -1, 0), s.now().UTC().AddDate(0, -2, 0)} {
		u := fmt.Sprintf("%s/free/%s-%s.mmdb.gz", s.DownloadBaseDBIP, name, m.Format("2006-01"))
		data, err := s.get(ctx, u, nil)
		if err != nil {
			last = err
			continue
		}
		gr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			last = err
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(gr, 300<<20+1))
		gr.Close()
		if err != nil || len(raw) == 0 || len(raw) > 300<<20 {
			last = fmt.Errorf("bad gzip body from %s", u)
			continue
		}
		return raw, name, m, nil
	}
	if last == nil {
		last = fmt.Errorf("no monthly file found")
	}
	return nil, "", time.Time{}, fmt.Errorf("db-ip download failed: %w", last)
}

// downloadMaxMind fetches a GeoLite2 tarball and extracts the .mmdb.
func (s *Store) downloadMaxMind(ctx context.Context, key, edition string) ([]byte, string, time.Time, error) {
	u := fmt.Sprintf("%s/app/geoip_download?edition_id=%s&license_key=%s&suffix=tar.gz", s.DownloadBaseMaxMind, edition, key)
	data, err := s.get(ctx, u, nil)
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("maxmind download failed: %w", err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, "", time.Time{}, fmt.Errorf("maxmind download failed: not a tarball (wrong key?)")
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return nil, "", time.Time{}, fmt.Errorf("maxmind download failed: no .mmdb inside")
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix(hdr.Name, ".mmdb") {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(tr, 300<<20+1))
		if err != nil || len(raw) == 0 || len(raw) > 300<<20 {
			return nil, "", time.Time{}, fmt.Errorf("maxmind download failed: bad .mmdb body")
		}
		mtime := hdr.ModTime.UTC()
		if mtime.IsZero() {
			mtime = s.now().UTC()
		}
		return raw, edition, mtime, nil
	}
}

func (s *Store) get(ctx context.Context, url string, header map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("GET %s: %s %s", url, res.Status, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(res.Body, 300<<20+1))
}
