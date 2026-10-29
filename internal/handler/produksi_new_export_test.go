package handler

import (
	"bytes"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/model"
	"opp-management/internal/widya"
)

func getBody(t *testing.T, client *http.Client, address string) (*http.Response, []byte) {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatalf("get %s: %v", address, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", address, err)
	}
	return response, body
}

// The export page opens on today and every vendor, offers the vendors the
// tickets carry, and links both downloads with the range it shows.
func TestProduksiNewExportPageDefaultsToTodayAndEveryVendor(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/export")
	for _, fragment := range []string{
		`name="from" type="date" value="2026-10-24"`, `name="to" type="date" value="2026-10-24"`,
		`<span data-unit-picker-summary>Semua vendor</span>`, `data-picker-all="Semua vendor"`,
		`<input type="checkbox" name="vendor" value="HPP">`, `<input type="checkbox" name="vendor" value="MNP">`,
		"3 rit · 67.50 m³ · 2 unit",
		`href="/produksi-new/export/download?format=xlsx&amp;from=2026-10-24&amp;to=2026-10-24"`,
		`href="/produksi-new/export/download?format=pdf&amp;from=2026-10-24&amp;to=2026-10-24"`,
		// Each table can be taken alone too.
		`href="/produksi-new/export/download?format=xlsx&amp;from=2026-10-24&amp;part=summary&amp;to=2026-10-24"`,
		`href="/produksi-new/export/download?format=pdf&amp;from=2026-10-24&amp;part=rit&amp;to=2026-10-24"`,
		"Rekap Vendor &amp; Armada", "Detail Unit per Vendor", "Semua bagian",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the export page is missing %q", fragment)
		}
	}
	if !strings.Contains(page, `href="/produksi-new/export"`) {
		t.Fatal("the sidebar does not link the export")
	}
}

// A part asked for is the only table in the file, and the file is named for
// it.
func TestProduksiNewExportHandsOutOneTable(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	for _, part := range produksiNewParts {
		response, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx&part="+part.Key)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: status %d: %s", part.Key, response.StatusCode, body)
		}
		if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "ritase-widya-"+part.Key+"-semua-vendor-2026-10-24.xlsx") {
			t.Fatalf("%s: disposition = %q", part.Key, got)
		}
		file, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s: open workbook: %v", part.Key, err)
		}
		if sheets := file.GetSheetList(); len(sheets) != 1 || sheets[0] != part.Label {
			t.Fatalf("%s: sheets = %v", part.Key, sheets)
		}
		file.Close()

		response, body = getBody(t, client, testServer.URL+"/produksi-new/export/download?format=pdf&part="+part.Key)
		if response.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("%PDF")) {
			t.Fatalf("%s: pdf status %d", part.Key, response.StatusCode)
		}
	}
}

