// Package rollup rebuilds hourly and daily statistics from raw events.
//
// Today and yesterday (UTC) are rebuilt on every run because raw events for
// those days are still arriving; older days are final. Visitor hashes are
// day-scoped, so a day's visitor count is exact and multi-day totals are the
// sum of daily uniques.
package rollup

import (
	"context"
	"fmt"
	"github.com/philippdormann/umago/server/internal/database"
	"log/slog"
	"time"

	"github.com/philippdormann/umago/server/internal/ids"
)

// Dimensions stored in daily_stats.
var Dims = []string{"page", "ref", "entry", "exit", "channel", "country", "region", "language", "device", "browser", "os", "title", "hostname", "tag", "event", "utm_source", "utm_medium", "utm_campaign", "utm_content", "utm_term", "gclid", "fbclid", "msclkid", "ttclid", "li_fat_id", "twclid", "distinct_id"}

// DimColumn is the events column behind each dimension. entry and exit are
// per-visit first/last page paths and use their own queries below.
var DimColumn = map[string]string{
	"page": "path", "ref": "ref_host", "channel": "channel", "country": "country", "region": "region",
	"language": "language", "device": "device", "browser": "browser", "os": "os", "title": "title",
	"hostname": "hostname", "event": "name",
	"utm_source": "utm_source", "utm_medium": "utm_medium", "utm_campaign": "utm_campaign", "utm_content": "utm_content", "utm_term": "utm_term",
	"gclid": "gclid", "fbclid": "fbclid", "msclkid": "msclkid", "ttclid": "ttclid", "li_fat_id": "li_fat_id", "twclid": "twclid",
	"distinct_id": "distinct_id", "tag": "tag",
	// entry/exit filter on the underlying page path: matching rows keeps the
	// visitor's whole visit, which is the useful reading of the filter.
	"entry": "path", "exit": "path",
}

// NonemptyDims skips blank keys, which carry no signal for these dimensions.
var NonemptyDims = map[string]bool{
	"region": true, "language": true, "title": true, "hostname": true,
	"utm_source": true, "utm_medium": true, "utm_campaign": true, "utm_content": true, "utm_term": true,
	"gclid": true, "fbclid": true, "msclkid": true, "ttclid": true, "li_fat_id": true, "twclid": true,
	"distinct_id": true, "tag": true,
}

// MaxKeysPerDim caps distinct keys kept per dimension per day; the rest are
// folded into "Other" so one noisy site cannot fill the disk.
const MaxKeysPerDim = 500

// Run rebuilds every site's rollups for today and yesterday.
func Run(ctx context.Context, db *database.DB, log *slog.Logger, now time.Time) error {
	rows, err := db.QueryContext(ctx, `SELECT id FROM sites`)
	if err != nil {
		return err
	}
	var siteIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		siteIDs = append(siteIDs, id)
	}
	rows.Close()
	today := now.UTC().Truncate(24 * time.Hour)
	for _, id := range siteIDs {
		for _, day := range []time.Time{today.AddDate(0, 0, -1), today} {
			if err := Day(ctx, db, id, day); err != nil {
				log.Error("rollup.failed", "site", id, "day", day.Format("2006-01-02"), "error", err.Error())
			}
		}
	}
	return nil
}

