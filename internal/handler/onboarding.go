package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// onboardingPath is where an account still holding the password it was handed
// is sent, and the only page it may reach until it has a password of its own.
const onboardingPath = "/onboarding"

// OnboardingPageData is the password-setting screen. It carries no shell: the
// person cannot reach any of the pages a sidebar would offer, so drawing one
// would be an invitation to nowhere.
type OnboardingPageData struct {
	Title       string
	NamaLengkap string
	CSRFToken   string
	Error       string
}

func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleOnboardingPage(w, r)
	case http.MethodPost:
		s.handleOnboardingSave(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleOnboardingPage(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.onboardingUser(w, r)
	if !ok {
		return
	}
	// Somebody who already has a password of their own has nothing to do here.
	if !sessionValue.MustChangePassword {
		redirect(w, r, "/dashboard")
		return
	}
	s.renderOnboarding(w, user, sessionValue.CSRFToken, "", http.StatusOK)
}

func (s *Server) handleOnboardingSave(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.onboardingUser(w, r)
	if !ok {
		return
	}
	if !sessionValue.MustChangePassword {
		redirect(w, r, "/dashboard")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderOnboarding(w, user, sessionValue.CSRFToken, "Form tidak valid", http.StatusUnprocessableEntity)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	err := s.auth.ChangePassword(r.Context(), user.UserID,
		r.FormValue("password"), r.FormValue("konfirmasi_password"))
	if err != nil {
		message := "Password tidak bisa disimpan"
		status := http.StatusUnprocessableEntity
		if errors.Is(err, service.ErrValidation) {
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("change password: %v", err)
			status = http.StatusInternalServerError
		}
		s.renderOnboarding(w, user, sessionValue.CSRFToken, message, status)
		return
	}

	// The session carries on rather than being thrown away: making somebody
	// sign in again with a password they set one second ago teaches them
	// nothing and loses them the page they were headed for.
	s.sessions.PasswordChanged(r)
	redirect(w, r, "/dashboard")
}

// onboardingUser loads the account behind the session without the checks that
// would send it here in the first place.
func (s *Server) onboardingUser(w http.ResponseWriter, r *http.Request) (*model.User, session.Session, bool) {
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return nil, sessionValue, false
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return nil, sessionValue, false
	}
	return user, sessionValue, true
}

func (s *Server) renderOnboarding(w http.ResponseWriter, user *model.User, csrfToken, errMessage string, status int) {
	s.render(w, "onboarding", OnboardingPageData{
		Title:       "Buat password baru",
		NamaLengkap: user.NamaLengkap,
		CSRFToken:   csrfToken,
		Error:       errMessage,
	}, status)
}
