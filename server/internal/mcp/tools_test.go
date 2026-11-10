package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/events"
	"github.com/philippdormann/umago/server/internal/funnels"
	"github.com/philippdormann/umago/server/internal/sites"
	"github.com/philippdormann/umago/server/internal/stats"
	"io"
	"log/slog"
)

func TestNewTools(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return base }
	site, err := sites.New(db).Create(ctx, sites.Input{Domain: strp("m.example")})
	if err != nil {
		t.Fatal(err)
	}
	w := events.NewWriter(db, log)
	w.Enqueue(events.Event{SiteID: site.ID, At: base, Kind: "pageview", Path: "/", Visitor: "a", VisitID: "v1"})
	w.Enqueue(events.Event{SiteID: site.ID, At: base.Add(time.Minute), Kind: "event", Name: "signup", Path: "/", Visitor: "a", VisitID: "v1"})
	w.Enqueue(events.Event{SiteID: site.ID, At: base, Kind: "vitals", Path: "/", Visitor: "a", VisitID: "v1", Vitals: &events.Vitals{LCP: fp(700)}})
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	fn := funnels.New(db)
	f, err := fn.Create(ctx, site.ID, "S", []funnels.Step{{Type: "page", Value: "/"}}[:1])
	_ = f
	_ = err
	f2, err := fn.Create(ctx, site.ID, "S2", []funnels.Step{{Type: "page", Value: "/"}, {Type: "event", Value: "signup"}})
	if err != nil {
		t.Fatal(err)
	}
	tl := &tools{st: Stores{Sites: sites.New(db), Stats: stats.New(db), Funnels: fn, Now: now,
		RetentionDays: func(ctx context.Context) int { return 30 }}}

	if _, out, err := tl.sessions(ctx, nil, SessionsIn{Site: site.ID, Range: "24h"}); err != nil || len(out.Sessions) != 1 {
		t.Fatalf("sessions: %+v %v", out, err)
	}
	if _, out, err := tl.vitals(ctx, nil, VitalsIn{Site: site.ID, Range: "24h"}); err != nil || out.Vitals.LCP.Count != 1 {
		t.Fatalf("vitals: %+v %v", out, err)
	}
	if _, out, err := tl.funnel(ctx, nil, FunnelIn{Site: site.ID, Funnel: f2.Name, Range: "24h"}); err != nil || len(out.Result.Steps) != 2 || out.Result.Steps[1].Visitors != 1 {
		t.Fatalf("funnel: %+v %v", out, err)
	}
	if _, _, err := tl.funnel(ctx, nil, FunnelIn{Site: site.ID, Funnel: "nope", Range: "24h"}); err == nil {
		t.Fatal("missing funnel should error")
	}
}

func strp(s string) *string { return &s }
func fp(v float64) *float64 { return &v }
