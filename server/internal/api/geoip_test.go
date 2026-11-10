package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"

	"github.com/philippdormann/umago/server/internal/geoip"
)

// geoFixture builds a tiny city MMDB: 9.9.9.0/24 -> GB/London.
func geoFixture(t *testing.T) []byte {
	t.Helper()
	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType: "GeoLite2-City",
		Description:  map[string]string{"en": "test fixture"},
		Languages:    []string{"en"},
		IPVersion:    6,
		RecordSize:   24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, cidr, _ := net.ParseCIDR("9.9.9.0/24")
	if err := tree.Insert(cidr, mmdbtype.Map{
		"country": mmdbtype.Map{
			"iso_code": mmdbtype.String("GB"),
			"names":    mmdbtype.Map{"en": mmdbtype.String("United Kingdom")},
		},
		"city": mmdbtype.Map{
			"names": mmdbtype.Map{"en": mmdbtype.String("London")},
		},
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := tree.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func strptr(s string) *string { return &s }

func TestIngestGeoIP(t *testing.T) {
	s := newServer(t, "", "")
	s.Now = func() time.Time { return time.Date(2026, 9, 3, 12, 30, 0, 0, time.UTC) }
	s.TrustProxy = true
	h := s.Handler()

	rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"name": "G", "domain": "example.com"}, nil)
	if rr.Code != 201 {
		t.Fatalf("create: %d", rr.Code)
	}
	var site siteView
	if err := json.Unmarshal(rr.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GeoIP.Apply(t.Context(), geoip.Patch{Mode: strptr(geoip.ModeCity)}); err != nil {
		t.Fatal(err)
	}
	if err := s.GeoIP.StoreData(t.Context(), geoFixture(t), "GeoLite2-City", time.Now()); err != nil {
		t.Fatal(err)
	}

	hit := func(headers map[string]string) {
		body := map[string]any{"s": site.ID, "u": "https://example.com/", "tz": "America/New_York"}
		if rr := do(t, h, "POST", "/api/v1/collect", body, headers); rr.Code != 202 {
			t.Fatalf("collect: %d %s", rr.Code, rr.Body)
		}
	}
	ua := map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9"}
	hit(ua)                                                                                             // GeoIP beats the New York time zone.
	hit(map[string]string{"User-Agent": chromeMac, "X-Forwarded-For": "9.9.9.9", "CF-IPCountry": "DE"}) // ...but the proxy header beats GeoIP.
	if err := s.Writer.Flush(); err != nil {
		t.Fatal(err)
	}
	rows, err := s.DB.Query(`SELECT country, region FROM events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type cr struct{ c, r string }
	var got []cr
	for rows.Next() {
		var v cr
		if err := rows.Scan(&v.c, &v.r); err != nil {
			t.Fatal(err)
		}
		got = append(got, v)
	}
	if len(got) != 2 || got[0] != (cr{"GB", "London"}) || got[1] != (cr{"DE", "New York"}) {
		t.Fatalf("geoip ingest: %+v", got)
	}
}

func TestGeoIPAPI(t *testing.T) {
	s := newServer(t, "boss", "correct-horse")
	h := s.Handler()

	get := func() map[string]any {
		rr := doAs(t, h, "GET", "/api/v1/geoip", nil, "boss", "correct-horse")
		if rr.Code != 200 {
			t.Fatalf("get: %d %s", rr.Code, rr.Body)
		}
		var v map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	if v := get(); v["mode"] != "off" || v["has_data"] != false {
		t.Fatalf("defaults: %v", v)
	}
	// Updating needs a JSON body; bad values are 422.
	rr := doAs(t, h, "PUT", "/api/v1/geoip", map[string]any{"mode": "planet"}, "boss", "correct-horse")
	if rr.Code != 422 {
		t.Fatalf("bad mode: %d %s", rr.Code, rr.Body)
	}
	rr = doAs(t, h, "PUT", "/api/v1/geoip", map[string]any{"mode": "city", "source": "dbip"}, "boss", "correct-horse")
	if rr.Code != 200 {
		t.Fatalf("put: %d %s", rr.Code, rr.Body)
	}
	if v := get(); v["mode"] != "city" || v["source"] != "dbip" {
		t.Fatalf("updated: %v", v)
	}
	// Upload a fixture MMDB as multipart.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "test.mmdb")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(geoFixture(t)); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	upload := func(path string, body *bytes.Buffer, ctype string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, body)
		r.RemoteAddr = "203.0.113.9:1234"
		r.SetBasicAuth("boss", "correct-horse")
		r.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	rr = upload("/api/v1/geoip/upload", &buf, mw.FormDataContentType())
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"has_data":true`) {
		t.Fatalf("upload: %d %s", rr.Code, rr.Body)
	}
	// Garbage is 422.
	var bad bytes.Buffer
	mw2 := multipart.NewWriter(&bad)
	fw, _ = mw2.CreateFormFile("file", "x.mmdb")
	fw.Write([]byte("garbage"))
	mw2.Close()
	if rr := upload("/api/v1/geoip/upload", &bad, mw2.FormDataContentType()); rr.Code != 422 {
		t.Fatalf("bad upload: %d %s", rr.Code, rr.Body)
	}
	// Refresh against no test server fails but keeps the blob.
	// (Default download hosts are unreachable here on purpose: no network
	// in tests. Point at a dead local port instead for speed.)
	s.GeoIP.DownloadBaseDBIP = "http://127.0.0.1:1"
	if rr := doAs(t, h, "POST", "/api/v1/geoip/refresh", nil, "boss", "correct-horse"); rr.Code != 502 {
		t.Fatalf("refresh: %d %s", rr.Code, rr.Body)
	}
	if v := get(); v["has_data"] != true || v["sync_error"] == "" {
		t.Fatalf("after failed refresh: %v", v)
	}
	// Delete the blob.
	if rr := doAs(t, h, "DELETE", "/api/v1/geoip/data", nil, "boss", "correct-horse"); rr.Code != 200 {
		t.Fatalf("clear: %d %s", rr.Code, rr.Body)
	}
	if v := get(); v["has_data"] != false {
		t.Fatalf("cleared: %v", v)
	}
	// No credentials on an auth-enabled server is 401.
	if rr := do(t, h, "GET", "/api/v1/geoip", nil, nil); rr.Code != 401 {
		t.Fatalf("unauthenticated: %d", rr.Code)
	}
}
