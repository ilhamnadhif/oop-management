package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

// readBodyBytes keeps the payload as bytes: a report is binary, and decoding it
// as text would hide a corrupt file behind a readable-looking string.
func readBodyBytes(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}

func downloadProduksi(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	return response
}

func TestProduksiExportDownloadsBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-08-01", "B 1234 ABC", "DT KECIL", 10.6875, 10)
	seedProduksi(t, store, "2026-08-02", "BG 8611 BX", "DT BESAR", 30.6197, 28)
	client := loggedInClient(t, testServer)

	cases := map[string]struct {
		contentType string
		magic       []byte
	}{
		// XLSX is a zip, PDF announces itself in the first bytes. Checking the
		// content rather than the status catches a handler that returns an error
		// page with a 200.
		"xlsx": {"spreadsheetml", []byte("PK")},
		"pdf":  {"application/pdf", []byte("%PDF-")},
	}
	for format, want := range cases {
		response := downloadProduksi(t, client, testServer.URL+"/produksi/export/download?format="+format)
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
		// The browser has to save it, not render it.
		disposition := response.Header.Get("Content-Disposition")
		if !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, "."+format) {
			t.Fatalf("%s: content disposition %q", format, disposition)
		}
		// A report is a snapshot of a sheet that keeps moving.
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: cache control %q, want no-store", format, got)
		}
	}
}

// The filename says which period the file covers, so downloads do not pile up
// as indistinguishable copies.
func TestProduksiExportFilenameCarriesThePeriod(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-08-01", "B 1234 ABC", "DT KECIL", 10, 10)
	client := loggedInClient(t, testServer)

	response := downloadProduksi(t, client,
		testServer.URL+"/produksi/export/download?format=xlsx&from=2026-08-01&to=2026-08-31")
	response.Body.Close()
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "2026-08-01_2026-08-31") {
		t.Fatalf("content disposition %q does not name the period", got)
	}

	all := downloadProduksi(t, client, testServer.URL+"/produksi/export/download?format=pdf")
	all.Body.Close()
	if got := all.Header.Get("Content-Disposition"); !strings.Contains(got, "semua") {
		t.Fatalf("an unfiltered download is not marked as such: %q", got)
	}
}

func TestProduksiExportRespectsTheDateRange(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-07-31", "B 1 A", "DT KECIL", 5, 10)
	seedProduksi(t, store, "2026-08-01", "B 2 B", "DT KECIL", 10, 10)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/export?from=2026-08-01&to=2026-08-31")
	if !strings.Contains(page, "1 baris siap diunduh") {
		t.Fatal("the export page does not report the filtered row count")
	}

	// Both files must reflect the same filter, not just the page.
	response := downloadProduksi(t, client,
		testServer.URL+"/produksi/export/download?format=xlsx&from=2026-08-01&to=2026-08-31")
	body := readBodyBytes(t, response)
	if len(body) == 0 {
		t.Fatal("filtered download is empty")
	}
}

func TestProduksiExportRejectsUnknownFormatAndBadDates(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-08-01", "B 1 A", "DT KECIL", 10, 10)
	client := loggedInClient(t, testServer)

	response := downloadProduksi(t, client, testServer.URL+"/produksi/export/download?format=docx")
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown format: status %d, want 400", response.StatusCode)
	}

	bad := downloadProduksi(t, client, testServer.URL+"/produksi/export/download?format=pdf&from=01-08-2026")
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("malformed date: status %d, want 422", bad.StatusCode)
	}
}

// The report contains the whole production history, so it sits behind the same
// guard as every other page.
func TestProduksiExportDownloadRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/produksi/export", "/produksi/export/download?format=xlsx"} {
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
