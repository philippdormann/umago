package api

import (
	"net/http"

	"github.com/philippdormann/umago/server/internal/stats"
	"github.com/philippdormann/umago/server/internal/users"
)

// ---- users (global admins only) ----

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	list, err := s.Users.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.Users.Create(r.Context(), in.Username, in.Password, in.Role)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		Role     *string `json:"role"`
		Password *string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	uid := r.PathValue("uid")
	if in.Role != nil {
		if err := s.Users.SetRole(r.Context(), uid, *in.Role); err != nil {
			s.fail(w, err)
			return
		}
	}
	if in.Password != nil {
		if err := s.Users.SetPassword(r.Context(), uid, *in.Password); err != nil {
			s.fail(w, err)
			return
		}
	}
	u, err := s.Users.Get(r.Context(), uid)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	me, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	uid := r.PathValue("uid")
	if me.userID == uid {
		writeError(w, http.StatusUnprocessableEntity, "invalid", "cannot delete your own account")
		return
	}
	if err := s.Users.Delete(r.Context(), uid); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- 2FA (the signed-in user) ----

func (s *Server) totpStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok || id.userID == "" {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	u, err := s.Users.Get(r.Context(), id.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": u.TOTPEnabled})
}

func (s *Server) totpSetup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok || id.userID == "" {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	secret, url, qr, err := s.Users.TOTPSetup(r.Context(), id.userID, id.username)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secret": secret, "otpauth_url": url, "qr": qr})
}

func (s *Server) totpConfirm(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok || id.userID == "" {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	codes, err := s.Users.TOTPConfirm(r.Context(), id.userID, in.Code)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "backup_codes": codes})
}

func (s *Server) totpDisable(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok || id.userID == "" {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	h, err := s.Users.PassHash(r.Context(), id.userID)
	if err != nil || !users.CheckPassword(h, in.Password) {
		writeError(w, http.StatusUnauthorized, "bad_credentials", "wrong password")
		return
	}
	if err := s.Users.TOTPDisable(r.Context(), id.userID); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

// ---- teams (global admins only) ----

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	list, err := s.Users.ListTeams(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"teams": list})
}

func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	me, ok := s.requireAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	t, err := s.Users.CreateTeam(r.Context(), in.Name, me.userID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) renameTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Users.RenameTeam(r.Context(), r.PathValue("tid"), in.Name); err != nil {
		s.fail(w, err)
		return
	}
	t, err := s.Users.GetTeam(r.Context(), r.PathValue("tid"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) deleteTeam(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.Users.DeleteTeam(r.Context(), r.PathValue("tid")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listMembers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	m, err := s.Users.Members(r.Context(), r.PathValue("tid"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": m})
}

func (s *Server) addMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		UserID string `json:"user_id"`
		Role   string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.Users.AddMember(r.Context(), r.PathValue("tid"), in.UserID, in.Role); err != nil {
		s.fail(w, err)
		return
	}
	m, err := s.Users.Members(r.Context(), r.PathValue("tid"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": m})
}

func (s *Server) removeMember(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.Users.RemoveMember(r.Context(), r.PathValue("tid"), r.PathValue("uid")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- API keys (global admins only) ----

func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	list, err := s.Keys.List(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": list})
}

func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in struct {
		Name   string `json:"name"`
		Scopes string `json:"scopes"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	k, raw, err := s.Keys.Create(r.Context(), in.Name, in.Scopes)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("key.created", "id", k.ID, "name", k.Name)
	writeJSON(w, http.StatusCreated, map[string]any{"key": k, "secret": raw})
}

func (s *Server) rotateKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	k, raw, err := s.Keys.Rotate(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Log.Info("key.rotated", "id", k.ID)
	writeJSON(w, http.StatusOK, map[string]any{"key": k, "secret": raw})
}

func (s *Server) deleteKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.Keys.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- shares ----

func (s *Server) listShares(w http.ResponseWriter, r *http.Request) {
	list, err := s.Users.ListShares(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"shares": list})
}

func (s *Server) createShare(w http.ResponseWriter, r *http.Request) {
	id, ok := s.identify(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	sh, err := s.Users.CreateShare(r.Context(), r.PathValue("id"), creatorLabel(id))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sh)
}

func (s *Server) deleteShare(w http.ResponseWriter, r *http.Request) {
	if err := s.Users.DeleteShare(r.Context(), r.PathValue("id"), r.PathValue("sid")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// publicShare serves a site's default-range dashboard data for a slug.
func (s *Server) publicShare(w http.ResponseWriter, r *http.Request) {
	sh, err := s.Users.ShareBySlug(r.Context(), r.PathValue("slug"))
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such share")
		return
	}
	st, err := s.Sites.Get(r.Context(), sh.SiteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such share")
		return
	}
	s.freshen(r.Context())
	rng := siteRange(r.URL.Query().Get("range"), st)
	sum, err := s.Stats.Summary(r.Context(), st.ID, rng, s.Now(), 10)
	if err != nil {
		s.fail(w, err)
		return
	}
	live, _ := s.Stats.LiveVisitors(r.Context(), st.ID, s.Now())
	if !stats.ValidRange(rng) {
		rng = stats.DefaultRange
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"site": map[string]any{"name": st.Name, "domain": st.Domain},
		"live": live, "stats": sum,
	})
}
