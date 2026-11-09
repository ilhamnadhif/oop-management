package handler

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"opp-management/internal/model"
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

// postA2BBaruFill sends the fuel keluar form as a browser does, with a photo
// unless image is nil.
func postA2BBaruFill(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string, image []byte) (int, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fields["csrf_token"] = csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/fuel-keluar"))
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if image != nil {
		part, err := writer.CreateFormFile("foto_flow_meter", "fm.jpg")
		if err != nil {
			t.Fatalf("create photo: %v", err)
		}
		_, _ = part.Write(image)
	}
	_ = writer.Close()
	response, err := client.Post(testServer.URL+"/a2b-baru/fuel-keluar", writer.FormDataContentType(), &body)
	if err != nil {
		t.Fatalf("post fuel keluar: %v", err)
	}
	defer response.Body.Close()
	page, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(page)
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

	// The litres are the distance between the two flow meter readings.
	for _, fill := range []map[string]string{
		{"tanggal": "2026-08-02", "id_unit": "EXC-01", "hm_awal_fm": "100", "hm_akhir_fm": "220", "hm_saat_isi": "22532,8", "jam_kerja": "8", "operator": "Langgeng"},
		{"tanggal": "2026-08-03", "id_unit": "GSTOPP-01", "hm_awal_fm": "220", "hm_akhir_fm": "233", "operator": "Nopal"},
	} {
		if status, body := postA2BBaruFill(t, client, testServer, fill, testJPEG(t)); status != http.StatusOK || !strings.Contains(body, "FUELOUT-20260807-") {
			t.Fatalf("fuel keluar %v: %d %s", fill, status, body)
		}
	}
	for name, refused := range map[string]struct {
		fields map[string]string
		image  []byte
		want   string
	}{
		"unknown unit":   {map[string]string{"tanggal": "2026-08-03", "id_unit": "BLD-09", "hm_awal_fm": "233", "hm_akhir_fm": "240", "operator": "Nopal"}, testJPEG(t), "belum terdaftar"},
		"no photo":       {map[string]string{"tanggal": "2026-08-03", "id_unit": "EXC-01", "hm_awal_fm": "233", "hm_akhir_fm": "240", "operator": "Nopal"}, nil, "foto akhir flow meter wajib"},
		"meter backward": {map[string]string{"tanggal": "2026-08-03", "id_unit": "EXC-01", "hm_awal_fm": "233", "hm_akhir_fm": "230", "operator": "Nopal"}, testJPEG(t), "harus lebih besar"},
		"hm went back":   {map[string]string{"tanggal": "2026-08-03", "id_unit": "EXC-01", "hm_awal_fm": "233", "hm_akhir_fm": "240", "hm_saat_isi": "22000", "operator": "Nopal"}, testJPEG(t), "lebih kecil dari pengisian terakhir"},
	} {
		if status, body := postA2BBaruFill(t, client, testServer, refused.fields, refused.image); status != http.StatusUnprocessableEntity || !strings.Contains(body, refused.want) {
			t.Fatalf("%s was not refused: %d", name, status)
		}
	}
	// A meter that leapt further than the day allows is saved with a caution.
	if status, body := postA2BBaruFill(t, client, testServer, map[string]string{
		"tanggal": "2026-08-03", "id_unit": "EXC-01", "hm_awal_fm": "233", "hm_akhir_fm": "240", "hm_saat_isi": "25000", "operator": "langgeng"}, testJPEG(t)); status != http.StatusOK || !strings.Contains(body, "kelebihan digit") {
		t.Fatalf("a leap was not cautioned: %d", status)
	}

	fills, _ := store.ListA2BBaruFuelKeluar(t.Context())
	if len(fills) != 3 || fills[0].NamaUnit != "Excavator PC200 Kobelco (Rent)" || fills[0].FuelLiter != 120 ||
		fills[0].HMSaatIsi == nil || *fills[0].HMSaatIsi != 22532.8 || !fills[0].HasFoto || fills[1].FuelLiter != 13 {
		t.Fatalf("stored fills = %+v", fills)
	}
	// The operator typed in another case is filed under the name already used.
	if fills[2].Operator != "Langgeng" {
		t.Fatalf("operator = %q", fills[2].Operator)
	}
	photoResponse, err := client.Get(testServer.URL + "/a2b-baru/fuel-keluar/foto?fuel_id=" + fills[0].FuelOutID)
	if err != nil {
		t.Fatalf("get photo: %v", err)
	}
	photoResponse.Body.Close()
	if photoResponse.StatusCode != http.StatusOK || photoResponse.Header.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("photo answered %d %s", photoResponse.StatusCode, photoResponse.Header.Get("Content-Type"))
	}
	// A row without a photo shows the icon but no link to follow.
	if err := store.AppendA2BBaruFuelKeluar(t.Context(), []model.A2BBaruFuelKeluar{{FuelOutID: "LAMA-0001", Tanggal: "2026-06-02", IDUnit: "EXC-01", FuelLiter: 10}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	history := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/fuel-keluar")
	if !strings.Contains(history, `class="photo-none"`) || strings.Contains(history, "fuel_id=LAMA-0001") {
		t.Fatal("a row without a photo is not shown as an inert icon")
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview")
	for _, want := range []string{
		"JUMLAH UNIT", "STOCK FUEL",
		// 7.900 received less 150 dispensed, the photo-less row included.
		"7.750", "150",
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

// A table's filter narrows that table alone, and its form carries the other
// tables' filters along so applying it does not clear them.
func TestA2BBaruOverviewFiltersOneTableAtATime(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	_ = store.AppendA2BBaruUnits(t.Context(), []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}, {IDUnit: "BLD-01", NamaUnit: "Bulldozer"}})
	_ = store.AppendA2BBaruFuelKeluar(t.Context(), []model.A2BBaruFuelKeluar{
		{FuelOutID: "FO-EXC", Tanggal: "2026-08-01", IDUnit: "EXC-01", FuelLiter: 100, JamKerja: 10},
		{FuelOutID: "FO-BLD", Tanggal: "2026-08-02", IDUnit: "BLD-01", FuelLiter: 200, JamKerja: 10},
		{FuelOutID: "FO-EXC-SEP", Tanggal: "2026-09-01", IDUnit: "EXC-01", FuelLiter: 90, JamKerja: 10},
	})
	client := loggedInClientAs(t, testServer, "Logistik")
	page := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_unit=BLD-01&fk_unit=WT-01&ratio_from=2026-08-02")
	if strings.Contains(page, "FO-EXC") || !strings.Contains(page, "FO-BLD") {
		t.Fatal("the fuel keluar table was not narrowed to BLD-01")
	}
	// BLD-01 worked in August only, so its September cell has nothing to
	// show: it reads as a dash, not a blank.
	monthly := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview")
	if strings.Contains(monthly, `<td class="numeric"></td>`) || !strings.Contains(monthly, `<td class="numeric">-</td>`) {
		t.Fatal("a month without a ratio is left blank")
	}
	if !strings.Contains(page, `<input type="hidden" name="ratio_from" value="2026-08-02">`) ||
		!strings.Contains(page, `<input type="hidden" name="fk_unit" value="BLD-01">`) ||
		!strings.Contains(page, `<input type="hidden" name="fk_unit" value="WT-01">`) {
		t.Fatal("a table's form does not carry the other tables' filters")
	}
	if !strings.Contains(page, `href="/a2b-baru/overview?ratio_from=2026-08-02#fuel-keluar"`) {
		t.Fatal("resetting one table clears the others")
	}
	// The unit filter is the searchable list of ticks, with the picked ones
	// ticked.
	if !strings.Contains(page, `data-unit-picker`) || !strings.Contains(page, `name="fk_unit" value="BLD-01" checked`) {
		t.Fatal("the unit filter is not the multi-select picker")
	}
	both := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_unit=BLD-01&fk_unit=EXC-01")
	if !strings.Contains(both, "FO-EXC") || !strings.Contains(both, "FO-BLD") {
		t.Fatal("two units picked do not both show")
	}
	empty := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_from=2027-01-01")
	if !strings.Contains(empty, "Tidak ada data untuk filter ini.") {
		t.Fatal("an empty filtered table says nothing was ever recorded")
	}
}

// The fuel keluar list on the overview is paged, ten rows a page unless the
// reader picks 25, 50 or 100; the total is still every row, and filtering
// another table keeps the page.
func TestA2BBaruOverviewPagesTheFuelKeluarList(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	_ = store.AppendA2BBaruUnits(t.Context(), []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}})
	var fills []model.A2BBaruFuelKeluar
	for day := 1; day <= 25; day++ {
		fills = append(fills, model.A2BBaruFuelKeluar{
			FuelOutID: fmt.Sprintf("FO-%02d", day), Tanggal: fmt.Sprintf("2026-08-%02d", day), IDUnit: "EXC-01", FuelLiter: 10,
		})
	}
	_ = store.AppendA2BBaruFuelKeluar(t.Context(), fills)
	client := loggedInClientAs(t, testServer, "Logistik")
	count := func(page string) int { return strings.Count(page, ">FO-") }

	first := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview")
	if count(first) != 10 || !strings.Contains(first, ">FO-25<") || strings.Contains(first, ">FO-15<") {
		t.Fatalf("the first page holds %d rows", count(first))
	}
	if !strings.Contains(first, "Menampilkan 1–10 dari 25") || !strings.Contains(first, "250") {
		t.Fatal("the bar or the total is wrong on the first page")
	}
	if !strings.Contains(first, `href="/a2b-baru/overview?fk_page=2#fuel-keluar"`) {
		t.Fatal("there is no way to the next page")
	}

	last := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_page=3")
	if count(last) != 5 || !strings.Contains(last, ">FO-01<") || !strings.Contains(last, "Menampilkan 21–25 dari 25") {
		t.Fatalf("the last page holds %d rows", count(last))
	}
	// Another table's filter form carries the page along.
	if !strings.Contains(last, `<input type="hidden" name="fk_page" value="3">`) {
		t.Fatal("filtering another table would lose the page")
	}
	beyond := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_page=9")
	if count(beyond) != 5 {
		t.Fatal("a page past the end does not show the last one")
	}
	// The sizes are links that start again from the first page.
	if !strings.Contains(last, `href="/a2b-baru/overview?fk_size=25#fuel-keluar"`) {
		t.Fatal("there is no link to 25 rows a page")
	}
	all := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_size=25")
	if count(all) != 25 || strings.Contains(all, "fk_page=2") {
		t.Fatalf("25 a page shows %d rows", count(all))
	}
	odd := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/overview?fk_size=7")
	if count(odd) != 10 {
		t.Fatal("a size nobody offers was taken")
	}
}
