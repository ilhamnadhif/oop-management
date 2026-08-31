package session

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

const CookieName = "opp_session"

type Session struct {
	UserID    string
	CSRFToken string
	ExpiresAt time.Time
	// Project is the project this session is currently working in, by name.
	// Only an account that reaches every project can change it, and changing it
	// is what the switcher does: it is a property of the session rather than of
	// the account, so switching does not rewrite anybody's record.
	//
	// Empty means it has not been settled yet, which is how a session starts.
	// The request that first needs it settles it and stores it back.
	Project string
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]Session
	ttl      time.Duration
	secure   bool
}

func NewManager(ttl time.Duration, secure bool) *Manager {
	return &Manager{
		sessions: make(map[string]Session),
		ttl:      ttl,
		secure:   secure,
	}
}

func (m *Manager) Create(w http.ResponseWriter, userID string, now time.Time) (Session, error) {
	sessionID, err := randomToken()
	if err != nil {
		return Session{}, err
	}
	csrfToken, err := randomToken()
	if err != nil {
		return Session{}, err
	}

	s := Session{UserID: userID, CSRFToken: csrfToken, ExpiresAt: now.Add(m.ttl)}
	m.mu.Lock()
	m.sessions[sessionID] = s
	m.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(m.ttl.Seconds()),
		Expires:  s.ExpiresAt,
	})
	return s, nil
}

func (m *Manager) Get(r *http.Request, now time.Time) (Session, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}

	m.mu.RLock()
	s, ok := m.sessions[cookie.Value]
	m.mu.RUnlock()
	if !ok {
		return Session{}, false
	}
	if !now.Before(s.ExpiresAt) {
		m.Delete(r, nil)
		return Session{}, false
	}
	return s, true
}

func (m *Manager) Delete(r *http.Request, w http.ResponseWriter) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		m.mu.Lock()
		delete(m.sessions, cookie.Value)
		m.mu.Unlock()
	}
	if w != nil {
		http.SetCookie(w, &http.Cookie{
			Name:     CookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   m.secure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
			Expires:  time.Unix(1, 0),
		})
	}
}

// ValidCSRFToken compares a token the caller already extracted. Multipart
// handlers use this after they have size-limited and parsed the body
// themselves, which is what ValidCSRF refuses to do on their behalf.
// SetProject records the project this session is working in. It is stored
// against the session id rather than sent to the browser: a project the cookie
// carried could be edited into one the account may not open.
func (m *Manager) SetProject(r *http.Request, project string) (Session, bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil || cookie.Value == "" {
		return Session{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.sessions[cookie.Value]
	if !ok {
		return Session{}, false
	}
	stored.Project = project
	m.sessions[cookie.Value] = stored
	return stored, true
}

func (m *Manager) ValidCSRFToken(provided string, s Session) bool {
	return provided != "" && provided == s.CSRFToken
}

func (m *Manager) ValidCSRF(r *http.Request, s Session) bool {
	provided := r.Header.Get("X-CSRF-Token")
	if provided == "" && !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		provided = r.FormValue("csrf_token")
	} else if provided == "" {
		// Multipart requests are size-limited by the attendance handler before
		// their body is parsed. Require the header form for those requests.
		provided = ""
	}
	return provided != "" && provided == s.CSRFToken
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
