package api

import (
	"encoding/base64"
	"net/http"

	"github.com/philippdormann/umago/server/internal/enrich"
	"github.com/philippdormann/umago/server/internal/events"
	"github.com/philippdormann/umago/server/internal/links"
)

// ---- short links & pixels ----

func (s *Server) listLinks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Links.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": list})
}

func (s *Server) createLink(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Slug string `json:"slug"`
		Name string `json:"name"`
		URL  string `json:"url"`
		Kind string `json:"kind"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	id, ok := s.identify(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "login_required", "sign in to Umago")
		return
	}
	l, err := s.Links.Create(r.Context(), r.PathValue("id"), in.Slug, in.Name, in.URL, in.Kind, creatorLabel(id))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, l)
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	if err := s.Links.Delete(r.Context(), r.PathValue("id"), r.PathValue("linkID")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// click records a link/pixel hit with the visit machinery shared with
// collect, then redirects or serves a 1px gif.
func (s *Server) click(w http.ResponseWriter, r *http.Request, kind string) {
	l, err := s.Links.BySlug(r.Context(), r.PathValue("slug"))
	if err != nil || l.Kind != kind {
		writeError(w, http.StatusNotFound, "not_found", "no such link")
		return
	}
	site, ok := s.Sites.Lookup(r.Context(), l.SiteID)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "no such link")
		return
	}
	now := s.Now()
	salt, serr := s.Settings.Salt(r.Context(), now)
	if serr != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such link")
		return
	}
	ua := enrich.ParseUA(r.UserAgent(), 0)
	if ua.Bot {
		// Bots follow links too; count the click but keep it simple.
	} else {
		visitor := events.VisitorHash(salt, site.ID, s.clientIP(r), r.UserAgent())
		visit, verr := events.Visit(r.Context(), s.DB, site.ID, visitor, now)
		if verr == nil {
			refHost := enrich.Referrer(r.Referer(), site.Domain)
			country := enrich.Country(r.Header, "")
			region := ""
			if s.GeoIP != nil && enrich.HeaderCountry(r.Header) == "" {
				if c, city, ok := s.GeoIP.Lookup(r.Context(), s.clientIP(r)); ok {
					country = c
					region = city
				}
			}
			evKind := events.KindLink
			if kind == links.KindPixel {
				evKind = events.KindPixel
			}
			s.Writer.Enqueue(events.Event{
				SiteID: site.ID, At: now, Kind: evKind, Name: l.Slug, Path: r.URL.Path,
				RefHost: refHost, Country: country, Region: region, Device: ua.Device,
				Browser: ua.Browser, OS: ua.OS, Channel: enrich.Channel(refHost, "", ""),
				Visitor: visitor, VisitID: visit,
			})
		}
	}
	if kind == links.KindPixel {
		w.Header().Set("Content-Type", "image/gif")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(pixelGIF)
		return
	}
	http.Redirect(w, r, l.URL, http.StatusFound)
}

var pixelGIF, _ = base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAP///////yH5BAEKAAEALAAAAAABAAEAAAICTAEAOw==")

func (s *Server) linkClick(w http.ResponseWriter, r *http.Request) { s.click(w, r, links.KindLink) }
func (s *Server) pixelHit(w http.ResponseWriter, r *http.Request)  { s.click(w, r, links.KindPixel) }
