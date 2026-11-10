// Package api exposes the Umago HTTP API: the public collect endpoint and
// snippet, and the admin API behind the login.
package api

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/philippdormann/umago/server/internal/database"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/philippdormann/umago/server/internal/auth"
	"github.com/philippdormann/umago/server/internal/enrich"
	"github.com/philippdormann/umago/server/internal/events"
	"github.com/philippdormann/umago/server/internal/favicons"
	"github.com/philippdormann/umago/server/internal/funnels"
	"github.com/philippdormann/umago/server/internal/geoip"
	"github.com/philippdormann/umago/server/internal/journeys"
	"github.com/philippdormann/umago/server/internal/links"
	"github.com/philippdormann/umago/server/internal/mcp"
	"github.com/philippdormann/umago/server/internal/polar"
	"github.com/philippdormann/umago/server/internal/rollup"
	"github.com/philippdormann/umago/server/internal/searchconsole"
	"github.com/philippdormann/umago/server/internal/settings"
	"github.com/philippdormann/umago/server/internal/sites"
	"github.com/philippdormann/umago/server/internal/stats"
	"github.com/philippdormann/umago/server/internal/tokens"
	"github.com/philippdormann/umago/server/internal/users"
)

// Version is the server version, overridden at build time via -ldflags.
var Version = "dev"

//go:embed umago.js
var snippet []byte

var snippetETag = func() string {
	sum := sha256.Sum256(snippet)
	return `"` + hex.EncodeToString(sum[:8]) + `"`
}()

// Server holds every dependency the handlers need.
type Server struct {
	DB       *database.DB
	Log      *slog.Logger
	Sites    *sites.Store
	Settings *settings.Store
	Writer   *events.Writer
	Stats    *stats.Store
	Favicons *favicons.Fetcher
	Admin    *auth.Admin
	Web      http.Handler
	Now      func() time.Time
	// TrustProxy reads the client IP from X-Forwarded-For (set by Traefik).
	TrustProxy bool
	// MCPToken grants read-only access to /mcp when set.
	MCPToken string
	Tokens   *tokens.Store
	Links    *links.Store
	Funnels  *funnels.Store
	Journeys *journeys.Store
	// GeoIP resolves IP location when its mode is country or city; nil or
	// off means time-zone mapping only.
	GeoIP *geoip.Store
	Users *users.Store
	Keys  *users.KeyStore
	// Google links sites to Search Console for search terms.
	Google *searchconsole.Service
	// Polar links sites to a Polar organisation for revenue.
	Polar *polar.Service
	// Retention defaults and whether the environment pins them.
	RetentionDays    int
	RetentionFromEnv bool
	StartedAt        time.Time
	DatabasePath     string
	// ScriptNames lists the top-level .js paths the snippet is served
	// from. Empty means the default (umago.js); duplicates are served
	// once.
	ScriptNames []string

	rollMu   sync.Mutex
	lastRoll time.Time
}

// searchStore is nil when no Google service is wired (tests).
func (s *Server) searchStore() *searchconsole.Store {
	if s.Google == nil {
		return nil
	}
	return s.Google.Store
}

// polarStore is nil when no Polar service is wired (tests).
func (s *Server) polarStore() *polar.Store {
	if s.Polar == nil {
		return nil
	}
	return s.Polar.Store
}

func (s *Server) retentionDaysCtx(ctx context.Context) int {
	if g, err := s.Settings.General(ctx, s.RetentionDays, s.RetentionFromEnv); err == nil && g.RetentionDays > 0 {
		return g.RetentionDays
	}
	return s.RetentionDays
}

// retentionDays is how far back raw events, and so filtered views, reach.
func (s *Server) retentionDays(r *http.Request) int {
	if g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv); err == nil && g.RetentionDays > 0 {
		return g.RetentionDays
	}
	return s.RetentionDays
}

