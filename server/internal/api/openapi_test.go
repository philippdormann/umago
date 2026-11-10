package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPISpec(t *testing.T) {
	s := newServer(t, "", "")
	h := s.Handler()
	rr := do(t, h, "GET", "/api/v1/openapi.json", nil, nil)
	if rr.Code != 200 {
		t.Fatalf("openapi: %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("content-type: %q", ct)
	}
	var v map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if v["openapi"] == nil || v["paths"] == nil {
		t.Fatal("spec needs openapi and paths")
	}
	paths := v["paths"].(map[string]any)
	for _, p := range []string{"/health", "/api/v1/collect", "/api/v1/sites", "/api/v1/sites/{id}/stats", "/api/v1/sites/{id}/breakdown", "/api/v1/sites/{id}/journeys", "/api/v1/geoip", "/api/v1/openapi.json", "/mcp"} {
		if paths[p] == nil {
			t.Fatalf("spec missing path %s", p)
		}
	}
}
