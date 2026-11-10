package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/events"
	"github.com/philippdormann/umago/server/internal/funnels"
	"github.com/philippdormann/umago/server/internal/rollup"
	"github.com/philippdormann/umago/server/internal/sites"
	"github.com/philippdormann/umago/server/internal/stats"
	"github.com/philippdormann/umago/server/internal/users"
	"io"
	"log/slog"
)

// TestPostgresAnalytics exercises migrations plus the newest SQL (entry/exit
// rollups, visit_stats, funnel CASE, event_data join) on Postgres. It runs
// only when UMAGO_TEST_POSTGRES is set, e.g. a local scratch database.
func TestPostgresAnalytics(t *testing.T) {
	url := os.Getenv("UMAGO_TEST_POSTGRES")
	if url == "" {
		t.Skip("UMAGO_TEST_POSTGRES not set")
	}
	ctx := context.Background()
	db, err := database.OpenPostgres(url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if db.Dialect != database.Postgres {
		t.Fatalf("dialect: %s", db.Dialect)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	siteStore, w, st, fn := sites.New(db), events.NewWriter(db, log), stats.New(db), funnels.New(db)

	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	site, err := siteStore.Create(ctx, sites.Input{Domain: str("pg-example.com")})
	if err != nil {
		t.Fatal(err)
	}
	v1, err := events.Visit(ctx, db, site.ID, "h1", base)
	if err != nil {
		t.Fatal(err)
	}
	w.Enqueue(events.Event{SiteID: site.ID, At: base, Kind: "pageview", Path: "/", Visitor: "h1", VisitID: v1, Channel: "Direct"})
	w.Enqueue(events.Event{SiteID: site.ID, At: base.Add(5 * time.Minute), Kind: "pageview", Path: "/pricing", Visitor: "h1", VisitID: v1, Channel: "Direct"})
	w.Enqueue(events.Event{SiteID: site.ID, At: base.Add(6 * time.Minute), Kind: "event", Name: "signup", Path: "/pricing", Visitor: "h1", VisitID: v1, Props: map[string]any{"plan": "pro"}})
	w.Enqueue(events.Event{SiteID: site.ID, At: base.Add(7 * time.Minute), Kind: "vitals", Path: "/", Visitor: "h1", VisitID: v1, Vitals: &events.Vitals{LCP: f(800)}})
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := rollup.Day(ctx, db, site.ID, base); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	sum, err := st.Summary(ctx, site.ID, "24h", base.Add(2*time.Hour), 10)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if sum.Totals.Visits != 1 || sum.Totals.Bounces != 0 {
		t.Fatalf("visits: %+v", sum.Totals)
	}
	if len(sum.Breakdowns["entry"]) != 1 || len(sum.Breakdowns["exit"]) != 1 {
		t.Fatalf("entry/exit: %v %v", sum.Breakdowns["entry"], sum.Breakdowns["exit"])
	}
	keys, err := st.EventKeys(ctx, site.ID, "signup", base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || len(keys) != 1 || keys[0].Key != "plan" {
		t.Fatalf("event keys: %v %v", keys, err)
	}
	rep, err := st.Vitals(ctx, site.ID, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || rep.LCP.Count != 1 || rep.LCP.P75 != 800 {
		t.Fatalf("vitals: %+v %v", rep, err)
	}
	sess, err := st.Sessions(ctx, site.ID, base.Add(-time.Hour), base.Add(time.Hour), 10)
	if err != nil || len(sess) != 1 || sess[0].Pageviews != 2 {
		t.Fatalf("sessions: %+v %v", sess, err)
	}
	f, err := fn.Create(ctx, site.ID, "F", []funnels.Step{{Type: "page", Value: "/"}, {Type: "event", Value: "signup"}})
	if err != nil {
		t.Fatal(err)
	}
	res, err := fn.Evaluate(ctx, site.ID, f.Steps, base.Add(-time.Hour), base.Add(time.Hour))
	if err != nil || len(res.Steps) != 2 || res.Steps[1].Visitors != 1 {
		t.Fatalf("funnel: %+v %v", res, err)
	}
	us := users.New(db)
	u, err := us.Create(ctx, "pg-admin", "pg-password", users.RoleAdmin)
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	tm, err := us.CreateTeam(ctx, "PG", u.ID)
	if err != nil {
		t.Fatalf("team: %v", err)
	}
	if _, _, err := us.SiteAccess(ctx, u.ID, u.Role, site.ID); err != nil {
		t.Fatalf("access: %v", err)
	}
	_ = tm
	sh, err := us.CreateShare(ctx, site.ID, u.Username)
	if err != nil {
		t.Fatalf("share: %v", err)
	}
	if _, err := us.ShareBySlug(ctx, sh.Slug); err != nil {
		t.Fatalf("share lookup: %v", err)
	}
	ks := users.NewKeyStore(db)
	k, raw, err := ks.Create(ctx, "pg", users.ScopeReadWrite)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if _, ok := ks.Authenticate(ctx, raw); !ok {
		t.Fatalf("key auth")
	}
	if _, _, err := ks.Rotate(ctx, k.ID); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, ok := ks.Authenticate(ctx, raw); ok {
		t.Fatalf("old secret still works")
	}
}

func str(s string) *string { return &s }
func f(v float64) *float64 { return &v }
