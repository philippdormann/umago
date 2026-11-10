package stats

import (
	"context"
	"sort"
	"time"
)

// VitalStats are percentiles for one Web Vital metric.
type VitalStats struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50"`
	P75   float64 `json:"p75"`
	P95   float64 `json:"p95"`
}

// VitalsReport is the vitals picture for a window: overall percentiles plus
// the slowest paths by LCP p75.
type VitalsReport struct {
	LCP   VitalStats  `json:"lcp"`
	INP   VitalStats  `json:"inp"`
	CLS   VitalStats  `json:"cls"`
	FCP   VitalStats  `json:"fcp"`
	TTFB  VitalStats  `json:"ttfb"`
	Paths []PathVital `json:"paths"`
}

// PathVital is one path's LCP p75 and sample count.
type PathVital struct {
	Path  string  `json:"path"`
	Count int     `json:"count"`
	P75   float64 `json:"p75"`
}

// maxVitalRows caps the raw sample percentiles are computed from.
const maxVitalRows = 20000

// Vitals computes Web Vitals percentiles from raw vitals hits.
func (s *Store) Vitals(ctx context.Context, siteID string, from, to time.Time) (VitalsReport, error) {
	var out VitalsReport
	rows, err := s.db.QueryContext(ctx, `SELECT path, lcp, inp, cls, fcp, ttfb FROM events
		WHERE site_id = ? AND kind = 'vitals' AND ts >= ? AND ts < ? LIMIT ?`,
		siteID, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"), maxVitalRows)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	var lcp, inp, cls, fcp, ttfb []float64
	byPath := map[string][]float64{}
	for rows.Next() {
		var path string
		var a, b, c, d, e *float64
		if err := rows.Scan(&path, &a, &b, &c, &d, &e); err != nil {
			return out, err
		}
		if a != nil {
			lcp = append(lcp, *a)
			byPath[path] = append(byPath[path], *a)
		}
		if b != nil {
			inp = append(inp, *b)
		}
		if c != nil {
			cls = append(cls, *c)
		}
		if d != nil {
			fcp = append(fcp, *d)
		}
		if e != nil {
			ttfb = append(ttfb, *e)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.LCP, out.INP, out.CLS, out.FCP, out.TTFB =
		percentiles(lcp), percentiles(inp), percentiles(cls), percentiles(fcp), percentiles(ttfb)
	for path, vals := range byPath {
		st := percentiles(vals)
		out.Paths = append(out.Paths, PathVital{Path: path, Count: st.Count, P75: st.P75})
	}
	sort.Slice(out.Paths, func(i, j int) bool { return out.Paths[i].P75 > out.Paths[j].P75 })
	if len(out.Paths) > 20 {
		out.Paths = out.Paths[:20]
	}
	return out, nil
}

func percentiles(vals []float64) VitalStats {
	st := VitalStats{Count: len(vals)}
	if len(vals) == 0 {
		return st
	}
	sort.Float64s(vals)
	at := func(q float64) float64 {
		i := int(q * float64(len(vals)-1))
		return vals[i]
	}
	st.P50, st.P75, st.P95 = at(0.5), at(0.75), at(0.95)
	return st
}
