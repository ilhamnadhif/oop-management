package handler

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func postUnitDT(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, withPhoto bool) *http.Response {
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

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/unit-dt", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post unit-dt: %v", err)
	}
	return response
}

// csrfFromBody reads the dashboard's data attribute; this page carries the
// token in a hidden field instead.
func csrfFromForm(t *testing.T, page string) string {
	t.Helper()
	matches := regexp.MustCompile(`name="csrf_token" value="([^"]+)"`).FindStringSubmatch(page)
	if len(matches) != 2 {
		t.Fatal("csrf field not found on the form")
	}
	return matches[1]
}

func validUnitFields() map[string]string {
	return map[string]string{
		"nopol":      "B 1234 ABC",
		"panjang":    "7.5",
		"lebar":      "2.4",
		"tinggi":     "1.8",
		"driver":     "Slamet",
		"keterangan": "DT KECIL",
	}
}

func TestUnitDTFormRendersFields(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/unit-dt")

	for _, fragment := range []string{
		`name="nopol"`, `name="panjang"`, `name="lebar"`, `name="tinggi"`,
		`name="driver"`, `name="keterangan"`, `name="foto_unit"`,
		`enctype="multipart/form-data"`,
		`placeholder="B 1234 ABC"`,
		// The generated ID is shown but never posted; the server assigns the
		// authoritative value.
		`id="unit_id"`,
		`value="UNT-2026-0001" disabled`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("unit dt form missing %q", fragment)
		}
	}
}

// The driver field is a creatable picker backed by names already registered.
func TestUnitDTDriverFieldIsCreatable(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/unit-dt")

	if !strings.Contains(page, `list="driverList"`) {
		t.Fatal("driver field is not backed by a datalist")
	}
	if !strings.Contains(page, `<option value="Slamet">`) {
		t.Fatal("driver suggestions do not include a registered name")
	}
}

func TestUnitDTSubmitStoresUnit(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/unit-dt")
	csrf := csrfFromForm(t, page)

	response := postUnitDT(t, client, testServer, csrf, validUnitFields(), true)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("submit status: %d body=%s", response.StatusCode, body)
	}
	if !strings.Contains(body, "berhasil disimpan") {
		t.Fatal("success message missing")
	}

	units := store.UnitDTList()
	if len(units) != 1 {
		t.Fatalf("stored %d units, want 1", len(units))
	}
	unit := units[0]
	if unit.UnitID != "UNT-2026-0001" {
		t.Fatalf("UnitID = %q, want UNT-2026-0001", unit.UnitID)
	}
	if !strings.Contains(body, "UNT-2026-0001") {
		t.Fatal("success message does not name the generated ID")
	}
	if unit.Nopol != "B 1234 ABC" || unit.Driver != "Slamet" {
		t.Fatalf("unexpected unit: %+v", unit)
	}
	if unit.Panjang != 7.5 || unit.Lebar != 2.4 || unit.Tinggi != 1.8 {
		t.Fatalf("unexpected dimensions: %+v", unit)
	}
	if !strings.HasPrefix(unit.Foto, "data:image/jpeg;base64,") {
		t.Fatalf("photo was not normalised: %.40s", unit.Foto)
	}
}

func TestUnitDTSubmitRejectsBadNopolAndKeepsInput(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-dt"))

	fields := validUnitFields()
	fields["nopol"] = "B1234ABC"
	response := postUnitDT(t, client, testServer, csrf, fields, true)
	body := readBody(t, response)

	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if len(store.UnitDTList()) != 0 {
		t.Fatal("invalid unit was stored")
	}
	// The rejected form must come back filled in; retyping everything after one
	// bad plate is what makes people give up.
	if !strings.Contains(body, `value="Slamet"`) {
		t.Fatal("driver was not preserved on error")
	}
}

func TestUnitDTSubmitRejectsDuplicateNopol(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-dt"))

	first := postUnitDT(t, client, testServer, csrf, validUnitFields(), true)
	first.Body.Close()

	duplicate := postUnitDT(t, client, testServer, csrf, validUnitFields(), true)
	body := readBody(t, duplicate)
	if duplicate.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", duplicate.StatusCode)
	}
	if !strings.Contains(body, "sudah terdaftar") {
		t.Fatal("duplicate message missing")
	}
	if len(store.UnitDTList()) != 1 {
		t.Fatalf("stored %d units, want 1", len(store.UnitDTList()))
	}
}

func TestUnitDTSubmitAcceptsMissingPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/unit-dt"))

	response := postUnitDT(t, client, testServer, csrf, validUnitFields(), false)
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode, body)
	}
	units := store.UnitDTList()
	if len(units) != 1 {
		t.Fatalf("stored %d units, want 1", len(units))
	}
	if units[0].Foto != "" {
		t.Fatalf("Foto = %.40q, want empty", units[0].Foto)
	}
}

func TestUnitDTKeteranganIsAClosedDropdown(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/unit-dt")

	for _, fragment := range []string{
		`<option value="DT KECIL" selected>DT KECIL</option>`,
		`<option value="DT BESAR" >DT BESAR</option>`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("keterangan dropdown missing %q", fragment)
		}
	}

	// The dropdown is a convenience; a direct POST must not store free text.
	csrf := csrfFromForm(t, page)
	fields := validUnitFields()
	fields["keterangan"] = "DT SEDANG"
	response := postUnitDT(t, client, testServer, csrf, fields, false)
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if len(store.UnitDTList()) != 0 {
		t.Fatal("unlisted keterangan was stored")
	}
}

// The multipart path validates CSRF from the parsed form, so it needs its own
// coverage; the shared ValidCSRF helper deliberately refuses that source.
func TestUnitDTSubmitRequiresCSRF(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	response := postUnitDT(t, client, testServer, "", validUnitFields(), true)
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}

	wrong := postUnitDT(t, client, testServer, "not-the-token", validUnitFields(), true)
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", wrong.StatusCode)
	}
	if len(store.UnitDTList()) != 0 {
		t.Fatal("unit stored without a valid CSRF token")
	}
}
