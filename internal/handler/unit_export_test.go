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

func seedUnitA2B(t *testing.T, store *repository.TestRepository) {
	t.Helper()
	unit := &model.UnitA2B{
		NoUrut: 1, TanggalIn: "2026-08-01", IDUnit: "EX-01",
		NamaUnit: "EXCAVATOR PC 200", MerekType: "KOMATSU",
		FuelStorage: 400, FRUnit: 18.5, Lokasi: "PIT A", HMAwal: 1200,
	}
	if err := store.CreateUnitA2B(context.Background(), unit); err != nil {
		t.Fatalf("seed unit a2b: %v", err)
	}
}

// Both registers download in both formats. Checking the bytes rather than the
// status catches a handler that returns an error page with a 200.
func TestUnitExportDownloadsBothRegistersInBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	seedUnitA2B(t, store)
	client := loggedInClient(t, testServer)

	formats := map[string]struct {
		contentType string
		magic       []byte
	}{
		"xlsx": {"spreadsheetml", []byte("PK")},
		"pdf":  {"application/pdf", []byte("%PDF-")},
	}
	for _, dataset := range []string{"dt", "a2b"} {
		for format, want := range formats {
			url := testServer.URL + "/unit/export/download?dataset=" + dataset + "&format=" + format
			response := downloadProduksi(t, client, url)
			body := readBodyBytes(t, response)

			if response.StatusCode != http.StatusOK {
				t.Fatalf("%s %s: status %d", dataset, format, response.StatusCode)
			}
			if got := response.Header.Get("Content-Type"); !strings.Contains(got, want.contentType) {
				t.Fatalf("%s %s: content type %q", dataset, format, got)
			}
			if !bytes.HasPrefix(body, want.magic) {
				t.Fatalf("%s %s: body does not start with %q", dataset, format, want.magic)
			}
			// The browser has to save it, not render it, and the two registers
			// must not overwrite each other in the download folder.
			disposition := response.Header.Get("Content-Disposition")
			if !strings.HasPrefix(disposition, "attachment;") ||
				!strings.Contains(disposition, "unit-"+dataset) ||
				!strings.Contains(disposition, "."+format) {
				t.Fatalf("%s %s: content disposition %q", dataset, format, disposition)
			}
			if got := response.Header.Get("Cache-Control"); got != "no-store" {
				t.Fatalf("%s %s: cache control %q, want no-store", dataset, format, got)
			}
		}
	}
}

// The two registers are separate files; a download naming neither would leave
// the person with two identically named copies.
func TestUnitExportSeparatesTheTwoRegisters(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	seedUnitA2B(t, store)
	client := loggedInClient(t, testServer)

	dt := downloadProduksi(t, client, testServer.URL+"/unit/export/download?dataset=dt&format=xlsx")
	dtBody := readBodyBytes(t, dt)
	a2b := downloadProduksi(t, client, testServer.URL+"/unit/export/download?dataset=a2b&format=xlsx")
	a2bBody := readBodyBytes(t, a2b)

	if bytes.Equal(dtBody, a2bBody) {
		t.Fatal("both datasets produced the same file")
	}
	if dtName, a2bName := dt.Header.Get("Content-Disposition"), a2b.Header.Get("Content-Disposition"); dtName == a2bName {
		t.Fatalf("both datasets download under the same name: %q", dtName)
	}
}

func TestUnitExportRejectsUnknownFormatAndDataset(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)

	for _, query := range []string{
		"dataset=dt&format=docx",
		"dataset=trailer&format=pdf",
		"format=pdf",
		"dataset=dt",
	} {
		response := downloadProduksi(t, client, testServer.URL+"/unit/export/download?"+query)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", query, response.StatusCode)
		}
	}
}

// The page has to say how much data each button will produce, and offer no
// button at all when a register is still empty.
func TestUnitExportPageReportsRowCounts(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	empty := fetchAuthedPage(t, client, testServer.URL+"/unit/export")
	if strings.Contains(empty, "dataset=dt&") {
		t.Fatal("an empty register still offers a download")
	}

	seedUnit(t, store)
	seedUnitA2B(t, store)
	page := fetchAuthedPage(t, client, testServer.URL+"/unit/export")
	for _, want := range []string{
		"dataset=dt&amp;format=xlsx", "dataset=dt&amp;format=pdf",
		"dataset=a2b&amp;format=xlsx", "dataset=a2b&amp;format=pdf",
		"1 unit terdaftar",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the export page is missing %q", want)
		}
	}
}

// The registers name every driver and every machine, so they sit behind the
// same guard as the rest of the app.
func TestUnitExportDownloadRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/unit/export", "/unit/export/download?dataset=dt&format=xlsx"} {
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
