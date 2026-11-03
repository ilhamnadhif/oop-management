package export

import (
	"testing"

	"opp-management/internal/model"
)

func fuelKeluarFixtures() []model.FuelKeluar {
	meter := 1294.9
	return []model.FuelKeluar{
		{
			FuelOutID: "FUELOUT-20260902-0001", Tanggal: "2026-09-02",
			IDUnit: "EXC-01", NamaUnit: "Excavator PC200 Kobelco",
			HMAwalFlowMeter: 20, HMAkhirFlowMeter: 170, Liter: 150,
			HMAlatBerat: &meter, Operator: "Budi",
		},
		{
			// The machine's own meter is optional: this fill was taken without
			// anybody reading it.
			FuelOutID: "FUELOUT-20260902-0002", Tanggal: "2026-09-02",
			IDUnit: "BLD-03", NamaUnit: "Bulldozer D65 Komatsu",
			HMAwalFlowMeter: 170, HMAkhirFlowMeter: 250, Liter: 80,
			Operator: "Cahyo",
		},
	}
}

// fuelKeluarRatios goes with fuelKeluarFixtures: the excavator's fill has a
// ratio, the bulldozer's, with no meter read, has none.
func fuelKeluarRatios() []*float64 {
	ratio := 12.5
	return []*float64{&ratio, nil}
}

// The ratio prints beside the machine's meter, and a fill without one prints a
// dash and stores an empty cell.
func TestFuelKeluarPrintsTheFuelRatio(t *testing.T) {
	table := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios())
	if header := table.Columns[9].Header; header != "Fuel Ratio (L/jam)" {
		t.Fatalf("column 9 is %q", header)
	}
	if got := table.Rows[0][9]; got != "12.5" {
		t.Fatalf("ratio printed as %q", got)
	}
	if got := table.Values[0][9]; got != 12.5 {
		t.Fatalf("ratio stored as %v", got)
	}
	if got := table.Rows[1][9]; got != "-" {
		t.Fatalf("no ratio printed as %q, want a dash", got)
	}
	if got := table.Values[1][9]; got != nil {
		t.Fatalf("no ratio stored as %v, want an empty cell", got)
	}
}

// The dispensing report must line up with the letterhead rule, the same way
// every other report's does.
func TestFuelKeluarColumnWidthsFitLandscapeA4(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	total := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios()).totalWidth()
	if total > usable {
		t.Fatalf("columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
	if total < usable-6 {
		t.Fatalf("columns total %.1fmm, leaving %.1fmm of the page empty", total, usable-total)
	}
}

// The photo is left out on purpose: it is a base64 data URL tens of thousands
// of characters long, which no cell or printed page can show.
func TestFuelKeluarLeavesThePhotoOut(t *testing.T) {
	table := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios())
	for _, column := range table.Columns {
		if header := column.Header; header == "Foto" {
			t.Fatal("the report exports a photo column")
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

// The litres add up; the meter readings do not, since one fill's odometer
// added to the next is a number about nothing.
func TestFuelKeluarTotalsOnlyTheLitres(t *testing.T) {
	table := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios())
	if got := table.Totals[7]; got != 230 {
		t.Fatalf("litres = %v, want 150 + 80", got)
	}
	for column := range table.Totals {
		if column != 7 {
			t.Fatalf("column %d (%s) carries a meaningless total",
				column, table.Columns[column].Header)
		}
	}
	if FuelKeluarTable(nil, nil).hasTotals() {
		t.Fatal("an empty report still prints a totals row")
	}
}

// A fill nobody read the machine's meter at prints a dash. A nought would say
// the meter read zero, which is a different fact.
func TestFuelKeluarPrintsADashForAnUnreadMeter(t *testing.T) {
	table := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios())
	if got := table.Rows[1][8]; got != "-" {
		t.Fatalf("unread meter printed as %q, want a dash", got)
	}
	if got := table.Values[1][8]; got != nil {
		t.Fatalf("unread meter stored as %v, want an empty cell", got)
	}
	if got := table.Values[0][8]; got != 1294.9 {
		t.Fatalf("a meter that was read stored as %v", got)
	}
}

// The litres stay numbers so the spreadsheet can sort and sum them.
func TestFuelKeluarValuesStayNumeric(t *testing.T) {
	table := FuelKeluarTable(fuelKeluarFixtures(), fuelKeluarRatios())
	if _, ok := table.Values[0][7].(float64); !ok {
		t.Fatalf("litres exported as %T", table.Values[0][7])
	}
}

func TestFuelKeluarRendersBothFormats(t *testing.T) {
	meta := unitMeta("Fuel Keluar")
	if _, err := FuelKeluarXLSX(fuelKeluarFixtures(), fuelKeluarRatios(), meta); err != nil {
		t.Fatalf("render xlsx: %v", err)
	}
	if _, err := FuelKeluarPDF(fuelKeluarFixtures(), fuelKeluarRatios(), meta); err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	// An empty report still has to print as a document, not as an error.
	if _, err := FuelKeluarPDF(nil, nil, meta); err != nil {
		t.Fatalf("empty pdf: %v", err)
	}
}
