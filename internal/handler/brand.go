package handler

import (
	"log"
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/photo"
)

// Brand marks are served rather than inlined. A data URL of thirty-odd
// kilobytes in the page shell would travel again on every click; a URL is
// fetched once and then cached, and the version in the query is what makes a
// replaced mark appear immediately rather than after the cache expires.
const (
	brandLogoPath       = "/brand/logo"
	brandExportLogoPath = "/brand/logo-export"
	brandFaviconPath    = "/brand/favicon"
)

// brandLogoURL is where the sidebar and topbar fetch this project's mark, or
// empty when it has none and the app's own artwork should be used instead.
func brandMarkURL(path, mark string, version int64) string {
	if strings.TrimSpace(mark) == "" {
		return ""
	}
	return path + "?v=" + strconv.FormatInt(version, 10)
}

// handleBrandLogo serves the project's own system logo. The favicon is served
// by the same code because they differ only in which column they come from.
func (s *Server) handleBrandLogo(w http.ResponseWriter, r *http.Request) {
	s.serveBrandMark(w, r, func(settings model.ProjectSettings) string { return settings.LogoSistem })
}

// handleBrandExportLogo serves the letterhead mark. Nothing in the app draws
// it - it goes into the file - but the settings screen has to show what is
// stored, and a column that showed nothing read as an upload that had failed.
func (s *Server) handleBrandExportLogo(w http.ResponseWriter, r *http.Request) {
	s.serveBrandMark(w, r, func(settings model.ProjectSettings) string { return settings.LogoExport })
}

func (s *Server) handleBrandFavicon(w http.ResponseWriter, r *http.Request) {
	s.serveBrandMark(w, r, func(settings model.ProjectSettings) string { return settings.Favicon })
}

// serveBrandMark writes one stored mark. A project without one is a 404 rather
// than a redirect to the app's artwork: the page already knows to ask for the
// static file instead, and a redirect would only be a second round trip.
//
// It binds the project first, because which mark to serve is the question the
// project answers. Beranda is the menu it binds against: every position reaches
// it, and every position sees the sidebar these marks are in.
//
// The value is validated on the way out. The sheet can be edited by hand, and
// what is served to a browser is not trusted to be what this app wrote.
func (s *Server) serveBrandMark(w http.ResponseWriter, r *http.Request, pick func(model.ProjectSettings) string) {
	s, _, _, ok := s.requireAccess(w, r, "beranda")
	if !ok {
		return
	}
	mark := strings.TrimSpace(pick(s.project.Settings))
	if mark == "" {
		http.NotFound(w, r)
		return
	}
	decoded, err := photo.DecodeLogoDataURL(mark)
	if err != nil {
		log.Printf("read brand mark for %s: %v", s.project.Nama, err)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", photo.LogoContentType(mark))
	w.Header().Set("Content-Length", strconv.Itoa(len(decoded)))
	// Immutable for a day: the URL carries the version, so a replaced mark is a
	// different URL rather than the same one gone stale.
	w.Header().Set("Cache-Control", "private, max-age=86400")
	_, _ = w.Write(decoded)
}
