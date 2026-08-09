package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// fileNota stores one nota through the form, which is the only path that
// produces a real identifier.
func fileNota(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string) {
	t.Helper()
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))
	response := postNota(t, client, testServer, csrf, fields, notaItems(), []string{"foto_kwitansi"})
	page := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("file nota: status %d: %s", response.StatusCode, page)
	}
}

func postSettlement(t *testing.T, client *http.Client, testServer *httptest.Server,
	csrf, notaID string, withProof bool) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		_ = writer.WriteField("csrf_token", csrf)
	}
	_ = writer.WriteField("nota_id", notaID)
	if withProof {
		part, err := writer.CreateFormFile("bukti_bayar", "transfer.jpg")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(testJPEG(t)); err != nil {
			t.Fatalf("write proof: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/nota/rekonsiliasi", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post rekonsiliasi: %v", err)
	}
	return response
}

func storedNota(t *testing.T, store *repository.TestRepository, notaID string) model.Nota {
	t.Helper()
	for _, nota := range store.NotaList() {
		if nota.NotaID == notaID {
			return nota
		}
	}
	t.Fatalf("nota %q was not stored", notaID)
	return model.Nota{}
}

func TestRekonsiliasiListsUnpaidReimbursementsOnly(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	fileNota(t, client, testServer, notaFields())
	advance := notaFields()
	advance["metode"] = model.NotaMetodeCA
	delete(advance, "penerima_reimburse")
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))
	response := postNota(t, client, testServer, csrf, advance, notaItems(),
		[]string{"foto_kwitansi", "bukti_transfer"})
	response.Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi")
	if !strings.Contains(page, "NTA-20260807-0001") {
		t.Fatal("the unpaid reimbursement is not listed")
	}
	// The cash advance left the company when the nota was filed, so finance has
	// nothing to pay against it.
	if strings.Contains(page, "NTA-20260807-0002") {
		t.Fatal("a cash advance appears in the reconciliation list")
	}
	if !strings.Contains(page, `name="bukti_bayar"`) || !strings.Contains(page, `name="q"`) {
		t.Fatal("the page offers no upload or no search")
	}
}

