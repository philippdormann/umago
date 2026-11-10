// Package auth guards the admin UI and API with a login: either the single
// username/password from the environment, or accounts in the users table
// (which the env admin is bootstrapped into) with roles and TOTP 2FA.
package auth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/philippdormann/umago/server/internal/ids"
	"github.com/philippdormann/umago/server/internal/users"
)

// SessionCookie is the name of the admin session cookie.
const SessionCookie = "umago_session"

// SessionTTL is how long an admin login lasts.
const SessionTTL = 30 * 24 * time.Hour

// pendingTTL is how long a 2FA challenge stays valid.
const pendingTTL = 5 * time.Minute

// Session is who a request acts as.
type Session struct {
	UserID   string
	Username string
	Role     string
	// EnvAdmin is the legacy environment login (full admin, no user row).
	EnvAdmin bool
}

type cachedSession struct {
	exp      time.Time
	userID   string
	username string
	role     string
	envAdmin bool
}

type pendingLogin struct {
	userID string
	pwHash string
	exp    time.Time
}

// Admin guards /admin and the admin endpoints. When Enabled is false
// everything is open (only sensible behind your own proxy or VPN).
//
// Sessions are kept in memory and, when a SessionStore is set, in the database so
// they survive restarts. A session is only valid for the password it was
// created with, so changing the password logs everyone out.
type Admin struct {
	username string
	password string // stored hashed
	enabled  bool
	store    *SessionStore
	users    *users.Store

	mu       sync.Mutex
	sessions map[string]cachedSession
	pending  map[string]pendingLogin
	now      func() time.Time
}

// NewAdmin returns an Admin; it is enabled only when both values are non-empty.
func NewAdmin(username, password string, store *SessionStore) *Admin {
	a := &Admin{sessions: map[string]cachedSession{}, pending: map[string]pendingLogin{}, now: time.Now, store: store}
	if username != "" && password != "" {
		a.enabled = true
		a.username = username
		a.password = Hash(password)
	}
	return a
}

// SetUsers wires the accounts table. With it set, logins go through users
// (with roles and 2FA); without it, only the env login works.
func (a *Admin) SetUsers(us *users.Store) { a.users = us }

// Enabled reports whether admin authentication is configured.
func (a *Admin) Enabled() bool { return a != nil && a.enabled }

// Check verifies a username/password pair in constant time.
func (a *Admin) Check(username, password string) bool {
	if !a.Enabled() {
		return false
	}
	u := subtle.ConstantTimeCompare([]byte(Hash(username)), []byte(Hash(a.username))) == 1
	p := subtle.ConstantTimeCompare([]byte(Hash(password)), []byte(a.password)) == 1
	return u && p
}

// Login creates a session and returns its raw token. With accounts wired,
// users who enrolled in 2FA are refused here and must use StartLogin.
func (a *Admin) Login(ctx context.Context, username, password string) (string, bool) {
	if a.users != nil {
		u, err := a.users.GetByUsername(ctx, username)
		if err != nil {
			time.Sleep(400 * time.Millisecond)
			return "", false
		}
		h, err := a.users.PassHash(ctx, u.ID)
		if err != nil || !users.CheckPassword(h, password) {
			time.Sleep(400 * time.Millisecond)
			return "", false
		}
		if u.TOTPEnabled {
			return "", false
		}
		return a.newSession(ctx, u.ID, h, u.Username, u.Role, false), true
	}
	if !a.Check(username, password) {
		return "", false
	}
	return a.newSession(ctx, "", a.password, a.username, users.RoleAdmin, true), true
}

// StartLogin verifies credentials. It returns a session token directly, or
// a pending challenge when the account uses 2FA.
func (a *Admin) StartLogin(ctx context.Context, username, password string) (tok string, need2FA bool, ok bool) {
	if a.users == nil {
		tok, ok := a.Login(ctx, username, password)
		return tok, false, ok
	}
	u, err := a.users.GetByUsername(ctx, username)
	if err != nil {
		time.Sleep(400 * time.Millisecond)
		return "", false, false
	}
	h, err := a.users.PassHash(ctx, u.ID)
	if err != nil || !users.CheckPassword(h, password) {
		time.Sleep(400 * time.Millisecond)
		return "", false, false
	}
	if !u.TOTPEnabled {
		return a.newSession(ctx, u.ID, h, u.Username, u.Role, false), false, true
	}
	p := "umago_2fa_" + ids.Random(20)
	a.mu.Lock()
	a.pending[p] = pendingLogin{userID: u.ID, pwHash: h, exp: a.now().Add(pendingTTL)}
	a.mu.Unlock()
	return p, true, true
}

