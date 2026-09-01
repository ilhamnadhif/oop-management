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

func unitMeta(title string) Meta {
	return Meta{
		Company:   "PT Orecon Putra Perkasa",
		Title:     title,
		Period:    SnapshotLabel(time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC)),
		Generated: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC),
		Signatory: Signatory{Title: "Direktur"},
	}
}

// The register must line up with the letterhead rule, which spans the whole
// usable width; a table wider than that runs off the printed page.
func TestUnitColumnWidthsFitLandscapeA4(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	for name, columns := range map[string][]Column{
		"unit dt": unitDTColumns,
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

	dtXLSX, err := UnitDTXLSX(unitDTFixtures(), unitMeta("Daftar Unit DT"))
	if err != nil {
		t.Fatalf("render dt xlsx: %v", err)
	}
	if !bytes.HasPrefix(dtXLSX, []byte("PK")) {
		t.Fatal("dt xlsx is not a zip")
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
}
