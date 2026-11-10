package api

import (
	"net/http"
	"strings"

	"github.com/philippdormann/umago/server/internal/auth"
	"github.com/philippdormann/umago/server/internal/users"
)

// ---- identity, roles, team scoping ----

// identity is who a request acts as.
type identity struct {
	userID   string
	username string
	role     string
	envAdmin bool
	// global bypasses team scoping (env admin, global admins, API keys).
	global bool
	// write allows non-GET methods.
	write bool
	// share pins a public share link to its site, skipping team checks.
	share bool
}

// identify resolves session, Basic, API key and share-slab credentials.
func (s *Server) identify(r *http.Request) (identity, bool) {
	// Public share links grant read-only access to one site's GET endpoints.
	if slug := strings.TrimSpace(r.URL.Query().Get("share")); slug != "" && r.Method == http.MethodGet && r.PathValue("id") != "" {
		if sh, err := s.Users.ShareBySlug(r.Context(), slug); err == nil && sh.SiteID == r.PathValue("id") {
			return identity{role: users.RoleViewer, share: true}, true
		}
		return identity{}, false
	}
	if tok := bearer(r); tok != "" {
		if s.Keys != nil {
			if k, ok := s.Keys.Authenticate(r.Context(), tok); ok {
				return identity{username: k.Name, role: users.RoleAdmin, global: true, write: k.Scopes == users.ScopeReadWrite || r.Method == http.MethodGet}, true
			}
		}
		if s.MCPToken != "" && auth.Equal(auth.Hash(tok), auth.Hash(s.MCPToken)) {
			return identity{global: true, write: r.Method == http.MethodGet}, true
		}
		if s.Tokens != nil {
			if _, ok := s.Tokens.Authenticate(r.Context(), tok); ok {
				return identity{global: true, write: r.Method == http.MethodGet}, true
			}
		}
		return identity{}, false
	}
	sess, ok := s.Admin.RequestIdentity(r)
	if !ok {
		return identity{}, false
	}
	id := identity{userID: sess.UserID, username: sess.Username, role: sess.Role, envAdmin: sess.EnvAdmin}
	if sess.EnvAdmin || sess.Role == users.RoleAdmin {
		id.global = true
		id.write = true
		return id, true
	}
	// Members read and write; viewers read. Team scoping happens per site.
	// The method gate lives in adminAuth so forbidden writes read 403.
	id.write = sess.Role == users.RoleMember
	return id, true
}

func (s *Server) adminAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := s.identify(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
			return
		}
		if r.Method != http.MethodGet && !id.write {
			writeError(w, http.StatusForbidden, "read_only", "this credential is read-only")
			return
		}
		if siteID := r.PathValue("id"); siteID != "" && !id.global && !id.share {
			read, write, err := s.Users.SiteAccess(r.Context(), id.userID, id.role, siteID)
			if err != nil || !read || (r.Method != http.MethodGet && !write) {
				writeError(w, http.StatusForbidden, "forbidden", "no access to this site")
				return
			}
		}
		next(w, r)
	})
}

// requireAdmin gates global-admin endpoints (users, teams, keys, settings).
// creatorLabel is the audit display name stored on created links and shares:
// the signed-in username, the API key name, or "admin" when auth is off.
func creatorLabel(id identity) string {
	if id.username != "" {
		return id.username
	}
	return "admin"
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) (identity, bool) {
	id, ok := s.identify(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return identity{}, false
	}
	if !id.global || (!id.envAdmin && id.role != users.RoleAdmin) {
		writeError(w, http.StatusForbidden, "forbidden", "admins only")
		return identity{}, false
	}
	if r.Method != http.MethodGet && !id.write {
		writeError(w, http.StatusForbidden, "read_only", "this credential is read-only")
		return identity{}, false
	}
	return id, true
}