// FinishLogin redeems a 2FA challenge with a TOTP or backup code.
func (a *Admin) FinishLogin(ctx context.Context, pending, code string) (string, bool) {
	a.mu.Lock()
	p, found := a.pending[pending]
	if found {
		delete(a.pending, pending)
	}
	a.mu.Unlock()
	if !found || a.now().After(p.exp) {
		return "", false
	}
	u, err := a.users.Get(ctx, p.userID)
	if err != nil {
		return "", false
	}
	if !a.users.TOTPVerify(ctx, u.ID, code) {
		time.Sleep(400 * time.Millisecond)
		return "", false
	}
	return a.newSession(ctx, u.ID, p.pwHash, u.Username, u.Role, false), true
}

func (a *Admin) newSession(ctx context.Context, userID, pwHash, username, role string, envAdmin bool) string {
	tok := "umago_sess_" + ids.Random(20)
	exp := a.now().Add(SessionTTL)
	a.mu.Lock()
	a.sessions[Hash(tok)] = cachedSession{exp: exp, userID: userID, username: username, role: role, envAdmin: envAdmin}
	a.mu.Unlock()
	if a.store != nil {
		_ = a.store.Save(ctx, Hash(tok), pwHash, exp, userID)
	}
	return tok
}

// Logout revokes a session token.
func (a *Admin) Logout(ctx context.Context, token string) {
	a.mu.Lock()
	delete(a.sessions, Hash(token))
	a.mu.Unlock()
	if a.store != nil {
		_ = a.store.Delete(ctx, Hash(token))
	}
}

// Valid reports whether a raw session token is live.
func (a *Admin) Valid(ctx context.Context, token string) bool {
	_, ok := a.Identity(ctx, token)
	return ok
}

// Identity resolves a raw session token to who it acts as.
func (a *Admin) Identity(ctx context.Context, token string) (Session, bool) {
	if token == "" {
		return Session{}, false
	}
	h := Hash(token)
	a.mu.Lock()
	cached, ok := a.sessions[h]
	a.mu.Unlock()
	if !ok && a.store != nil {
		pw, userID, storedExp, found, err := a.store.Lookup(ctx, h)
		if err != nil || !found {
			return Session{}, false
		}
		cached = cachedSession{exp: storedExp, userID: userID, envAdmin: userID == ""}
		if userID == "" {
			if !Equal(pw, a.password) {
				return Session{}, false
			}
			cached.username, cached.role, cached.envAdmin = a.username, users.RoleAdmin, true
		} else {
			if a.users == nil {
				return Session{}, false
			}
			u, err := a.users.Get(ctx, userID)
			if err != nil {
				return Session{}, false
			}
			cur, err := a.users.PassHash(ctx, userID)
			if err != nil || !Equal(pw, cur) {
				return Session{}, false
			}
			cached.username, cached.role = u.Username, u.Role
		}
		a.mu.Lock()
		a.sessions[h] = cached
		a.mu.Unlock()
		ok = true
	}
	if !ok {
		return Session{}, false
	}
	if a.now().After(cached.exp) {
		a.mu.Lock()
		delete(a.sessions, h)
		a.mu.Unlock()
		return Session{}, false
	}
	return Session{UserID: cached.userID, Username: cached.username, Role: cached.role, EnvAdmin: cached.envAdmin}, true
}

// Authorized reports whether r carries a valid session cookie or HTTP Basic
// credentials. Always true when auth is not enabled.
func (a *Admin) Authorized(r *http.Request) bool {
	_, ok := a.RequestIdentity(r)
	return ok
}

// RequestIdentity resolves who r acts as: session cookie first, then HTTP
// Basic. Basic auth cannot satisfy 2FA, so TOTP users must use sessions.
func (a *Admin) RequestIdentity(r *http.Request) (Session, bool) {
	if !a.Enabled() {
		return Session{Role: users.RoleAdmin, EnvAdmin: true}, true
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		if s, ok := a.Identity(r.Context(), c.Value); ok {
			return s, true
		}
	}
	if u, p, ok := r.BasicAuth(); ok {
		if a.users != nil {
			usr, err := a.users.GetByUsername(r.Context(), u)
			if err != nil {
				return Session{}, false
			}
			h, err := a.users.PassHash(r.Context(), usr.ID)
			if err != nil || !users.CheckPassword(h, p) || usr.TOTPEnabled {
				return Session{}, false
			}
			return Session{UserID: usr.ID, Username: usr.Username, Role: usr.Role}, true
		}
		if a.Check(u, p) {
			return Session{UserID: "", Username: a.username, Role: users.RoleAdmin, EnvAdmin: true}, true
		}
	}
	return Session{}, false
}

// SetCookie writes the session cookie for token onto w.
func (a *Admin) SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: int(SessionTTL.Seconds()),
	})
}

// ClearCookie removes the session cookie.
func (a *Admin) ClearCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r), MaxAge: -1})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Hash returns the hex SHA-256 of s.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Equal compares two hashes in constant time.
func Equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
