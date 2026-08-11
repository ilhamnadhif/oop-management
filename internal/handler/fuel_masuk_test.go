package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/service"
)

func fuelFields() map[string]string {
	return map[string]string{
		"tanggal_input": "2026-08-07T09:30",
		"vendor":        "PT Sumber Energi",
		"driver":        "Slamet",
		"nopol":         "B 1234 ABC",
		"jumlah_liter":  "8010",
		"keterangan":    model.FuelKeteranganSesuai,
	}
}

// postFuelMasuk submits the delivery form. skipPhotos names the evidence fields
// left out, so a test can check what happens when one is missing.
func postFuelMasuk(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, image []byte, skipPhotos ...string) *http.Response {
	t.Helper()
	skipped := make(map[string]bool, len(skipPhotos))
	for _, field := range skipPhotos {
		skipped[field] = true
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		if err := writer.WriteField("csrf_token", csrf); err != nil {
			t.Fatalf("write csrf: %v", err)
		}
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if image != nil {
		for _, kind := range service.FuelPhotoKinds {
			if skipped[kind.Field] {
				continue
			}
			part, err := writer.CreateFormFile(kind.Field, kind.Field+".jpg")
			if err != nil {
				t.Fatalf("create %s: %v", kind.Field, err)
			}
			if _, err := part.Write(image); err != nil {
				t.Fatalf("write %s: %v", kind.Field, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fuel form: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/a2b/fuel-masuk", &body)
	if err != nil {
		t.Fatalf("create fuel request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post fuel masuk: %v", err)
	}
	return response
}

func fuelFormCSRF(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	return csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-masuk"))
}

func requireFuelResponse(t *testing.T, response *http.Response, status int, contains ...string) string {
	t.Helper()
	body := readBody(t, response)
	if response.StatusCode != status {
		t.Fatalf("fuel response status = %d, want %d; body=%s", response.StatusCode, status, body)
	}
	for _, fragment := range contains {
		if !strings.Contains(body, fragment) {
			t.Fatalf("fuel response missing %q: %s", fragment, body)
		}
	}
	return body
}

// A delivery is stored with the four photos it was witnessed by, waiting for
// somebody to sign it off.
func TestFuelMasukIsStoredWaitingForApproval(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")
	image := testJPEG(t)

	response := postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fuelFields(), image)
	requireFuelResponse(t, response, http.StatusOK, "FUEL-20260807-0001", "menunggu approval")

	rows := store.FuelMasukList()
	if len(rows) != 1 {
		t.Fatalf("stored %d deliveries, want 1", len(rows))
	}
	stored := rows[0]
	if stored.FuelID != "FUEL-20260807-0001" {
		t.Fatalf("transaction number = %q", stored.FuelID)
	}
	if stored.StatusApproval != model.FuelStatusMenunggu {
		t.Fatalf("status = %q, want %q", stored.StatusApproval, model.FuelStatusMenunggu)
	}
	if stored.TanggalInput.Format("2006-01-02 15:04") != "2026-08-07 09:30" {
		t.Fatalf("input time = %s", stored.TanggalInput)
	}
	if stored.Vendor != "PT Sumber Energi" || stored.Driver != "Slamet" || stored.Nopol != "B 1234 ABC" {
		t.Fatalf("delivery fields were not stored: %+v", stored)
	}
	if stored.JumlahLiter != 8010 {
		t.Fatalf("litres = %v, want 8010", stored.JumlahLiter)
	}
	for name, value := range map[string]string{
		"truck":          stored.FotoTruckDepan,
		"tangki sebelum": stored.FotoTangkiSebelum,
		"flowmeter":      stored.FotoFlowmeter,
		"tangki setelah": stored.FotoTangkiSetelah,
	} {
		if value == "" {
			t.Fatalf("photo %s was not stored", name)
		}
	}
	if stored.CreatedBy == "" || stored.CreatedByID == "" {
		t.Fatalf("the delivery has nobody's name against it: %+v", stored)
	}
}

// The photos are the whole reason the delivery is recorded, so one missing is a
// refusal rather than a row with a hole in it.
func TestFuelMasukRefusesAMissingPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer),
		fuelFields(), testJPEG(t), "foto_flowmeter")
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "foto flowmeter wajib dilampirkan")

	if rows := store.FuelMasukList(); len(rows) != 0 {
		t.Fatalf("a delivery without evidence was stored: %+v", rows)
	}
}

