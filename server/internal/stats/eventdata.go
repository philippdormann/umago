package stats

import (
	"context"
	"time"
)

// KeyCount is one property key of a custom event.
type KeyCount struct {
	Key    string `json:"key"`
	Events int    `json:"events"`
}

// ValueCount is one value of a property key.
type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// EventKeys lists the property keys recorded for an event in [from, to).
// Reads raw events, so it only reaches as far back as retention.
func (s *Store) EventKeys(ctx context.Context, siteID, event string, from, to time.Time) ([]KeyCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ed.key, COUNT(DISTINCT ed.event_id) FROM event_data ed
		JOIN events e ON e.id = ed.event_id
		WHERE e.site_id = ? AND e.name = ? AND e.kind = 'event' AND e.ts >= ? AND e.ts < ?
		GROUP BY ed.key ORDER BY 2 DESC, 1 ASC LIMIT 100`,
		siteID, event, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KeyCount{}
	for rows.Next() {
		var k KeyCount
		if err := rows.Scan(&k.Key, &k.Events); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// EventValues lists the top values of one property key.
func (s *Store) EventValues(ctx context.Context, siteID, event, key string, from, to time.Time, limit int) ([]ValueCount, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT ed.sval, COUNT(*) FROM event_data ed
		JOIN events e ON e.id = ed.event_id
		WHERE e.site_id = ? AND e.name = ? AND e.kind = 'event' AND e.ts >= ? AND e.ts < ? AND ed.key = ?
		GROUP BY ed.sval ORDER BY 2 DESC, 1 ASC LIMIT ?`,
		siteID, event, from.Format("2006-01-02T15:04:05"), to.Format("2006-01-02T15:04:05"), key, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ValueCount{}
	for rows.Next() {
		var v ValueCount
		if err := rows.Scan(&v.Value, &v.Count); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
