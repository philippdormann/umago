package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/philippdormann/umago/server/internal/users"
	"github.com/pquerna/otp/totp"
)

func newPlatformServer(t *testing.T) *Server {
	t.Helper()
	s := newServer(t, "root", "root-password")
	s.Users = users.New(s.DB)
	s.Keys = users.NewKeyStore(s.DB)
	s.Admin.SetUsers(s.Users)
	if err := s.Users.Bootstrap(context.Background(), "root", "root-password"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestUsersRolesTeams(t *testing.T) {
	s := newPlatformServer(t)
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "root", "root-password")
	}
	// Create member and viewer.
	rr := admin("POST", "/api/v1/users", map[string]any{"username": "mem", "password": "member-password", "role": "member"})
	if rr.Code != 201 {
		t.Fatalf("create member: %d %s", rr.Code, rr.Body)
	}
	rr = admin("POST", "/api/v1/users", map[string]any{"username": "vue", "password": "viewer-password", "role": "viewer"})
	if rr.Code != 201 {
		t.Fatalf("create viewer: %d %s", rr.Code, rr.Body)
	}
	memberAuth := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "mem", "member-password")
	}
	viewerAuth := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "vue", "viewer-password")
	}
	// Viewer reads but cannot write.
	if rr := viewerAuth("GET", "/api/v1/sites", nil); rr.Code != 200 {
		t.Fatalf("viewer read: %d", rr.Code)
	}
	if rr := viewerAuth("POST", "/api/v1/sites", map[string]any{"domain": "v.example"}); rr.Code != 403 {
		t.Fatalf("viewer write: %d", rr.Code)
	}
	// Member creates a site.
	rr = memberAuth("POST", "/api/v1/sites", map[string]any{"domain": "m.example"})
	if rr.Code != 201 {
		t.Fatalf("member create: %d %s", rr.Code, rr.Body)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	// Viewer-only management is admin-gated.
	if rr := viewerAuth("GET", "/api/v1/users", nil); rr.Code != 403 {
		t.Fatalf("viewer users: %d", rr.Code)
	}
	if rr := memberAuth("GET", "/api/v1/teams", nil); rr.Code != 403 {
		t.Fatalf("member teams: %d", rr.Code)
	}
	// Team: create, add member, pin site.
	rr = admin("POST", "/api/v1/teams", map[string]any{"name": "Acme"})
	if rr.Code != 201 {
		t.Fatalf("team: %d %s", rr.Code, rr.Body)
	}
	var team struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &team)
	var mem struct {
		ID string `json:"id"`
	}
	rr = admin("GET", "/api/v1/users", nil)
	for _, u := range parseUsers(rr) {
		if u["username"] == "mem" {
			mem.ID = u["id"]
		}
	}
	if mem.ID == "" {
		t.Fatal("member id")
	}
	if rr := admin("POST", "/api/v1/teams/"+team.ID+"/members", map[string]any{"user_id": mem.ID, "role": "member"}); rr.Code != 200 {
		t.Fatalf("add member: %d %s", rr.Code, rr.Body)
	}
	rr = admin("PATCH", "/api/v1/sites/"+site.ID, map[string]any{"owner_team_id": team.ID})
	if rr.Code != 200 {
		t.Fatalf("pin site: %d %s", rr.Code, rr.Body)
	}
	// Member sees the pinned site; viewer does not.
	if rr := memberAuth("GET", "/api/v1/sites", nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), site.ID) {
		t.Fatalf("member list: %d %s", rr.Code, rr.Body)
	}
	if rr := viewerAuth("GET", "/api/v1/sites", nil); rr.Code != 200 || strings.Contains(rr.Body.String(), site.ID) {
		t.Fatalf("viewer list leaks team site: %d %s", rr.Code, rr.Body)
	}
	if rr := viewerAuth("GET", "/api/v1/sites/"+site.ID+"/stats?range=24h", nil); rr.Code != 403 {
		t.Fatalf("viewer team site: %d", rr.Code)
	}
	// Cannot delete the last admin or self.
	var rootID string
	for _, u := range parseUsers(admin("GET", "/api/v1/users", nil)) {
		if u["username"] == "root" {
			rootID = u["id"]
		}
	}
	if rr := admin("DELETE", "/api/v1/users/"+rootID, nil); rr.Code != 422 {
		t.Fatalf("delete self: %d", rr.Code)
	}
}

