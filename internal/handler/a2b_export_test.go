package handler

import (
	"bytes"
	"context"
	"io"
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

// seedHourMeterReadingFor files a reading for one named machine.
func seedHourMeterReadingFor(t *testing.T, store *repository.TestRepository, idUnit, tanggal string, hmAwal, hmAkhir float64) {
	t.Helper()
	reading := &model.HourMeter{
		HMID: "HM-" + idUnit + "-" + tanggal, Tanggal: tanggal, Shift: "Shift 1",
		IDUnit: idUnit, NamaUnit: "Excavator " + idUnit,
		HMAwal: hmAwal, HMAkhir: hmAkhir, TotalHM: hmAkhir - hmAwal,
		PA: 80, UA: 80,
	}
	if err := store.CreateHourMeter(context.Background(), reading); err != nil {
		t.Fatalf("seed hour meter: %v", err)
	}
}

// The A2B export page carries the performance card and the hour meter card.
func TestA2BExportPageShowsPerformanceAndTheReadings(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedHourMeterReading(t, store, "2026-08-07", 1200, 1208)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	for _, want := range []string{
		"PERFORMANCE UNIT",
		"/a2b/export/download?format=xlsx",
		"/a2b/export/download?format=pdf",
		`name="from"`, `name="to"`, `name="unit"`,
		"Semua unit",
		"INPUT HM",
		"/a2b/export/hm/download?format=xlsx",
		"/a2b/export/hm/download?format=pdf",
		`name="hm_from"`, `name="hm_to"`, `name="hm_unit"`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
	// The register download is gone: the machines are still listed on their own
	// page, but this card is the performance report now.
	if strings.Contains(page, "unit terdaftar") {
		t.Fatal("the page still offers the machine register download")
	}
}

// The dropdown offers every machine in the register, with the fleet first.
func TestA2BExportPageListsEveryMachineInTheFilter(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedMachine(t, store, 2, "BLD-02", "Caterpillar", "PIT B", 500, 26.0)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	for _, want := range []string{"Semua unit", "EXC-01", "BLD-02"} {
		if !strings.Contains(page, want) {
			t.Fatalf("the unit filter is missing %q", want)
		}
	}
}

// The page counts what the download will hold, so the two never disagree about
// what the filters mean.
func TestA2BExportPageCountsTheFilteredUnits(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedMachine(t, store, 2, "BLD-02", "Caterpillar", "PIT B", 500, 26.0)
	seedHourMeterReadingFor(t, store, "exc01", "2026-08-07", 1200, 1208)
	seedHourMeterReadingFor(t, store, "bld-02", "2026-08-08", 300, 307)
	client := loggedInClient(t, testServer)

	all := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	if !strings.Contains(all, "2 unit siap diunduh") {
		t.Fatal("without a filter the page does not count the whole fleet")
	}

	one := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?unit=BLD-02")
	if !strings.Contains(one, "1 unit siap diunduh") {
		t.Fatal("the unit filter does not narrow the count")
	}

	// A range holding no reading has nothing to offer.
	none := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?from=2026-01-01&to=2026-01-31")
	if strings.Contains(none, "unit siap diunduh") {
		t.Fatal("a range with no readings still offers a performance download")
	}
}

// The download carries the filters it was given, and answers as a file.
func TestA2BPerformanceDownloadHonoursTheFilters(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedHourMeterReadingFor(t, store, "exc01", "2026-08-07", 1200, 1208)
	client := loggedInClient(t, testServer)

	for _, format := range []string{"xlsx", "pdf"} {
		response, err := client.Get(testServer.URL + "/a2b/export/download?format=" + format + "&unit=EXC-01")
		if err != nil {
			t.Fatalf("download %s: %v", format, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status = %d for %s", response.StatusCode, format)
		}
		if format == "pdf" && !bytes.HasPrefix(body, []byte("%PDF-")) {
			t.Fatal("the pdf download is not a pdf")
		}
		if format == "xlsx" && !bytes.HasPrefix(body, []byte("PK")) {
			t.Fatal("the xlsx download is not a zip")
		}
	}

	// A range that reads backwards is the person's mistake to make, not a
	// crash: the service swaps it.
	response, err := client.Get(testServer.URL + "/a2b/export/download?format=xlsx&from=2026-08-31&to=2026-08-01")
	if err != nil {
		t.Fatalf("download reversed range: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d for a reversed range, want it swapped and served", response.StatusCode)
	}
}

// The performance report answers to the same project setting the register did,
// so switching it off still holds at the URL.
func TestSwitchedOffPerformanceRefusesItsDownload(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	client := loggedInClient(t, testServer)

	response := saveExportConfig(t, client, testServer, store.ProjectList()[0].ProjectID, testProjectName,
		map[string]string{"export_key": string(model.ExportUnitA2B), "ttd_count": "1"})
	response.Body.Close()

	download, err := client.Get(testServer.URL + "/a2b/export/download?format=xlsx")
	if err != nil {
		t.Fatalf("download performance: %v", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a report the project switched off", download.StatusCode)
	}
}

// The readings card counts what its own filters leave, and the download links
// carry them, so the page and the file cannot disagree.
func TestA2BExportPageCountsTheFilteredReadings(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedHourMeterReading(t, store, "2026-08-07", 1200, 1208)
	seedHourMeterReading(t, store, "2026-09-02", 1208, 1216)
	client := loggedInClient(t, testServer)

	all := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	if !strings.Contains(all, "2 pembacaan") {
		t.Fatalf("the unfiltered page does not count both readings:\n%s", firstLines(all))
	}

	august := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?hm_from=2026-08-01&hm_to=2026-08-31")
	if !strings.Contains(august, "1 pembacaan") {
		t.Fatalf("the august page does not count one reading:\n%s", firstLines(august))
	}
	if !strings.Contains(august, "from=2026-08-01") || !strings.Contains(august, "to=2026-08-31") {
		t.Fatalf("the download links lost the range:\n%s", firstLines(august))
	}
}

// The readings card filters by machine too, and the two cards' filters do not
// reach into each other.
func TestA2BExportReadingsFilterByUnitOnTheirOwn(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedMachine(t, store, 2, "BLD-02", "Caterpillar", "PIT B", 500, 26.0)
	seedHourMeterReadingFor(t, store, "exc01", "2026-08-07", 1200, 1208)
	seedHourMeterReadingFor(t, store, "bld-02", "2026-08-08", 300, 307)
	client := loggedInClient(t, testServer)

	one := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?hm_unit=bld-02")
	if !strings.Contains(one, "1 pembacaan") {
		t.Fatalf("the unit filter does not narrow the readings:\n%s", firstLines(one))
	}
	// The performance card above is untouched by the readings card's filter.
	if !strings.Contains(one, "2 unit siap diunduh") {
		t.Fatalf("the readings filter narrowed the performance card too:\n%s", firstLines(one))
	}
}

// The readings download in both formats, and the file names the range.
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
		response := downloadProduksi(t, client, testServer.URL+"/a2b/export/hm/download?format="+format+"&from=2026-08-01&to=2026-08-31")
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
		if !strings.Contains(disposition, "input-hm-2026-08-01_2026-08-31."+format) {
			t.Fatalf("%s: content disposition %q", format, disposition)
		}
	}
}

// The readings export refuses a date that is not a date.
func TestA2BHMExportRejectsAnInvalidDate(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/a2b/export/hm/download?format=xlsx&from=bukan-tanggal")
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
