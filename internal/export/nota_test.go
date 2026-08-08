package export

import (
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
)

func notaFixtures() []model.Nota {
	return []model.Nota{
		{
			NotaID: "NTA-20260807-0001", Tanggal: "2026-08-07", PIC: "Budi",
			MetodePembayaran:  model.NotaMetodeReimburse,
			StatusPembayaran:  model.NotaStatusBelumDibayar,
			PenerimaReimburse: "Budi Santoso", Kategori: "Umum ADM", SubKategori: "ATK",
			Total:        230000,
			FotoKwitansi: "data:image/jpeg;base64," + strings.Repeat("x", 40_000),
			Items: []model.NotaItem{
				{NotaID: "NTA-20260807-0001", Baris: 1, NamaProduk: "Kertas A4", Satuan: "rim", Volume: 2, Harga: 55000, Subtotal: 110000},
				{NotaID: "NTA-20260807-0001", Baris: 2, NamaProduk: "Tinta printer", Satuan: "pcs", Volume: 1, Harga: 120000, Subtotal: 120000},
			},
		},
		{
			NotaID: "NTA-20260808-0001", Tanggal: "2026-08-08", PIC: "Sari",
			MetodePembayaran: model.NotaMetodeCA,
			StatusPembayaran: model.NotaStatusSudahDibayar,
			Kategori:         "Operasional", SubKategori: "QHSE",
			Total: 75000,
			Items: []model.NotaItem{
				{NotaID: "NTA-20260808-0001", Baris: 1, NamaProduk: "Sarung tangan", Satuan: "pasang", Volume: 5, Harga: 15000, Subtotal: 75000},
			},
		},
	}
}

func notaMeta() Meta {
	return Meta{
		Company:   "PT Orecon Putra Perkasa",
		Title:     "Laporan Nota",
		Generated: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC),
		Signatory: Signatory{Title: "Direktur"},
	}
}

// One row per line, not one per nota: a nota-level export would hide what was
// bought, which is the reason the note exists.
func TestNotaTableIsOneRowPerItem(t *testing.T) {
	table := NotaTable(notaFixtures())
	if len(table.Rows) != 3 {
		t.Fatalf("wrote %d rows, want 3", len(table.Rows))
	}
	if table.Rows[1][1] != "NTA-20260807-0001" {
		t.Fatalf("the second line lost its nota: %q", table.Rows[1][1])
	}
	if table.Rows[2][10] != "Sarung tangan" {
		t.Fatalf("third row product = %q", table.Rows[2][10])
	}
	// The running number counts lines, so it stays unique across notes.
	if table.Rows[0][0] != "1" || table.Rows[2][0] != "3" {
		t.Fatalf("row numbering wrong: %q, %q", table.Rows[0][0], table.Rows[2][0])
	}
}

// The nota total repeats on every one of its lines, so totalling it would count
// the same money once per line. Only the subtotals add up.
func TestNotaTableTotalsSubtotalsOnly(t *testing.T) {
	table := NotaTable(notaFixtures())
	if got := table.Totals[notaSubtotalColumn]; got != 305000 {
		t.Fatalf("subtotal total = %v, want 305000", got)
	}
	for column := range table.Totals {
		if column != notaSubtotalColumn {
			t.Fatalf("column %d (%s) carries a total that double counts",
				column, table.Columns[column].Header)
		}
	}
}

// The attachments are base64 images tens of thousands of characters long.
func TestNotaTableLeavesAttachmentsOut(t *testing.T) {
	table := NotaTable(notaFixtures())
	for _, column := range table.Columns {
		header := strings.ToLower(column.Header)
		if strings.Contains(header, "foto") || strings.Contains(header, "bukti") {
			t.Fatalf("the report exports an attachment column: %q", column.Header)
		}
	}
	for _, cells := range table.Rows {
		for _, cell := range cells {
			if len(cell) > 200 {
				t.Fatalf("a cell carries %d characters, which is not printable", len(cell))
			}
		}
	}
}

// At 6pt a run of six digits is hard to read, and a misread figure on an
// expense report is an argument about money.
func TestNotaPrintsMoneyGrouped(t *testing.T) {
	table := NotaTable(notaFixtures())
	if table.Rows[0][13] != "55.000" || table.Rows[0][14] != "110.000" {
		t.Fatalf("money printed as %q and %q", table.Rows[0][13], table.Rows[0][14])
	}
	// The volume is not money and keeps its plain form.
	if table.Rows[0][12] != "2" {
		t.Fatalf("volume printed as %q", table.Rows[0][12])
	}
}

func TestFormatMoneyGroupsThousands(t *testing.T) {
	for value, want := range map[float64]string{
		0: "0", 999: "999", 1000: "1.000", 110000: "110.000", 12500000: "12.500.000", -5000: "-5.000",
	} {
		if got := FormatMoney(value); got != want {
			t.Fatalf("FormatMoney(%v) = %q, want %q", value, got, want)
		}
	}
}

func TestNotaValuesStayNumeric(t *testing.T) {
	table := NotaTable(notaFixtures())
	for _, column := range []int{12, 13, 14} {
		if _, ok := table.Values[0][column].(float64); !ok {
			t.Fatalf("column %d exported as %T", column, table.Values[0][column])
		}
	}
}

func TestNotaColumnWidthsFitLandscapeA4(t *testing.T) {
	total := 0.0
	for _, column := range notaColumns {
		total += column.Width
	}
	usable := pageWidth - 2*pageMargin
	if total > usable {
		t.Fatalf("columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
	if total < usable-6 {
		t.Fatalf("columns total %.1fmm, leaving %.1fmm of the page empty", total, usable-total)
	}
}

func TestNotaRendersBothFormats(t *testing.T) {
	pdf, err := NotaPDF(notaFixtures(), notaMeta())
	if err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	if !strings.HasPrefix(string(pdf[:5]), "%PDF-") {
		t.Fatal("the report is not a pdf")
	}
	sheet, err := NotaXLSX(notaFixtures(), notaMeta())
	if err != nil {
		t.Fatalf("render xlsx: %v", err)
	}
	if !strings.HasPrefix(string(sheet[:2]), "PK") {
		t.Fatal("the report is not a zip")
	}
	// An empty period still has to print as a document, not as an error.
	if _, err := NotaPDF(nil, notaMeta()); err != nil {
		t.Fatalf("empty pdf: %v", err)
	}
}
