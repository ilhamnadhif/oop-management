package handler

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

// Each register downloads from its own menu: they describe different machines
// with different columns, and one file holding both would leave half of every
// row empty.
func TestRegisterExportsDownloadBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	client := loggedInClient(t, testServer)

	formats := map[string]struct {
		contentType string
		magic       []byte
	}{
		"xlsx": {"spreadsheetml", []byte("PK")},
		"pdf":  {"application/pdf", []byte("%PDF-")},
	}
	for base, slug := range map[string]string{
		"/unit/export": "unit-dt",
		"/a2b/export":  "unit-a2b",
	} {
		for format, want := range formats {
			response := downloadProduksi(t, client, testServer.URL+base+"/download?format="+format)
			body := readBodyBytes(t, response)

			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s %s: status %d", base, format, response.StatusCode)
			}
			if got := response.Header.Get("Content-Type"); !strings.Contains(got, want.contentType) {
				t.Fatalf("%s %s: content type %q", base, format, got)
			}
			if !bytes.HasPrefix(body, want.magic) {
				t.Fatalf("%s %s: body does not start with %q", base, format, want.magic)
			}
			// The browser has to save it, and the two registers must not
			// overwrite each other in the download folder.
			disposition := response.Header.Get("Content-Disposition")
			if !strings.HasPrefix(disposition, "attachment;") ||
				!strings.Contains(disposition, slug) ||
				!strings.Contains(disposition, "."+format) {
				t.Fatalf("%s %s: content disposition %q", base, format, disposition)
			}
			if got := response.Header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("%s %s: cache control %q, want no-store", base, format, got)
			}
		}
	}
}

func TestRegisterExportsAreSeparateFiles(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	client := loggedInClient(t, testServer)

	dt := downloadProduksi(t, client, testServer.URL+"/unit/export/download?format=xlsx")
	dtBody := readBodyBytes(t, dt)
	a2b := downloadProduksi(t, client, testServer.URL+"/a2b/export/download?format=xlsx")
	a2bBody := readBodyBytes(t, a2b)

	if bytes.Equal(dtBody, a2bBody) {
		t.Fatal("both registers produced the same file")
	}
	if dtName, a2bName := dt.Header.Get("Content-Disposition"), a2b.Header.Get("Content-Disposition"); dtName == a2bName {
		t.Fatalf("both registers download under the same name: %q", dtName)
	}
}

func TestRegisterExportsRejectAnUnknownFormat(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)

	for _, url := range []string{
		"/unit/export/download?format=docx",
		"/unit/export/download",
		"/a2b/export/download?format=docx",
		"/a2b/export/download",
	} {
		response := downloadProduksi(t, client, testServer.URL+url)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", url, response.StatusCode)
		}
	}
}

// Each page says how much it will produce, and offers no button at all when its
// register is still empty.
func TestRegisterExportPagesReportRowCounts(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	for _, base := range []string{"/unit/export", "/a2b/export"} {
		empty := fetchAuthedPage(t, client, testServer.URL+base)
		if strings.Contains(empty, base+"/download") {
			t.Fatalf("%s offers a download for an empty register", base)
		}
	}

	seedUnit(t, store)
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 400, 18.5)
	for base, register := range map[string]string{
		"/unit/export": "Unit DT",
		"/a2b/export":  "Unit A2B",
	} {
		page := fetchAuthedPage(t, client, testServer.URL+base)
		for _, want := range []string{
			register, "1 unit terdaftar",
			base + "/download?format=xlsx", base + "/download?format=pdf",
		} {
			if !strings.Contains(page, want) {
				t.Fatalf("%s is missing %q", base, want)
			}
		}
		// One page, one register. The menu names both, so the check looks at
		// the content area rather than the whole page.
		content := sectionBetween(t, page, `<main class="app-shell">`, "</main>")
		other := "Unit A2B"
		if register == "Unit A2B" {
			other = "Unit DT"
		}
		if strings.Contains(content, other) {
			t.Fatalf("%s also lists %q", base, other)
		}
	}
}

// The registers name every driver and every machine, so they sit behind the
// same guard as the rest of the app.
func TestRegisterExportsRequireASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{
		"/unit/export", "/unit/export/download?format=xlsx",
		"/a2b/export", "/a2b/export/download?format=xlsx",
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
