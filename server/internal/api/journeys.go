package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/umago/server/internal/journeys"
)

// journeys reports the paths visits take: top page sequences, top pages
// per step, and top step-to-step transitions. It reads raw events, so it
// only reaches as far back as retention; longer windows are clipped and
// flagged truncated, like sessions and funnels.
func (s *Server) journeys(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	rng, _, from, to, ok := windowFor(r, st, s.Now())
	if !ok {
		rangeError(w)
		return
	}
	q := r.URL.Query()
	steps := journeys.MinSteps + 2 // default 4
	if v := strings.TrimSpace(q.Get("steps")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < journeys.MinSteps || n > journeys.MaxSteps {
			writeError(w, http.StatusBadRequest, "invalid", "steps must be 2-7")
			return
		}
		steps = n
	}
	limit := 20
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 100 {
			writeError(w, http.StatusBadRequest, "invalid", "limit must be 1-100")
			return
		}
		limit = n
	}
	start := strings.TrimSpace(q.Get("start"))
	if len(start) > 200 {
		writeError(w, http.StatusBadRequest, "invalid", "start must be 200 chars or shorter")
		return
	}
	floor := s.Now().UTC().AddDate(0, 0, -s.retentionDays(r)).Truncate(24 * time.Hour)
	truncated := false
	if floor.After(from) {
		from, truncated = floor, true
	}
	res, err := s.Journeys.Report(r.Context(), st.ID, from, to, steps, limit, start)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	if truncated {
		res.Truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rng, "journeys": res, "truncated": truncated})
}
