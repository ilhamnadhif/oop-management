package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postUnitA2B(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, withPhoto bool) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		_ = writer.WriteField("csrf_token", csrf)
	}
	for name, value := range fields {
		_ = writer.WriteField(name, value)
	}
	if withPhoto {
		part, err := writer.CreateFormFile("foto_unit", "unit.jpg")
		if err != nil {
			t.Fatalf("create form file: %v", err)
		}
		if _, err := part.Write(testJPEG(t)); err != nil {
			t.Fatalf("write photo: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/unit-a2b", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post unit-a2b: %v", err)
	}
	return response
}

func validUnitA2BFields() map[string]string {
	return map[string]string{
		"tanggal":      "2026-08-07",
		"id_unit":      "EXCA-01",
		"nama_unit":    "Excavator",
		"merek_type":   "Komatsu PC200",
		"fuel_storage": "400",
		"fr_unit":      "8.5",
		"lokasi":       "Blok A",
		"hm_awal":      "1200.5",
	}
}

func TestUnitA2BFormRendersEverySection(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/unit-a2b")

	for _, fragment := range []string{
		"1. INFORMASI REGISTRASI",
		"2. SPESIFIKASI &amp; OPERASIONAL UNIT",
		"3. UPLOAD FOTO UNIT",
		`name="tanggal"`, `name="id_unit"`, `name="nama_unit"`, `name="merek_type"`,
		`name="fuel_storage"`, `name="fr_unit"`, `name="lokasi"`, `name="hm_awal"`,
		`name="foto_unit"`,
		`enctype="multipart/form-data"`,
		// The running number is shown but never posted; the server assigns it.
		`id="no_urut"`,
		`value="1" disabled`,
		`value="2026-08-07"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("unit a2b form missing %q", fragment)
		}
	}
}

func TestUnitA2BSubmitStoresUnit(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"))

	response := postUnitA2B(t, client, testServer, csrf, validUnitA2BFields(), true)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode, body)
	}
	if !strings.Contains(body, "tersimpan") {
		t.Fatal("success message missing")
	}

	units := store.UnitA2BList()
	if len(units) != 1 {
		t.Fatalf("stored %d units, want 1", len(units))
	}
	unit := units[0]
	if unit.NoUrut != 1 || unit.IDUnit != "EXCA-01" || unit.Lokasi != "Blok A" {
		t.Fatalf("unexpected unit: %+v", unit)
	}
	if unit.FuelStorage != 400 || unit.FRUnit != 8.5 || unit.HMAwal != 1200.5 {
		t.Fatalf("numeric fields wrong: %+v", unit)
	}
	if !strings.HasPrefix(unit.Foto, "data:image/jpeg;base64,") {
		t.Fatalf("photo was not normalised: %.40s", unit.Foto)
	}
}

func TestUnitA2BSubmitAcceptsMissingPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"))

	response := postUnitA2B(t, client, testServer, csrf, validUnitA2BFields(), false)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	units := store.UnitA2BList()
	if len(units) != 1 || units[0].Foto != "" {
		t.Fatalf("unexpected units: %+v", units)
	}
}

func TestUnitA2BSubmitRejectsDuplicateID(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"))

	first := postUnitA2B(t, client, testServer, csrf, validUnitA2BFields(), false)
	first.Body.Close()

	duplicate := postUnitA2B(t, client, testServer, csrf, validUnitA2BFields(), false)
	body := readBody(t, duplicate)
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", duplicate.StatusCode)
	}
	if !strings.Contains(body, "sudah terdaftar") {
		t.Fatal("duplicate message missing")
	}
	if len(store.UnitA2BList()) != 1 {
		t.Fatalf("stored %d units, want 1", len(store.UnitA2BList()))
	}
}

func TestUnitA2BSubmitRejectsNonNumericFR(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"))

	fields := validUnitA2BFields()
	fields["fr_unit"] = "FR-01"
	response := postUnitA2B(t, client, testServer, csrf, fields, false)
	body := readBody(t, response)

	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if len(store.UnitA2BList()) != 0 {
		t.Fatal("invalid unit was stored")
	}
	// The rejected form must come back filled in.
	if !strings.Contains(body, `value="Excavator"`) {
		t.Fatal("typed values were not preserved on error")
	}
}

func TestUnitA2BSubmitRequiresCSRF(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	for _, token := range []string{"", "not-the-token"} {
		response := postUnitA2B(t, client, testServer, token, validUnitA2BFields(), false)
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("token %q: status = %d, want 403", token, response.StatusCode)
		}
	}
	if len(store.UnitA2BList()) != 0 {
		t.Fatal("unit stored without a valid CSRF token")
	}
}

func TestUnitA2BRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(testServer.URL + "/unit-a2b")
	if err != nil {
		t.Fatalf("get unit-a2b: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}

func TestUnitA2BHasItsOwnMenuEntry(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/unit-a2b"))

	if !strings.Contains(nav, `href="/unit-a2b"`) || !strings.Contains(nav, ">Unit A2B<") {
		t.Fatal("sidebar is missing the Unit A2B entry")
	}
	if strings.Index(nav, ">Absensi<") > strings.Index(nav, ">Unit A2B<") {
		t.Fatal("Absensi is no longer first")
	}
}
