package export

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
)

func unitDTFixtures() []model.UnitDT {
	return []model.UnitDT{
		{UnitID: "UNT-2026-0001", Nopol: "AD 8590 FG", Panjang: 375, Lebar: 190, Tinggi: 150,
			Driver: "Yusuf", Keterangan: "DT KECIL", Foto: strings.Repeat("x", 40_000)},
		{UnitID: "UNT-2026-0002", Nopol: "AB 8698 GD", Panjang: 380, Lebar: 185, Tinggi: 160,
			Driver: "Cacing", Keterangan: "DT BESAR"},
	}
}

func unitA2BFixtures() []model.UnitA2B {
	return []model.UnitA2B{
		{NoUrut: 1, TanggalIn: "2026-06-02", IDUnit: "EXC-01", NamaUnit: "Excavator PC200 Kobelco",
			MerekType: "Kobelco PC200", FuelStorage: 300, FRUnit: 19.3, Lokasi: "Site", HMAwal: 0},
		{NoUrut: 2, TanggalIn: "2026-06-02", IDUnit: "BLD-01", NamaUnit: "Bulldozer D6 CAT",
			MerekType: "Caterpillar", FuelStorage: 400, FRUnit: 26.3, Lokasi: "Site", HMAwal: 120},
	}
}

func unitMeta(title string) Meta {
	return Meta{
		Company:   "PT Orecon Putra Perkasa",
		Title:     title,
		Period:    SnapshotLabel(time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)),
		Generated: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC),
		Signatory: Signatory{Title: "Direktur"},
	}
}

// Both registers must line up with the letterhead rule, which spans the whole
// usable width; a table wider than that runs off the printed page.
func TestUnitColumnWidthsFitLandscapeA4(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	for name, columns := range map[string][]Column{
		"unit dt":  unitDTColumns,
		"unit a2b": unitA2BColumns,
	} {
		total := 0.0
		for _, column := range columns {
			total += column.Width
		}
		if total > usable {
			t.Fatalf("%s columns total %.1fmm, wider than the %.1fmm usable page", name, total, usable)
		}
		// A table far narrower than the page reads as a rendering fault.
		if total < usable-6 {
			t.Fatalf("%s columns total %.1fmm, leaving %.1fmm of the page empty", name, total, usable-total)
		}
	}
}

// A photo is a base64 data URL tens of thousands of characters long. Carrying
// it into the report would bloat the file and print as garbage.
func TestUnitTablesLeaveThePhotoOut(t *testing.T) {
	table := UnitDTTable(unitDTFixtures())
	for _, column := range table.Columns {
		if strings.Contains(strings.ToLower(column.Header), "foto") {
			t.Fatalf("the register exports a photo column: %q", column.Header)
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

// Dimensions and capacities stay numbers so the spreadsheet can sum and sort
// them; a register exported as text is a register nobody can work with.
func TestUnitValuesStayNumeric(t *testing.T) {
	dt := UnitDTTable(unitDTFixtures())
	if _, ok := dt.Values[0][3].(float64); !ok {
		t.Fatalf("unit dt panjang exported as %T", dt.Values[0][3])
	}
	a2b := UnitA2BTable(unitA2BFixtures())
	if _, ok := a2b.Values[0][6].(float64); !ok {
		t.Fatalf("unit a2b fuel storage exported as %T", a2b.Values[0][6])
	}
}

// Tank capacity adds up to something real; hour meters do not, so they carry no
// total.
func TestUnitA2BTotalsOnlyFuelCapacity(t *testing.T) {
	table := UnitA2BTable(unitA2BFixtures())
	if got := table.Totals[6]; got != 700 {
		t.Fatalf("fuel total %.1f, want 700", got)
	}
	for column := range table.Totals {
		if column != 6 {
			t.Fatalf("column %d (%s) carries a meaningless total",
				column, table.Columns[column].Header)
		}
	}
}

// The DT register has nothing worth summing, so it prints no total row at all.
func TestUnitDTHasNoTotalsRow(t *testing.T) {
	if UnitDTTable(unitDTFixtures()).hasTotals() {
		t.Fatal("the dt register prints a total row over dimensions")
	}
}

func TestUnitRegistersRenderBothFormats(t *testing.T) {
	dtPDF, err := UnitDTPDF(unitDTFixtures(), unitMeta("Daftar Unit DT"))
	if err != nil {
		t.Fatalf("render dt pdf: %v", err)
	}
	if !bytes.HasPrefix(dtPDF, []byte("%PDF-")) {
		t.Fatal("dt pdf is not a pdf")
	}

	a2bXLSX, err := UnitA2BXLSX(unitA2BFixtures(), unitMeta("Daftar Unit A2B"))
	if err != nil {
		t.Fatalf("render a2b xlsx: %v", err)
	}
	if !bytes.HasPrefix(a2bXLSX, []byte("PK")) {
		t.Fatal("a2b xlsx is not a zip")
	}
}

// A register is a snapshot, not a period; labelling it "Seluruh periode" would
// misdate the document. The PDF text streams are compressed, so the label is
// checked at its source.
func TestSnapshotLabelNamesTheReadingDate(t *testing.T) {
	got := SnapshotLabel(time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC))
	if got != "Data per 9 Agustus 2026" {
		t.Fatalf("snapshot label %q", got)
	}
	if (Meta{}).periodLabel() == got {
		t.Fatal("a register carries the same label as an unfiltered report")
	}
}

// An empty register still has to print as a document, not as an error.
func TestUnitRegistersSurviveNoRows(t *testing.T) {
	if _, err := UnitDTPDF(nil, unitMeta("Daftar Unit DT")); err != nil {
		t.Fatalf("empty dt pdf: %v", err)
	}
	if _, err := UnitA2BPDF(nil, unitMeta("Daftar Unit A2B")); err != nil {
		t.Fatalf("empty a2b pdf: %v", err)
	}
}