// freshen rebuilds today's rollups if the last rebuild is older than 30s, so
// a dashboard request never shows raw-event counts ahead of the totals.
func (s *Server) freshen(ctx context.Context) {
	s.rollMu.Lock()
	defer s.rollMu.Unlock()
	if time.Since(s.lastRoll) < 30*time.Second {
		return
	}
	if err := s.Writer.Flush(); err != nil {
		s.Log.Warn("rollup.flush_failed", "error", err.Error())
	}
	if err := rollup.Run(ctx, s.DB, s.Log, s.Now()); err != nil {
		s.Log.Warn("rollup.on_demand_failed", "error", err.Error())
		return
	}
	s.lastRoll = time.Now()
}

// Handler builds the router.
func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)

	// Public.
	for _, name := range s.scriptNames() {
		mux.HandleFunc("GET /"+name, s.script)
	}
	mux.HandleFunc("GET /api/v1/theme", s.theme)
	mux.HandleFunc("POST /api/v1/collect", s.collect)
	mux.HandleFunc("OPTIONS /api/v1/collect", s.collectOptions)
	mux.HandleFunc("POST /api/v1/batch", s.batch)
	mux.HandleFunc("GET /l/{slug}", s.linkClick)
	mux.HandleFunc("GET /p/{slug}", s.pixelHit)

	// Admin session.
	mux.HandleFunc("GET /api/v1/auth/me", s.authMe)
	mux.HandleFunc("POST /api/v1/auth/login", s.authLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", s.authLogout)
	mux.HandleFunc("POST /api/v1/auth/2fa/verify", s.verify2FA)
	mux.Handle("GET /api/v1/auth/2fa/status", s.adminAuth(s.totpStatus))
	mux.Handle("POST /api/v1/auth/2fa/setup", s.adminAuth(s.totpSetup))
	mux.Handle("POST /api/v1/auth/2fa/confirm", s.adminAuth(s.totpConfirm))
	mux.Handle("POST /api/v1/auth/2fa/disable", s.adminAuth(s.totpDisable))
	mux.HandleFunc("GET /api/v1/share/{slug}", s.publicShare)

	// Admin.
	mux.Handle("GET /api/v1/sites", s.adminAuth(s.listSites))
	mux.Handle("POST /api/v1/sites", s.adminAuth(s.createSite))
	mux.Handle("POST /api/v1/sites/reorder", s.adminAuth(s.reorderSites))
	mux.Handle("GET /api/v1/sites/{id}", s.adminAuth(s.getSite))
	mux.Handle("PATCH /api/v1/sites/{id}", s.adminAuth(s.updateSite))
	mux.Handle("DELETE /api/v1/sites/{id}", s.adminAuth(s.deleteSite))
	mux.Handle("GET /api/v1/sites/{id}/stats", s.adminAuth(s.siteStats))
	mux.Handle("GET /api/v1/sites/{id}/breakdown", s.adminAuth(s.siteBreakdown))
	mux.Handle("GET /api/v1/sites/{id}/live", s.adminAuth(s.siteLive))
	mux.Handle("POST /api/v1/sites/{id}/refresh-favicon", s.adminAuth(s.refreshFavicon))
	mux.Handle("GET /api/v1/sites/{id}/favicon", s.adminAuth(s.siteFavicon))
	mux.Handle("GET /api/v1/sites/{id}/google", s.adminAuth(s.googleStatus))
	mux.Handle("GET /api/v1/sites/{id}/google/connect", s.adminAuth(s.googleConnect))
	mux.Handle("PATCH /api/v1/sites/{id}/google", s.adminAuth(s.googleSetProperty))
	mux.Handle("DELETE /api/v1/sites/{id}/google", s.adminAuth(s.googleDisconnect))
	mux.Handle("POST /api/v1/sites/{id}/google/sync", s.adminAuth(s.googleSync))
	mux.Handle("GET /api/v1/sites/{id}/search-terms", s.adminAuth(s.searchTerms))
	mux.HandleFunc("GET "+googleCallbackPath, s.googleCallback)
	mux.Handle("GET /api/v1/sites/{id}/polar", s.adminAuth(s.polarGet))
	mux.Handle("PUT /api/v1/sites/{id}/polar", s.adminAuth(s.polarConnect))
	mux.Handle("DELETE /api/v1/sites/{id}/polar", s.adminAuth(s.polarDisconnect))
	mux.Handle("POST /api/v1/sites/{id}/polar/sync", s.adminAuth(s.polarSync))
	mux.Handle("GET /api/v1/sites/{id}/revenue", s.adminAuth(s.siteRevenue))
	mux.Handle("GET /api/v1/sites/{id}/links", s.adminAuth(s.listLinks))
	mux.Handle("POST /api/v1/sites/{id}/links", s.adminAuth(s.createLink))
	mux.Handle("DELETE /api/v1/sites/{id}/links/{linkID}", s.adminAuth(s.deleteLink))
	mux.Handle("GET /api/v1/sites/{id}/event-data", s.adminAuth(s.eventData))
	mux.Handle("GET /api/v1/sites/{id}/vitals", s.adminAuth(s.vitals))
	mux.Handle("GET /api/v1/sites/{id}/sessions", s.adminAuth(s.sessions))
	mux.Handle("POST /api/v1/sites/{id}/reset", s.adminAuth(s.resetSite))
	mux.Handle("GET /api/v1/sites/{id}/funnels", s.adminAuth(s.listFunnels))
	mux.Handle("POST /api/v1/sites/{id}/funnels", s.adminAuth(s.createFunnel))
	mux.Handle("PUT /api/v1/sites/{id}/funnels/{fid}", s.adminAuth(s.updateFunnel))
	mux.Handle("DELETE /api/v1/sites/{id}/funnels/{fid}", s.adminAuth(s.deleteFunnel))
	mux.Handle("GET /api/v1/sites/{id}/funnels/{fid}", s.adminAuth(s.evaluateFunnel))
	mux.Handle("GET /api/v1/sites/{id}/journeys", s.adminAuth(s.journeys))
	mux.HandleFunc("POST /api/v1/polar/webhook/{id}", s.polarWebhook)
	mux.Handle("GET /api/v1/favicon", s.adminAuth(s.refFavicon))
	mux.Handle("GET /api/v1/status", s.adminAuth(s.status))
	mux.Handle("GET /api/v1/settings", s.adminAuth(s.getSettings))
	mux.Handle("PATCH /api/v1/settings", s.adminAuth(s.updateSettings))
	mux.Handle("GET /api/v1/tokens", s.adminAuth(s.listTokens))
	mux.Handle("POST /api/v1/tokens", s.adminAuth(s.createToken))
	mux.Handle("DELETE /api/v1/tokens/{id}", s.adminAuth(s.deleteToken))
	mux.Handle("GET /api/v1/keys", s.adminAuth(s.listKeys))
	mux.Handle("POST /api/v1/keys", s.adminAuth(s.createKey))
	mux.Handle("POST /api/v1/keys/{id}/rotate", s.adminAuth(s.rotateKey))
	mux.Handle("DELETE /api/v1/keys/{id}", s.adminAuth(s.deleteKey))
	mux.Handle("GET /api/v1/users", s.adminAuth(s.listUsers))
	mux.Handle("POST /api/v1/users", s.adminAuth(s.createUser))
	mux.Handle("PATCH /api/v1/users/{uid}", s.adminAuth(s.updateUser))
	mux.Handle("DELETE /api/v1/users/{uid}", s.adminAuth(s.deleteUser))
	mux.Handle("GET /api/v1/teams", s.adminAuth(s.listTeams))
	mux.Handle("POST /api/v1/teams", s.adminAuth(s.createTeam))
	mux.Handle("PATCH /api/v1/teams/{tid}", s.adminAuth(s.renameTeam))
	mux.Handle("DELETE /api/v1/teams/{tid}", s.adminAuth(s.deleteTeam))
	mux.Handle("GET /api/v1/teams/{tid}/members", s.adminAuth(s.listMembers))
	mux.Handle("POST /api/v1/teams/{tid}/members", s.adminAuth(s.addMember))
	mux.Handle("DELETE /api/v1/teams/{tid}/members/{uid}", s.adminAuth(s.removeMember))
	mux.Handle("GET /api/v1/sites/{id}/shares", s.adminAuth(s.listShares))
	mux.Handle("POST /api/v1/sites/{id}/shares", s.adminAuth(s.createShare))
	mux.Handle("DELETE /api/v1/sites/{id}/shares/{sid}", s.adminAuth(s.deleteShare))
	mux.Handle("POST /api/v1/rollup", s.adminAuth(s.rollupNow))
	mux.Handle("GET /api/v1/export", s.adminAuth(s.export))
	mux.Handle("GET /api/v1/geoip", s.adminAuth(s.geoipGet))
	mux.Handle("PUT /api/v1/geoip", s.adminAuth(s.geoipUpdate))
	mux.Handle("POST /api/v1/geoip/refresh", s.adminAuth(s.geoipRefresh))
	mux.Handle("POST /api/v1/geoip/upload", s.adminAuth(s.geoipUpload))
	mux.Handle("DELETE /api/v1/geoip/data", s.adminAuth(s.geoipClearData))
	mux.HandleFunc("GET /api/v1/openapi.json", s.openapi)

	// MCP (read-only) for AI agents: Streamable HTTP at /mcp.
	mux.Handle("/mcp", s.mcpAuth(mcp.Handler(mcp.NewServer(mcp.Stores{RetentionDays: s.retentionDaysCtx, Sites: s.Sites, Stats: s.Stats, Search: s.searchStore(), Revenue: s.polarStore(), Funnels: s.Funnels, Journeys: s.Journeys, Now: s.Now}, Version), s.Log)))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such endpoint")
	})
	if s.Web != nil {
		mux.Handle("/", s.Web)
	}
	return s.logging(mux)
}

