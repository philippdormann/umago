package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIngestVisitsAndProps(t *testing.T) {
	s := newServer(t, "", "")
	fixed := time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC)
	s.Now = func() time.Time { return fixed }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "V", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body)
	}
	var site siteView
	if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}

	hit := func(body map[string]any, headers map[string]string) {
		if rr := do(t, h, "POST", "/api/v1/collect", body, headers); rr.Code != 202 {
			t.Fatalf("collect: %d %s", rr.Code, rr.Body)
		}
	}
	ua := map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9"}
	base := map[string]any{"s": site.ID, "u": "https://example.com/?utm_source=news&utm_medium=email&utm_campaign=launch&utm_content=top&utm_term=x", "r": "https://www.google.com/", "w": 1440, "tz": "Europe/London", "lang": "en-GB", "title": "Home", "tag": "blog"}
	hit(merge(base, map[string]any{"n": "pageview"}), ua)
	hit(merge(base, map[string]any{"n": "signup", "x": map[string]any{"plan": "pro", "seats": 3, "meta": map[string]any{"src": "ad"}}}), ua)
	hit(merge(base, map[string]any{"n": "identify", "id": "user-123"}), ua)
	hit(merge(base, map[string]any{"n": "vitals", "lcp": 1200.0, "cls": 0.05}), ua)
	// DNT kills the hit.
	hit(merge(base, map[string]any{"n": "pageview"}), map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9", "DNT": "1"})
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}

	var n int
	_ = s.DB.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 4 {
		t.Fatalf("events: want 4, got %d", n)
	}
	var visit, src, med, camp, cont, term, ch, lang, title, did, tag string
	_ = s.DB.QueryRow(`SELECT visit_id, utm_source, utm_medium, utm_campaign, utm_content, utm_term, channel, language, title, distinct_id, tag FROM events WHERE kind = 'pageview'`).
		Scan(&visit, &src, &med, &camp, &cont, &term, &ch, &lang, &title, &did, &tag)
	if tag != "blog" {
		t.Fatalf("tag: %q", tag)
	}
	if !strings.HasPrefix(visit, "vst_") {
		t.Fatalf("visit not assigned: %q", visit)
	}
	if src != "news" || med != "email" || camp != "launch" || cont != "top" || term != "x" {
		t.Fatalf("utm: %q %q %q %q %q", src, med, camp, cont, term)
	}
	if ch != "Email" {
		t.Fatalf("channel: %q", ch)
	}
	if lang != "en-GB" || title != "Home" {
		t.Fatalf("lang/title: %q %q", lang, title)
	}
	var kinds string
	_ = s.DB.QueryRow(`SELECT GROUP_CONCAT(kind, ',') FROM (SELECT kind FROM events ORDER BY id)`).Scan(&kinds)
	if kinds != "pageview,event,identify,vitals" {
		t.Fatalf("kinds: %q", kinds)
	}
	_ = s.DB.QueryRow(`SELECT distinct_id FROM events WHERE kind = 'identify'`).Scan(&did)
	if did != "user-123" {
		t.Fatalf("identify distinct_id: %q", did)
	}
	var lcp, cls float64
	_ = s.DB.QueryRow(`SELECT lcp, cls FROM events WHERE kind = 'vitals'`).Scan(&lcp, &cls)
	if lcp != 1200 || cls != 0.05 {
		t.Fatalf("vitals: %v %v", lcp, cls)
	}
	// Props flattened, nested dotted.
	rows, err := s.DB.Query(`SELECT key, sval, nval FROM event_data ORDER BY key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var k, v string
		var nv *float64
		_ = rows.Scan(&k, &v, &nv)
		got[k] = v
	}
	if got["plan"] != "pro" || got["seats"] != "3" || got["meta.src"] != "ad" {
		t.Fatalf("props: %v", got)
	}
	// Same visitor within 30 minutes keeps the visit; later rotates.
	s.Now = func() time.Time { return fixed.Add(10 * time.Minute) }
	hit(merge(base, map[string]any{"n": "pageview"}), ua)
	s.Now = func() time.Time { return fixed.Add(60 * time.Minute) }
	hit(merge(base, map[string]any{"n": "pageview"}), ua)
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	var visits string
	_ = s.DB.QueryRow(`SELECT GROUP_CONCAT(visit_id, ',') FROM (SELECT DISTINCT visit_id FROM events ORDER BY visit_id)`).Scan(&visits)
	if len(strings.Split(visits, ",")) != 2 {
		t.Fatalf("visit rotation: %q", visits)
	}
}

func TestIngestAdClickIDs(t *testing.T) {
	s := newServer(t, "", "")
	s.Now = func() time.Time { return time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC) }
	s.TrustProxy = true
	h := s.Handler()
	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "Ads", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}
	ua := map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9"}
	body := map[string]any{"s": site.ID, "u": "https://example.com/?gclid=abc123&fbclid=fb1&utm_source=google", "r": "https://www.google.com/", "tz": "Europe/London"}
	if rr := do(t, h, "POST", "/api/v1/collect", body, ua); rr.Code != 202 {
		t.Fatalf("collect: %d", rr.Code)
	}
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	var gclid, fbclid, msclkid, channel string
	_ = s.DB.QueryRow("SELECT gclid, fbclid, msclkid, channel FROM events").Scan(&gclid, &fbclid, &msclkid, &channel)
	if gclid != "abc123" || fbclid != "fb1" || msclkid != "" {
		t.Fatalf("ad ids: %q %q %q", gclid, fbclid, msclkid)
	}
	if channel != "Paid Ads" {
		t.Fatalf("channel: %q", channel)
	}
}

func TestBatchAndLinks(t *testing.T) {
	s := newServer(t, "", "")
	s.Now = func() time.Time { return time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC) }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "B", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}
	ua := map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9"}
	batch := map[string]any{"events": []any{
		map[string]any{"s": site.ID, "u": "https://example.com/a", "tz": "Europe/London"},
		map[string]any{"s": site.ID, "n": "dl", "u": "https://example.com/a", "tz": "Europe/London", "x": map[string]any{"f": "pdf"}},
		map[string]any{"s": "nope", "u": "https://example.com/a"},
	}}
	if rr := do(t, h, "POST", "/api/v1/batch", batch, ua); rr.Code != 202 {
		t.Fatalf("batch: %d %s", rr.Code, rr.Body)
	}
	// Link CRUD + click + pixel.
	rr = doAs(t, h, "POST", "/api/v1/sites/"+site.ID+"/links", map[string]any{"slug": "go", "url": "https://example.com/landing", "name": "Go"}, "", "")
	if rr.Code != 201 {
		t.Fatalf("link create: %d %s", rr.Code, rr.Body)
	}
	rr = doAs(t, h, "POST", "/api/v1/sites/"+site.ID+"/links", map[string]any{"slug": "px", "url": "https://example.com/x", "kind": "pixel"}, "", "")
	if rr.Code != 201 {
		t.Fatalf("pixel create: %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "GET", "/l/go", nil, ua); rr.Code != 302 || rr.Header().Get("Location") != "https://example.com/landing" {
		t.Fatalf("link redirect: %d %q", rr.Code, rr.Header().Get("Location"))
	}
	if rr := do(t, h, "GET", "/p/px", nil, ua); rr.Code != 200 || !strings.HasPrefix(rr.Header().Get("Content-Type"), "image/gif") {
		t.Fatalf("pixel: %d %s", rr.Code, rr.Header().Get("Content-Type"))
	}
	if rr := do(t, h, "GET", "/l/missing", nil, ua); rr.Code != 404 {
		t.Fatalf("missing slug: %d", rr.Code)
	}
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	var kinds string
	_ = s.DB.QueryRow(`SELECT GROUP_CONCAT(kind, ',') FROM (SELECT kind FROM events ORDER BY id)`).Scan(&kinds)
	if kinds != "pageview,event,link,pixel" {
		t.Fatalf("kinds: %q", kinds)
	}
	rr = doAs(t, h, "GET", "/api/v1/sites/"+site.ID+"/links", nil, "", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"clicks":1`) {
		t.Fatalf("link clicks: %d %s", rr.Code, rr.Body)
	}
}

func merge(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
