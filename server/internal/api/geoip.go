package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/philippdormann/umago/server/internal/geoip"
)

// ---- optional GeoIP location (global admins) ----

func (s *Server) geoipGet(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	c, err := s.GeoIP.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// geoipUpdate saves mode, source and the MaxMind key. A PUT with a body,
// so read-only tokens are already refused by adminAuth.
func (s *Server) geoipUpdate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	var in geoip.Patch
	if !readJSON(w, r, &in) {
		return
	}
	c, err := s.GeoIP.Apply(r.Context(), in)
	if err != nil {
		s.geoipFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// geoipRefresh downloads the database for the configured source right now,
// keeping the previous blob when the download fails.
func (s *Server) geoipRefresh(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.GeoIP.Refresh(r.Context()); err != nil {
		s.geoipFail(w, err)
		return
	}
	c, err := s.GeoIP.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// geoipUpload stores a user-supplied .mmdb file (DB-IP or MaxMind, city or
// country). Multipart form with a "file" field, up to 300 MB.
func (s *Server) geoipUpload(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 320<<20)
	if err := r.ParseMultipartForm(320 << 20); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "file must be 300 MB or smaller")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid", `multipart form must carry the .mmdb as "file"`)
		return
	}
	defer f.Close()
	if hdr.Size > 300<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "file must be 300 MB or smaller")
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 300<<20+1))
	if err != nil || len(data) == 0 || len(data) > 300<<20 {
		writeError(w, http.StatusBadRequest, "invalid", "could not read the uploaded file")
		return
	}
	typ, err := geoip.Validate(data)
	if err != nil {
		s.geoipFail(w, err)
		return
	}
	if err := s.GeoIP.StoreData(r.Context(), data, typ, s.Now()); err != nil {
		s.fail(w, err)
		return
	}
	c, err := s.GeoIP.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// geoipClearData drops the stored blob (settings stay) and unloads it.
func (s *Server) geoipClearData(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	if err := s.GeoIP.ClearData(r.Context()); err != nil {
		s.fail(w, err)
		return
	}
	c, err := s.GeoIP.Config(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) geoipFail(w http.ResponseWriter, err error) {
	if errors.Is(err, geoip.ErrInvalid) {
		writeError(w, http.StatusUnprocessableEntity, "invalid", err.Error())
		return
	}
	writeError(w, http.StatusBadGateway, "geoip", err.Error())
}
