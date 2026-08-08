package handler

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedNota stores one note with its lines, the shape the export reads back.
func seedNota(t *testing.T, store *repository.TestRepository, notaID, tanggal string, items ...model.NotaItem) {
	t.Helper()
	total := 0.0
	for i := range items {
		items[i].NotaID = notaID
		items[i].Baris = i + 1
		items[i].Subtotal = items[i].Volume * items[i].Harga
		total += items[i].Subtotal
	}
	nota := &model.Nota{
		NotaID: notaID, Tanggal: tanggal, PIC: "Budi",
		MetodePembayaran:  model.NotaMetodeReimburse,
		StatusPembayaran:  model.NotaStatusBelumDibayar,
		PenerimaReimburse: "Budi Santoso", Kategori: "Umum ADM", SubKategori: "ATK",
		Total: total, Items: items,
	}
	if err := store.CreateNota(t.Context(), nota); err != nil {
		t.Fatalf("seed nota: %v", err)
	}
}

func notaLine(nama string, volume, harga float64) model.NotaItem {
	return model.NotaItem{NamaProduk: nama, Satuan: "pcs", Volume: volume, Harga: harga}
}

func TestNotaExportDownloadsBothFormats(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Kertas A4", 2, 55000))
	client := loggedInClient(t, testServer)

	cases := map[string]struct {
		contentType string
		magic       []byte
	}{
		"xlsx": {"spreadsheetml", []byte("PK")},
		"pdf":  {"application/pdf", []byte("%PDF-")},
	}
	for format, want := range cases {
		response := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format="+format)
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
		if !strings.HasPrefix(disposition, "attachment;") ||
			!strings.Contains(disposition, "laporan-nota") ||
			!strings.Contains(disposition, "."+format) {
			t.Fatalf("%s: content disposition %q", format, disposition)
		}
		if got := response.Header.Get("Cache-Control"); got != "no-store" {
			t.Fatalf("%s: cache control %q, want no-store", format, got)
		}
	}
}

// The report is one row per item, so the count on the page has to be the number
// of rows the file will hold rather than the number of notes.
func TestNotaExportCountsItemsNotNotes(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07",
		notaLine("Kertas A4", 2, 55000), notaLine("Tinta", 1, 120000))
	seedNota(t, store, "NTA-20260808-0001", "2026-08-08", notaLine("Map", 3, 5000))
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/export")
	if !strings.Contains(page, "3 baris siap diunduh") {
		t.Fatal("the export page does not count the item rows")
	}
	// The buttons must carry the nota paths, not the produksi ones.
	for _, fragment := range []string{
		`action="/nota/export"`,
		"/nota/export/download?format=xlsx",
		"/nota/export/download?format=pdf",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the nota export page is missing %q", fragment)
		}
	}
}

func TestNotaExportRespectsTheDateRange(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260731-0001", "2026-07-31", notaLine("Kertas", 1, 50000))
	seedNota(t, store, "NTA-20260801-0001", "2026-08-01", notaLine("Tinta", 1, 120000))
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/export?from=2026-08-01&to=2026-08-31")
	if !strings.Contains(page, "1 baris siap diunduh") {
		t.Fatal("the export page does not report the filtered row count")
	}
	// The filename names the period, so downloads do not pile up as
	// indistinguishable copies.
	response := downloadProduksi(t, client,
		testServer.URL+"/nota/export/download?format=xlsx&from=2026-08-01&to=2026-08-31")
	readBodyBytes(t, response)
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "2026-08-01_2026-08-31") {
		t.Fatalf("content disposition %q does not name the period", got)
	}
}

func TestNotaExportRejectsUnknownFormatAndBadDates(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Kertas", 1, 50000))
	client := loggedInClient(t, testServer)

	response := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format=docx")
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown format: status %d, want 400", response.StatusCode)
	}

	bad := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format=pdf&from=07-08-2026")
	bad.Body.Close()
	if bad.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("malformed date: status %d, want 422", bad.StatusCode)
	}
}

// The report names every payee and every purchase, so it sits behind the same
// guard as the rest of the app.
func TestNotaExportRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/nota/export", "/nota/export/download?format=xlsx"} {
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