// The shortfall and the keterangan cannot contradict each other: a mismatch
// needs a number, and a matching delivery stores a zero whatever was typed.
func TestFuelMasukKeepsKeteranganAndShortfallInStep(t *testing.T) {
	t.Run("mismatch needs a number", func(t *testing.T) {
		testServer, store := newTestServerWithStore(t)
		client := loggedInClientAs(t, testServer, "Logistik")
		fields := fuelFields()
		fields["keterangan"] = model.FuelKeteranganTidakSesuai

		response := postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fields, testJPEG(t))
		requireFuelResponse(t, response, http.StatusUnprocessableEntity, "Liter tidak sesuai")
		if rows := store.FuelMasukList(); len(rows) != 0 {
			t.Fatalf("a mismatch with no shortfall was stored: %+v", rows)
		}
	})

	t.Run("mismatch stores the shortfall", func(t *testing.T) {
		testServer, store := newTestServerWithStore(t)
		client := loggedInClientAs(t, testServer, "Logistik")
		fields := fuelFields()
		fields["keterangan"] = model.FuelKeteranganTidakSesuai
		fields["liter_tidak_sesuai"] = "150"

		response := postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fields, testJPEG(t))
		requireFuelResponse(t, response, http.StatusOK, "menunggu approval")
		rows := store.FuelMasukList()
		if len(rows) != 1 || rows[0].LiterTidakSesuai != 150 {
			t.Fatalf("shortfall was not stored: %+v", rows)
		}
	})

	t.Run("a matching delivery stores zero", func(t *testing.T) {
		testServer, store := newTestServerWithStore(t)
		client := loggedInClientAs(t, testServer, "Logistik")
		fields := fuelFields()
		fields["liter_tidak_sesuai"] = "150"

		response := postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fields, testJPEG(t))
		requireFuelResponse(t, response, http.StatusOK, "menunggu approval")
		rows := store.FuelMasukList()
		if len(rows) != 1 || rows[0].LiterTidakSesuai != 0 {
			t.Fatalf("a matching delivery kept a shortfall: %+v", rows)
		}
	})
}

// Recording a delivery and signing it off are different jobs: everyone in the
// A2B menu records, only Logistik and Management decide.
func TestFuelMasukApprovalIsNarrowerThanInput(t *testing.T) {
	cases := map[string]bool{
		"Surveyor":   false,
		"Produksi":   false,
		"SPV":        false,
		"Logistik":   true,
		"Management": true,
	}
	for jabatan, mayApprove := range cases {
		t.Run(jabatan, func(t *testing.T) {
			testServer := newTestServer(t)
			client := loggedInClientAs(t, testServer, jabatan)

			if status := statusOf(t, client, testServer.URL+"/a2b/fuel-masuk"); status != http.StatusOK {
				t.Fatalf("%s cannot open the input page: %d", jabatan, status)
			}
			status := statusOf(t, client, testServer.URL+"/a2b/fuel-masuk/approval")
			nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-masuk"))
			shown := strings.Contains(nav, `href="/a2b/fuel-masuk/approval"`)
			if mayApprove {
				if status != http.StatusOK {
					t.Fatalf("%s should reach the approval page, got %d", jabatan, status)
				}
				if !shown {
					t.Fatalf("%s cannot see the approval menu", jabatan)
				}
				return
			}
			if status != http.StatusForbidden {
				t.Fatalf("%s should be refused the approval page, got %d", jabatan, status)
			}
			if shown {
				t.Fatalf("%s is invited into a page that will turn them away", jabatan)
			}
		})
	}
}

