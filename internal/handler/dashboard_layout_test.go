package handler

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
)

// loggedInClient signs in as Management, the position that may open every menu,
// so a feature test exercises the feature rather than the access rules. The
// rules have their own tests.
func loggedInClient(t *testing.T, testServer *httptest.Server) *http.Client {
	t.Helper()
	return loggedInClientAs(t, testServer, "Management")
}

// loggedInClientAs registers and signs in one person holding the given
// position. Management keeps the original identity so the tests that read the
// name and NRP off the page still find them; anyone else gets their own, since
// two people cannot share an NRP and the second registration would be refused,
// silently signing the test in as the first.
func loggedInClientAs(t *testing.T, testServer *httptest.Server, jabatan string) *http.Client {
	t.Helper()
	nrp, email := "123456", "budi@example.com"
	if jabatan != "Management" {
		nrp = fmt.Sprintf("%06d", 200000+len(jabatan)*1000+int(jabatan[0]))
		email = strings.ToLower(jabatan) + "@example.com"
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	registerResponse, err := client.PostForm(testServer.URL+"/register", urlValues(map[string]string{
		"tanggal_gabung":  "2026-08-07",
		"nama_lengkap":    "Budi Santoso",
		"nrp":             nrp,
		"jabatan":         jabatan,
		"email":           email,
		"password":        "rahasia123",
		"status_pengguna": model.StatusAktif,
	}))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	registerResponse.Body.Close()

	loginResponse, err := client.PostForm(testServer.URL+"/login", urlValues(map[string]string{
		"identifier": nrp,
		"password":   "rahasia123",
	}))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	loginResponse.Body.Close()
	return client
}

func fetchDashboard(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	response, err := client.Get(testServer.URL + "/absensi")
	if err != nil {
		t.Fatalf("dashboard request: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status: %d", response.StatusCode)
	}
	return body
}

func buttonIsDisabled(t *testing.T, dashboard, action string) bool {
	t.Helper()
	marker := `data-attendance-action="` + action + `"`
	index := strings.Index(dashboard, marker)
	if index < 0 {
		t.Fatalf("button %q not found in dashboard", action)
	}
	rest := dashboard[index:]
	end := strings.Index(rest, ">")
	if end < 0 {
		t.Fatalf("malformed button tag for %q", action)
	}
	return strings.Contains(rest[:end], "disabled")
}

// Only the legal action of the moment may be pressable. attendance.js can
// disable a button while waiting for GPS, but it never enables one the server
// rendered as disabled.
func TestClockButtonStatesFollowAttendanceProgress(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	photoBytes := testJPEG(t)

	dashboard := fetchDashboard(t, client, testServer)
	if buttonIsDisabled(t, dashboard, "/absensi/clock-in") {
		t.Fatal("clock-in must be enabled before any attendance")
	}
	if !buttonIsDisabled(t, dashboard, "/absensi/clock-out") {
		t.Fatal("clock-out must be disabled before clock-in")
	}

	csrf := csrfFromBody(t, dashboard)
	clockIn := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-in", csrf, photoBytes, "-6.2", "106.8")
	if clockIn.StatusCode != http.StatusOK {
		t.Fatalf("clock in status: %d body=%s", clockIn.StatusCode, readBody(t, clockIn))
	}

	dashboard = fetchDashboard(t, client, testServer)
	if !buttonIsDisabled(t, dashboard, "/absensi/clock-in") {
		t.Fatal("clock-in must be disabled after clocking in")
	}
	if buttonIsDisabled(t, dashboard, "/absensi/clock-out") {
		t.Fatal("clock-out must be enabled after clocking in")
	}

	clockOut := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-out", csrf, photoBytes, "-6.2", "106.8")
	if clockOut.StatusCode != http.StatusOK {
		t.Fatalf("clock out status: %d body=%s", clockOut.StatusCode, readBody(t, clockOut))
	}

	dashboard = fetchDashboard(t, client, testServer)
	if !buttonIsDisabled(t, dashboard, "/absensi/clock-in") || !buttonIsDisabled(t, dashboard, "/absensi/clock-out") {
		t.Fatal("both buttons must be disabled once the day is complete")
	}
}

// Each clock button carries its own timestamp, so an unrecorded action must
// show a placeholder rather than a stale or zero time.
func TestClockCardsShowRecordedTimes(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	dashboard := fetchDashboard(t, client, testServer)
	if strings.Count(dashboard, "--:--") != 2 {
		t.Fatalf("expected both clock cards empty, got %d placeholders", strings.Count(dashboard, "--:--"))
	}
	if !strings.Contains(dashboard, "<small>WIB</small>") {
		t.Fatal("clock cards are missing the timezone label")
	}

	csrf := csrfFromBody(t, dashboard)
	clockIn := doAttendanceRequest(t, client, testServer.URL+"/absensi/clock-in", csrf, testJPEG(t), "-6.2", "106.8")
	if clockIn.StatusCode != http.StatusOK {
		t.Fatalf("clock in status: %d body=%s", clockIn.StatusCode, readBody(t, clockIn))
	}

	// newTestServer pins the clock to 08:00 WIB.
	dashboard = fetchDashboard(t, client, testServer)
	if !strings.Contains(dashboard, "08:00 <small>WIB</small>") {
		t.Fatal("clock-in card does not show the recorded time")
	}
	if strings.Count(dashboard, "--:--") != 1 {
		t.Fatalf("clock-out card should still be empty, got %d placeholders", strings.Count(dashboard, "--:--"))
	}
}

func TestDashboardShowsIndonesianDateAndClock(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	// newTestServer pins the clock to 2026-08-07 08:00 WIB, a Friday.
	dashboard := fetchDashboard(t, client, testServer)
	for _, fragment := range []string{"Jumat, 7 Agustus 2026", `data-clock-start="08:00"`} {
		if !strings.Contains(dashboard, fragment) {
			t.Fatalf("dashboard missing %q", fragment)
		}
	}
}
