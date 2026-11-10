package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVisitsEntryExit(t *testing.T) {
	s := newServer(t, "", "")
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "W", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)

	// Visitor A: / then /pricing 5 minutes later (one visit, not bouncing).
	// Visitor B: / once (one visit, bouncing).
	at := func(min int) time.Time { return base.Add(time.Duration(min) * time.Minute) }
	hit := func(ts time.Time, ip, url string) {
		s.Now = func() time.Time { return ts }
		body := map[string]any{"s": site.ID, "u": url, "tz": "Europe/London"}
		if rr := do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": ip}); rr.Code != 202 {
			t.Fatalf("collect: %d", rr.Code)
		}
	}
	hit(at(0), "1.1.1.1", "https://example.com/")
	hit(at(5), "1.1.1.1", "https://example.com/pricing")
	hit(at(6), "2.2.2.2", "https://example.com/")
	s.Now = func() time.Time { return base }
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if rr := do(t, h, "POST", "/api/v1/rollup", nil, nil); rr.Code != 204 {
		t.Fatalf("rollup: %d", rr.Code)
	}

	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/stats?range=24h", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("stats: %d %s", rr.Code, rr.Body)
	}
	// Decode loosely instead.
	var raw map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &raw)
	tot := raw["stats"].(map[string]any)["totals"].(map[string]any)
	if int(tot["visits"].(float64)) != 2 || int(tot["bounces"].(float64)) != 1 || int(tot["duration"].(float64)) != 300 {
		t.Fatalf("visit totals: %v", tot)
	}
	bd := raw["stats"].(map[string]any)["breakdowns"].(map[string]any)
	entry := bd["entry"].([]any)
	if len(entry) != 1 || entry[0].(map[string]any)["key"] != "/" || int(entry[0].(map[string]any)["pageviews"].(float64)) != 2 {
		t.Fatalf("entry: %v", entry)
	}
	exit := bd["exit"].([]any)
	keys := map[string]bool{}
	for _, r := range exit {
		keys[r.(map[string]any)["key"].(string)] = true
	}
	if !keys["/pricing"] || !keys["/"] {
		t.Fatalf("exit: %v", exit)
	}
}

func TestCustomRangeCompareReset(t *testing.T) {
	s := newServer(t, "", "")
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "C", "domain": "example.com"}, nil)
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	hit := func(ts time.Time) {
		s.Now = func() time.Time { return ts }
		do(t, h, "POST", "/api/v1/collect", map[string]any{"s": site.ID, "u": "https://example.com/", "tz": "Europe/London"},
			map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "5.5.5.5"})
	}
	hit(base.Add(-48 * time.Hour))
	hit(base)
	s.Now = func() time.Time { return base }
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if rr := do(t, h, "POST", "/api/v1/rollup", nil, nil); rr.Code != 204 {
		t.Fatalf("rollup: %d", rr.Code)
	}

	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/stats?range=custom&from=2026-09-01&to=2026-09-03", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"range":"custom"`) {
		t.Fatalf("custom: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/stats?range=custom&from=nope&to=2026-09-03", nil, nil)
	if rr.Code != 400 {
		t.Fatalf("bad custom: %d", rr.Code)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/stats?range=24h&compare=prev", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"prev_series"`) || !strings.Contains(rr.Body.String(), `"prev_breakdowns"`) {
		t.Fatalf("compare: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/sessions?range=24h", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"sessions"`) {
		t.Fatalf("sessions: %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "POST", "/api/v1/sites/"+site.ID+"/reset", nil, nil); rr.Code != 204 {
		t.Fatalf("reset: %d", rr.Code)
	}
	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 0 {
		t.Fatalf("events after reset: %d", n)
	}
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM daily_stats`).Scan(&n)
	if n != 0 {
		t.Fatalf("rollups after reset: %d", n)
	}
}

