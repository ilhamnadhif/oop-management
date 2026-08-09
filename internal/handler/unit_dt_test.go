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
	if !strings.Contains(page, "/static/js/combobox.js") {
		t.Fatal("the combobox enhancer is not loaded")
	}
}

// The custom combobox enhances the native markup rather than replacing it, so
// the plain input and datalist must still be in the HTML: if the script fails
// to load the field still works.
func TestComboboxDegradesToNativeMarkup(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/produksi", "/unit-dt", "/unit-a2b"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		if !strings.Contains(page, "<datalist") {
			t.Fatalf("%s: no datalist left for the no-script case", path)
		}
		if !strings.Contains(page, "/static/js/combobox.js") {
			t.Fatalf("%s: the combobox enhancer is not loaded", path)
		}
	}

	body := fetchPage(t, testServer.URL+"/static/js/combobox.js")
	if len(body) == 0 {
		t.Fatal("combobox.js is empty")
	}
	// On a touch screen the on-screen keyboard covers half the form, and once a
	// choice is made there is nothing left to type, so the field gives up focus
	// there. A desktop keeps it, where focus is how someone tabs onwards.
	for _, behaviour := range []string{
		`matchMedia("(hover: none) and (pointer: coarse)")`,
		"if (touchScreen.matches) input.blur();",
	} {
		if !strings.Contains(body, behaviour) {
			t.Fatalf("combobox.js is missing %q", behaviour)
		}
	}

	// Keyboard support and the create row are the reason this exists at all.
	for _, fragment := range []string{"ArrowDown", "Escape", "aria-expanded", `Buat "`} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("combobox.js is missing %q", fragment)
		}
	}
	// Selecting a value dispatches an input event for other scripts. Without a
	// guard that event lands back on the combobox and reopens the list it just
	// closed.
	if !strings.Contains(body, "settingValue") {
		t.Fatal("combobox.js has no guard against its own dispatched input event")
	}
	// produksi.js reads the unit datalist to fill the dimensions, so the element
	// must survive the upgrade even though the attribute pointing at it does not.
	if strings.Contains(body, "datalist.remove()") {
		t.Fatal("combobox.js removes the datalist other scripts depend on")
	}
	// Closed-set fields opt out of the create row.
	if !strings.Contains(body, "data-no-create") {
		t.Fatal("combobox.js has no way to mark a picker as a closed set")
	}
}

// The production preview reads each unit's dimensions out of the datalist, so
// the element has to reach the browser and stay reachable by id.
func TestProduksiUnitDatalistSurvivesTheComboboxUpgrade(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")

	if !strings.Contains(page, `<datalist id="unitList">`) {
		t.Fatal("the unit datalist is missing from the page")
	}
	if !strings.Contains(page, `data-panjang="375"`) {
		t.Fatal("the unit datalist carries no dimensions for produksi.js to read")
	}
	// Load order matters: produksi.js runs after the enhancer.
	comboboxAt := strings.Index(page, "/static/js/combobox.js")
	produksiAt := strings.Index(page, "/static/js/produksi.js")
	if comboboxAt < 0 || produksiAt < 0 || produksiAt < comboboxAt {
		t.Fatal("produksi.js must load after combobox.js")
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