// The workbook has a sheet per table, with the columns the overview shows.
func TestProduksiNewExportWritesASheetPerTable(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	response, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx&from=2026-10-24&to=2026-10-24")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "ritase-widya-semua-vendor-2026-10-24.xlsx") {
		t.Fatalf("disposition = %q", got)
	}
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer file.Close()
	want := []string{"Summary Ritase", "Rekap Vendor & Armada", "Detail Unit per Vendor", "Detail Rit per Unit"}
	if sheets := file.GetSheetList(); strings.Join(sheets, "|") != strings.Join(want, "|") {
		t.Fatalf("sheets = %v", sheets)
	}

	headers := map[string][]string{
		"Summary Ritase":         {"Deskripsi", "Nilai", "Satuan"},
		"Rekap Vendor & Armada":  {"Vendor / Armada", "Unit", "Rit", "Volume (m³)", "Porsi Vol", "Avg m³/rit", "Rit/unit"},
		"Detail Unit per Vendor": {"No", "Unit", "Kategori", "Rit", "Volume (m³)", "Avg m³/rit", "Porsi Vol", "Rit 1", "Rit 2", "Interval rata-rata (menit)"},
		"Detail Rit per Unit":    {"Vendor / Unit / Waktu", "Rit ke", "Volume (m³)", "Kategori"},
	}
	for sheet, columns := range headers {
		rows, err := file.GetRows(sheet)
		if err != nil {
			t.Fatalf("read %s: %v", sheet, err)
		}
		if got := strings.Join(rows[4], "|"); got != strings.Join(columns, "|") {
			t.Fatalf("%s header = %q", sheet, got)
		}
	}

	// The figures are numbers, so the sheet can add them up.
	rows, _ := file.GetRows("Rekap Vendor & Armada")
	var hpp []string
	for _, row := range rows {
		if len(row) > 0 && row[0] == "HPP" {
			hpp = row
		}
	}
	if hpp == nil || hpp[2] != "2" || hpp[3] != "55.50" {
		t.Fatalf("hpp row = %v", hpp)
	}
	if cellType, _ := file.GetCellType("Rekap Vendor & Armada", "C7"); cellType == excelize.CellTypeInlineString || cellType == excelize.CellTypeSharedString {
		t.Fatalf("the rit count is stored as text")
	}

	// A vendor's line has its note across the rit columns and the average
	// interval of its trucks in the last column: HPP's one truck, 64 minutes.
	details, _ := file.GetRows("Detail Unit per Vendor")
	var vendor []string
	for _, row := range details {
		if len(row) > 1 && row[1] == "HPP" {
			vendor = row
		}
	}
	if len(vendor) != 10 || !strings.HasPrefix(vendor[7], "Mulai 08:06") || vendor[9] != "64" {
		t.Fatalf("hpp detail row = %q", vendor)
	}

	// The per-load list titles the day and lists each load to the second.
	loads, _ := file.GetRows("Detail Rit per Unit")
	text := strings.Join(flatten(loads), "|")
	for _, fragment := range []string{"Ritase 24 Oktober 2026 | Mid Day", "08:06:00", "TOTAL"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("the per-load sheet is missing %q", fragment)
		}
	}
}

func flatten(rows [][]string) []string {
	var cells []string
	for _, row := range rows {
		cells = append(cells, row...)
	}
	return cells
}

// One vendor picked keeps that vendor's loads only, and the file says so.
func TestProduksiNewExportFiltersByVendor(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/export?vendor=mnp")
	if !strings.Contains(page, `<input type="checkbox" name="vendor" value="MNP" checked>`) ||
		!strings.Contains(page, `<span data-unit-picker-summary>MNP</span>`) ||
		!strings.Contains(page, "1 rit · 12.00 m³ · 1 unit") {
		t.Fatal("the vendor filter does not hold")
	}

	response, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx&vendor=MNP")
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "ritase-widya-vendor-mnp-2026-10-24.xlsx") {
		t.Fatalf("disposition = %q", got)
	}
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer file.Close()
	rows, _ := file.GetRows("Rekap Vendor & Armada")
	if text := strings.Join(flatten(rows), "|"); strings.Contains(text, "HPP") || !strings.Contains(text, "Vendor MNP") {
		t.Fatalf("the MNP export carries another vendor or does not name its own")
	}
}

// The PDF is one signed file of several pages.
func TestProduksiNewExportPrintsAPDF(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	response, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=pdf")
	if response.StatusCode != http.StatusOK || !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("status %d, not a pdf", response.StatusCode)
	}
	// Four sections, a page each at least.
	if pages := bytes.Count(body, []byte("/Type /Page\n")) + bytes.Count(body, []byte("/Type /Page ")); pages < 4 {
		t.Fatalf("the pdf has %d pages, want a page per section", pages)
	}
}

