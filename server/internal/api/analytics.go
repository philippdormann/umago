package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/philippdormann/umago/server/internal/funnels"
	"github.com/philippdormann/umago/server/internal/sites"
	"github.com/philippdormann/umago/server/internal/stats"
)

// windowFor resolves range/custom/from/to into a window ending now.
func windowFor(r *http.Request, st sites.Site, now time.Time) (rng, bucket string, from, to time.Time, ok bool) {
	q := r.URL.Query()
	rng = siteRange(q.Get("range"), st)
	if rng == "custom" {
		from, to, bucket, ok = stats.ParseCustom(map[string]string{"from": q.Get("from"), "to": q.Get("to")})
		return rng, bucket, from, to, ok
	}
	if !stats.ValidRange(rng) {
		return "", "", time.Time{}, time.Time{}, false
	}
	from, to, bucket = stats.Window(rng, now)
	return rng, bucket, from, to, true
}

func rangeError(w http.ResponseWriter) {
	writeError(w, http.StatusBadRequest, "invalid", "range must be one of "+strings.Join(stats.Ranges, ", ")+", custom (with from/to YYYY-MM-DD)")
}

func (s *Server) siteStats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.freshen(r.Context())
	now := s.Now()
	rng, bucket, from, to, ok := windowFor(r, st, now)
	if !ok {
		rangeError(w)
		return
	}
	var sum stats.Summary
	if filters := stats.ParseFilters(r.URL.Query()); len(filters) > 0 {
		sum, err = s.Stats.FilteredSummaryCustom(r.Context(), st.ID, rng, from, to, bucket, now, 10, filters, s.retentionDays(r))
	} else if rng == "custom" {
		sum, err = s.Stats.SummaryCustom(r.Context(), st.ID, from, to, bucket, 10)
	} else {
		sum, err = s.Stats.Summary(r.Context(), st.ID, rng, now, 10)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	if r.URL.Query().Get("compare") == "prev" && len(sum.Filters) == 0 {
		if err := s.Stats.Compare(r.Context(), st.ID, &sum, now, 10); err != nil {
			s.fail(w, err)
			return
		}
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, now)
	writeJSON(w, http.StatusOK, map[string]any{"site": st, "live": live, "stats": sum})
}

func (s *Server) siteBreakdown(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	dim := q.Get("dim")
	if !stats.ValidDim(dim) {
		writeError(w, http.StatusBadRequest, "invalid", "dim must be one of "+strings.Join(stats.Dims, ", "))
		return
	}
	rng, _, from, to, ok := windowFor(r, st, s.Now())
	if !ok {
		rangeError(w)
		return
	}
	limit := 500
	if v, err := strconv.Atoi(q.Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	var rows []stats.Row
	if filters := stats.ParseFilters(q); len(filters) > 0 {
		rows, err = s.Stats.FilteredBreakdownCustom(r.Context(), st.ID, dim, from, to, limit, filters)
	} else if rng == "custom" {
		rows, err = s.Stats.BreakdownCustom(r.Context(), st.ID, dim, from, to, limit)
	} else {
		rows, err = s.Stats.BreakdownCustom(r.Context(), st.ID, dim, from, to, limit)
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dim": dim, "range": rng, "rows": rows})
}

// eventData lists property keys (or top values for ?key=) of a custom event.
func (s *Server) eventData(w http.ResponseWriter, r *http.Request) {
	st, err := s.Sites.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	q := r.URL.Query()
	event := strings.TrimSpace(q.Get("event"))
	if event == "" {
		writeError(w, http.StatusBadRequest, "invalid", "event is required")
		return
	}
	rng, _, from, to, ok := windowFor(r, st, s.Now())
	if !ok {
		rangeError(w)
		return
	}
	// Raw-event queries only reach back to retention.
	floor := s.Now().UTC().AddDate(0, 0, -s.retentionDays(r)).Truncate(24 * time.Hour)
	truncated := false
	if floor.After(from) {
		from, truncated = floor, true
	}
	if key := q.Get("key"); key != "" {
		vals, err := s.Stats.EventValues(r.Context(), st.ID, event, key, from, to, 100)
		if err != nil {
			s.fail(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"event": event, "key": key, "range": rng, "values": vals, "truncated": truncated})
		return
	}
	keys, err := s.Stats.EventKeys(r.Context(), st.ID, event, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"event": event, "range": rng, "keys": keys, "truncated": truncated})
}

// vitals reports Web Vitals percentiles.
func (s *Server) vitals(w http.ResponseWriter, r *http.Request) {
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
	rep, err := s.Stats.Vitals(r.Context(), st.ID, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rng, "vitals": rep})
}

// sessions lists recent visits.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
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
	floor := s.Now().UTC().AddDate(0, 0, -s.retentionDays(r)).Truncate(24 * time.Hour)
	truncated := false
	if floor.After(from) {
		from, truncated = floor, true
	}
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	rows, err := s.Stats.Sessions(r.Context(), st.ID, from, to, limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"range": rng, "sessions": rows, "truncated": truncated})
}

// resetSite deletes a site's events and rollups but keeps the site itself.
func (s *Server) resetSite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.Sites.Get(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	for _, q := range []string{
		`DELETE FROM event_data WHERE site_id = ?`,
		`DELETE FROM events WHERE site_id = ?`,
		`DELETE FROM visits WHERE site_id = ?`,
		`DELETE FROM hourly_stats WHERE site_id = ?`,
		`DELETE FROM daily_stats WHERE site_id = ?`,
		`DELETE FROM visit_stats WHERE site_id = ?`,
	} {
		if _, err := s.DB.ExecContext(r.Context(), q, id); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.Log.Info("site.reset", "id", id)
	w.WriteHeader(http.StatusNoContent)
}

// ---- funnels ----

func (s *Server) listFunnels(w http.ResponseWriter, r *http.Request) {
	list, err := s.Funnels.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"funnels": list})
}

func (s *Server) createFunnel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string         `json:"name"`
		Steps []funnels.Step `json:"steps"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	f, err := s.Funnels.Create(r.Context(), r.PathValue("id"), in.Name, in.Steps)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) updateFunnel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name  string         `json:"name"`
		Steps []funnels.Step `json:"steps"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	f, err := s.Funnels.Update(r.Context(), r.PathValue("id"), r.PathValue("fid"), in.Name, in.Steps)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) deleteFunnel(w http.ResponseWriter, r *http.Request) {
	if err := s.Funnels.Delete(r.Context(), r.PathValue("id"), r.PathValue("fid")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) evaluateFunnel(w http.ResponseWriter, r *http.Request) {
	f, err := s.Funnels.Get(r.Context(), r.PathValue("id"), r.PathValue("fid"))
	if err != nil {
		s.fail(w, err)
		return
	}
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
	floor := s.Now().UTC().AddDate(0, 0, -s.retentionDays(r)).Truncate(24 * time.Hour)
	truncated := false
	if floor.After(from) {
		from, truncated = floor, true
	}
	res, err := s.Funnels.Evaluate(r.Context(), st.ID, f.Steps, from, to)
	if err != nil {
		s.fail(w, err)
		return
	}
	if truncated {
		res.Truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"funnel": f, "range": rng, "result": res})
}
