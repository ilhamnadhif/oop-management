package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/tally"
)

type fakeTallyScanner struct {
	sheet   tally.Sheet
	err     error
	calls   int
	dataURL string
}

func (f *fakeTallyScanner) Scan(_ context.Context, imageDataURL string) (tally.Sheet, error) {
	f.calls++
	f.dataURL = imageDataURL
	return f.sheet, f.err
}

func scannedTallySheet() tally.Sheet {
	return tally.Sheet{
		Rows: []tally.Row{
			{Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
				Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: 0.2},
			{Nomor: 2, Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
				Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC"},
			{Nomor: 3, Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
				Lokasi: "Blok A", Layer: "L1", Nopol: "B 9021 XY"},
		},
	}
}

func newTallyScanServer(t *testing.T, scanner tally.Scanner) (*httptest.Server, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }
	server, err := NewServer(testDeps(t, store, location, nowFunc, defaultTestBranding()))
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if scanner != nil {
		server.WithTallyScanner(scanner, 90*time.Second)
	}
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)

	unit := &model.UnitDT{
		UnitID: "UNT-2026-0001", Nopol: "B 1234 ABC",
		Panjang: 375, Lebar: 190, Tinggi: 150, Driver: "Slamet", Keterangan: "DT KECIL",
	}
	if err := store.CreateUnitDT(context.Background(), unit); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	return testServer, store
}

// The panel is how the feature is found at all, so it is checked rather than
// assumed.
func TestProduksiPageOffersTheSheetScan(t *testing.T) {
	testServer, _ := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")
	for _, fragment := range []string{
		"Scan lembar tally", `action="/produksi/scan/commit"`, `data-scan-enabled="true"`,
		"/static/js/produksi-scan.js",
		// The browser is told the same budget the reader was given, so it cannot
		// abandon a scan the server is still running.
		`data-scan-timeout="90000"`,
		// The two the reader is never asked for are typed on the dialog, and the
		// date opens on today so the common case needs no typing at all.
		`name="tanggal"`, `name="supplier"`, `list="scanSupplierList"`,
		`id="scan_tanggal" name="tanggal" type="date" value="2026-08-07"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the produksi page is missing %q", fragment)
		}
	}
	// A script referenced but not embedded leaves the panel inert with nothing
	// on the page to say so.
	asset, err := client.Get(testServer.URL + "/static/js/produksi-scan.js")
	if err != nil {
		t.Fatalf("get script: %v", err)
	}
	defer asset.Body.Close()
	if asset.StatusCode != http.StatusOK {
		t.Fatalf("the panel script is not served: %d", asset.StatusCode)
	}
}

// Reading the sheet writes nothing. It answers with what the paper appears to
// say, already judged against the register.
func TestProduksiScanPreviewsWithoutStoringAnything(t *testing.T) {
	scanner := &fakeTallyScanner{sheet: scannedTallySheet()}
	testServer, store := newTallyScanServer(t, scanner)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	response := postSheetScan(t, client, testServer.URL+"/produksi/scan", csrf, map[string]string{}, testJPEG(t))
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		OK      bool `json:"ok"`
		Siap    int  `json:"siap"`
		Ditolak int  `json:"ditolak"`
		Rows    []struct {
			Nopol  string `json:"nopol"`
			Alasan string `json:"alasan"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.OK || payload.Siap != 2 || payload.Ditolak != 1 {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.Rows[2].Alasan == "" {
		t.Fatalf("the unregistered plate was not flagged: %+v", payload.Rows[2])
	}
	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 0 {
		t.Fatalf("a preview stored %d rows", len(rows))
	}
	// The model is sent the raw photograph, not the shrunk archive copy.
	if !strings.HasPrefix(scanner.dataURL, "data:image/") {
		t.Fatalf("scanner received %q", scanner.dataURL)
	}
}

// Committing files every storable row and names the plates still to register.
func TestProduksiScanCommitStoresRowsAndNamesWhatItSkipped(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": scannedRowsJSON(), "tanggal": "2026-08-07"}, testJPEG(t))
	defer response.Body.Close()
	page := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: %s", response.StatusCode, page)
	}
	for _, fragment := range []string{"2 baris produksi tersimpan", "1 baris dilewati", "B 9021 XY"} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the result message is missing %q: %s", fragment, page)
		}
	}

	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 2 {
		t.Fatalf("stored %d rows, want 2", len(rows))
	}
	if rows[0].Driver != "Slamet" || rows[0].Volume <= 0 {
		t.Fatalf("row not completed from the register: %+v", rows[0])
	}
}

// The same file filed twice is the same work counted twice.
func TestProduksiScanCommitRefusesTheSamePhotoTwice(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))
	image := testJPEG(t)

	first := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf, map[string]string{"rows": scannedRowsJSON(), "tanggal": "2026-08-07"}, image)
	first.Body.Close()

	second := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf, map[string]string{"rows": scannedRowsJSON(), "tanggal": "2026-08-07"}, image)
	defer second.Body.Close()
	page := readBody(t, second)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", second.StatusCode, page)
	}
	if !strings.Contains(page, "sudah pernah discan") {
		t.Fatalf("the page does not say the sheet was already filed: %s", page)
	}
	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 2 {
		t.Fatalf("the refused commit stored more rows: %d", len(rows))
	}
}

// Without a key the panel still renders and says so, and the endpoint refuses
// rather than pretending.
func TestProduksiScanIsRefusedWhenNotConfigured(t *testing.T) {
	testServer, _ := newTallyScanServer(t, nil)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")
	if !strings.Contains(page, `data-scan-enabled="false"`) {
		t.Fatalf("the panel does not report the scan as unconfigured: %s", page)
	}

	csrf := csrfFromForm(t, page)
	response := postSheetScan(t, client, testServer.URL+"/produksi/scan", csrf, map[string]string{}, testJPEG(t))
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.StatusCode)
	}
}

