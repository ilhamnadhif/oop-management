package handler

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/service"
)

// seedNota stores one note with its lines, the shape the export reads back.
func seedNota(t *testing.T, store *repository.TestRepository, notaID, tanggal string, items ...model.NotaItem) {
	t.Helper()
	seedNotaMetode(t, store, notaID, tanggal, model.NotaMetodeReimburse, items...)
}

func seedNotaMetode(t *testing.T, store *repository.TestRepository, notaID, tanggal, metode string, items ...model.NotaItem) {
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
		MetodePembayaran:  metode,
		StatusPembayaran:  service.StatusFor(metode),
		PenerimaReimburse: "Budi Santoso", Kategori: "Umum ADM", SubKategori: "ATK",
		Total: total, Items: items,
	}
	if metode != model.NotaMetodeReimburse {
		nota.PenerimaReimburse = ""
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

// notaPhoto builds a stored photo of a given size. The sizes have to differ
// between attachments: fpdf embeds identical bytes once, which is right for the
// file but would hide a second photo going missing.
func notaPhoto(t *testing.T, width, height int) string {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture.SetRGBA(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, picture, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode photo: %v", err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// The receipts have to survive the whole path: the store reads them, the
// service carries them and the PDF prints them. Testing the renderer alone
// passed while the read stopped short of the photo column and the file came out
// bare. The payment proofs travel with the same notes and must not be printed.
func TestNotaExportPDFCarriesTheReceiptsOnly(t *testing.T) {
	// The letterhead logo is an embedded image too, so the appendix is counted
	// as the difference against the same report with no photos attached.
	download := func(withPhotos bool) int {
		t.Helper()
		testServer, store := newTestServerWithStore(t)
		for i, notaID := range []string{"NTA-20260807-0001", "NTA-20260807-0002"} {
			nota := &model.Nota{
				NotaID: notaID, Tanggal: "2026-08-07", PIC: "Budi",
				MetodePembayaran:  model.NotaMetodeReimburse,
				StatusPembayaran:  model.NotaStatusSudahDibayar,
				PenerimaReimburse: "Budi Santoso", Kategori: "Umum ADM", SubKategori: "ATK",
				Total: 110000,
				Items: []model.NotaItem{{
					NotaID: notaID, Baris: 1,
					NamaProduk: "Kertas A4", Satuan: "rim", Volume: 2, Harga: 55000, Subtotal: 110000,
				}},
			}
			if withPhotos {
				// Distinct sizes throughout, so a photo printed in place of
				// another cannot pass as the right count.
				nota.FotoKwitansi = notaPhoto(t, 48+i, 64)
				nota.BuktiTransfer = notaPhoto(t, 64, 48+i)
				nota.BuktiBayar = notaPhoto(t, 40+i, 40)
			}
			if err := store.CreateNota(t.Context(), nota); err != nil {
				t.Fatalf("seed nota: %v", err)
			}
		}
		client := loggedInClient(t, testServer)
		response := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format=pdf")
		body := readBodyBytes(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("status %d", response.StatusCode)
		}
		return bytes.Count(body, []byte("/Subtype /Image"))
	}

	// Two notes, six photos stored, two receipts printed.
	bare := download(false)
	if added := download(true) - bare; added != 2 {
		t.Fatalf("the appendix printed %d photos, want the 2 receipts alone", added)
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

// A cash advance and a reimbursement are settled by different people out of
// different money, so a report is often wanted for one of them alone.
func TestNotaExportFiltersByMetodePembayaran(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNotaMetode(t, store, "NTA-20260807-0001", "2026-08-07", model.NotaMetodeReimburse,
		notaLine("Kertas A4", 2, 55000), notaLine("Tinta", 1, 120000))
	seedNotaMetode(t, store, "NTA-20260808-0001", "2026-08-08", model.NotaMetodeCA,
		notaLine("Sarung tangan", 5, 15000))
	client := loggedInClient(t, testServer)

	if page := fetchAuthedPage(t, client, testServer.URL+"/nota/export"); !strings.Contains(page, "3 baris siap diunduh") {
		t.Fatal("the unfiltered page does not count every row")
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/export?metode=REIMBURSE")
	if !strings.Contains(page, "2 baris siap diunduh") {
		t.Fatal("the page does not count only the reimbursement rows")
	}
	// The choice has to survive the round trip and reach the download links,
	// or the file would cover a wider set than the page just reported.
	for _, fragment := range []string{
		`<option value="REIMBURSE" selected>`,
		"/nota/export/download?format=xlsx&amp;metode=REIMBURSE",
		"/nota/export/download?format=pdf&amp;metode=REIMBURSE",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the export page is missing %q", fragment)
		}
	}

	if page := fetchAuthedPage(t, client, testServer.URL+"/nota/export?metode=CA"); !strings.Contains(page, "1 baris siap diunduh") {
		t.Fatal("the page does not count only the cash advance rows")
	}
}

// The filename says which method the file covers, so two downloads of the same
// period do not land as indistinguishable copies.
func TestNotaExportDownloadNamesTheMetode(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNotaMetode(t, store, "NTA-20260807-0001", "2026-08-07", model.NotaMetodeCA,
		notaLine("Sarung tangan", 5, 15000))
	client := loggedInClient(t, testServer)

	response := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format=pdf&metode=CA")
	readBodyBytes(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "laporan-nota-ca-semua.pdf") {
		t.Fatalf("content disposition %q does not name the method", got)
	}
}

// A method nobody recognises is refused rather than ignored: exporting the
// whole set under a heading naming one method would misstate what it is.
func TestNotaExportRejectsUnknownMetode(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Kertas A4", 2, 55000))
	client := loggedInClient(t, testServer)

	response := downloadProduksi(t, client, testServer.URL+"/nota/export/download?format=pdf&metode=TUNAI")
	body := readBodyBytes(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", response.StatusCode)
	}
	if !strings.Contains(string(body), "metode pembayaran tidak dikenal") {
		t.Fatalf("body %q does not say what was wrong", body)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/export?metode=TUNAI")
	if !strings.Contains(page, "metode pembayaran tidak dikenal") {
		t.Fatal("the export page does not report the rejected method")
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
