package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedHourMeterReading files one reading straight into a project's store, so
// the export has something to count.
func seedHourMeterReading(t *testing.T, store *repository.TestRepository, tanggal string, hmAwal, hmAkhir float64) {
	t.Helper()
	reading := &model.HourMeter{
		HMID: "HM-" + tanggal, Tanggal: tanggal, Shift: "Shift 1",
		IDUnit: "exc01", NamaUnit: "Excavator PC200",
		HMAwal: hmAwal, HMAkhir: hmAkhir, TotalHM: hmAkhir - hmAwal,
		PA: 80, UA: 80,
	}
	if err := store.CreateHourMeter(context.Background(), reading); err != nil {
		t.Fatalf("seed hour meter: %v", err)
	}
}

// The A2B export page carries both the register card and the hour meter card.
func TestA2BExportPageShowsTheRegisterAndTheReadings(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedHourMeterReading(t, store, "2026-08-07", 1200, 1208)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	for _, want := range []string{
		"Unit A2B",
		"/a2b/export/download?format=xlsx",
		"/a2b/export/download?format=pdf",
		"INPUT HM",
		"/a2b/export/hm/download?format=xlsx",
		"/a2b/export/hm/download?format=pdf",
		`name="month"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
}

// The month filter counts only the readings in that month, and an empty month
// counts everything.
func TestA2BExportPageCountsTheSelectedMonth(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedHourMeterReading(t, store, "2026-08-07", 1200, 1208)
	seedHourMeterReading(t, store, "2026-09-02", 1208, 1216)
	client := loggedInClient(t, testServer)

	all := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	if !strings.Contains(all, "2 pembacaan") {
		t.Fatalf("the unfiltered page does not count both readings:\n%s", firstLines(all))
	}

	august := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?month=2026-08")
	if !strings.Contains(august, "1 pembacaan") {
		t.Fatalf("the august page does not count one reading:\n%s", firstLines(august))
	}
	if !strings.Contains(august, "month=2026-08") {
		t.Fatalf("the download links lost the month filter:\n%s", firstLines(august))
	}
}

// The readings download in both formats, and the file names the month.
func TestA2BHMExportDownloadsBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedHourMeterReading(t, store, "2026-08-07", 1200, 1208)
	client := loggedInClient(t, testServer)

	for format, want := range map[string]struct {
		contentType string
		magic       []byte
	}{
		"xlsx": {"spreadsheetml", []byte("PK")},
		"pdf":  {"application/pdf", []byte("%PDF-")},
	} {
		response := downloadProduksi(t, client, testServer.URL+"/a2b/export/hm/download?format="+format+"&month=2026-08")
		body := readBodyBytes(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", format, response.StatusCode)
		}
		if got := response.Header.Get("Content-Type"); !strings.Contains(got, want.contentType) {
			t.Fatalf("%s: content type %q", format, got)
		}
		if !bytes.HasPrefix(body, want.magic) {
			t.Fatalf("%s: body does not start with %q", format, want.magic)
		}
		disposition := response.Header.Get("Content-Disposition")
		if !strings.Contains(disposition, "input-hm-2026-08."+format) {
			t.Fatalf("%s: content disposition %q", format, disposition)
		}
	}
}

// The readings export refuses a month that is not a month.
func TestA2BHMExportRejectsAnInvalidMonth(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/a2b/export/hm/download?format=xlsx&month=bukan-bulan")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
}

// Both the register download and the readings download sit behind the same
// session guard.
func TestA2BExportsRequireASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{
		"/a2b/export",
		"/a2b/export/hm/download?format=xlsx",
	} {
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
