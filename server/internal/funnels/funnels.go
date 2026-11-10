// Package funnels stores funnel definitions and evaluates them: how many
// visitor-days reach each step in order.
package funnels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// ErrNotFound is returned when a funnel does not exist.
var ErrNotFound = errors.New("funnel not found")

// ErrInvalid wraps validation failures.
var ErrInvalid = errors.New("invalid funnel")

// MaxSteps caps funnel length.
const MaxSteps = 8

// Step is one funnel stage: a page path or a custom event name.
type Step struct {
	Type  string `json:"type"` // page | event
	Value string `json:"value"`
}

// Funnel is a saved definition.
type Funnel struct {
	ID        string `json:"id"`
	SiteID    string `json:"site_id"`
	Name      string `json:"name"`
	Steps     []Step `json:"steps"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// StepResult is one evaluated stage.
type StepResult struct {
	Step     Step    `json:"step"`
	Visitors int     `json:"visitors"`
	Rate     float64 `json:"rate"` // share of step 1
	Drop     float64 `json:"drop"` // share lost vs previous step
}

// Result is an evaluated funnel.
type Result struct {
	Steps      []StepResult `json:"steps"`
	Truncated  bool         `json:"truncated,omitempty"`
	Conversion float64      `json:"conversion"`
}

// Store persists funnels and reads raw events to evaluate them.
type Store struct{ db *database.DB }

// New returns a Store.
func New(db *database.DB) *Store { return &Store{db: db} }

func checkSteps(steps []Step) error {
	if len(steps) < 2 || len(steps) > MaxSteps {
		return fmt.Errorf("%w: a funnel needs 2-%d steps", ErrInvalid, MaxSteps)
	}
	for _, st := range steps {
		if st.Type != "page" && st.Type != "event" {
			return fmt.Errorf("%w: step type must be page or event", ErrInvalid)
		}
		if strings.TrimSpace(st.Value) == "" || len(st.Value) > 200 {
			return fmt.Errorf("%w: step value must be 1-200 chars", ErrInvalid)
		}
	}
	return nil
}

// List returns a site's funnels.
func (s *Store) List(ctx context.Context, siteID string) ([]Funnel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, site_id, name, steps, created_at, updated_at FROM funnels WHERE site_id = ? ORDER BY created_at`, siteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Funnel{}
	for rows.Next() {
		var f Funnel
		var steps string
		if err := rows.Scan(&f.ID, &f.SiteID, &f.Name, &steps, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(steps), &f.Steps); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Get returns one funnel.
func (s *Store) Get(ctx context.Context, siteID, id string) (Funnel, error) {
	var f Funnel
	var steps string
	err := s.db.QueryRowContext(ctx, `SELECT id, site_id, name, steps, created_at, updated_at FROM funnels WHERE site_id = ? AND id = ?`, siteID, id).
		Scan(&f.ID, &f.SiteID, &f.Name, &steps, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		return Funnel{}, ErrNotFound
	}
	if err := json.Unmarshal([]byte(steps), &f.Steps); err != nil {
		return Funnel{}, err
	}
	return f, nil
}

// Create saves a funnel definition.
func (s *Store) Create(ctx context.Context, siteID, name string, steps []Step) (Funnel, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return Funnel{}, fmt.Errorf("%w: name must be 1-80 chars", ErrInvalid)
	}
	if err := checkSteps(steps); err != nil {
		return Funnel{}, err
	}
	b, _ := json.Marshal(steps)
	f := Funnel{ID: ids.New("fnl"), SiteID: siteID, Name: name, Steps: steps, CreatedAt: ids.Now(), UpdatedAt: ids.Now()}
	_, err := s.db.ExecContext(ctx, `INSERT INTO funnels (id, site_id, name, steps, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		f.ID, f.SiteID, f.Name, string(b), f.CreatedAt, f.UpdatedAt)
	if err != nil {
		return Funnel{}, err
	}
	return f, nil
}

// Update replaces a funnel definition.
func (s *Store) Update(ctx context.Context, siteID, id, name string, steps []Step) (Funnel, error) {
	f, err := s.Get(ctx, siteID, id)
	if err != nil {
		return Funnel{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 {
		return Funnel{}, fmt.Errorf("%w: name must be 1-80 chars", ErrInvalid)
	}
	if err := checkSteps(steps); err != nil {
		return Funnel{}, err
	}
	b, _ := json.Marshal(steps)
	f.Name, f.Steps, f.UpdatedAt = name, steps, ids.Now()
	_, err = s.db.ExecContext(ctx, `UPDATE funnels SET name = ?, steps = ?, updated_at = ? WHERE id = ? AND site_id = ?`,
		f.Name, string(b), f.UpdatedAt, id, siteID)
	return f, err
}

// Delete removes a funnel.
func (s *Store) Delete(ctx context.Context, siteID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM funnels WHERE id = ? AND site_id = ?`, id, siteID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// maxFunnelRows caps the raw sample a funnel folds.
const maxFunnelRows = 100000

// Evaluate counts visitor-days reaching each step in order within
// [from, to). It reads raw events, so it only reaches as far back as
// retention; longer windows flag truncated when the row cap hits.
func (s *Store) Evaluate(ctx context.Context, siteID string, steps []Step, from, to time.Time) (Result, error) {
	var out Result
	if err := checkSteps(steps); err != nil {
		return out, err
	}
	var orConds, caseWhens []string
	var vals []any
	for i, st := range steps {
		v := strings.TrimSpace(st.Value)
		if st.Type == "page" {
			orConds = append(orConds, `(kind = 'pageview' AND path = ?)`)
			caseWhens = append(caseWhens, fmt.Sprintf(`WHEN kind = 'pageview' AND path = ? THEN %d`, i+1))
		} else {
			orConds = append(orConds, `(kind = 'event' AND name = ?)`)
			caseWhens = append(caseWhens, fmt.Sprintf(`WHEN kind = 'event' AND name = ? THEN %d`, i+1))
		}
		vals = append(vals, v)
	}
	args := append([]any{}, vals...) // CASE placeholders come first in the query text
	args = append(args, siteID)
	args = append(args, vals...) // then the OR list
	args = append(args, ids.Format(from), ids.Format(to), maxFunnelRows+1)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT substr(ts, 1, 10), visitor, ts, id,
		CASE %s ELSE 0 END AS stepidx FROM events
		WHERE site_id = ? AND (%s) AND ts >= ? AND ts < ?
		ORDER BY 1, 2, 3, 4 LIMIT ?`, strings.Join(caseWhens, " "), strings.Join(orConds, " OR ")), args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	reached := make([]int, len(steps))
	n := 0
	var curDay, curVisitor string
	var next int
	for rows.Next() {
		var day, visitor, ts string
		var rowid int64
		var idx int
		if err := rows.Scan(&day, &visitor, &ts, &rowid, &idx); err != nil {
			return out, err
		}
		n++
		if n > maxFunnelRows {
			out.Truncated = true
			break
		}
		if day != curDay || visitor != curVisitor {
			curDay, curVisitor, next = day, visitor, 0
		}
		if idx == next+1 {
			next++
			reached[next-1]++
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Steps = make([]StepResult, len(steps))
	for i, st := range steps {
		r := StepResult{Step: st, Visitors: reached[i]}
		if reached[0] > 0 {
			r.Rate = float64(reached[i]) / float64(reached[0])
		}
		if i > 0 && reached[i-1] > 0 {
			r.Drop = 1 - float64(reached[i])/float64(reached[i-1])
		}
		out.Steps[i] = r
	}
	if reached[0] > 0 {
		out.Conversion = float64(reached[len(steps)-1]) / float64(reached[0])
	}
	return out, nil
}
