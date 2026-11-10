package stats

import (
	"context"
	"time"

	"github.com/philippdormann/umago/server/internal/ids"
)

// VisitRow is one visit for the sessions view.
type VisitRow struct {
	VisitID   string `json:"visit_id"`
	Started   string `json:"started"`
	Duration  int    `json:"duration_secs"`
	Pageviews int    `json:"pageviews"`
	Events    int    `json:"events"`
	Entry     string `json:"entry"`
	Exit      string `json:"exit"`
	Country   string `json:"country"`
	Browser   string `json:"browser"`
	Device    string `json:"device"`
}

// Sessions lists recent visits in [from, to), newest first. Reads raw
// events, so it only reaches as far back as retention.
func (s *Store) Sessions(ctx context.Context, siteID string, from, to time.Time, limit int) ([]VisitRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT visit_id, MIN(ts), MAX(ts),
		SUM(CASE WHEN kind = 'pageview' THEN 1 ELSE 0 END), SUM(CASE WHEN kind = 'event' THEN 1 ELSE 0 END)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? AND visit_id != ''
		GROUP BY visit_id ORDER BY MAX(ts) DESC LIMIT ?`,
		siteID, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"), limit)
	if err != nil {
		return nil, err
	}
	type agg struct {
		visit    string
		min, max string
		pv, ev   int
	}
	var aggs []agg
	for rows.Next() {
		var a agg
		if err := rows.Scan(&a.visit, &a.min, &a.max, &a.pv, &a.ev); err != nil {
			rows.Close()
			return nil, err
		}
		aggs = append(aggs, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]VisitRow, 0, len(aggs))
	for _, a := range aggs {
		r := VisitRow{VisitID: a.visit, Started: a.min, Pageviews: a.pv, Events: a.ev}
		if t0, e0 := ids.Parse(a.min); e0 == nil {
			if t1, e1 := ids.Parse(a.max); e1 == nil {
				if d := int(t1.Sub(t0).Seconds()); d > 0 {
					r.Duration = d
				}
			}
		}
		// Entry/exit paths plus landing-hit attributes.
		_ = s.db.QueryRowContext(ctx, `SELECT path, country, browser, device FROM events
			WHERE site_id = ? AND visit_id = ? AND kind = 'pageview' ORDER BY ts, id LIMIT 1`,
			siteID, a.visit).Scan(&r.Entry, &r.Country, &r.Browser, &r.Device)
		_ = s.db.QueryRowContext(ctx, `SELECT path FROM events
			WHERE site_id = ? AND visit_id = ? AND kind = 'pageview' ORDER BY ts DESC, id DESC LIMIT 1`,
			siteID, a.visit).Scan(&r.Exit)
		out = append(out, r)
	}
	return out, nil
}
