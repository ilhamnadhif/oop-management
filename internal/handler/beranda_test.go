package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedAttendance writes one day of attendance for the signed-in test user.
func seedAttendance(t *testing.T, store *repository.TestRepository, date string, clockIn time.Time, minutes int) {
	t.Helper()
	users := store.UserList()
	if len(users) == 0 {
		t.Fatal("no user is registered yet")
	}
	attendance := &model.Attendance{
		AbsensiID: "ABS-" + date, UserID: users[0].UserID, NRP: users[0].NRP,
		NamaLengkap: users[0].NamaLengkap, Jabatan: users[0].Jabatan,
		TanggalAbsensi: date, ClockInAt: clockIn,
		StatusAbsensi: model.AttendanceBelumClockOut,
	}
	if minutes > 0 {
		out := clockIn.Add(time.Duration(minutes) * time.Minute)
		attendance.ClockOutAt = &out
		attendance.DurasiMenit = &minutes
		attendance.StatusAbsensi = model.AttendanceSelesai
	}
	if err := store.CreateAttendance(context.Background(), attendance); err != nil {
		t.Fatalf("seed attendance: %v", err)
	}
}

// The dashboard is the first thing in the menu and reports the signed-in
// person's own month.
func TestDashboardSummarisesOwnAttendance(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	location := time.FixedZone("WIB", 7*60*60)
	seedAttendance(t, store, "2026-08-05", time.Date(2026, 8, 5, 8, 0, 0, 0, location), 540)
	seedAttendance(t, store, "2026-08-06", time.Date(2026, 8, 6, 8, 5, 0, 0, location), 480)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	for _, fragment := range []string{
		"HARI HADIR", "TOTAL JAM KERJA", "RATA-RATA PER HARI", "BELUM CLOCK OUT",
		"RIWAYAT KEHADIRAN", "JAM KERJA 14 HARI TERAKHIR",
		// Two days, seventeen hours between them, so an average of 8.5.
		`<p class="kpi-value">2</p>`, "17.0 <small>jam</small>", "8.5 <small>jam</small>",
		"2026-08-06", "08:05", "8j",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the dashboard is missing %q", fragment)
		}
	}
}

// On the day itself, whether you have clocked in matters more than any figure
// about the month, so it is stated first and links to the page that fixes it.
func TestDashboardTellsYouWhatIsMissingToday(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	location := time.FixedZone("WIB", 7*60*60)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(page, "Anda belum clock in hari ini") || !strings.Contains(page, `href="/absensi"`) {
		t.Fatalf("a day with no attendance is not reported: %s", page)
	}

	// The test clock stands at 2026-08-07 08:00.
	seedAttendance(t, store, "2026-08-07", time.Date(2026, 8, 7, 7, 55, 0, 0, location), 0)
	open := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(open, "belum clock out") {
		t.Fatalf("an open day is not reported: %s", open)
	}
	if !strings.Contains(open, "07:55") {
		t.Fatal("the dashboard does not show when the day started")
	}
}

// The working day is 09:00-17:00 with fifteen minutes of grace, and the
// dashboard both counts against it and says what it is.
func TestDashboardCountsAgainstTheWorkingDay(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	location := time.FixedZone("WIB", 7*60*60)

	// On time (inside the grace period), late and long, then early leave.
	seedAttendance(t, store, "2026-08-03", time.Date(2026, 8, 3, 9, 10, 0, 0, location), 470)
	seedAttendance(t, store, "2026-08-04", time.Date(2026, 8, 4, 9, 40, 0, 0, location), 560)
	seedAttendance(t, store, "2026-08-05", time.Date(2026, 8, 5, 8, 30, 0, 0, location), 450)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	for _, fragment := range []string{
		"TEPAT WAKTU", "TERLAMBAT", "PULANG CEPAT", "LEMBUR",
		// The rule is stated, not left to be inferred from a label.
		"Jam kerja 09:00–17:00, toleransi keterlambatan",
		// 09:40 is forty minutes past nine, and the day ran to 19:00.
		"Terlambat 40m", "Lembur 2j",
		// 08:30 to 16:00 is early on both ends.
		"Masuk lebih awal", "Pulang cepat 1j",
		"Tepat waktu",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the dashboard is missing %q", fragment)
		}
	}
}

// The page that records the time states the rule too, not only the page that
// judges it afterwards.
func TestAbsensiStatesTheWorkingDay(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchAuthedPage(t, loggedInClient(t, testServer), testServer.URL+"/absensi")

	if !strings.Contains(page, "Jam kerja 09:00–17:00, toleransi keterlambatan") {
		t.Fatalf("the attendance page does not state the working day: %s", page)
	}
}

// Leave is not recorded anywhere, and a zero would read as "never took leave".
func TestDashboardSaysLeaveIsNotTrackedYet(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchAuthedPage(t, loggedInClient(t, testServer), testServer.URL+"/dashboard")

	if !strings.Contains(page, "CUTI &amp; IZIN") || !strings.Contains(page, "belum dicatat di aplikasi ini") {
		t.Fatal("the dashboard does not say that leave is untracked")
	}
}

// The dashboard leads the menu and the attendance page keeps its own entry.
func TestDashboardLeadsTheMenu(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"))

	dashboardAt := strings.Index(nav, `href="/dashboard"`)
	absensiAt := strings.Index(nav, `href="/absensi"`)
	if dashboardAt < 0 || absensiAt < 0 {
		t.Fatalf("the menu is missing one of the two entries: %s", nav)
	}
	if dashboardAt > absensiAt {
		t.Fatal("the dashboard is not the first entry")
	}
	if !strings.Contains(nav, ">Dashboard</span>") {
		t.Fatal("the dashboard entry is unnamed")
	}
}

func TestDashboardRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(testServer.URL + "/dashboard")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}
