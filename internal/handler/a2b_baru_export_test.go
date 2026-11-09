package handler

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedA2BBaruBook(t *testing.T, store *repository.TestRepository) {
	t.Helper()
	_ = store.AppendA2BBaruUnits(t.Context(), []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}, {IDUnit: "BLD-01", NamaUnit: "Bulldozer"}})
	_ = store.AppendA2BBaruFuelMasuk(t.Context(), []model.A2BBaruFuelMasuk{
		{Phase: "PO-1", Tanggal: "2026-08-01", NoPO: "046/POR", POVolume: 8000, POHargaSatuan: 17928, SiteVolume: 8010, SiteHargaSatuan: 17928},
	})
	_ = store.AppendA2BBaruFuelKeluar(t.Context(), []model.A2BBaruFuelKeluar{
		{FuelOutID: "FO-EXC", Tanggal: "2026-08-02", IDUnit: "EXC-01", NamaUnit: "Excavator", FuelLiter: 120, JamKerja: 10, Operator: "Langgeng"},
		{FuelOutID: "FO-BLD", Tanggal: "2026-08-03", IDUnit: "BLD-01", NamaUnit: "Bulldozer", FuelLiter: 200, JamKerja: 10, Operator: "Yoga"},
	})
}

// The page offers each table alone and all three together, as a workbook and
// a PDF, carrying the range and units picked.
func TestA2BBaruExportPageOffersEachTable(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedA2BBaruBook(t, store)
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b-baru/export?unit=EXC-01&from=2026-08-01")
	for _, want := range []string{
		"1 PO · 1 pengisian · 120 L keluar",
		"/a2b-baru/export/download?format=xlsx&amp;from=2026-08-01&amp;part=ratio&amp;unit=EXC-01",
		"/a2b-baru/export/download?format=pdf&amp;from=2026-08-01&amp;unit=EXC-01",
		`name="unit" value="EXC-01" checked`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the export page is missing %q", want)
		}
	}
}

// Both formats download, every table is in the workbook, and the unit filter
// reaches the dispenses.
func TestA2BBaruExportDownloads(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedA2BBaruBook(t, store)
	client := loggedInClientAs(t, testServer, "Logistik")

	for format, magic := range map[string][]byte{"xlsx": []byte("PK"), "pdf": []byte("%PDF-")} {
		response := downloadProduksi(t, client, testServer.URL+"/a2b-baru/export/download?format="+format+"&unit=BLD-01")
		body := readBodyBytes(t, response)
		if response.StatusCode != http.StatusOK || !bytes.HasPrefix(body, magic) {
			t.Fatalf("%s: status %d", format, response.StatusCode)
		}
		if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "fuel-a2b-baru-BLD-01-semua."+format) {
			t.Fatalf("%s: content disposition %q", format, got)
		}
		if format != "xlsx" {
			continue
		}
		book, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("open workbook: %v", err)
		}
		sheets := book.GetSheetList()
		if strings.Join(sheets, "|") != "Fuel Masuk|Ratio Fuel per Unit|Fuel Keluar & HM" {
			t.Fatalf("sheets = %v", sheets)
		}
		rows, _ := book.GetRows("Fuel Keluar & HM")
		flat := ""
		for _, row := range rows {
			flat += strings.Join(row, "|") + "\n"
		}
		if !strings.Contains(flat, "FO-BLD") || strings.Contains(flat, "FO-EXC") {
			t.Fatalf("the unit filter did not reach the dispenses:\n%s", flat)
		}
		orders, _ := book.GetRows("Fuel Masuk")
		ordersFlat := ""
		for _, row := range orders {
			ordersFlat += strings.Join(row, "|") + "\n"
		}
		if !strings.Contains(ordersFlat, "046/POR") || !strings.Contains(ordersFlat, "TOTAL") {
			t.Fatalf("the orders sheet is wrong:\n%s", ordersFlat)
		}
	}

	one := downloadProduksi(t, client, testServer.URL+"/a2b-baru/export/download?format=xlsx&part=ratio")
	body := readBodyBytes(t, one)
	book, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	if sheets := book.GetSheetList(); len(sheets) != 1 || sheets[0] != "Ratio Fuel per Unit" {
		t.Fatalf("one part gave sheets %v", sheets)
	}
}

// The report answers to its own project setting, at the address as well as
// the button.
func TestSwitchedOffA2BBaruExportRefusesItsDownload(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedA2BBaruBook(t, store)
	client := loggedInClient(t, testServer)
	response := saveExportConfig(t, client, testServer, store.ProjectList()[0].ProjectID, testProjectName,
		map[string]string{"export_key": string(model.ExportA2BBaru), "ttd_count": "1"})
	response.Body.Close()

	download, err := client.Get(testServer.URL + "/a2b-baru/export/download?format=xlsx")
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	download.Body.Close()
	if download.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", download.StatusCode)
	}
}