// ---- middleware ----

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(rw, r)
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/collect" {
			s.Log.Debug("http.request", "method", r.Method, "path", r.URL.Path, "status", rw.status, "ms", time.Since(start).Milliseconds())
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// mcpAuth accepts the configured UMAGO_MCP_TOKEN as a bearer token, or an
// admin session / HTTP Basic login (or nothing when admin auth is off).
func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv); err == nil && !g.MCPEnabled {
			writeError(w, http.StatusNotFound, "mcp_disabled", "the MCP endpoint is turned off in Settings")
			return
		}
		if tok := bearer(r); tok != "" {
			if s.MCPToken != "" && auth.Equal(auth.Hash(tok), auth.Hash(s.MCPToken)) {
				next.ServeHTTP(w, r)
				return
			}
			if s.Tokens != nil {
				if _, ok := s.Tokens.Authenticate(r.Context(), tok); ok {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid MCP token")
			return
		}
		if !s.Admin.Authorized(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "an MCP token (UMAGO_MCP_TOKEN) or admin login is required: Authorization: Bearer ...")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---- public: snippet and collect ----

// scriptNames returns the deduplicated snippet paths to serve, defaulting
// to umago.js for backwards compatibility.
func (s *Server) scriptNames() []string {
	seen := map[string]bool{}
	var names []string
	for _, n := range s.ScriptNames {
		n = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(n), "/"))
		if n == "" || seen[n] || strings.Contains(n, "/") {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	if len(names) == 0 {
		return []string{"umago.js"}
	}
	return names
}

func (s *Server) script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", snippetETag)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Header.Get("If-None-Match") == snippetETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(snippet)
}

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

func (s *Server) collectOptions(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.WriteHeader(http.StatusNoContent)
}

type collectBody struct {
	Site     string          `json:"s"`
	Name     string          `json:"n"`
	URL      string          `json:"u"`
	Referrer string          `json:"r"`
	Width    int             `json:"w"`
	TZ       string          `json:"tz"`
	Props    json.RawMessage `json:"x"`
	// ID is the site's own user id, set by umago.identify().
	ID string `json:"id"`
	// Tag labels traffic, from the snippet's data-tag attribute.
	Tag string `json:"tag"`
	// Lang is navigator.language, Title is document.title.
	Lang  string `json:"lang"`
	Title string `json:"title"`
	// Web Vitals, sent by kind=vitals hits. Absent metrics are nil.
	LCP  *float64 `json:"lcp"`
	INP  *float64 `json:"inp"`
	CLS  *float64 `json:"cls"`
	FCP  *float64 `json:"fcp"`
	TTFB *float64 `json:"ttfb"`
}

// collect accepts one event. It always answers 202 so the snippet cannot
// be used to probe which site ids exist; invalid events are dropped.
func (s *Server) collect(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	var in collectBody
	b, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil || json.Unmarshal(b, &in) != nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	s.ingest(r, in)
	w.WriteHeader(http.StatusAccepted)
}

// batch accepts up to 50 events in one request, sharing ingest with collect.
func (s *Server) batch(w http.ResponseWriter, r *http.Request) {
	cors(w)
	w.Header().Set("Cache-Control", "no-store")
	var in struct {
		Events []collectBody `json:"events"`
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || json.Unmarshal(b, &in) != nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	for i, e := range in.Events {
		if i >= 50 {
			break
		}
		s.ingest(r, e)
	}
	w.WriteHeader(http.StatusAccepted)
}

// ingest validates one hit and enqueues it, dropping quietly on any problem.
func (s *Server) ingest(r *http.Request, in collectBody) {
	ctx := r.Context()
	if strings.TrimSpace(r.Header.Get("DNT")) == "1" {
		s.Log.Debug("collect.dropped", "reason", "do not track")
		return
	}
	site, ok := s.Sites.Lookup(ctx, strings.TrimSpace(in.Site))
	if !ok {
		s.Log.Debug("collect.dropped", "reason", "unknown site", "site", in.Site)
		return
	}
	path, host := enrich.Path(in.URL)
	if !enrich.LocalHost(host) && !enrich.SameSite(host, site.Domain) {
		s.Log.Debug("collect.dropped", "reason", "host does not match site domain", "host", host, "domain", site.Domain)
		return
	}
	ua := enrich.ParseUA(r.UserAgent(), in.Width)
	if ua.Bot {
		s.Log.Debug("collect.dropped", "reason", "bot user agent", "ua", r.UserAgent())
		return
	}
	now := s.Now()
	salt, err := s.Settings.Salt(ctx, now)
	if err != nil {
		s.Log.Error("collect.salt_failed", "error", err.Error())
		return
	}
	visitor := events.VisitorHash(salt, site.ID, s.clientIP(r), r.UserAgent())
	visit, err := events.Visit(ctx, s.DB, site.ID, visitor, now)
	if err != nil {
		s.Log.Error("collect.visit_failed", "error", err.Error())
		return
	}
	refHost := enrich.Referrer(in.Referrer, site.Domain)
	utmSrc, utmMed, utmCamp, utmCont, utmTerm := enrich.UTMTags(in.URL)
	ads := enrich.ParseAdTags(in.URL)
	country, region := enrich.Country(r.Header, in.TZ), enrich.Region(in.TZ)
	if s.GeoIP != nil && enrich.HeaderCountry(r.Header) == "" {
		// No proxy header: an enabled GeoIP database beats the time zone,
		// and a known city beats the time-zone city part.
		if c, city, ok := s.GeoIP.Lookup(ctx, s.clientIP(r)); ok {
			country = c
			if city != "" {
				region = city
			}
		}
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > 500 {
		title = title[:500]
	}
	lang := strings.TrimSpace(in.Lang)
	if len(lang) > 35 {
		lang = lang[:35]
	}
	tag := strings.TrimSpace(in.Tag)
	if len(tag) > 50 {
		tag = tag[:50]
	}
	ev := events.Event{
		SiteID: site.ID, At: now, Kind: events.KindPageview, Path: path, Hostname: host, Tag: tag,
		RefHost: refHost, Country: country, Region: region,
		Device: ua.Device, Browser: ua.Browser, OS: ua.OS,
		UTMSrc: utmSrc, UTMMed: utmMed, UTMCamp: utmCamp, UTMCont: utmCont, UTMTerm: utmTerm,
		Gclid: ads.Gclid, Fbclid: ads.Fbclid, Msclkid: ads.Msclkid, Ttclid: ads.Ttclid, LiFatID: ads.LiFatID, Twclid: ads.Twclid,
		Channel:  enrich.ChannelWithAds(refHost, utmSrc, utmMed, ads),
		Language: lang, Title: title, Visitor: visitor, VisitID: visit,
		DistinctID: strings.TrimSpace(in.ID),
	}
	switch name := strings.TrimSpace(in.Name); {
	case name == "" || name == "pageview":
	case name == "identify":
		ev.Kind = events.KindIdentify
		ev.Props = events.ParseProps(in.Props)
	case name == "vitals":
		ev.Kind = events.KindVitals
		ev.Vitals = &events.Vitals{LCP: clampVital(in.LCP, 3600000), INP: clampVital(in.INP, 3600000), CLS: clampVital(in.CLS, 100), FCP: clampVital(in.FCP, 3600000), TTFB: clampVital(in.TTFB, 3600000)}
	default:
		if len(name) > 50 {
			name = name[:50]
		}
		ev.Kind, ev.Name = events.KindEvent, name
		ev.Props = events.ParseProps(in.Props)
	}
	if len(ev.DistinctID) > 50 {
		ev.DistinctID = ev.DistinctID[:50]
	}
	s.Writer.Enqueue(ev)
}

// clampVital drops impossible measurements so a rogue client cannot skew
// the vitals percentiles.
func clampVital(v *float64, max float64) *float64 {
	if v == nil || *v < 0 || *v > max {
		return nil
	}
	return v
}

func (s *Server) clientIP(r *http.Request) string {
	if s.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i > 0 {
				xff = xff[:i]
			}
			return strings.TrimSpace(xff)
		}
		if rip := r.Header.Get("X-Real-IP"); rip != "" {
			return strings.TrimSpace(rip)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---- auth ----

func (s *Server) authMe(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.Admin.RequestIdentity(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_required": s.Admin.Enabled(), "authenticated": ok,
		"username": sess.Username, "role": sess.Role,
	})
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !s.Admin.Enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"auth_required": false, "authenticated": true})
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tok, need2FA, ok := s.Admin.StartLogin(r.Context(), in.Username, in.Password)
	if !ok {
		time.Sleep(400 * time.Millisecond)
		s.Log.Warn("auth.login_failed", "remote", r.RemoteAddr)
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong username or password")
		return
	}
	if need2FA {
		writeJSON(w, http.StatusOK, map[string]any{"auth_required": true, "authenticated": false, "requires_2fa": true, "challenge": tok})
		return
	}
	s.Admin.SetCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": true, "authenticated": true})
}

// verify2FA redeems a login challenge with a TOTP or backup code.
func (s *Server) verify2FA(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	tok, ok := s.Admin.FinishLogin(r.Context(), in.Token, in.Code)
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_code", "wrong code")
		return
	}
	s.Admin.SetCookie(w, r, tok)
	writeJSON(w, http.StatusOK, map[string]any{"auth_required": true, "authenticated": true})
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookie); err == nil {
		s.Admin.Logout(r.Context(), c.Value)
	}
	s.Admin.ClearCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// ---- sites (admin) ----

