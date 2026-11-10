package geoip

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"

	"github.com/philippdormann/umago/server/internal/database"
)

// fixture builds a tiny city-type MMDB: 9.9.9.0/24 -> GB/London.
func fixture(t *testing.T) []byte {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-City",
		Description:  map[string]string{"en": "test fixture"},
		Languages:    []string{"en"},
		IPVersion:    6,
		RecordSize:   24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cidr, _ := net.ParseCIDR("9.9.9.0/24")
	err = tree.Insert(cidr, mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String("GB"),
			"names":    mmdbtype.Map{"en": mmdbtype.String("United Kingdom")},
		},
		"city": mmdbtype.Map{
			"names": mmdbtype.Map{"en": mmdbtype.String("London")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := tree.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func newStore(t *testing.T) *Store {
	t.Helper()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestDefaultOff(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	c, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != ModeOff || c.Source != SourceDBIP || c.HasData {
		t.Fatalf("defaults: %+v", c)
	}
	if _, _, ok := s.Lookup(ctx, "9.9.9.9"); ok {
		t.Fatal("disabled store must not resolve")
	}
}

func TestUploadAndLookup(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	raw := fixture(t)
	if _, err := Validate(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := Validate([]byte("nope")); err == nil {
		t.Fatal("garbage must fail validation")
	}
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCity)}); err != nil {
		t.Fatal(err)
	}
	if err := s.StoreData(ctx, raw, "GeoLite2-City", time.Now()); err != nil {
		t.Fatal(err)
	}
	c, err := s.Config(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasData || c.Edition != "GeoLite2-City" || c.SizeBytes != int64(len(raw)) {
		t.Fatalf("config: %+v", c)
	}
	country, city, ok := s.Lookup(ctx, "9.9.9.9")
	if !ok || country != "GB" || city != "London" {
		t.Fatalf("lookup: %q %q %v", country, city, ok)
	}
	// Country mode resolves the country but no city.
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCountry)}); err != nil {
		t.Fatal(err)
	}
	country, city, ok = s.Lookup(ctx, "9.9.9.9")
	if !ok || country != "GB" || city != "" {
		t.Fatalf("country lookup: %q %q %v", country, city, ok)
	}
	// Private IPs never resolve, even when enabled.
	if _, _, ok := s.Lookup(ctx, "192.168.1.20"); ok {
		t.Fatal("private IP must not resolve")
	}
	if _, _, ok := s.Lookup(ctx, "8.8.8.8"); ok {
		t.Fatal("unmapped IP must not resolve")
	}
	// Clearing the blob disables lookups again.
	if err := s.ClearData(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Lookup(ctx, "9.9.9.9"); ok {
		t.Fatal("cleared store must not resolve")
	}
}

func TestApplyValidation(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Apply(ctx, Patch{Mode: strp("planet")}); err == nil {
		t.Fatal("bad mode must fail")
	}
	if _, err := s.Apply(ctx, Patch{Source: strp("acme")}); err == nil {
		t.Fatal("bad source must fail")
	}
	s.ModeSet, s.Mode = true, ModeCountry
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCity)}); err == nil {
		t.Fatal("env-pinned mode must refuse dashboard changes")
	}
	if got := s.effectiveMode(ctx); got != ModeCountry {
		t.Fatalf("env pin: %q", got)
	}
}

func TestFilePathOverride(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	raw := fixture(t)
	path := filepath.Join(t.TempDir(), "test.mmdb")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s.FilePath = path
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCity)}); err != nil {
		t.Fatal(err)
	}
	// No blob stored, yet the file resolves.
	c, _ := s.Config(ctx)
	if c.HasData || c.FilePath != path {
		t.Fatalf("config: %+v", c)
	}
	if country, city, ok := s.Lookup(ctx, "9.9.9.9"); !ok || country != "GB" || city != "London" {
		t.Fatalf("file lookup: %q %q %v", country, city, ok)
	}
}

func TestRefreshDBIP(t *testing.T) {
	s := newStore(t)
	raw := fixture(t)
	var gz bytes.Buffer
	gzw := gzip.NewWriter(&gz)
	if _, err := gzw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	month := time.Now().UTC().Format("2006-01")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/free/") || !strings.HasSuffix(r.URL.Path, "dbip-city-lite-"+month+".mmdb.gz") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(gz.Bytes())
	}))
	defer srv.Close()
	s.DownloadBaseDBIP = srv.URL
	ctx := context.Background()
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCity), Source: strp(SourceDBIP)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if country, city, ok := s.Lookup(ctx, "9.9.9.9"); !ok || country != "GB" || city != "London" {
		t.Fatalf("post-refresh lookup: %q %q %v", country, city, ok)
	}
	// A failing source keeps the old blob and records the error.
	s.DownloadBaseDBIP = srv.URL + "/missing"
	if err := s.Refresh(ctx); err == nil {
		t.Fatal("broken download must fail")
	}
	if country, _, ok := s.Lookup(ctx, "9.9.9.9"); !ok || country != "GB" {
		t.Fatal("old blob must survive a failed refresh")
	}
	c, _ := s.Config(ctx)
	if c.SyncError == "" {
		t.Fatal("sync error must be recorded")
	}
}

func TestRefreshMaxMind(t *testing.T) {
	s := newStore(t)
	raw := fixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("edition_id") != "GeoLite2-City" || r.URL.Query().Get("license_key") != "key123" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		gz := gzip.NewWriter(w)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "GeoLite2-City_123/GeoLite2-City.mmdb", Mode: 0o644, Size: int64(len(raw)), ModTime: time.Now()})
		if _, err := tw.Write(raw); err != nil {
			t.Error(err)
		}
		tw.Close()
		gz.Close()
	}))
	defer srv.Close()
	s.DownloadBaseMaxMind = srv.URL
	ctx := context.Background()
	if _, err := s.Apply(ctx, Patch{Mode: strp(ModeCity), Source: strp(SourceMaxMind)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err == nil {
		t.Fatal("refresh without a key must fail")
	}
	if _, err := s.Apply(ctx, Patch{MaxMindKey: strp("key123")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if country, city, ok := s.Lookup(ctx, "9.9.9.9"); !ok || country != "GB" || city != "London" {
		t.Fatalf("post-refresh lookup: %q %q %v", country, city, ok)
	}
}

func strp(s string) *string { return &s }