// An export the project switched off is refused at the download as well as
// hidden on the page.
func TestProduksiNewExportHonoursTheProjectSwitch(t *testing.T) {
	testServer, store := newProduksiNewServerWithStore(t, widyaFixture())
	client := loggedInClient(t, testServer)
	// Saved without export_aktif, which is how the settings form switches one off.
	saveExportConfig(t, client, testServer, store.ProjectList()[0].ProjectID, testProjectName, map[string]string{
		"export_key": string(model.ExportProduksiNew),
		"ttd_count":  "1",
	}).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/export")
	if !strings.Contains(html.UnescapeString(page), "Export ini dinonaktifkan") {
		t.Fatal("the page offers a switched-off export")
	}
	response, _ := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx")
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("a switched-off export downloaded with status %d", response.StatusCode)
	}
}

// Each sheet carries the overview's key and notes under its table, and the
// cells are coloured as the overview colours them.
func TestProduksiNewExportCarriesTheKeyAndTheColours(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	_, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx")
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer file.Close()

	for sheet, fragments := range map[string][]string{
		"Summary Ritase":         {"Keterangan", "Hijau", "Vendor aktif", "Kuning", "Jam puncak", "Sumber: tiket Widya Load Scanner"},
		"Rekap Vendor & Armada":  {"Keterangan", "Baris subtotal per vendor", "Unit dihitung sekali per vendor"},
		"Detail Unit per Vendor": {"Keterangan", "Rit yang tercatat saat Rest 1 (12:00–13:00)", "Interval rata-rata = "},
		"Detail Rit per Unit":    {"Jam sampai detik berdasarkan jam scan penuh."},
	} {
		rows, _ := file.GetRows(sheet)
		text := strings.Join(flatten(rows), "|")
		for _, fragment := range fragments {
			if !strings.Contains(text, fragment) {
				t.Fatalf("%s is missing %q", sheet, fragment)
			}
		}
	}

	// The vendor that hauled reads "Aktif" on green, as on the page.
	rows, _ := file.GetRows("Summary Ritase")
	for index, row := range rows {
		if len(row) > 1 && row[1] == "Aktif" {
			cell := "B" + strconv.Itoa(index+1)
			styleID, _ := file.GetCellStyle("Summary Ritase", cell)
			style, _ := file.GetStyle(styleID)
			if len(style.Fill.Color) == 0 || !strings.EqualFold(style.Fill.Color[0], "E8F5E4") {
				t.Fatalf("%s is not green: %+v", cell, style.Fill)
			}
			return
		}
	}
	t.Fatal("no vendor reads Aktif")
}

// Several vendors can be ticked at once: the report keeps all of them, and
// says which in its name and its title.
func TestProduksiNewExportTakesSeveralVendors(t *testing.T) {
	feed := widyaFixture()
	feed.tickets = append(feed.tickets,
		widya.Ticket{Vehicle: "B 3 A", Supplier: "Gosyen", Volume: 20, ScanFullAt: time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)})
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/export?vendor=HPP&vendor=MNP")
	for _, fragment := range []string{
		`<span data-unit-picker-summary>HPP, MNP</span>`, "3 rit · 67.50 m³ · 2 unit",
		`href="/produksi-new/export/download?format=xlsx&amp;from=2026-10-24&amp;to=2026-10-24&amp;vendor=HPP&amp;vendor=MNP"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("two vendors picked, the page is missing %q", fragment)
		}
	}

	response, body := getBody(t, client, testServer.URL+"/produksi-new/export/download?format=xlsx&vendor=HPP&vendor=MNP")
	if got := response.Header.Get("Content-Disposition"); !strings.Contains(got, "ritase-widya-vendor-hpp-mnp-2026-10-24.xlsx") {
		t.Fatalf("disposition = %q", got)
	}
	file, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open workbook: %v", err)
	}
	defer file.Close()
	rows, _ := file.GetRows("Rekap Vendor & Armada")
	text := strings.Join(flatten(rows), "|")
	if strings.Contains(text, "Gosyen") || !strings.Contains(text, "Vendor HPP, MNP") {
		t.Fatal("the export did not keep exactly the two vendors picked")
	}
}