func parseUsers(rr *httptestRecorder) []map[string]string {
	var out struct {
		Users []map[string]string `json:"users"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return out.Users
}

func TestKeysAndShares(t *testing.T) {
	s := newPlatformServer(t)
	h := s.Handler()
	admin := func(method, path string, body any) *httptestRecorder {
		return doAs(t, h, method, path, body, "root", "root-password")
	}
	rr := admin("POST", "/api/v1/keys", map[string]any{"name": "CI", "scopes": "read,write"})
	if rr.Code != 201 {
		t.Fatalf("key create: %d %s", rr.Code, rr.Body)
	}
	var created struct {
		Key struct {
			ID string `json:"id"`
		} `json:"key"`
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if !strings.HasPrefix(created.Secret, "umago_key_") {
		t.Fatalf("prefix: %q", created.Secret)
	}
	keyAuth := map[string]string{"Authorization": "Bearer " + created.Secret}
	// Full key writes.
	rr = do(t, h, "POST", "/api/v1/sites", map[string]any{"domain": "k.example"}, keyAuth)
	if rr.Code != 201 {
		t.Fatalf("key write: %d %s", rr.Code, rr.Body)
	}
	var site siteView
	_ = json.Unmarshal(rr.Body.Bytes(), &site)
	// Rotate kills the old secret.
	rr = admin("POST", "/api/v1/keys/"+created.Key.ID+"/rotate", nil)
	if rr.Code != 200 {
		t.Fatalf("rotate: %d %s", rr.Code, rr.Body)
	}
	var rotated struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &rotated)
	if rr := do(t, h, "GET", "/api/v1/sites", nil, keyAuth); rr.Code != 401 {
		t.Fatalf("old secret: %d", rr.Code)
	}
	newAuth := map[string]string{"Authorization": "Bearer " + rotated.Secret}
	if rr := do(t, h, "GET", "/api/v1/sites", nil, newAuth); rr.Code != 200 {
		t.Fatalf("new secret: %d", rr.Code)
	}
	// Read-only key.
	rr = admin("POST", "/api/v1/keys", map[string]any{"name": "RO", "scopes": "read"})
	var ro struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &ro)
	roAuth := map[string]string{"Authorization": "Bearer " + ro.Secret}
	if rr := do(t, h, "POST", "/api/v1/sites", map[string]any{"domain": "ro.example"}, roAuth); rr.Code != 403 {
		t.Fatalf("read-only write: %d", rr.Code)
	}
	// Shares: public stats JSON without auth.
	rr = admin("POST", "/api/v1/sites/"+site.ID+"/shares", nil)
	if rr.Code != 201 {
		t.Fatalf("share: %d %s", rr.Code, rr.Body)
	}
	var sh struct {
		Slug string `json:"slug"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &sh)
	if rr := do(t, h, "GET", "/api/v1/share/"+sh.Slug, nil, nil); rr.Code != 200 || !strings.Contains(rr.Body.String(), `"stats"`) {
		t.Fatalf("public share: %d %s", rr.Code, rr.Body)
	}
	if rr := do(t, h, "GET", "/api/v1/share/nope-nope", nil, nil); rr.Code != 404 {
		t.Fatalf("bad slug: %d", rr.Code)
	}
	q := "/api/v1/sites/" + site.ID + "/stats?range=24h&share=" + sh.Slug
	if rr := do(t, h, "GET", q, nil, nil); rr.Code != 200 {
		t.Fatalf("share-scoped stats: %d %s", rr.Code, rr.Body)
	}
}

