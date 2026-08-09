package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
)

// notaFields is one valid reimbursement. Item inputs repeat under one name, so
// they are kept apart from the single-valued fields.
func notaFields() map[string]string {
	return map[string]string{
		"tanggal":            "2026-08-07",
		"pic":                "Budi",
		"metode":             model.NotaMetodeReimburse,
		"penerima_reimburse": "Budi Santoso",
		"kategori":           "Umum ADM",
		"sub_kategori":       "ATK",
	}
}

func notaItems() []map[string]string {
	return []map[string]string{
		{"item_nama": "Kertas A4", "item_satuan": "rim", "item_volume": "2", "item_harga": "55000"},
	}
}

func postNota(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string,
	fields map[string]string, items []map[string]string, attachments []string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		_ = writer.WriteField("csrf_token", csrf)
	}
	for name, value := range fields {
		_ = writer.WriteField(name, value)
	}
	// Every line contributes one value to each of the four item arrays, in the
	// same order, which is what the handler pairs up.
	for _, item := range items {
		for _, name := range []string{"item_nama", "item_satuan", "item_volume", "item_harga"} {
			_ = writer.WriteField(name, item[name])
		}
	}
	for _, field := range attachments {
		part, err := writer.CreateFormFile(field, field+".jpg")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(testJPEG(t)); err != nil {
			t.Fatalf("write attachment: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/nota", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post nota: %v", err)
	}
	return response
}

func TestNotaFormRendersEverySection(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/nota")

	for _, fragment := range []string{
		"1. DETAIL TRANSAKSI",
		"2. KATEGORI BIAYA",
		"3. DETAIL PRODUK / ITEM NOTA",
		"4. LAMPIRAN DOKUMEN",
		`name="tanggal"`, `name="pic"`, `name="metode"`, `name="penerima_reimburse"`,
		`name="kategori"`, `name="sub_kategori"`, `name="jenis_perjalanan"`,
		`name="item_nama"`, `name="item_satuan"`, `name="item_volume"`,
		// Text, not number: a number input cannot display grouping dots.
		`name="item_harga" type="text" inputmode="numeric"`,
		`name="foto_kwitansi"`, `name="bukti_transfer"`,
		`enctype="multipart/form-data"`,
		// The identifier is shown but never posted; the server assigns it.
		`id="nota_id"`,
		`value="NTA-20260807-0001" disabled`,
		// Both categories and every sub category, so the pairing holds without
		// JavaScript.
		"Operasional", "QHSE", "Material Bantu",
		"Konsumsi", "Perjalanan Dinas", "Lain-lain",
		"CA (Cash Advance)", "Reimburse",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("nota form missing %q", fragment)
		}
	}
	// The status is a stated consequence of the method, never an input.
	if strings.Contains(page, `name="status`) {
		t.Fatal("the form submits a payment status")
	}
}