type siteView struct {
	sites.Site
	Card stats.SiteCard `json:"card"`
	Live int            `json:"live"`
}

func (s *Server) listSites(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	list, err := s.Sites.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	// Team members only see their sites.
	if !id.global && !id.share {
		visible := list[:0]
		for _, st := range list {
			if read, _, err := s.Users.SiteAccess(r.Context(), id.userID, id.role, st.ID); err == nil && read {
				visible = append(visible, st)
			}
		}
		list = visible
	}
	s.freshen(r.Context())
	now := s.Now()
	out := make([]siteView, 0, len(list))
	for _, st := range list {
		card, err := s.Stats.Card(r.Context(), st.ID, now)
		if err != nil {
			s.fail(w, err)
			return
		}
		live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, now)
		out = append(out, siteView{Site: st, Card: card, Live: live})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sites": out})
}

func (s *Server) createSite(w http.ResponseWriter, r *http.Request) {
	var in sites.Input
	if !readJSON(w, r, &in) {
		return
	}
	// Only global admins may pin a site to a team at creation.
	if in.OwnerTeamID != nil && *in.OwnerTeamID != "" {
		id, ok := s.identify(r)
		if !ok || !id.global || (!id.envAdmin && id.role != users.RoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "admins only")
			return
		}
		if _, err := s.Users.GetTeam(r.Context(), *in.OwnerTeamID); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "invalid", "no such team")
			return
		}
	}
	st, err := s.Sites.Create(r.Context(), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("site.created", "id", st.ID, "domain", st.Domain)
	go s.fetchSiteFavicon(st)
	card, err := s.Stats.Card(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusCreated, siteView{Site: st, Card: card, Live: live})
}