// A decision is recorded once, with the deciding person's name against it.
func TestFuelMasukApprovalRecordsOneDecision(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")
	postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fuelFields(), testJPEG(t)).Body.Close()

	approvalPage := fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-masuk/approval")
	if !strings.Contains(approvalPage, "FUEL-20260807-0001") {
		t.Fatalf("the waiting delivery is missing from the queue: %s", approvalPage)
	}
	csrf := csrfFromForm(t, approvalPage)

	response, err := client.PostForm(testServer.URL+"/a2b/fuel-masuk/approval", urlValues(map[string]string{
		"csrf_token":       csrf,
		"fuel_id":          "FUEL-20260807-0001",
		"decision":         "approve",
		"catatan_approval": "Segel utuh",
	}))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	requireFuelResponse(t, response, http.StatusOK, "berhasil ditandai disetujui")

	stored := store.FuelMasukList()[0]
	if stored.StatusApproval != model.FuelStatusDisetujui {
		t.Fatalf("status = %q", stored.StatusApproval)
	}
	if stored.DiprosesOleh == "" || stored.DiprosesPada == nil {
		t.Fatalf("the decision has nobody's name or time against it: %+v", stored)
	}
	if stored.CatatanApproval != "Segel utuh" {
		t.Fatalf("note = %q", stored.CatatanApproval)
	}

	// A second decision on the same delivery is refused rather than silently
	// overwriting the first.
	second, err := client.PostForm(testServer.URL+"/a2b/fuel-masuk/approval", urlValues(map[string]string{
		"csrf_token":       csrf,
		"fuel_id":          "FUEL-20260807-0001",
		"decision":         "reject",
		"catatan_approval": "Berubah pikiran",
	}))
	if err != nil {
		t.Fatalf("second decision: %v", err)
	}
	requireFuelResponse(t, second, http.StatusConflict, "sudah diproses")
	if store.FuelMasukList()[0].StatusApproval != model.FuelStatusDisetujui {
		t.Fatal("the second decision overwrote the first")
	}
}

// A rejection without a reason is not a decision anyone can act on.
func TestFuelMasukRejectionNeedsANote(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")
	postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fuelFields(), testJPEG(t)).Body.Close()
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-masuk/approval"))

	response, err := client.PostForm(testServer.URL+"/a2b/fuel-masuk/approval", urlValues(map[string]string{
		"csrf_token": csrf,
		"fuel_id":    "FUEL-20260807-0001",
		"decision":   "reject",
	}))
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "catatan wajib diisi saat menolak")
	if store.FuelMasukList()[0].StatusApproval != model.FuelStatusMenunggu {
		t.Fatal("a rejection with no reason was recorded")
	}
}

// The evidence is served as an image to anyone who may see the delivery, and
// nothing is served for a photo that does not exist.
func TestFuelMasukPhotoIsServedByTransactionAndKind(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClientAs(t, testServer, "Logistik")
	postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fuelFields(), testJPEG(t)).Body.Close()

	response, err := client.Get(testServer.URL + "/a2b/fuel-masuk/foto?fuel_id=FUEL-20260807-0001&foto=flowmeter")
	if err != nil {
		t.Fatalf("get photo: %v", err)
	}
	payload := readBodyBytes(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("photo status = %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q", got)
	}
	if len(payload) == 0 {
		t.Fatal("the photo came back empty")
	}

	for _, query := range []string{
		"?fuel_id=FUEL-20260807-0001&foto=tidak-ada",
		"?fuel_id=FUEL-20260807-9999&foto=flowmeter",
	} {
		if status := statusOf(t, client, testServer.URL+"/a2b/fuel-masuk/foto"+query); status != http.StatusNotFound {
			t.Fatalf("%s returned %d, want 404", query, status)
		}
	}
}

// Anonymous callers are sent to the login page rather than shown the queue.
func TestFuelMasukPagesRedirectAnonymousUsersToLogin(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/a2b/fuel-masuk", "/a2b/fuel-masuk/approval", "/a2b/fuel-masuk/foto"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
			t.Fatalf("%s returned %d for an anonymous caller", path, response.StatusCode)
		}
	}
}