func TestLivePages(t *testing.T) {
	s := newServer(t, "", "")
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "L", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	hit := func(ts time.Time, ip, url string) {
		s.Now = func() time.Time { return ts }
		body := map[string]any{"s": site.ID, "u": url, "tz": "Europe/London"}
		if rr := do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": ip}); rr.Code != 202 {
			t.Fatalf("collect: %d", rr.Code)
		}
	}
	hit(base.Add(-2*time.Minute), "1.1.1.1", "https://example.com/pricing")
	hit(base.Add(-1*time.Minute), "2.2.2.2", "https://example.com/pricing")
	hit(base.Add(-1*time.Minute), "2.2.2.2", "https://example.com/")
	hit(base.Add(-30*time.Minute), "3.3.3.3", "https://example.com/old")
	s.Now = func() time.Time { return base }
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/live", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("live: %d %s", rr.Code, rr.Body)
	}
	var live struct {
		Total int `json:"total"`
		Pages []struct {
			Key       string `json:"key"`
			Pageviews int    `json:"pageviews"`
			Visitors  int    `json:"visitors"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &live); err != nil {
		t.Fatal(err)
	}
	if len(live.Pages) != 2 || live.Pages[0].Key != "/pricing" || live.Pages[0].Pageviews != 2 || live.Pages[0].Visitors != 2 {
		t.Fatalf("live pages: %+v", live.Pages)
	}
	if live.Total != 2 {
		t.Fatalf("live total: %d", live.Total)
	}
}

func TestJourneys(t *testing.T) {
	s := newServer(t, "", "")
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "J", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	hit := func(ts time.Time, ip, url string) {
		s.Now = func() time.Time { return ts }
		body := map[string]any{"s": site.ID, "u": url, "tz": "Europe/London"}
		if rr := do(t, h, "POST", "/api/v1/collect", body, map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": ip}); rr.Code != 202 {
			t.Fatalf("collect: %d", rr.Code)
		}
	}
	hit(base.Add(0*time.Minute), "1.1.1.1", "https://example.com/")
	hit(base.Add(1*time.Minute), "1.1.1.1", "https://example.com/pricing")
	hit(base.Add(0*time.Minute), "2.2.2.2", "https://example.com/")
	hit(base.Add(2*time.Minute), "2.2.2.2", "https://example.com/pricing")
	hit(base.Add(0*time.Minute), "3.3.3.3", "https://example.com/")
	hit(base.Add(3*time.Minute), "3.3.3.3", "https://example.com/about")
	s.Now = func() time.Time { return base.Add(4 * time.Minute) }
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/journeys?range=24h", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("journeys: %d %s", rr.Code, rr.Body)
	}
	var res struct {
		Journeys struct {
			Steps  int `json:"steps"`
			Visits int `json:"visits"`
			Paths  []struct {
				Pages  []string `json:"pages"`
				Visits int      `json:"visits"`
			} `json:"paths"`
			Transitions []struct {
				Step   int    `json:"step"`
				From   string `json:"from"`
				To     string `json:"to"`
				Visits int    `json:"visits"`
			} `json:"transitions"`
			StepPages [][]struct {
				Page   string `json:"page"`
				Visits int    `json:"visits"`
			} `json:"step_pages"`
		} `json:"journeys"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	j := res.Journeys
	if j.Visits != 3 {
		t.Fatalf("visits: %d", j.Visits)
	}
	if len(j.Paths) == 0 || len(j.Paths[0].Pages) != 2 || j.Paths[0].Pages[1] != "/pricing" || j.Paths[0].Visits != 2 {
		t.Fatalf("top path: %+v", j.Paths)
	}
	if len(j.Transitions) == 0 || j.Transitions[0].From != "/" || j.Transitions[0].To != "/pricing" || j.Transitions[0].Visits != 2 {
		t.Fatalf("top transition: %+v", j.Transitions)
	}
	if len(j.StepPages) == 0 || len(j.StepPages[0]) == 0 || j.StepPages[0][0].Page != "/" || j.StepPages[0][0].Visits != 3 {
		t.Fatalf("step 1: %+v", j.StepPages)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/journeys?range=24h&steps=9", nil, nil)
	if rr.Code != 400 {
		t.Fatalf("bad steps: %d", rr.Code)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/journeys?range=24h&start=/", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"visits":3`) {
		t.Fatalf("start filter: %d %s", rr.Code, rr.Body)
	}
}

func TestFunnelsVitalsEventData(t *testing.T) {
	s := newServer(t, "", "")
	base := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return base }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "F", "domain": "example.com"}, nil)
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	ua := map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "7.7.7.7"}
	body := func(n string, u string) map[string]any {
		return map[string]any{"s": site.ID, "n": n, "u": u, "tz": "Europe/London"}
	}
	// Funnel path: / -> /pricing -> signup, plus a dropout at /pricing.
	do(t, h, "POST", "/api/v1/collect", body("pageview", "https://example.com/"), ua)
	do(t, h, "POST", "/api/v1/collect", body("pageview", "https://example.com/pricing"), ua)
	do(t, h, "POST", "/api/v1/collect", body("signup", "https://example.com/pricing"), ua)
	do(t, h, "POST", "/api/v1/collect", map[string]any{"s": site.ID, "n": "pageview", "u": "https://example.com/", "tz": "Europe/London"},
		map[string]string{"User-Agent": safariPhone, "X-Forwarded-For": "8.8.8.8"})
	do(t, h, "POST", "/api/v1/collect", map[string]any{"s": site.ID, "n": "pageview", "u": "https://example.com/pricing", "tz": "Europe/London"},
		map[string]string{"User-Agent": safariPhone, "X-Forwarded-For": "8.8.8.8"})
	do(t, h, "POST", "/api/v1/collect", map[string]any{"s": site.ID, "n": "dl", "u": "https://example.com/", "tz": "Europe/London", "x": map[string]any{"fmt": "pdf"}},
		ua)
	do(t, h, "POST", "/api/v1/collect", map[string]any{"s": site.ID, "n": "vitals", "u": "https://example.com/", "tz": "Europe/London", "lcp": 900.0, "inp": 120.0}, ua)
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}

	// Funnel CRUD.
	steps := []any{map[string]any{"type": "page", "value": "/"}, map[string]any{"type": "page", "value": "/pricing"}, map[string]any{"type": "event", "value": "signup"}}
	rr = do(t, h, "POST", "/api/v1/sites/"+site.ID+"/funnels", map[string]any{"name": "Signup", "steps": steps}, nil)
	if rr.Code != 201 {
		t.Fatalf("funnel create: %d %s", rr.Code, rr.Body)
	}
	var f struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &f)
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/funnels/"+f.ID+"?range=24h", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("funnel eval: %d %s", rr.Code, rr.Body)
	}
	for _, want := range []string{`"visitors":2`, `"visitors":1`, `"conversion":0.5`} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("funnel eval missing %s: %s", want, rr.Body)
		}
	}
	if rr := do(t, h, "POST", "/api/v1/sites/"+site.ID+"/funnels", map[string]any{"name": "Bad", "steps": steps[:1]}, nil); rr.Code != 422 {
		t.Fatalf("short funnel: %d", rr.Code)
	}

	// Event data.
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/event-data?event=dl&range=24h", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"key":"fmt"`) {
		t.Fatalf("event keys: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/event-data?event=dl&key=fmt&range=24h", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"value":"pdf"`) {
		t.Fatalf("event values: %d %s", rr.Code, rr.Body)
	}

	// Vitals.
	rr = do(t, h, "GET", "/api/v1/sites/"+site.ID+"/vitals?range=24h", nil, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"count":1`) || !strings.Contains(rr.Body.String(), `"p75":900`) {
		t.Fatalf("vitals: %d %s", rr.Code, rr.Body)
	}
}
