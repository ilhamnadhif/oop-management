package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/session"
)

// handleProjectSwitch moves the session to another project. It refuses anything
// the account cannot reach, so the dropdown is a convenience rather than the
// check: a posted project name is a request, not a decision.
func (s *Server) handleProjectSwitch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusUnprocessableEntity)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	wanted := strings.TrimSpace(r.FormValue("project"))
	reachable, err := s.projects.Reachable(r.Context(), user)
	if err != nil {
		log.Printf("read projects: %v", err)
		http.Error(w, "Daftar project gagal dimuat", http.StatusBadGateway)
		return
	}
	for _, project := range reachable {
		if strings.EqualFold(strings.TrimSpace(project.Nama), wanted) {
			s.sessions.SetProject(r, project.Nama)
			// Back where they were, so switching mid-task does not also lose
			// the page. A referer from anywhere else is ignored.
			redirect(w, r, safeReturnPath(r.Referer()))
			return
		}
	}
	// A project they cannot reach leaves the session where it was rather than
	// erroring: the dropdown is stale, and the next page will redraw it.
	redirect(w, r, safeReturnPath(r.Referer()))
}

// safeReturnPath keeps a redirect inside this app. A referer is supplied by the
// browser and could name anywhere, so only its path is used and only when it
// looks like one of ours.
func safeReturnPath(referer string) string {
	referer = strings.TrimSpace(referer)
	if referer == "" {
		return "/dashboard"
	}
	if index := strings.Index(referer, "://"); index >= 0 {
		rest := referer[index+3:]
		slash := strings.Index(rest, "/")
		if slash < 0 {
			return "/dashboard"
		}
		referer = rest[slash:]
	}
	// A path starting "//" is a protocol-relative URL to another host.
	if !strings.HasPrefix(referer, "/") || strings.HasPrefix(referer, "//") {
		return "/dashboard"
	}
	return referer
}

// forProjectJSON settles the project for an endpoint that answers in JSON. It
// writes the error response itself and returns it, so the caller only has to
// check for one.
func (s *Server) forProjectJSON(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session) (*Server, error) {
	reachable, err := s.projects.Reachable(r.Context(), user)
	if err != nil {
		log.Printf("read projects: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"ok": false, "error": "Daftar project gagal dimuat."})
		return nil, err
	}
	if len(reachable) == 0 {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "Akun ini belum ditugaskan ke project mana pun."})
		return nil, errNoProject
	}
	project := reachable[0]
	for _, candidate := range reachable {
		if strings.EqualFold(strings.TrimSpace(candidate.Nama), strings.TrimSpace(sessionValue.Project)) {
			project = candidate
			break
		}
	}
	bound, err := s.forProject(r.Context(), project, reachable)
	if err != nil {
		log.Printf("bind project %s: %v", project.Nama, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": "Project " + project.Nama + " tidak bisa dibuka saat ini."})
		return nil, err
	}
	return bound, nil
}

// errNoProject is returned when an account belongs nowhere. The caller has
// already been answered by the time it sees this.
var errNoProject = errors.New("akun tanpa project")
