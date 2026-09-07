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

// seedFuelKeluarRow files one dispense straight into a project's store, so the
// export has something to count without going through the form's rules.
func seedFuelKeluarRow(t *testing.T, store *repository.TestRepository, id, tanggal, idUnit string, liter float64) {
	t.Helper()
	if err := store.CreateFuelKeluar(context.Background(), &model.FuelKeluar{
		FuelOutID: id, Tanggal: tanggal, IDUnit: idUnit, NamaUnit: "Excavator " + idUnit,
		HMAwalFlowMeter: 100, HMAkhirFlowMeter: 100 + liter, Liter: liter, Operator: "kadal",
	}); err != nil {
		t.Fatalf("seed fuel keluar %s: %v", id, err)
	}
}

// The page carries a third card for the dispensing sheet, filtered the same way
// as the two above it.
func TestA2BExportPageShowsTheFuelKeluarCard(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedFuelKeluarRow(t, store, "FO-1", "2026-08-07", "EXC-01", 150)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	for _, want := range []string{
		"FUEL KELUAR",
		`name="fk_from"`, `name="fk_to"`, `name="fk_unit"`,
		"/a2b/export/fuel-keluar/download?format=xlsx",
		"/a2b/export/fuel-keluar/download?format=pdf",
		"1 pemakaian siap diunduh",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
}

// The three cards keep their own filters: applying a range to one must not
// reset the other two.
func TestA2BExportCardsKeepEachOthersFilters(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedFuelKeluarRow(t, store, "FO-1", "2026-08-07", "EXC-01", 150)
	seedFuelKeluarRow(t, store, "FO-2", "2026-09-02", "EXC-01", 200)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client,
		testServer.URL+"/a2b/export?fk_from=2026-08-01&fk_to=2026-08-31&unit=EXC-01&hm_unit=EXC-01")
	if !strings.Contains(page, "1 pemakaian siap diunduh") {
		t.Fatalf("the range did not narrow the dispensing sheet:\n%s", firstLines(page))
	}
	// The other two cards' filters are carried as hidden fields, or applying
	// this one would drop them.
	for _, want := range []string{
		`<input type="hidden" name="unit" value="EXC-01">`,
		`<input type="hidden" name="hm_unit" value="EXC-01">`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page does not carry %q", want)
		}
	}
}

// The download carries its filters and answers as a file, and the name says
// which slice of the sheet it holds.
func TestFuelKeluarExportDownloadsBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedFuelKeluarRow(t, store, "FO-1", "2026-08-07", "EXC-01", 150)
	client := loggedInClient(t, testServer)

	for format, magic := range map[string][]byte{"xlsx": []byte("PK"), "pdf": []byte("%PDF-")} {
		response := downloadProduksi(t, client,
			testServer.URL+"/a2b/export/fuel-keluar/download?format="+format+"&from=2026-08-01&to=2026-08-31&unit=EXC-01")
		body := readBodyBytes(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d", format, response.StatusCode)
		}
		if !bytes.HasPrefix(body, magic) {
			t.Fatalf("%s: body does not start with %q", format, magic)
		}
		if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "fuel-keluar-EXC-01-2026-08-01_2026-08-31."+format) {
			t.Fatalf("%s: content disposition %q", format, got)
		}
	}
}

// A date that is not a date is refused rather than quietly ignored.
func TestFuelKeluarExportRejectsAnInvalidDate(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/a2b/export/fuel-keluar/download?format=xlsx&from=bukan-tanggal")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
}

// The dispensing sheet answers to its own project setting, so switching it off
// holds at the URL and not only at the hidden button.
func TestSwitchedOffFuelKeluarExportRefusesItsDownload(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedFuelKeluarRow(t, store, "FO-1", "2026-08-07", "EXC-01", 150)
	client := loggedInClient(t, testServer)

	response := saveExportConfig(t, client, testServer, store.ProjectList()[0].ProjectID, testProjectName,
		map[string]string{"export_key": string(model.ExportFuelKeluar), "ttd_count": "1"})
	response.Body.Close()

	download, err := client.Get(testServer.URL + "/a2b/export/fuel-keluar/download?format=xlsx")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a report the project switched off", download.StatusCode)
	}
}