// Five inputs abreast on a phone truncate their own placeholders, so each line
// becomes a labelled, numbered card there and stays a single row on a wide
// screen, where the captions are written once above the list.
func TestNotaItemRowsReadOnAPhone(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/nota")

	for _, fragment := range []string{
		// Captions once, and hidden from screen readers because every input
		// carries its own name.
		`<div class="nota-items-head" aria-hidden="true">`,
		`<p class="nota-item-index">Item <span data-item-number>1</span></p>`,
		// Each caption carries an icon of what the field asks for.
		`<span class="field-label">`,
		`>Nama produk</span>`,
		`>Satuan</span>`,
		`>Harga (Rp)</span>`,
		`aria-label="Volume"`,
		`aria-label="Harga"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the item row is missing %q", fragment)
		}
	}
	// A placeholder that says "Satuan (Sat)" is what got cut off; the label
	// carries the name now and the placeholder shows an example.
	if strings.Contains(page, `placeholder="Satuan (Sat)"`) || strings.Contains(page, `placeholder="Harga (Rp)"`) {
		t.Fatal("the item inputs still carry the placeholders that truncated")
	}

	css := stylesheet(t)
	for _, rule := range []string{
		// Desktop: captions above, no repeated labels.
		".nota-item .field-label,",
		".nota-item-index { display: none; }",
		// Phone: the table header goes, the labels come back.
		".nota-items-head { display: none; }",
		".nota-item .field-label { display: block;",
		".nota-item-index { display: block;",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}

	script := fetchPage(t, testServer.URL+"/static/js/nota.js")
	// The cards are numbered, so the numbers have to follow an add or a remove.
	if !strings.Contains(script, "const renumber = () => {") {
		t.Fatal("nota.js does not renumber the item cards")
	}

	// After filling the last card the next thing wanted is another card, so the
	// button sits below the list rather than above it.
	listAt := strings.Index(page, `data-item-list`)
	addAt := strings.Index(page, `data-add-item`)
	totalAt := strings.Index(page, `data-nota-total`)
	if listAt < 0 || addAt < 0 || totalAt < 0 {
		t.Fatal("the item list, the add button or the total is missing")
	}
	if addAt < listAt || addAt > totalAt {
		t.Fatal("the add button is not between the item list and the total")
	}
}

// The conditional fields render hidden rather than absent, so a browser with no
// JavaScript still has them once the method or sub category calls for them.
func TestNotaFormHidesConditionalFieldsByDefault(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/nota")

	for _, fragment := range []string{
		"data-perjalanan-field hidden",
		"data-transfer-field hidden",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("nota form is missing %q", fragment)
		}
	}
	// Nothing has been chosen yet, so the payee field is the one on show: an
	// unpaid nota is the default reading.
	if strings.Contains(page, "data-reimburse-field hidden") {
		t.Fatal("the payee field starts hidden even though the status starts unpaid")
	}
}

func TestNotaCreateStoresTheNoteAndItsItems(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))

	items := append(notaItems(), map[string]string{
		"item_nama": "Tinta printer", "item_satuan": "pcs", "item_volume": "1", "item_harga": "120000",
	})
	response := postNota(t, client, testServer, csrf, notaFields(), items, []string{"foto_kwitansi"})
	page := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, page)
	}

	stored := store.NotaList()
	if len(stored) != 1 {
		t.Fatalf("stored %d notes, want 1", len(stored))
	}
	nota := stored[0]
	if nota.NotaID != "NTA-20260807-0001" {
		t.Fatalf("nota id = %q", nota.NotaID)
	}
	if nota.StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatalf("status = %q", nota.StatusPembayaran)
	}
	if len(nota.Items) != 2 || nota.Total != 230000 {
		t.Fatalf("items %+v total %v", nota.Items, nota.Total)
	}
	if nota.FotoKwitansi == "" {
		t.Fatal("the receipt was not stored")
	}
	// The success message repeats the identifier, so the person can find the
	// row they just filed.
	if !strings.Contains(page, "NTA-20260807-0001") || !strings.Contains(page, "230.000") {
		t.Fatalf("the confirmation does not name the nota and its total: %s", page)
	}
}

// The status belongs to the method. A browser that posts one anyway must not
// turn an unpaid reimbursement into a settled one.
// The price field is a text input so it can show thousand separators; a
// submission that still carries them must store the plain number.
func TestNotaCreateStoresAGroupedPriceAsANumber(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))

	items := []map[string]string{
		{"item_nama": "Laptop", "item_satuan": "unit", "item_volume": "1", "item_harga": "12.500.000"},
	}
	response := postNota(t, client, testServer, csrf, notaFields(), items, []string{"foto_kwitansi"})
	response.Body.Close()

	stored := store.NotaList()
	if len(stored) != 1 {
		t.Fatalf("stored %d notes, want 1", len(stored))
	}
	if stored[0].Total != 12500000 {
		t.Fatalf("total = %v, want 12500000", stored[0].Total)
	}
}

func TestNotaCreateIgnoresASubmittedStatus(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))

	fields := notaFields()
	fields["status_pembayaran"] = model.NotaStatusSudahDibayar
	fields["total"] = "1"
	response := postNota(t, client, testServer, csrf, fields, notaItems(), []string{"foto_kwitansi"})
	response.Body.Close()

	stored := store.NotaList()
	if len(stored) != 1 {
		t.Fatalf("stored %d notes, want 1", len(stored))
	}
	if stored[0].StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatalf("status = %q, want unpaid", stored[0].StatusPembayaran)
	}
	if stored[0].Total != 110000 {
		t.Fatalf("total = %v, want the sum of the items", stored[0].Total)
	}
}

func TestNotaCreateCashAdvanceNeedsBothAttachments(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))

	fields := notaFields()
	fields["metode"] = model.NotaMetodeCA
	delete(fields, "penerima_reimburse")

	response := postNota(t, client, testServer, csrf, fields, notaItems(), []string{"foto_kwitansi"})
	page := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", response.StatusCode)
	}
	if !strings.Contains(page, "bukti transfer") {
		t.Fatalf("the error does not name the missing attachment: %s", page)
	}
	if len(store.NotaList()) != 0 {
		t.Fatal("an incomplete cash advance was stored")
	}

	csrf = csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))
	complete := postNota(t, client, testServer, csrf, fields, notaItems(),
		[]string{"foto_kwitansi", "bukti_transfer"})
	complete.Body.Close()
	stored := store.NotaList()
	if len(stored) != 1 {
		t.Fatalf("stored %d notes, want 1", len(stored))
	}
	if stored[0].StatusPembayaran != model.NotaStatusSudahDibayar {
		t.Fatalf("status = %q, want paid", stored[0].StatusPembayaran)
	}
	if stored[0].BuktiTransfer == "" {
		t.Fatal("the transfer proof was not stored")
	}
}

// A rejected submission comes back filled in; retyping a whole nota because one
// field was wrong is how people give up on a form.
func TestNotaCreateKeepsTheTypedValuesOnError(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))

	fields := notaFields()
	fields["penerima_reimburse"] = ""
	response := postNota(t, client, testServer, csrf, fields, notaItems(), []string{"foto_kwitansi"})
	page := readBody(t, response)

	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", response.StatusCode)
	}
	for _, fragment := range []string{
		`value="Budi"`, `value="Kertas A4"`, `value="rim"`, `value="55000"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the form came back without %q", fragment)
		}
	}
}

