package database

import "testing"

func TestRebind(t *testing.T) {
	q := `SELECT a, substr(ts, 1, ?) FROM events WHERE site_id = ? AND ts >= ? AND kind = 'what?' -- trailing? comment
		/* block? comment */ AND name != "weird?" AND esc = 'it''s ? fine' GROUP BY 1 LIMIT ?`
	got := Rebind(q, Postgres)
	want := `SELECT a, substr(ts, 1, $1) FROM events WHERE site_id = $2 AND ts >= $3 AND kind = 'what?' -- trailing? comment
		/* block? comment */ AND name != "weird?" AND esc = 'it''s ? fine' GROUP BY 1 LIMIT $4`
	if got != want {
		t.Fatalf("rebind mismatch:\n got: %s\nwant: %s", got, want)
	}
	if Rebind(q, SQLite) != q {
		t.Fatal("sqlite queries must pass through untouched")
	}
	if Rebind(`SELECT 1`, Postgres) != `SELECT 1` {
		t.Fatal("placeholder-free queries must pass through untouched")
	}
}