// fetchSiteFavicon runs in the background after create or on request.
func (s *Server) fetchSiteFavicon(st sites.Site) {
	ctx, cancel := bgContext()
	defer cancel()
	data, ctype, err := s.Favicons.ForDomain(ctx, st.Domain)
	if err != nil {
		s.Log.Debug("favicon.not_found", "domain", st.Domain, "error", err.Error())
		_ = s.Sites.SetFavicon(ctx, st.ID, nil, "")
		return
	}
	if err := s.Sites.SetFavicon(ctx, st.ID, data, ctype); err != nil {
		s.Log.Error("favicon.store_failed", "site", st.ID, "error", err.Error())
	}
}

func (s *Server) getSite(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	card, err := s.Stats.Card(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusOK, siteView{Site: st, Card: card, Live: live})
}

func (s *Server) updateSite(w http.ResponseWriter, r *http.Request) {
	var in sites.Input
	if !readJSON(w, r, &in) {
		return
	}
	// Only global admins may move a site between teams.
	if in.OwnerTeamID != nil {
		id, ok := s.identify(r)
		if !ok || !id.global || (!id.envAdmin && id.role != users.RoleAdmin) {
			writeError(w, http.StatusForbidden, "forbidden", "admins only")
			return
		}
		if *in.OwnerTeamID != "" {
			if _, err := s.Users.GetTeam(r.Context(), *in.OwnerTeamID); err != nil {
				writeError(w, http.StatusUnprocessableEntity, "invalid", "no such team")
				return
			}
		}
	}
	st, err := s.Sites.Update(r.Context(), r.PathValue("id"), in)
	if err != nil {
		s.fail(w, err)
		return
	}
	if in.Domain != nil {
		go s.fetchSiteFavicon(st)
	}
	card, err := s.Stats.Card(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusOK, siteView{Site: st, Card: card, Live: live})
}

