package events

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// Limits for stored event properties.
const (
	maxProps     = 20
	maxPropKey   = 100
	maxPropValue = 500
)

type prop struct {
	key  string
	sval string
	nval *float64
}

// flattenProps turns a property map into storable rows: nested objects
// flatten with dot keys (two levels), arrays stringify, numbers keep a
// numeric value alongside their text. Keys sort for determinism.
func flattenProps(v map[string]any) []prop {
	out := []prop{}
	var walk func(prefix string, m map[string]any, depth int)
	walk = func(prefix string, m map[string]any, depth int) {
		for _, k := range sortedKeys(m) {
			if len(out) >= maxProps {
				return
			}
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			if key == "" {
				continue
			}
			if len(key) > maxPropKey {
				key = key[:maxPropKey]
			}
			out = append(out, propValue(key, m[k], depth)...)
		}
	}
	walk("", v, 0)
	return out
}

func propValue(key string, v any, depth int) []prop {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if len(t) > maxPropValue {
			t = t[:maxPropValue]
		}
		return []prop{{key: key, sval: t}}
	case bool:
		return []prop{{key: key, sval: strconv.FormatBool(t)}}
	case float64:
		s := strconv.FormatFloat(t, 'f', -1, 64)
		if len(s) > maxPropValue {
			s = s[:maxPropValue]
		}
		n := t
		return []prop{{key: key, sval: s, nval: &n}}
	case json.Number:
		s := t.String()
		if len(s) > maxPropValue {
			s = s[:maxPropValue]
		}
		p := prop{key: key, sval: s}
		if f, err := t.Float64(); err == nil {
			p.nval = &f
		}
		return []prop{p}
	case map[string]any:
		if depth >= 2 {
			return nil
		}
		out := []prop{}
		for _, k := range sortedKeys(t) {
			if len(out) >= maxProps {
				break
			}
			key2 := key + "." + k
			if len(key2) > maxPropKey {
				key2 = key2[:maxPropKey]
			}
			out = append(out, propValue(key2, t[k], depth+1)...)
		}
		return out
	default:
		b, err := json.Marshal(t)
		if err != nil || len(b) > maxPropValue {
			return nil
		}
		return []prop{{key: key, sval: string(b)}}
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Visit returns the visitor's current visit id, rotating to a new one after
// VisitTimeout of inactivity. It reads one indexed row per call; the writer
// batch refreshes last_at, so steady traffic never rotates.
func Visit(ctx context.Context, db *database.DB, siteID, visitor string, now time.Time) (string, error) {
	if visitor == "" {
		return "", nil
	}
	at := ids.Format(now)
	var id, last string
	err := db.QueryRowContext(ctx, `SELECT visit_id, last_at FROM visits WHERE site_id = ? AND visitor = ?`, siteID, visitor).Scan(&id, &last)
	if err == nil && id != "" {
		if t, perr := ids.Parse(last); perr == nil && now.Sub(t) < VisitTimeout {
			return id, nil
		}
	}
	id = ids.New("vst")
	_, err = db.ExecContext(ctx, `INSERT INTO visits (site_id, visitor, visit_id, started_at, last_at) VALUES (?,?,?,?,?)
		ON CONFLICT(site_id, visitor) DO UPDATE SET visit_id = excluded.visit_id, started_at = excluded.started_at, last_at = excluded.last_at`,
		siteID, visitor, id, at, at)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ParseProps decodes the raw property payload of a collect hit.
func ParseProps(raw []byte) map[string]any {
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