// The machine filter is a list of ticks with a search over it, not a dropdown:
// a report is often about a handful of machines, and one choice at a time makes
// that three downloads.
func TestA2BExportUnitFiltersAreTickLists(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	seedMachine(t, store, 2, "BLD-02", "Caterpillar", "PIT B", 500, 26.0)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export")
	flat := strings.Join(strings.Fields(page), " ")
	for _, field := range []string{"unit", "hm_unit", "fk_unit"} {
		if !strings.Contains(flat, `type="checkbox" name="`+field+`" value="EXC-01"`) {
			t.Fatalf("%s is not a tick list", field)
		}
		if !strings.Contains(flat, `id="`+field+`-search"`) {
			t.Fatalf("%s has no search box", field)
		}
	}
	// The list says what is picked without having to be opened.
	if !strings.Contains(page, "Semua unit") {
		t.Fatal("the closed filter does not say that nothing is narrowed")
	}
	// What an empty filter means is a note beside the label rather than a line
	// of grey under every one of the three.
	if !strings.Contains(page, "Penjelasan: Kosongkan untuk semua unit") {
		t.Fatal("the filter does not explain what leaving it empty does")
	}
	if strings.Count(page, `class="hint">Kosongkan untuk semua unit`) != 0 {
		t.Fatal("the note is still printed under the field as well")
	}
	// The script is what makes the search work, so it has to be on the page.
	if !strings.Contains(page, `src="/static/js/unit-picker.js"`) {
		t.Fatal("the page does not load the unit picker script")
	}
}

// Two machines ticked is one report about both, and the page says so.
func TestA2BExportFiltersToSeveralUnits(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	for i, id := range []string{"EXC-01", "BLD-02", "SVD-03"} {
		seedMachine(t, store, i+1, id, "Komatsu", "PIT A", 400, 18.5)
		seedHourMeterReadingFor(t, store, id, "2026-08-07", 1200, 1208)
		seedFuelKeluarRow(t, store, "FO-"+id, "2026-08-07", id, 150)
	}
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client,
		testServer.URL+"/a2b/export?unit=EXC-01&unit=BLD-02&hm_unit=EXC-01&hm_unit=BLD-02&fk_unit=EXC-01&fk_unit=BLD-02")
	for _, want := range []string{
		"2 unit siap diunduh",      // performance
		"2 pembacaan siap diunduh", // input hm
		"2 pemakaian siap diunduh", // fuel keluar
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q:\n%s", want, firstLines(page))
		}
	}
	// The summary names both rather than making the list be opened.
	if !strings.Contains(page, "EXC-01, BLD-02") {
		t.Fatal("the closed filter does not name what is picked")
	}
	// The ticks travel to the download too.
	if !strings.Contains(page, "unit=EXC-01&amp;unit=BLD-02") {
		t.Fatalf("the download links lost the ticks:\n%s", firstLines(page))
	}
}

// Past two machines the summary counts instead of listing: a summary line
// holding ten ids is a list, not a summary.
func TestA2BExportSummaryCountsPastTwoUnits(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	for i, id := range []string{"EXC-01", "BLD-02", "SVD-03"} {
		seedMachine(t, store, i+1, id, "Komatsu", "PIT A", 400, 18.5)
	}
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/export?unit=EXC-01&unit=BLD-02&unit=SVD-03")
	if !strings.Contains(page, "3 unit dipilih") {
		t.Fatalf("the summary lists the ids rather than counting them:\n%s", firstLines(page))
	}
}

// The file is named for what it holds, and past one machine that is a count:
// ten ids in a name is unreadable and long enough to trouble a filesystem.
func TestA2BExportNamesTheFileForItsUnits(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	for i, id := range []string{"EXC-01", "BLD-02"} {
		seedMachine(t, store, i+1, id, "Komatsu", "PIT A", 400, 18.5)
		seedHourMeterReadingFor(t, store, id, "2026-08-07", 1200, 1208)
	}
	client := loggedInClient(t, testServer)

	one := downloadProduksi(t, client, testServer.URL+"/a2b/export/hm/download?format=xlsx&unit=EXC-01")
	readBodyBytes(t, one)
	if got := one.Header.Get("Content-Disposition"); !strings.Contains(got, "input-hm-EXC-01.xlsx") {
		t.Fatalf("one machine: content disposition %q", got)
	}

	both := downloadProduksi(t, client, testServer.URL+"/a2b/export/hm/download?format=xlsx&unit=EXC-01&unit=BLD-02")
	readBodyBytes(t, both)
	if got := both.Header.Get("Content-Disposition"); !strings.Contains(got, "input-hm-2-unit.xlsx") {
		t.Fatalf("two machines: content disposition %q", got)
	}
}

// A list left open sits over the download buttons, so anything that says "done
// here" closes it - while a click inside is left alone, or picking three
// machines would be three trips.
func TestTheUnitPickerScriptClosesOnAClickOutside(t *testing.T) {
	testServer := newTestServer(t)
	script := fetchPage(t, testServer.URL+"/static/js/unit-picker.js")

	for _, want := range []string{
		`addEventListener("click"`,
		"picker.contains(event.target)",
		`event.key !== "Escape"`,
		// The search box is shipped hidden and unhidden here: one that does not
		// search is worse than none.
		"search.hidden = false",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("the picker script is missing %q", want)
		}
	}
}