// A commit arrives from a browser, so its rows are judged again rather than
// believed.
func TestProduksiScanCommitRevalidatesThePostedRows(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	tampered := `[{"no":1,"project":"PCPM","supplier":"HPP","quary":"HS",` +
		`"kategori":"Replace","lokasi":"Blok A","layer":"L1","nopol":"B 0000 ZZ","tt":9}]`
	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": tampered, "tanggal": "2026-08-07"}, testJPEG(t))
	defer response.Body.Close()
	page := readBody(t, response)
	if !strings.Contains(page, "0 baris produksi tersimpan") {
		t.Fatalf("an unregistered plate was accepted: %s", page)
	}
	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 0 {
		t.Fatalf("stored %d rows, want none", len(rows))
	}
}

func scannedRowsJSON() string {
	return `[` +
		`{"no":1,"project":"PCPM","supplier":"HPP","quary":"HS","kategori":"Replace","lokasi":"Blok A","layer":"L1","nopol":"B 1234 ABC","tt":0.2},` +
		`{"no":2,"project":"PCPM","supplier":"HPP","quary":"HS","kategori":"Replace","lokasi":"Blok A","layer":"L1","nopol":"B 1234 ABC","tt":0},` +
		`{"no":3,"project":"PCPM","supplier":"HPP","quary":"HS","kategori":"Replace","lokasi":"Blok A","layer":"L1","nopol":"B 9021 XY","tt":0,"alasan":"Nopol belum terdaftar di Unit DT"}` +
		`]`
}

// postSheetScan is the fetch the panel makes: the token rides in a header,
// which is the only form a multipart request may carry it in before its body
// has been read.
func postSheetScan(t *testing.T, client *http.Client, url, csrf string, fields map[string]string, image []byte) *http.Response {
	t.Helper()
	return postSheet(t, client, url, csrf, true, fields, image)
}

// postSheetCommit is the dialog's submit button: a plain form post, which
// cannot set a header and carries the token as a field like every other form on
// the site.
func postSheetCommit(t *testing.T, client *http.Client, url, csrf string, fields map[string]string, image []byte) *http.Response {
	t.Helper()
	if fields == nil {
		fields = map[string]string{}
	}
	fields["csrf_token"] = csrf
	return postSheet(t, client, url, "", false, fields, image)
}

func postSheet(t *testing.T, client *http.Client, url, csrf string, header bool, fields map[string]string, image []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}
	part, err := writer.CreateFormFile("lembar", "lembar.jpg")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(image); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, url, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if header && csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return response
}

// The dialog lets a misread plate or height be typed over. What is stored is
// what the dialog was left showing, judged again from the register.
func TestProduksiScanCommitStoresTheCorrectedRow(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	// The reader saw "B 1Z34 A8C" and 0.02; the operator corrects both.
	corrected := `[{"no":1,"project":"PCPM","supplier":"HPP","quary":"HS","kategori":"Replace",` +
		`"lokasi":"Blok A","layer":"L1","nopol":"b 1234 abc","tt":0.25}]`
	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": corrected, "tanggal": "2026-08-07"}, testJPEG(t))
	defer response.Body.Close()
	page := readBody(t, response)
	if !strings.Contains(page, "1 baris produksi tersimpan") {
		t.Fatalf("the corrected row did not store: %s", page)
	}

	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	// The register spells the plate however it was typed, and the height is the
	// corrected one.
	if rows[0].Nopol != "B 1234 ABC" || rows[0].TT != 0.25 {
		t.Fatalf("stored = %+v", rows[0])
	}
	// TF = 150 + 0.25/2 = 150.125, so the volume follows the correction.
	if rows[0].TF != 150.13 {
		t.Fatalf("TF = %v, want 150.13", rows[0].TF)
	}
}

// A height typed below zero is refused by the server too, not only greyed out
// in the dialog.
func TestProduksiScanCommitRefusesANegativeTopUpHeight(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	rows := `[{"no":1,"project":"PCPM","supplier":"HPP","quary":"HS","kategori":"Replace",` +
		`"lokasi":"Blok A","layer":"L1","nopol":"B 1234 ABC","tt":-3}]`
	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": rows, "tanggal": "2026-08-07"}, testJPEG(t))
	defer response.Body.Close()
	page := readBody(t, response)
	if !strings.Contains(page, "0 baris produksi tersimpan") {
		t.Fatalf("a negative height was accepted: %s", page)
	}
	if stored, _ := store.ListProduksi(context.Background()); len(stored) != 0 {
		t.Fatalf("stored %d rows, want none", len(stored))
	}
}

// A plate the wrong shape and a plate nobody registered are different problems
// with different answers, and the result has to say which is which.
func TestProduksiScanCommitNamesAMalformedPlateAsSuch(t *testing.T) {
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: scannedTallySheet()})
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	rows := `[{"no":1,"project":"","supplier":"","quary":"","kategori":"","lokasi":"","layer":"","nopol":"AB6990OE","tt":0}]`
	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": rows, "tanggal": "2026-08-07"}, testJPEG(t))
	defer response.Body.Close()
	page := readBody(t, response)
	if !strings.Contains(page, "0 baris produksi tersimpan") {
		t.Fatalf("a misspelled plate was stored: %s", page)
	}
	if stored, _ := store.ListProduksi(context.Background()); len(stored) != 0 {
		t.Fatalf("stored %d rows, want none", len(stored))
	}
}
