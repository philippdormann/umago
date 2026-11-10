// Package journeys builds path-flow reports from raw page views: which
// page sequences visits take, which pages sit at each step, and how visits
// flow from one page to the next.
package journeys

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/philippdormann/umago/server/internal/database"
	"github.com/philippdormann/umago/server/internal/ids"
)

// Limits for a report.
const (
	MinSteps = 2
	MaxSteps = 7
	// maxRows caps the raw sample a report folds; hitting it flags truncated.
	maxRows = 100000
	// maxPaths caps distinct sequences kept; the rest still count visits.
	maxPaths = 20000
)

// Path is one page sequence with the visits that took it as a prefix.
type Path struct {
	Pages  []string `json:"pages"`
	Visits int      `json:"visits"`
	Rate   float64  `json:"rate"` // share of all visits in the window
}

// StepPage is one page's visits at one step position (1-based).
type StepPage struct {
	Page   string `json:"page"`
	Visits int    `json:"visits"`
}

// Transition is one step-to-step hop with its visits.
type Transition struct {
	Step   int    `json:"step"` // the step the hop leaves (1-based)
	From   string `json:"from"`
	To     string `json:"to"`
	Visits int    `json:"visits"`
}

// transitionKey is one step-to-step hop.
type transitionKey struct {
	step     int
	from, to string
}

// Report is a journey over a window.
type Report struct {
	Steps       int          `json:"steps"`
	Visits      int          `json:"visits"` // visits with at least one pageview
	Paths       []Path       `json:"paths"`
	StepPages   [][]StepPage `json:"step_pages"` // per step position, busiest first
	Transitions []Transition `json:"transitions"`
	Truncated   bool         `json:"truncated,omitempty"`
}

// Store reads raw events to build reports.
type Store struct{ db *database.DB }

// New returns a Store.
func New(db *database.DB) *Store { return &Store{db: db} }

// Report builds the journey for [from, to), following at most steps pages
// per visit. start, when non-empty, keeps only visits whose first page is
// start. limit caps the paths and transitions returned. It reads raw events,
// so it only reaches as far back as retention.
func (s *Store) Report(ctx context.Context, siteID string, from, to time.Time, steps, limit int, start string) (Report, error) {
	var out Report
	if steps < MinSteps || steps > MaxSteps {
		return out, fmt.Errorf("steps must be %d-%d", MinSteps, MaxSteps)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	out.Steps = steps
	rows, err := s.db.QueryContext(ctx, `SELECT visit_id, path FROM events
		WHERE site_id = ? AND ts >= ? AND ts < ? AND kind = 'pageview' AND visit_id != ''
		ORDER BY visit_id, ts, id LIMIT ?`,
		siteID, ids.Format(from), ids.Format(to), maxRows+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	visits := map[string][]string{}
	order := []string{}
	n := 0
	for rows.Next() {
		var vid, path string
		if err := rows.Scan(&vid, &path); err != nil {
			return out, err
		}
		n++
		if n > maxRows {
			out.Truncated = true
			break
		}
		if _, ok := visits[vid]; !ok {
			order = append(order, vid)
		}
		visits[vid] = append(visits[vid], path)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	stepCounts := make([]map[string]int, steps)
	for i := range stepCounts {
		stepCounts[i] = map[string]int{}
	}
	transCounts := map[transitionKey]int{}
	pathCounts := map[string]int{}
	pathOrder := []string{}
	kept := 0
	for _, vid := range order {
		pages := visits[vid]
		if len(pages) == 0 {
			continue
		}
		if start != "" && pages[0] != start {
			continue
		}
		if len(pages) > steps {
			pages = pages[:steps]
		}
		out.Visits++
		for i, p := range pages {
			stepCounts[i][p]++
			if i > 0 {
				transCounts[transitionKey{step: i, from: pages[i-1], to: p}]++
			}
		}
		key := strings.Join(pages, "\x00")
		if _, ok := pathCounts[key]; !ok {
			if kept >= maxPaths {
				out.Truncated = true
				continue
			}
			kept++
			pathOrder = append(pathOrder, key)
		}
		pathCounts[key]++
	}
	for _, key := range pathOrder {
		p := Path{Pages: strings.Split(key, "\x00"), Visits: pathCounts[key]}
		if out.Visits > 0 {
			p.Rate = float64(p.Visits) / float64(out.Visits)
		}
		out.Paths = append(out.Paths, p)
	}
	sort.Slice(out.Paths, func(i, j int) bool {
		if out.Paths[i].Visits != out.Paths[j].Visits {
			return out.Paths[i].Visits > out.Paths[j].Visits
		}
		return strings.Join(out.Paths[i].Pages, " ") < strings.Join(out.Paths[j].Pages, " ")
	})
	if len(out.Paths) > limit {
		out.Paths = out.Paths[:limit]
	}
	out.StepPages = make([][]StepPage, steps)
	for i, m := range stepCounts {
		for page, v := range m {
			out.StepPages[i] = append(out.StepPages[i], StepPage{Page: page, Visits: v})
		}
		sort.Slice(out.StepPages[i], func(a, b int) bool {
			if out.StepPages[i][a].Visits != out.StepPages[i][b].Visits {
				return out.StepPages[i][a].Visits > out.StepPages[i][b].Visits
			}
			return out.StepPages[i][a].Page < out.StepPages[i][b].Page
		})
		if len(out.StepPages[i]) > limit {
			out.StepPages[i] = out.StepPages[i][:limit]
		}
		if out.StepPages[i] == nil {
			out.StepPages[i] = []StepPage{}
		}
	}
	for k, v := range transCounts {
		out.Transitions = append(out.Transitions, Transition{Step: k.step, From: k.from, To: k.to, Visits: v})
	}
	sort.Slice(out.Transitions, func(i, j int) bool {
		if out.Transitions[i].Visits != out.Transitions[j].Visits {
			return out.Transitions[i].Visits > out.Transitions[j].Visits
		}
		if out.Transitions[i].Step != out.Transitions[j].Step {
			return out.Transitions[i].Step < out.Transitions[j].Step
		}
		if out.Transitions[i].From != out.Transitions[j].From {
			return out.Transitions[i].From < out.Transitions[j].From
		}
		return out.Transitions[i].To < out.Transitions[j].To
	})
	if len(out.Transitions) > limit {
		out.Transitions = out.Transitions[:limit]
	}
	if out.Paths == nil {
		out.Paths = []Path{}
	}
	if out.Transitions == nil {
		out.Transitions = []Transition{}
	}
	return out, nil
}
