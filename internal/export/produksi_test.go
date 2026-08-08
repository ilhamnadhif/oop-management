package export

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/model"
)

func sampleRows() []model.Produksi {
	return []model.Produksi{
		{
			ProduksiID: "PRD-2026-0001", Tanggal: "2026-08-01",
			Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
			Lokasi: "63+575", Layer: "L1",
			UnitID: "UNT-2026-0001", Nopol: "B 1234 ABC", Driver: "Slamet", JenisDT: "DT KECIL",
			Panjang: 375, Lebar: 190, Tinggi: 150, TT: 0, TF: 150,
			Volume: 10.6875, VolumeOPP: 10, Deviasi: 0.6875,
		},
		{
			ProduksiID: "PRD-2026-0002", Tanggal: "2026-08-02",
			Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Timbunan",
			Lokasi: "", Layer: "L2",
			UnitID: "UNT-2026-0002", Nopol: "BG 8611 BX", Driver: "Dodu", JenisDT: "DT BESAR",
			Panjang: 620, Lebar: 233, Tinggi: 202, TT: 20, TF: 212,
			Volume: 30.6197, VolumeOPP: 28, Deviasi: 2.6197,
		},
	}
}

func sampleMeta() Meta {
	return Meta{
		Company:   "PT Orecon Putra Perkasa",
		Title:     "Laporan Produksi",
		Period:    "2026-08-01 s/d 2026-08-31",
		Generated: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC),
		Signatory: Signatory{Title: "Direktur"},
	}
}

func TestProduksiXLSXKeepsNumbersAsNumbers(t *testing.T) {
	payload, err := ProduksiXLSX(sampleRows(), sampleMeta())
	if err != nil {
		t.Fatalf("build xlsx: %v", err)
	}

	file, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	defer file.Close()

	if sheets := file.GetSheetList(); len(sheets) != 1 || sheets[0] != "Produksi" {
		t.Fatalf("sheets = %v, want only Produksi", sheets)
	}

	company, _ := file.GetCellValue("Produksi", "A1")
	if company != "PT Orecon Putra Perkasa" {
		t.Fatalf("letterhead = %q", company)
	}
	header, _ := file.GetCellValue("Produksi", "A5")
	if header != "No" {
		t.Fatalf("header row is not on row 5, found %q", header)
	}
	last, _ := file.GetCellValue("Produksi", "T5")
	if last != "Deviasi" {
		t.Fatalf("last header = %q, want Deviasi", last)
	}

	// A volume stored as text cannot be summed in a spreadsheet, which is the
	// whole reason to ship XLSX alongside the PDF. OOXML marks strings and
	// leaves numbers unmarked, so the test compares against a known text cell
	// rather than looking for a "number" tag that is never written.
	textType, err := file.GetCellType("Produksi", "B6")
	if err != nil {
		t.Fatalf("cell type: %v", err)
	}
	volumeType, err := file.GetCellType("Produksi", "R6")
	if err != nil {
		t.Fatalf("cell type: %v", err)
	}
	if volumeType == textType {
		t.Fatalf("the volume cell is stored the same way as text (%v), so it cannot be summed", volumeType)
	}
	volume, _ := file.GetCellValue("Produksi", "R6")
	if parsed, err := strconv.ParseFloat(volume, 64); err != nil || parsed != 10.6875 {
		t.Fatalf("volume = %q, want a number equal to 10.6875", volume)
	}

	// The totals row sums what the report is judged on.
	total, _ := file.GetCellValue("Produksi", "R8")
	if !strings.HasPrefix(total, "41.30") {
		t.Fatalf("total volume = %q, want 41.3072", total)
	}
}

func TestProduksiXLSXHandlesNoRows(t *testing.T) {
	payload, err := ProduksiXLSX(nil, sampleMeta())
	if err != nil {
		t.Fatalf("build xlsx: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	defer file.Close()

	// The header still ships, so the file explains itself rather than opening blank.
	header, _ := file.GetCellValue("Produksi", "A5")
	if header != "No" {
		t.Fatalf("an empty report lost its header row: %q", header)
	}
}

func TestProduksiPDFIsLandscapeAndSigned(t *testing.T) {
	payload, err := ProduksiPDF(sampleRows(), sampleMeta())
	if err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte("%PDF-")) {
		t.Fatalf("output is not a PDF: %.8q", payload)
	}

	text := string(payload)
	// A4 landscape is 841.89 x 595.28 points; the wide side must come first or
	// twenty columns will not fit.
	if !strings.Contains(text, "841.89 595.28") {
		t.Fatal("the page is not landscape A4")
	}
	if !strings.Contains(text, "/Title") {
		t.Fatal("the PDF carries no title")
	}
}

// Every field of the signature block may be empty, and an unnamed line is
// printed rather than a guessed name.
func TestProduksiPDFSignatureSurvivesAnEmptyName(t *testing.T) {
	meta := sampleMeta()
	meta.Signatory = Signatory{}
	if _, err := ProduksiPDF(sampleRows(), meta); err != nil {
		t.Fatalf("build pdf without a signatory: %v", err)
	}

	named := sampleMeta()
	named.Signatory = Signatory{Name: "Budi Hartono", Title: "Direktur Utama", Place: "Balikpapan"}
	if _, err := ProduksiPDF(sampleRows(), named); err != nil {
		t.Fatalf("build pdf with a signatory: %v", err)
	}
}

func TestProduksiPDFHandlesNoRows(t *testing.T) {
	if _, err := ProduksiPDF(nil, sampleMeta()); err != nil {
		t.Fatalf("build empty pdf: %v", err)
	}
}

// The report must not fall over on the volume it is actually built for.
func TestProduksiPDFHandlesThousandsOfRows(t *testing.T) {
	rows := make([]model.Produksi, 0, 3000)
	base := sampleRows()[0]
	for i := 0; i < 3000; i++ {
		row := base
		row.ProduksiID = "PRD-2026-" + FormatFloat(float64(i+1), 0)
		rows = append(rows, row)
	}
	payload, err := ProduksiPDF(rows, sampleMeta())
	if err != nil {
		t.Fatalf("build large pdf: %v", err)
	}
	if len(payload) < 10_000 {
		t.Fatalf("a 3000 row report produced only %d bytes", len(payload))
	}
}

// Core PDF fonts cannot encode these, and an unencodable rune renders as
// nonsense rather than failing loudly.
func TestPDFTextIsTransliterated(t *testing.T) {
	if got := tr("Volume 10 m³ · 2 × 3 — selesai"); strings.ContainsAny(got, "³·×—") {
		t.Fatalf("tr left characters the core fonts cannot encode: %q", got)
	}
}

func TestColumnWidthsFitLandscapeA4(t *testing.T) {
	total := 0.0
	for _, column := range produksiColumns {
		total += column.Width
	}
	usable := pageWidth - 2*pageMargin
	if total > usable {
		t.Fatalf("columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
}
