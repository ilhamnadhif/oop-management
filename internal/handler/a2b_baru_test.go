package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func postA2BBaru(t *testing.T, client *http.Client, testServer *httptest.Server, path string, fields map[string]string) (int, string) {
	t.Helper()
	form := url.Values{"csrf_token": {csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+path))}}
	for name, value := range fields {
		form.Set(name, value)
	}
	response, err := client.PostForm(testServer.URL+path, form)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

// The module is filled from its own pages and the overview adds it up: units,
// stock, fuel out, the order table with its deviation, and the ratio per unit.
func TestA2BBaruFillsAndAddsUp(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")

	for _, unit := range [][2]string{{"EXC-01", "Excavator PC200 Kobelco (Rent)"}, {"GSTOPP-01", "Genset 5 KVA"}} {
		if status, body := postA2BBaru(t, client, testServer, "/a2b-baru/unit", map[string]string{"id_unit": unit[0], "nama_unit": unit[1]}); status != http.StatusOK || !strings.Contains(body, "terdaftar") {
			t.Fatalf("unit %s: %d %s", unit[0], status, body)
		}
	}
	if status, body := postA2BBaru(t, client, testServer, "/a2b-baru/unit", map[string]string{"id_unit": "exc-01", "nama_unit": "Dobel"}); status != http.StatusUnprocessableEntity || !strings.Contains(body, "sudah terdaftar") {
		t.Fatalf("a second EXC-01 was not refused: %d", status)
	}

	status, body := postA2BBaru(t, client, testServer, "/a2b-baru/fuel-masuk", map[string]string{
		"phase": "PO-1", "tanggal": "2026-08-01", "no_po": "046/POR",
		"po_volume": "8000", "po_harga": "17928", "site_volume": "7900", "site_harga": "17928",
	})
	if status != http.StatusOK {
		t.Fatalf("fuel masuk: %d %s", status, body)
	}

	for _, fill := range []map[string]string{
		{"tanggal": "2026-08-02", "id_unit": "EXC-01", "hm_awal_fm": "100", "hm_akhir_fm": "220", "fuel_liter": "120", "hm_saat_isi": "22532,8", "jam_kerja": "8", "operator": "Langgeng"},
		{"tanggal": "2026-08-03", "id_unit": "GSTOPP-01", "fuel_liter": "13", "operator": "Nopal"},
	} {
		if status, body := postA2BBaru(t, client, testServer, "/a2b-baru/fuel-keluar", fill); status != http.StatusOK || !strings.Contains(body, "FUELOUT-20260807-") {
			t.Fatalf("fuel keluar %v: %d %s", fill, status, body)
		}
	}
	if status, body := postA2BBaru(t, client, testServer, "/a2b-baru/fuel-keluar", map[string]string{
		"tanggal": "2026-08-03", "id_unit": "BLD-09", "fuel_liter": "13", "operator": "Nopal"}); status != http.StatusUnprocessableEntity || !strings.Contains(body, "belum terdaftar") {
		t.Fatalf("an unknown unit was fuelled: %d", status)
	}

	fills, _ := store.ListA2BBaruFuelKeluar(t.Context())
	if len(fills) != 2 || fills[0].NamaUnit != "Excavator PC200 Kobelco (Rent)" || fills[0].HMSaatIsi == nil || *fills[0].HMSaatIsi != 22532.8 {
		t.Fatalf("stored fills = %+v", fills)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview")
	for _, want := range []string{
		"JUMLAH UNIT", "STOCK FUEL",
		// 7.900 received less 133 dispensed.
		"7.767", "133",
		// The order: 8.000 at 17.928 against 7.900 arrived, 100 short.
		"143.424.000", "141.631.200", "1.792.800", "98.75%",
		// EXC-01 burned 120 L over 8 hours; the genset has no ratio.
		"15", "Agustus 2026",
		"Langgeng",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("overview is missing %q", want)
		}
	}
}

// A position outside the module cannot open it.
func TestA2BBaruIsClosedToOtherPositions(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "HR")
	response, err := client.Get(testServer.URL + "/a2b-baru/overview")
	if err != nil {
		t.Fatalf("get overview: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("HR got %d, want 403", response.StatusCode)
	}
}
