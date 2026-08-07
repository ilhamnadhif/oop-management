package session

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateGetCSRFAndDelete(t *testing.T) {
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	manager := NewManager(time.Hour, false)
	response := httptest.NewRecorder()
	created, err := manager.Create(response, "usr_1", now)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	cookie := response.Result().Cookies()[0]
	request := httptest.NewRequest("POST", "/dashboard", nil)
	request.AddCookie(cookie)
	request.Header.Set("X-CSRF-Token", created.CSRFToken)
	loaded, ok := manager.Get(request, now.Add(10*time.Minute))
	if !ok || loaded.UserID != "usr_1" || !manager.ValidCSRF(request, loaded) {
		t.Fatalf("session was not loaded correctly: %+v, ok=%v", loaded, ok)
	}
	manager.Delete(request, response)
	if _, ok := manager.Get(request, now.Add(10*time.Minute)); ok {
		t.Fatal("deleted session remained valid")
	}
}

func TestExpiredSessionIsInvalid(t *testing.T) {
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	manager := NewManager(time.Hour, false)
	response := httptest.NewRecorder()
	manager.Create(response, "usr_1", now)
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(response.Result().Cookies()[0])
	if _, ok := manager.Get(request, now.Add(2*time.Hour)); ok {
		t.Fatal("expired session remained valid")
	}
}
