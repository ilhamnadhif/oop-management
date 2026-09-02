package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
)

// The page reports the month as totals: one row per active employee, with the
// figures the attendance report already computes.
func TestHRPerformanceListsEveryActiveEmployee(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	loggedInClientAs(t, testServer, "Logistik")

	var hrUser model.User
	for _, user := range store.UserList() {
		if user.Jabatan == "HR" {
			hrUser = user
		}
	}
	location := time.FixedZone("WIB", 7*60*60)
	in := time.Date(2026, 8, 5, 8, 0, 0, 0, location)
	out := in.Add(9 * time.Hour)
	minutes := 540
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-CLOSED", UserID: hrUser.UserID, NRP: hrUser.NRP,
		NamaLengkap: hrUser.NamaLengkap, Jabatan: hrUser.Jabatan,
		TanggalAbsensi: "2026-08-05", ClockInAt: in, ClockOutAt: &out, DurasiMenit: &minutes,
		StatusAbsensi: model.AttendanceSelesai,
	}); err != nil {
		t.Fatalf("seed closed day: %v", err)
	}
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: hrUser.UserID, NRP: hrUser.NRP,
		NamaLengkap: hrUser.NamaLengkap, Jabatan: hrUser.Jabatan,
		TanggalAbsensi: "2026-08-06",
		ClockInAt:      time.Date(2026, 8, 6, 8, 0, 0, 0, location),
		StatusAbsensi:  model.AttendanceBelumClockOut,
	}); err != nil {
		t.Fatalf("seed open day: %v", err)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/performance?month=2026-08")
	for _, want := range []string{
		"PERFORMANCE KARYAWAN",
		"Hadir", "Sakit", "Izin", "Cuti", "Tidak Absen",
		"Belum Clock Out", "Kehadiran",
		hrUser.NamaLengkap,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
	// Both accounts are active, so both are listed whatever they attended.
	if !strings.Contains(page, "2 karyawan aktif") {
		t.Fatalf("the page does not count both employees:\n%s", firstLines(page))
	}
	// The open-shift column needs a word: a day counted present that still has
	// to be chased up is not obvious from the number alone.
	if !strings.Contains(page, "tapi tidak ditutup") {
		t.Fatal("the page does not explain the Belum Clock Out column")
	}
}

// The percentage is read down the column at a glance, so it carries the colour
// rather than only the number: green at 80 and above, amber from 60, red below.
//
// The fixture reads on 7 August, and the month still running owes only the days
// that have passed, so this employee owes the first seven days of the month.
func TestHRPerformanceBadgesTheAttendanceRate(t *testing.T) {
	for _, testCase := range []struct {
		name string
		days int
		chip string
		rate string
	}{
		{name: "seven of seven is green", days: 7, chip: "approved", rate: "100%"},
		{name: "six of seven is green", days: 6, chip: "approved", rate: "85.71%"},
		{name: "five of seven is amber", days: 5, chip: "pending", rate: "71.43%"},
		{name: "four of seven is red", days: 4, chip: "rejected", rate: "57.14%"},
		{name: "nothing attended is red", days: 0, chip: "rejected", rate: "0%"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			testServer, store := newTestServerWithStore(t)
			hr := loggedInClientAs(t, testServer, "HR")

			// An account of its own, joined long before the month: the signed-in
			// HR account registers today, so it owes only today and could never
			// show a rate below a hundred.
			karyawan := &model.User{
				UserID: "usr_rate", NRP: "NRP901", NamaLengkap: "Budi Hartono", Jabatan: "Surveyor",
				Email: "usr_rate@example.test", TanggalGabung: "2026-01-02",
				StatusPengguna: model.StatusAktif,
			}
			if err := store.CreateUser(t.Context(), karyawan); err != nil {
				t.Fatalf("seed employee: %v", err)
			}
			location := time.FixedZone("WIB", 7*60*60)
			for day := 1; day <= testCase.days; day++ {
				in := time.Date(2026, 8, day, 8, 0, 0, 0, location)
				out := in.Add(9 * time.Hour)
				minutes := 540
				if err := store.CreateAttendance(t.Context(), &model.Attendance{
					AbsensiID: fmt.Sprintf("ABS-%02d", day), UserID: karyawan.UserID, NRP: karyawan.NRP,
					NamaLengkap: karyawan.NamaLengkap, Jabatan: karyawan.Jabatan,
					TanggalAbsensi: fmt.Sprintf("2026-08-%02d", day),
					ClockInAt:      in, ClockOutAt: &out, DurasiMenit: &minutes,
					StatusAbsensi: model.AttendanceSelesai,
				}); err != nil {
					t.Fatalf("seed day %d: %v", day, err)
				}
			}

			page := fetchAuthedPage(t, hr, testServer.URL+"/hr/performance?month=2026-08&jabatan=Surveyor")
			want := fmt.Sprintf(`<span class="status-chip %s">%s</span>`, testCase.chip, testCase.rate)
			if !strings.Contains(page, want) {
				t.Fatalf("missing %s", want)
			}
		})
	}
}

