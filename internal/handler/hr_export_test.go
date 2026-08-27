package handler

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedAbsensiReport fills the store with one surveyor who attended on the 3rd
// and one produksi employee on approved cuti sakit from the 10th to the 11th.
func seedAbsensiReport(t *testing.T, store *repository.TestRepository) {
	t.Helper()
	location := time.FixedZone("WIB", 7*60*60)
	for _, user := range []*model.User{
		{
			UserID: "usr_abs_1", NRP: "NRP701", NamaLengkap: "Budi Hartono", Jabatan: "Surveyor",
			Email: "usr_abs_1@example.test", TanggalGabung: "2026-01-02", StatusPengguna: model.StatusAktif,
		},
		{
			UserID: "usr_abs_2", NRP: "NRP702", NamaLengkap: "Citra Ayu", Jabatan: "Produksi",
			Email: "usr_abs_2@example.test", TanggalGabung: "2026-01-02", StatusPengguna: model.StatusAktif,
		},
	} {
		if err := store.CreateUser(t.Context(), user); err != nil {
			t.Fatalf("seed user %s: %v", user.UserID, err)
		}
	}

	day, _ := time.Parse("2006-01-02", "2026-08-03")
	in := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, location)
	if err := store.CreateAttendance(t.Context(), &model.Attendance{
		AbsensiID: "ABS-ABSENSI", UserID: "usr_abs_1", NRP: "NRP701", NamaLengkap: "Budi Hartono",
		Jabatan: "Surveyor", TanggalAbsensi: "2026-08-03", ClockInAt: in,
		StatusAbsensi: model.AttendanceSelesai,
	}); err != nil {
		t.Fatalf("seed attendance: %v", err)
	}

	if err := store.CreateLeave(t.Context(), &model.Leave{
		LeaveID: "LVE-ABSENSI", UserID: "usr_abs_2", NRP: "NRP702", NamaLengkap: "Citra Ayu",
		Jabatan: "Produksi", JenisLeave: model.LeaveJenisCutiSakit,
		TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-11", JumlahHari: 2,
		Status: model.LeaveStatusDisetujui, CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, location),
	}); err != nil {
		t.Fatalf("seed leave: %v", err)
	}
}

func downloadAbsensi(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return response
}

func TestAbsensiExportDownloadsTheMatrix(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedAbsensiReport(t, store)
	client := loggedInClient(t, testServer)

	response := downloadAbsensi(t, client, testServer.URL+"/hr/export/download")
	body := readBodyBytes(t, response)

	if response.StatusCode != http.StatusOK {
		t.Fatalf("download: status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "spreadsheetml") {
		t.Fatalf("content type = %q", got)
	}
	if !bytes.HasPrefix(body, []byte("PK")) {
		t.Fatal("the download is not an xlsx file")
	}
	disposition := response.Header.Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, ".xlsx") {
		t.Fatalf("content disposition %q", disposition)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control %q, want no-store", got)
	}
}

func TestAbsensiExportFilenameCarriesMonthAndJabatan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedAbsensiReport(t, store)
	client := loggedInClient(t, testServer)

	response := downloadAbsensi(t, client,
		testServer.URL+"/hr/export/download?month=2026-08&jabatan=Surveyor")
	response.Body.Close()
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "rekap-absensi-2026-08-surveyor.xlsx") {
		t.Fatalf("content disposition %q does not name month and jabatan", got)
	}

	all := downloadAbsensi(t, client, testServer.URL+"/hr/export/download?month=2026-08")
	all.Body.Close()
	if got := all.Header.Get("Content-Disposition"); !strings.Contains(got, "rekap-absensi-2026-08.xlsx") {
		t.Fatalf("unfiltered disposition %q should name only the month", got)
	}
}

func TestAbsensiExportPageReportsTheFilter(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedAbsensiReport(t, store)
	client := loggedInClient(t, testServer)

	// The signed-in Management account is itself an active employee, so the
	// matrix carries three rows: Management, the surveyor and the produksi one.
	page := fetchAuthedPage(t, client, testServer.URL+"/hr/export")
	for _, fragment := range []string{
		"3 karyawan siap diunduh",
		`name="month"`,
		`value="2026-08"`,
		"Semua jabatan",
		`value="Surveyor"`,
		"Unduh XLSX",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("absensi export page missing %q", fragment)
		}
	}
}

func TestAbsensiExportRejectsInvalidMonth(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedAbsensiReport(t, store)
	client := loggedInClient(t, testServer)

	response := downloadAbsensi(t, client, testServer.URL+"/hr/export/download?month=2026-13")
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid month: status %d, want 422", response.StatusCode)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/hr/export?month=agustus")
	if !strings.Contains(page, "bulan tidak valid") {
		t.Fatal("the page does not surface the invalid month error")
	}
}

func TestAbsensiExportDownloadRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/hr/export", "/hr/export/download"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("%s: anonymous request went to %q, want /login", path, location)
		}
	}
}

func TestAbsensiExportIsRestrictedToHR(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClientAs(t, testServer, "Produksi")

	for _, path := range []string{"/hr/export", "/hr/export/download"} {
		if status := statusOf(t, client, testServer.URL+path); status != http.StatusForbidden {
			t.Fatalf("a Produksi user reached %s: status %d, want 403", path, status)
		}
	}
}
