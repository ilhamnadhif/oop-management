package widya

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fakeScanner answers the way the scanner does: a token for the right account,
// the company that token belongs to, and the dashboard for that company only.
func fakeScanner(t *testing.T, logins *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(logins, 1)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Method != http.MethodPost || body["email"] != "star@example.com" || body["password"] != "rahasia" ||
			r.Header.Get("Company-Id") != "co-9" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"accessToken": "tok-1"})
	})
	mux.HandleFunc("/api/v1/config/ui", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"features":{"authStrategy":"local","loginCompanyId":"co-9"}}`))
	})
	authorized := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	mux.HandleFunc("/api/v1/auth/user", func(w http.ResponseWriter, r *http.Request) {
		if authorized(w, r) {
			_ = json.NewEncoder(w).Encode(map[string]string{"companyId": "co-9"})
		}
	})
	mux.HandleFunc("/api/v1/dashboard/statistics", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r) {
			return
		}
		if r.URL.Query().Get("companyId") != "co-9" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"ticketsCount":6803,"vehiclesCount":324,"materialsCount":5,"suppliersCount":8,"operatorsCount":10,"unitsCount":3,"usersCount":4,"webhooksCount":0}`))
	})
	mux.HandleFunc("/api/v1/dashboard/volume-per-materials", func(w http.ResponseWriter, r *http.Request) {
		if authorized(w, r) {
			_, _ = w.Write([]byte(`[{"name":"pasir","volume":"342008.779348019999995"},{"name":"batu","volume":178.5}]`))
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestClientSignsInAndReadsTheDashboard(t *testing.T) {
	var logins int32
	server := fakeScanner(t, &logins)
	client := NewClient(server.URL+"/", "star@example.com", "rahasia", "", server.Client())

	snapshot, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if snapshot.Stats.Tickets != 6803 || snapshot.Stats.Vehicles != 324 || snapshot.Stats.Users != 4 {
		t.Fatalf("stats = %+v", snapshot.Stats)
	}
	if len(snapshot.Materials) != 2 || snapshot.Materials[0].Name != "pasir" ||
		snapshot.Materials[0].Volume < 342008.77 || snapshot.Materials[1].Volume != 178.5 {
		t.Fatalf("materials = %+v", snapshot.Materials)
	}

	// The token is kept: a second read does not sign in again.
	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if logins != 1 {
		t.Fatalf("signed in %d times, want once", logins)
	}
}

// An expired token is answered by signing in again, without the caller seeing
// the refusal.
func TestClientSignsInAgainWhenTheTokenIsRefused(t *testing.T) {
	var logins int32
	server := fakeScanner(t, &logins)
	client := NewClient(server.URL, "star@example.com", "rahasia", "co-9", server.Client())
	client.token, client.companyID = "expired", "co-9"

	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if logins != 1 {
		t.Fatalf("signed in %d times, want once", logins)
	}
}

func TestClientReportsAWrongPassword(t *testing.T) {
	var logins int32
	server := fakeScanner(t, &logins)
	client := NewClient(server.URL, "star@example.com", "salah", "", server.Client())
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("a refused sign-in read as a success")
	}
}

type stubSource struct {
	calls    int
	snapshot Snapshot
	err      error
}

func (s *stubSource) Fetch(context.Context) (Snapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

// The feed reads the scanner once per interval however often it is asked, and
// keeps the last figures when the scanner stops answering.
func TestFeedCachesAndKeepsTheLastReading(t *testing.T) {
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)
	source := &stubSource{snapshot: Snapshot{Stats: Stats{Tickets: 10}}}
	feed := NewFeed(source, 30*time.Second, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		if _, _, err := feed.Latest(context.Background()); err != nil {
			t.Fatalf("latest: %v", err)
		}
	}
	if source.calls != 1 {
		t.Fatalf("read the scanner %d times inside one interval", source.calls)
	}

	now = now.Add(time.Minute)
	source.err = errors.New("down")
	snapshot, has, err := feed.Latest(context.Background())
	if err == nil || !has || snapshot.Stats.Tickets != 10 {
		t.Fatalf("a failed read lost the last figures: %+v has=%v err=%v", snapshot, has, err)
	}
	if !snapshot.FetchedAt.Equal(now.Add(-time.Minute)) {
		t.Fatalf("stale figures carry %v, want the time they were read", snapshot.FetchedAt)
	}
}

// The address is whatever page somebody copied it from; the API is read from
// the site root regardless.
func TestClientReadsFromTheSiteRoot(t *testing.T) {
	var logins int32
	server := fakeScanner(t, &logins)
	client := NewClient(server.URL+"/login", "star@example.com", "rahasia", "", server.Client())
	if _, err := client.Fetch(context.Background()); err != nil {
		t.Fatalf("fetch through a copied page address: %v", err)
	}
}