func (s *Server) deleteSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Sites.Delete(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("site.deleted", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderSites(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Sites.Reorder(r.Context(), in.IDs); err != nil {
		s.fail(w, err)
		return
	}
	s.listSites(w, r)
}

// siteRange resolves the range for a site request: the query parameter when
// given, else the range saved on the site, else the server default. It lets
// the dashboard open on a site's own default without a second round trip.
func siteRange(param string, st sites.Site) string {
	if param != "" {
		return param
	}
	if st.DefaultRange != "" {
		return st.DefaultRange
	}
	return stats.DefaultRange
}

func (s *Server) siteLive(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	live, err := s.Stats.LiveSnapshot(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, live)
}

func (s *Server) refreshFavicon(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.fetchSiteFavicon(st)
	st, _ = s.Sites.Get(r.Context(), st.ID)
	card, err := s.Stats.Card(r.Context(), st.ID, s.Now())
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	writeJSON(w, http.StatusOK, siteView{Site: st, Card: card, Live: live})
}

func (s *Server) siteFavicon(w http.ResponseWriter, r *http.Request) {
	data, ctype, err := s.Sites.Favicon(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	serveIcon(w, r, data, ctype)
}

func (s *Server) refFavicon(w http.ResponseWriter, r *http.Request) {
	data, ctype, err := s.Favicons.Cached(r.Context(), r.URL.Query().Get("host"))
	if err != nil {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	serveIcon(w, r, data, ctype)
}

func serveIcon(w http.ResponseWriter, r *http.Request, data []byte, ctype string) {
	if len(data) == 0 {
		w.Header().Set("Cache-Control", "private, max-age=3600")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=604800")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// ---- settings, tokens, theme ----

func (s *Server) theme(w http.ResponseWriter, r *http.Request) {
	g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]string{"accent": g.Accent, "title": g.Title, "script_name": s.scriptNames()[0]})
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	g, err := s.Settings.General(r.Context(), s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) updateSettings(w http.ResponseWriter, r *http.Request) {
	var p settings.Patch
	if !readJSON(w, r, &p) {
		return
	}
	g, err := s.Settings.Apply(r.Context(), p, s.RetentionDays, s.RetentionFromEnv)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Tokens.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": list, "env_token_set": s.MCPToken != ""})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t, raw, err := s.Tokens.Create(r.Context(), in.Name)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("token.created", "id", t.ID, "name", t.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"token": t, "secret": raw})
}

func (s *Server) deleteToken(w http.ResponseWriter, r *http.Request) {
	if err := s.Tokens.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- status & export ----

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	var sitesN, eventsN, dailyN int
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM sites`).Scan(&sitesN)
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM events`).Scan(&eventsN)
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM daily_stats`).Scan(&dailyN)
	var geoipBytes int64
	_ = s.DB.QueryRowContext(r.Context(), `SELECT COALESCE(size_bytes, 0) FROM geoip WHERE id = 'geoip'`).Scan(&geoipBytes)
	var dbBytes int64
	if s.DatabasePath != "" {
		if fi, err := os.Stat(s.DatabasePath); err == nil {
			dbBytes = fi.Size()
		}
	}
	uptime := int64(0)
	if !s.StartedAt.IsZero() {
		uptime = int64(time.Since(s.StartedAt).Seconds())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version": Version, "sites": sitesN, "raw_events": eventsN, "daily_rows": dailyN, "db_bytes": dbBytes, "geoip_bytes": geoipBytes, "uptime_seconds": uptime,
		"written": s.Writer.Written.Load(), "dropped": s.Writer.Dropped(), "admin_auth": s.Admin.Enabled(), "env_token_set": s.MCPToken != "",
	})
}

func (s *Server) export(w http.ResponseWriter, r *http.Request) {
	list, err := s.Sites.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT site_id, day, dim, key, pageviews, visitors FROM daily_stats ORDER BY site_id, day, dim, key`)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer rows.Close()
	type dayRow struct {
		SiteID    string `json:"site_id"`
		Day       string `json:"day"`
		Dim       string `json:"dim"`
		Key       string `json:"key"`
		Pageviews int    `json:"pageviews"`
		Visitors  int    `json:"visitors"`
	}
	daily := []dayRow{}
	for rows.Next() {
		var d dayRow
		if err := rows.Scan(&d.SiteID, &d.Day, &d.Dim, &d.Key, &d.Pageviews, &d.Visitors); err != nil {
			s.fail(w, err)
			return
		}
		daily = append(daily, d)
	}
	now := s.Now().UTC()
	w.Header().Set("Content-Disposition", `attachment; filename="umago-export-`+now.Format("20060102")+`.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"version": Version, "exported_at": now.Format(time.RFC3339), "sites": list, "daily_stats": daily})
}

// rollupNow flushes the queue and rebuilds today's and yesterday's rollups
// so the dashboard reflects the last few minutes immediately.
func (s *Server) rollupNow(w http.ResponseWriter, r *http.Request) {
	if err := s.Writer.Flush(); err != nil {
		s.fail(w, err)
		return
	}
	if err := rollup.Run(r.Context(), s.DB, s.Log, s.Now()); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body must be 1 MB or smaller")
		return false
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object")
		return false
	}
	if err := json.Unmarshal(b, v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sites.ErrNotFound), errors.Is(err, tokens.ErrNotFound), errors.Is(err, searchconsole.ErrNotConnected), errors.Is(err, polar.ErrNotConnected), errors.Is(err, links.ErrNotFound), errors.Is(err, funnels.ErrNotFound), errors.Is(err, users.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, polar.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	case strings.HasPrefix(err.Error(), "polar returned"):
		writeError(w, http.StatusBadGateway, "polar", err.Error())
	case errors.Is(err, searchconsole.ErrReconnect):
		writeError(w, http.StatusConflict, "reconnect", err.Error())
	case strings.HasPrefix(err.Error(), "google returned"):
		writeError(w, http.StatusBadGateway, "google", err.Error())
	case errors.Is(err, sites.ErrInvalid), errors.Is(err, tokens.ErrInvalid), errors.Is(err, settings.ErrInvalid), errors.Is(err, links.ErrInvalid), errors.Is(err, funnels.ErrInvalid), errors.Is(err, users.ErrInvalid):
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
	default:
		s.Log.Error("http.error", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "something went wrong")
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": Version, "queued_drops": strconv.FormatInt(s.Writer.Dropped(), 10)})
}
