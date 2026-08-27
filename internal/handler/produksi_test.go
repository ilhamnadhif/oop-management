package handler

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedUnit(t *testing.T, store *repository.TestRepository) {
	t.Helper()
	unit := &model.UnitDT{
		UnitID: "UNT-2026-0001", Nopol: "B 1234 ABC",
		Panjang: 375, Lebar: 190, Tinggi: 150,
		Driver: "Slamet", Keterangan: "DT KECIL",
	}
	if err := store.CreateUnitDT(context.Background(), unit); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
}

func validProduksiForm(csrf string) url.Values {
	return urlValues(map[string]string{
		"csrf_token": csrf,
		"tanggal":    "2026-08-07",
		"project":    "PCPM",
		"supplier":   "HPP",
		"quary":      "HS",
		"kategori":   "Replace",
		"lokasi":     "Blok A",
		"layer":      "L1",
		"nopol":      "B 1234 ABC",
		"tt":         "20",
	})
}

func TestProduksiFormRendersOptionsAndUnits(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")

	for _, fragment := range []string{
		`value="2026-08-07"`, // today is preselected
		// Creatable pickers: a text input backed by a datalist of what the
		// sheet already holds.
		`list="projectList"`, `list="supplierList"`, `list="quaryList"`,
		`list="kategoriList"`, `list="layerList"`, `list="lokasiList"`,
		`<option value="PCPM">`,
		`<option value="HS">`,
		`<option value="Replace">`,
		`<option value="L5">`,
		`name="lokasi"`,
		`id="unitList"`,
		// The unit's own data rides along on the option, so picking a plate
		// fills the read-only fields without another request.
		`data-driver="Slamet"`,
		`data-panjang="375"`,
		`data-jenis="DT KECIL"`,
		`data-calc="tf"`,
		`data-calc="deviasi"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("produksi form missing %q", fragment)
		}
	}

	// Nothing is preselected, and no picker is a closed <select> any more.
	if strings.Contains(page, `<select id="kategori"`) {
		t.Fatal("kategori is still a closed dropdown")
	}
	// Counted inside the entry form itself. The scan panel above it carries a
	// picker of its own, and counting the whole page would measure both.
	form := page[strings.Index(page, "data-produksi-form"):]
	if got := strings.Count(form, `placeholder="Pilih atau ketik…"`); got != 6 {
		t.Fatalf("expected 6 creatable pickers in the form, got %d", got)
	}
}

// A value typed into a picker that has never been used before must be saved.
func TestProduksiAcceptsNewPickerValues(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	form := validProduksiForm(csrf)
	form.Set("kategori", "Bongkar")
	form.Set("lokasi", "63+575")
	response, err := client.PostForm(testServer.URL+"/produksi", form)
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	response.Body.Close()

	rows := store.ProduksiList()
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	if rows[0].Kategori != "Bongkar" || rows[0].Lokasi != "63+575" {
		t.Fatalf("new picker values were not saved: %+v", rows[0])
	}
}

// Nopol is the one picker that must stay a closed set: the row it produces
// carries the unit's dimensions, so an unregistered plate has nothing to
// attach. The UI must not offer to create one, and the server must refuse it.
func TestProduksiNopolPickerIsNotCreatable(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")

	nopolTag := tagAt(t, page, `id="nopol"`)
	if !strings.Contains(nopolTag, "data-no-create") {
		t.Fatalf("nopol picker still offers to create values: %s", nopolTag)
	}
	if !strings.Contains(nopolTag, `data-empty-text=`) {
		t.Fatal("nopol picker has no message for an unregistered plate")
	}

	// Every other picker stays creatable.
	for _, id := range []string{"project", "supplier", "quary", "kategori", "layer", "lokasi"} {
		if strings.Contains(tagAt(t, page, `id="`+id+`"`), "data-no-create") {
			t.Fatalf("%s should still be creatable", id)
		}
	}

	// And the server is the real guard: it refuses a plate the register has
	// never seen, whatever the browser allowed.
	csrf := csrfFromForm(t, page)
	form := validProduksiForm(csrf)
	form.Set("nopol", "B 9999 ZZZ")
	response, err := client.PostForm(testServer.URL+"/produksi", form)
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if len(store.ProduksiList()) != 0 {
		t.Fatal("a row was stored for an unregistered plate")
	}
}

func TestProduksiSubmitStoresCalculatedRow(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	response, err := client.PostForm(testServer.URL+"/produksi", validProduksiForm(csrf))
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode, body)
	}

	rows := store.ProduksiList()
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	row := rows[0]
	// 375 x 190 x 160 / 10^6 = 11.4
	if row.TF != 160 || row.Volume != 11.4 || row.VolumeOPP != 10 || row.Deviasi != 1.4 {
		t.Fatalf("calculation wrong: %+v", row)
	}
	if row.Driver != "Slamet" || row.JenisDT != "DT KECIL" || row.UnitID != "UNT-2026-0001" {
		t.Fatalf("unit data not carried over: %+v", row)
	}
	if !strings.Contains(body, "PRD-2026-0001") {
		t.Fatal("success message does not name the row ID")
	}
}

// P/L/T are read-only on the form and disabled inputs are never posted, but a
// crafted request must not be able to inflate the volume either.
func TestProduksiIgnoresClientSuppliedDimensions(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	form := validProduksiForm(csrf)
	form.Set("panjang", "999")
	form.Set("lebar", "999")
	form.Set("tinggi", "999")
	form.Set("volume", "999999")
	form.Set("driver", "Orang Lain")

	response, err := client.PostForm(testServer.URL+"/produksi", form)
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	response.Body.Close()

	rows := store.ProduksiList()
	if len(rows) != 1 {
		t.Fatalf("stored %d rows, want 1", len(rows))
	}
	if rows[0].Panjang != 375 || rows[0].Volume != 11.4 || rows[0].Driver != "Slamet" {
		t.Fatalf("client values leaked into the row: %+v", rows[0])
	}
}

func TestProduksiRejectsUnknownNopol(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	form := validProduksiForm(csrf)
	form.Set("nopol", "B 9999 ZZZ")
	response, err := client.PostForm(testServer.URL+"/produksi", form)
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	body := readBody(t, response)

	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if !strings.Contains(body, "belum terdaftar") {
		t.Fatal("error message does not explain the unknown plate")
	}
	if len(store.ProduksiList()) != 0 {
		t.Fatal("row stored for an unregistered unit")
	}
}

func TestProduksiRequiresCSRF(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedUnit(t, store)
	client := loggedInClient(t, testServer)

	response, err := client.PostForm(testServer.URL+"/produksi", validProduksiForm("wrong-token"))
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
	if len(store.ProduksiList()) != 0 {
		t.Fatal("row stored without a valid CSRF token")
	}
}