// The backlog is a table so finance can scan it; the work happens in a dialog
// opened from the row.
func TestRekonsiliasiListsInATableWithARowAction(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi")
	for _, fragment := range []string{
		`class="data-table"`,
		"No transaksi",
		`data-open-dialog="dialog-NTA-20260807-0001"`,
		`id="dialog-NTA-20260807-0001"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the page is missing %q", fragment)
		}
	}
	// Two name columns pushed the action button off the page, so the PIC lives
	// in the dialog rather than the row.
	head, _, found := strings.Cut(page, "</thead>")
	if !found {
		t.Fatal("the page has no table header")
	}
	if strings.Contains(head, ">PIC<") {
		t.Fatal("the table still carries a PIC column")
	}
	if !strings.Contains(page, "<dt>PIC</dt>") {
		t.Fatal("the dialog does not show the PIC")
	}
	// Nothing was asked for, so every dialog stays shut.
	if strings.Contains(page, "<dialog class=\"modal\" id=\"dialog-NTA-20260807-0001\" aria-labelledby=\"title-NTA-20260807-0001\" open>") {
		t.Fatal("a dialog is open before anything was clicked")
	}
}

// The row action is a link, so the dialog opens even with the script switched
// off: the page comes back with that nota's dialog already showing.
func TestRekonsiliasiOpensTheDialogWithoutJavaScript(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())
	fileNota(t, client, testServer, notaFields())

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi?nota=NTA-20260807-0002")
	if !strings.Contains(page, `id="dialog-NTA-20260807-0002" aria-labelledby="title-NTA-20260807-0002" open>`) {
		t.Fatal("the requested dialog did not open")
	}
	if strings.Contains(page, `id="dialog-NTA-20260807-0001" aria-labelledby="title-NTA-20260807-0001" open>`) {
		t.Fatal("a dialog nobody asked for opened as well")
	}
	// A number that matches nothing leaves every dialog shut rather than
	// erroring: the row may simply have been settled by someone else.
	quiet := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi?nota=NTA-19990101-0001")
	// The open sidebar group also ends in " open>", so the check has to name
	// the dialogs rather than look for the attribute anywhere on the page.
	for _, notaID := range []string{"NTA-20260807-0001", "NTA-20260807-0002"} {
		if strings.Contains(quiet, `id="dialog-`+notaID+`" aria-labelledby="title-`+notaID+`" open>`) {
			t.Fatalf("an unknown nota opened the dialog for %s", notaID)
		}
	}
}

func TestRekonsiliasiSearchesByTransactionNumber(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())
	fileNota(t, client, testServer, notaFields())

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi?q=0002")
	if !strings.Contains(page, "NTA-20260807-0002") {
		t.Fatal("the search does not find the nota by a partial number")
	}
	if strings.Contains(page, "NTA-20260807-0001") {
		t.Fatal("the search returns notes it was not asked for")
	}

	empty := fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi?q=NTA-1999")
	if !strings.Contains(empty, "Tidak ada nota belum dibayar dengan nomor itu") {
		t.Fatal("a search that matches nothing does not say so")
	}
}

func TestRekonsiliasiSettlesTheNota(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())

	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi"))
	response := postSettlement(t, client, testServer, csrf, "NTA-20260807-0001", true)
	page := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, page)
	}

	nota := storedNota(t, store, "NTA-20260807-0001")
	if nota.StatusPembayaran != model.NotaStatusSudahDibayar {
		t.Fatalf("status = %q", nota.StatusPembayaran)
	}
	if nota.BuktiBayar == "" || nota.DibayarPada == nil || nota.DirekonsiliasiOleh == "" {
		t.Fatalf("the settlement left no audit trail: %+v", nota)
	}
	// Once paid it drops off the list, so nobody pays it a second time.
	if strings.Contains(page, `name="bukti_bayar"`) {
		t.Fatal("the settled nota is still offered for payment")
	}
	if !strings.Contains(page, "sudah dibayar") {
		t.Fatalf("the page does not confirm the payment: %s", page)
	}
}

// The proof is the point of the exercise, so the status cannot move without it.
func TestRekonsiliasiRefusesASettlementWithoutProof(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())

	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi"))
	response := postSettlement(t, client, testServer, csrf, "NTA-20260807-0001", false)
	page := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", response.StatusCode)
	}
	if !strings.Contains(page, "bukti bayar") {
		t.Fatalf("the error does not name the missing proof: %s", page)
	}
	// The dialog reopens on the nota that failed, so the message sits next to
	// the upload it is about.
	if !strings.Contains(page, `id="dialog-NTA-20260807-0001" aria-labelledby="title-NTA-20260807-0001" open>`) {
		t.Fatal("the dialog did not reopen on the nota that failed")
	}
	if storedNota(t, store, "NTA-20260807-0001").StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatal("the nota was settled without proof")
	}
}

// Paying the same nota twice would double the money leaving the company.
func TestRekonsiliasiRefusesASecondSettlement(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())

	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota/rekonsiliasi"))
	first := postSettlement(t, client, testServer, csrf, "NTA-20260807-0001", true)
	first.Body.Close()

	csrf = csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/nota"))
	second := postSettlement(t, client, testServer, csrf, "NTA-20260807-0001", true)
	page := readBody(t, second)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("status %d, want 409", second.StatusCode)
	}
	if !strings.Contains(page, "sudah ditandai dibayar") {
		t.Fatalf("the error does not explain the conflict: %s", page)
	}
}

func TestRekonsiliasiRejectsAMissingCSRFTokenAndAnonymousUsers(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	fileNota(t, client, testServer, notaFields())

	response := postSettlement(t, client, testServer, "", "NTA-20260807-0001", true)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", response.StatusCode)
	}
	if storedNota(t, store, "NTA-20260807-0001").StatusPembayaran != model.NotaStatusBelumDibayar {
		t.Fatal("a nota was settled without a CSRF token")
	}

	anonymous := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	page, err := anonymous.Get(testServer.URL + "/nota/rekonsiliasi")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	page.Body.Close()
	if location := page.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}