func TestTwoFactor(t *testing.T) {
	s := newPlatformServer(t)
	h := s.Handler()
	// Enroll root: setup -> confirm with a fresh code.
	rr := doAs(t, h, "POST", "/api/v1/auth/2fa/setup", nil, "root", "root-password")
	if rr.Code != 200 {
		t.Fatalf("setup: %d %s", rr.Code, rr.Body)
	}
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
		QR         string `json:"qr"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &setup)
	if setup.Secret == "" || !strings.HasPrefix(setup.OtpauthURL, "otpauth://") {
		t.Fatalf("setup payload: %+v", setup)
	}
	if !strings.HasPrefix(setup.QR, "data:image/png;base64,") {
		t.Fatalf("setup QR missing: %+v", setup)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rr = doAs(t, h, "POST", "/api/v1/auth/2fa/confirm", map[string]any{"code": code}, "root", "root-password")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "backup_codes") {
		t.Fatalf("confirm: %d %s", rr.Code, rr.Body)
	}
	var confirmed struct {
		BackupCodes []string `json:"backup_codes"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &confirmed)
	if len(confirmed.BackupCodes) != 10 {
		t.Fatalf("codes: %d", len(confirmed.BackupCodes))
	}
	// Password login now challenges.
	rr = do(t, h, "POST", "/api/v1/auth/login", map[string]any{"username": "root", "password": "root-password"}, nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"requires_2fa":true`) {
		t.Fatalf("challenge: %d %s", rr.Code, rr.Body)
	}
	var ch struct {
		Challenge string `json:"challenge"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &ch)
	// Wrong code fails.
	if rr := do(t, h, "POST", "/api/v1/auth/2fa/verify", map[string]any{"token": ch.Challenge, "code": "000000"}, nil); rr.Code != 401 {
		t.Fatalf("bad code: %d", rr.Code)
	}
	// A backup code works once (fresh challenge per attempt: they are single-use).
	rr = do(t, h, "POST", "/api/v1/auth/login", map[string]any{"username": "root", "password": "root-password"}, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &ch)
	// A backup code works once.
	rr = do(t, h, "POST", "/api/v1/auth/2fa/verify", map[string]any{"token": ch.Challenge, "code": confirmed.BackupCodes[0]}, nil)
	if rr.Code != 200 {
		t.Fatalf("backup code: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, h, "POST", "/api/v1/auth/login", map[string]any{"username": "root", "password": "root-password"}, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &ch)
	if rr := do(t, h, "POST", "/api/v1/auth/2fa/verify", map[string]any{"token": ch.Challenge, "code": confirmed.BackupCodes[0]}, nil); rr.Code != 401 {
		t.Fatalf("backup code reuse: %d", rr.Code)
	}
	// Fresh TOTP code works.
	rr = do(t, h, "POST", "/api/v1/auth/login", map[string]any{"username": "root", "password": "root-password"}, nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &ch)
	code, _ = totp.GenerateCode(setup.Secret, time.Now())
	rr = do(t, h, "POST", "/api/v1/auth/2fa/verify", map[string]any{"token": ch.Challenge, "code": code}, nil)
	if rr.Code != 200 {
		t.Fatalf("totp verify: %d %s", rr.Code, rr.Body)
	}
	// Disable with password over the verified session (Basic auth cannot
	// satisfy 2FA by design).
	cookie := map[string]string{"Cookie": strings.Split(rr.Header().Get("Set-Cookie"), ";")[0]}
	rr = do(t, h, "POST", "/api/v1/auth/2fa/disable", map[string]any{"password": "root-password"}, cookie)
	if rr.Code != 200 {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body)
	}
	rr = do(t, h, "POST", "/api/v1/auth/login", map[string]any{"username": "root", "password": "root-password"}, nil)
	if rr.Code != 200 || strings.Contains(rr.Body.String(), "requires_2fa") {
		t.Fatalf("plain login after disable: %d %s", rr.Code, rr.Body)
	}
}
