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
		`<option value="PCPM"`,
		`<option value="HPP"`,
		`<option value="HS"`,
		`<option value="Replace"`,
		`<option value="Timbunan"`,
		`<option value="Akses"`,
		`<option value="L1"`,
		`<option value="L5"`,
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

	// Every dropdown starts on the placeholder rather than a real value.
	if strings.Count(page, `disabled>Pilih…</option>`) != 5 {
		t.Fatalf("expected 5 unselected dropdowns, got %d", strings.Count(page, `disabled>Pilih…</option>`))
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