// Overtime is the other half of the working day: the table says how much was
// worked past closing, and a day nobody closed contributes none.
func TestHRPerformanceReportsOvertime(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	karyawan := &model.User{
		UserID: "usr_ot", NRP: "NRP902", NamaLengkap: "Budi Hartono", Jabatan: "Surveyor",
		Email: "usr_ot@example.test", TanggalGabung: "2026-01-02", StatusPengguna: model.StatusAktif,
	}
	if err := store.CreateUser(t.Context(), karyawan); err != nil {
		t.Fatalf("seed employee: %v", err)
	}
	location := time.FixedZone("WIB", 7*60*60)
	// The default working day ends at 17:00, so 19:00 is two hours past it.
	in := time.Date(2026, 8, 3, 8, 0, 0, 0, location)
	out := time.Date(2026, 8, 3, 19, 0, 0, 0, location)
	minutes := 660
	if err := store.CreateAttendance(t.Context(), &model.Attendance{
		AbsensiID: "ABS-OT", UserID: karyawan.UserID, NRP: karyawan.NRP,
		NamaLengkap: karyawan.NamaLengkap, Jabatan: karyawan.Jabatan,
		TanggalAbsensi: "2026-08-03", ClockInAt: in, ClockOutAt: &out, DurasiMenit: &minutes,
		StatusAbsensi: model.AttendanceSelesai,
	}); err != nil {
		t.Fatalf("seed overtime day: %v", err)
	}
	// A second day left open earns nothing however late it started.
	if err := store.CreateAttendance(t.Context(), &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: karyawan.UserID, NRP: karyawan.NRP,
		NamaLengkap: karyawan.NamaLengkap, Jabatan: karyawan.Jabatan,
		TanggalAbsensi: "2026-08-04",
		ClockInAt:      time.Date(2026, 8, 4, 8, 0, 0, 0, location),
		StatusAbsensi:  model.AttendanceBelumClockOut,
	}); err != nil {
		t.Fatalf("seed open day: %v", err)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/performance?month=2026-08&jabatan=Surveyor")
	if !strings.Contains(page, "Lembur") {
		t.Fatal("the table has no overtime column")
	}
	if !strings.Contains(page, ">2j<") {
		t.Fatalf("the overtime total is not the two hours worked past closing:\n%s", firstLines(page))
	}
}

// The jabatan filter narrows the table the same way the export's does.
func TestHRPerformanceFiltersByJabatan(t *testing.T) {
	testServer := newTestServer(t)
	hr := loggedInClientAs(t, testServer, "HR")
	loggedInClientAs(t, testServer, "Logistik")

	all := fetchAuthedPage(t, hr, testServer.URL+"/hr/performance?month=2026-08")
	if !strings.Contains(all, "2 karyawan aktif") {
		t.Fatalf("the unfiltered page does not list both:\n%s", firstLines(all))
	}
	one := fetchAuthedPage(t, hr, testServer.URL+"/hr/performance?month=2026-08&jabatan=Logistik")
	if !strings.Contains(one, "1 karyawan aktif") {
		t.Fatalf("the jabatan filter does not narrow the table:\n%s", firstLines(one))
	}
	if !strings.Contains(one, "· Logistik") {
		t.Fatal("the heading does not name the jabatan being shown")
	}
}

// A month that is not a month is said so on the page rather than crashing it.
func TestHRPerformanceReportsAnInvalidMonth(t *testing.T) {
	testServer := newTestServer(t)
	hr := loggedInClientAs(t, testServer, "HR")

	response, err := hr.Get(testServer.URL + "/hr/performance?month=bukan-bulan")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want the page to render its own message", response.StatusCode)
	}
	if !strings.Contains(body, "bulan tidak valid") {
		t.Fatalf("the page does not report the bad month:\n%s", firstLines(body))
	}
}

// The page belongs to HR, and sits behind the same guard as the rest of the
// menu: what everyone was paid attention for is not open to the site at large.
func TestHRPerformanceIsGuardedLikeTheRestOfHR(t *testing.T) {
	testServer := newTestServer(t)

	for _, jabatan := range []string{"Surveyor", "Produksi", "Logistik"} {
		client := loggedInClientAs(t, testServer, jabatan)
		response, err := client.Get(testServer.URL + "/hr/performance")
		if err != nil {
			t.Fatalf("get as %s: %v", jabatan, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s opened the page with status %d, want 403", jabatan, response.StatusCode)
		}
	}

	anonymous := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := anonymous.Get(testServer.URL + "/hr/performance")
	if err != nil {
		t.Fatalf("get anonymously: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}

// The menu names the page, so HR can find it without being told the URL.
func TestHRPerformanceIsInTheSidebar(t *testing.T) {
	testServer := newTestServer(t)
	hr := loggedInClientAs(t, testServer, "HR")
	nav := navSection(t, fetchAuthedPage(t, hr, testServer.URL+"/dashboard"))
	if !strings.Contains(nav, "/hr/performance") {
		t.Fatal("the HR menu does not list Performance")
	}
}