func TestNotaCreateRejectsAMissingCSRFToken(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	response := postNota(t, client, testServer, "", notaFields(), notaItems(), []string{"foto_kwitansi"})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", response.StatusCode)
	}
	if len(store.NotaList()) != 0 {
		t.Fatal("a nota was stored without a CSRF token")
	}
}

func TestNotaRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(testServer.URL + "/nota")
	if err != nil {
		t.Fatalf("get /nota: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}

// The line inputs arrive as four parallel arrays. A short one must drop the
// incomplete tail rather than pairing a name with the next line's price.
func TestNotaItemsFromFormPairsByPosition(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/nota", nil)
	request.Form = map[string][]string{
		"item_nama":   {"Kertas", "Tinta", "Map"},
		"item_satuan": {"rim", "pcs", "pcs"},
		"item_volume": {"2", "1"},
		"item_harga":  {"55000", "120000"},
	}
	items := notaItemsFromForm(request)
	if len(items) != 2 {
		t.Fatalf("read %d items, want 2", len(items))
	}
	if items[1].NamaProduk != "Tinta" || items[1].Harga != "120000" {
		t.Fatalf("second item paired wrongly: %+v", items[1])
	}
}

func TestFormatRupiahGroupsThousands(t *testing.T) {
	for value, want := range map[float64]string{
		0: "0", 999: "999", 1000: "1.000", 230000: "230.000", 1234567: "1.234.567", -5000: "-5.000",
	} {
		if got := formatRupiah(value); got != want {
			t.Fatalf("formatRupiah(%v) = %q, want %q", value, got, want)
		}
	}
}