// Day rebuilds one site's rollups for the UTC day starting at day.
func Day(ctx context.Context, db *database.DB, siteID string, day time.Time) error {
	day = day.UTC().Truncate(24 * time.Hour)
	dayKey := day.Format("2006-01-02")
	from, to := ids.Format(day), ids.Format(day.AddDate(0, 0, 1))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Hourly: pageviews and distinct visitors per hour.
	if _, err := tx.ExecContext(ctx, `DELETE FROM hourly_stats WHERE site_id = ? AND hour >= ? AND hour < ?`, siteID, dayKey+"T00", dayKey+"T24"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO hourly_stats (site_id, hour, pageviews, visitors)
		SELECT site_id, substr(ts, 1, 13), SUM(CASE WHEN kind = 'pageview' THEN 1 ELSE 0 END), COUNT(DISTINCT visitor)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? GROUP BY site_id, substr(ts, 1, 13)`, siteID, from, to); err != nil {
		return err
	}

	// Daily: totals plus each dimension.
	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_stats WHERE site_id = ? AND day = ?`, siteID, dayKey); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
		SELECT ?, ?, 'total', '', SUM(CASE WHEN kind = 'pageview' THEN 1 ELSE 0 END), COUNT(DISTINCT visitor)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? HAVING COUNT(*) > 0`, siteID, dayKey, siteID, from, to); err != nil {
		return err
	}
	for _, dim := range Dims {
		if dim == "entry" || dim == "exit" {
			if err := dayEntryExit(ctx, tx, siteID, dayKey, dim, from, to); err != nil {
				return err
			}
			continue
		}
		col := DimColumn[dim]
		kindFilter := `kind = 'pageview'`
		if dim == "event" {
			kindFilter = `kind = 'event'`
		}
		if NonemptyDims[dim] {
			kindFilter += ` AND ` + col + ` != ''`
		}
		// Top keys by pageviews (or event count), capped.
		q := fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
			SELECT site_id, ?, ?, %s, COUNT(*), COUNT(DISTINCT visitor)
			FROM events WHERE site_id = ? AND ts >= ? AND ts < ? AND %s
			GROUP BY site_id, %s ORDER BY COUNT(*) DESC LIMIT ?`, col, kindFilter, col)
		if _, err := tx.ExecContext(ctx, q, dayKey, dim, siteID, from, to, MaxKeysPerDim); err != nil {
			return err
		}
		// Fold the remainder into "Other".
		q = fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
			SELECT ?, ?, ?, 'Other', COUNT(*), COUNT(DISTINCT visitor) FROM events
			WHERE site_id = ? AND ts >= ? AND ts < ? AND %s
			AND %s NOT IN (SELECT key FROM daily_stats WHERE site_id = ? AND day = ? AND dim = ?)
			HAVING COUNT(*) > 0
			ON CONFLICT(site_id, day, dim, key) DO UPDATE SET pageviews = daily_stats.pageviews + excluded.pageviews, visitors = daily_stats.visitors + excluded.visitors`, kindFilter, col)
		if _, err := tx.ExecContext(ctx, q, siteID, dayKey, dim, siteID, from, to, siteID, dayKey, dim); err != nil {
			return err
		}
	}
	if err := dayVisits(ctx, tx, siteID, dayKey, from, to); err != nil {
		return err
	}
	return tx.Commit()
}

// dayEntryExit rolls up per-visit first (entry) or last (exit) page paths.
// One row per visit wins via the id tiebreak, so shared timestamps cannot
// double-count a visit.
func dayEntryExit(ctx context.Context, tx *database.Tx, siteID, dayKey, dim, from, to string) error {
	order := "m.ts, m.id"
	if dim == "exit" {
		order = "m.ts DESC, m.id DESC"
	}
	q := fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
		SELECT site_id, ?, ?, path, COUNT(*), COUNT(DISTINCT visitor)
		FROM events e WHERE site_id = ? AND ts >= ? AND ts < ? AND kind = 'pageview'
		AND id = (SELECT m.id FROM events m WHERE m.site_id = e.site_id AND m.visit_id = e.visit_id AND m.kind = 'pageview' ORDER BY %s LIMIT 1)
		GROUP BY site_id, path ORDER BY COUNT(*) DESC LIMIT ?`, order)
	if _, err := tx.ExecContext(ctx, q, dayKey, dim, siteID, from, to, MaxKeysPerDim); err != nil {
		return err
	}
	q = fmt.Sprintf(`INSERT INTO daily_stats (site_id, day, dim, key, pageviews, visitors)
		SELECT ?, ?, ?, 'Other', COUNT(*), COUNT(DISTINCT visitor) FROM events e
		WHERE site_id = ? AND ts >= ? AND ts < ? AND kind = 'pageview'
		AND id = (SELECT m.id FROM events m WHERE m.site_id = e.site_id AND m.visit_id = e.visit_id AND m.kind = 'pageview' ORDER BY %s LIMIT 1)
		AND path NOT IN (SELECT key FROM daily_stats WHERE site_id = ? AND day = ? AND dim = ?)
		HAVING COUNT(*) > 0
		ON CONFLICT(site_id, day, dim, key) DO UPDATE SET pageviews = daily_stats.pageviews + excluded.pageviews, visitors = daily_stats.visitors + excluded.visitors`, order)
	_, err := tx.ExecContext(ctx, q, siteID, dayKey, dim, siteID, from, to, siteID, dayKey, dim)
	return err
}

// dayVisits rebuilds the visit_stats row: visits, bounces (one pageview and
// no custom events), and total visit duration in seconds from raw events.
func dayVisits(ctx context.Context, tx *database.Tx, siteID, dayKey, from, to string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM visit_stats WHERE site_id = ? AND day = ?`, siteID, dayKey); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT visit_id, MIN(ts), MAX(ts),
		SUM(CASE WHEN kind = 'pageview' THEN 1 ELSE 0 END), SUM(CASE WHEN kind = 'event' THEN 1 ELSE 0 END)
		FROM events WHERE site_id = ? AND ts >= ? AND ts < ? AND visit_id != ''
		GROUP BY site_id, visit_id`, siteID, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	var visits, bounces int64
	var duration int64
	for rows.Next() {
		var vid, min, max string
		var pv, ev int64
		if err := rows.Scan(&vid, &min, &max, &pv, &ev); err != nil {
			return err
		}
		visits++
		if pv == 1 && ev == 0 {
			bounces++
		}
		if t0, e0 := ids.Parse(min); e0 == nil {
			if t1, e1 := ids.Parse(max); e1 == nil {
				if d := int64(t1.Sub(t0).Seconds()); d > 0 {
					duration += d
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if visits == 0 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO visit_stats (site_id, day, visits, bounces, duration) VALUES (?,?,?,?,?)`,
		siteID, dayKey, visits, bounces, duration)
	return err
}
