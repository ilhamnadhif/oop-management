package export

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/model"
	"opp-management/internal/service"
)

func samplePerformance() []service.A2BUnitPerformance {
	return []service.A2BUnitPerformance{
		{IDUnit: "exc01", NamaUnit: "Excavator PC200", Shifts: 3, TotalHM: 23.5, Fuel: 625, FuelRatio: 26.6, PA: 88.9, UA: 92.4},
		{IDUnit: "bul02", NamaUnit: "Bulldozer D6", Shifts: 2, TotalHM: 14, Fuel: 310, FuelRatio: 22.14, PA: 100, UA: 77.8},
	}
}

// The table has to span the letterhead rule it sits under: wider and the last
// columns fall off the page, far narrower and it reads as a rendering fault.
func TestA2BPerformanceTableFitsTheLandscapePage(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	total := A2BPerformanceTable(samplePerformance()).totalWidth()
	if total > usable {
		t.Fatalf("columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
	if total < usable-6 {
		t.Fatalf("columns total %.1fmm, leaving %.1fmm of the page empty", total, usable-total)
	}
}

// Shifts, hours and fuel are worth adding up. Percentages and ratios are not:
// a column of them sums to a number that means nothing.
func TestA2BPerformanceTotalsOnlyWhatAddsUp(t *testing.T) {
	table := A2BPerformanceTable(samplePerformance())
	if got := table.Totals[3]; got != 5 {
		t.Fatalf("shift total = %v, want 5", got)
	}
	if got := table.Totals[4]; got != 37.5 {
		t.Fatalf("hour total = %v, want 37.5", got)
	}
	if got := table.Totals[5]; got != 935 {
		t.Fatalf("fuel total = %v, want 935", got)
	}
	for _, index := range []int{6, 7, 8} {
		if _, totalled := table.Totals[index]; totalled {
			t.Fatalf("column %d is totalled, and a ratio or a percentage must not be", index)
		}
	}
}

func sampleReadings() []model.HourMeter {
	return []model.HourMeter{
		{
			HMID: "HM-1", Tanggal: "2026-08-05", Shift: "Shift 1",
			IDUnit: "exc01", NamaUnit: "Excavator PC200", Operator: "Budi",
			HMAwal: 1200, HMAkhir: 1208, TotalHM: 8, PA: 100, UA: 88.9,
			TotalStandby: 60, Standby: []model.HourMeterStandby{{Variable: "ISTIRAHAT", Menit: 60}},
		},
		{
			HMID: "HM-2", Tanggal: "2026-08-06", Shift: "Shift 1",
			IDUnit: "exc01", NamaUnit: "Excavator PC200", Operator: "Budi",
			HMAwal: 1208, HMAkhir: 1215.5, TotalHM: 7.5, PA: 75, UA: 66.7,
			TotalStandby: 30, Standby: []model.HourMeterStandby{{Variable: "HUJAN", Menit: 30}},
			TotalBreakdown: 120, Breakdown: []model.HourMeterBreakdown{{Variable: "RUSAK", Menit: 120}},
		},
	}
}

func sampleReport() *service.A2BPerformanceReport {
	return &service.A2BPerformanceReport{
		From: "2026-08-01", To: "2026-08-31",
		Units: samplePerformance(), Readings: sampleReadings(),
	}
}

func TestA2BPerformanceRendersBothFormats(t *testing.T) {
	meta := sampleMeta()
	if _, err := A2BPerformanceXLSX(sampleReport(), meta); err != nil {
		t.Fatalf("build xlsx: %v", err)
	}
	if _, err := A2BPerformancePDF(sampleReport(), meta); err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	// An export of a range nothing was read in is a page saying so, not a
	// failure.
	if _, err := A2BPerformancePDF(&service.A2BPerformanceReport{}, meta); err != nil {
		t.Fatalf("build empty pdf: %v", err)
	}
}

// The detail has to fit the same page the summary does.
func TestA2BReadingColumnWidthsFitLandscapeA4(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	total := A2BReadingTable(sampleReadings()).totalWidth()
	if total > usable {
		t.Fatalf("columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
	if total < usable-6 {
		t.Fatalf("columns total %.1fmm, leaving %.1fmm of the page empty", total, usable-total)
	}
}

// Hours and lost minutes add up. PA and UA do not: they are the summary's
// business, and a column of percentages summed is a number about nothing.
func TestA2BReadingTotalsOnlyWhatAddsUp(t *testing.T) {
	table := A2BReadingTable(sampleReadings())
	if got := table.Totals[7]; got != 15.5 {
		t.Fatalf("hours = %v, want 8 + 7.5", got)
	}
	if got := table.Totals[10]; got != 90 {
		t.Fatalf("standby = %v, want 60 + 30", got)
	}
	if got := table.Totals[11]; got != 120 {
		t.Fatalf("breakdown = %v, want 120", got)
	}
	for _, column := range []int{8, 9} {
		if _, summed := table.Totals[column]; summed {
			t.Fatalf("column %d (%s) carries a meaningless total",
				column, table.Columns[column].Header)
		}
	}
	if A2BReadingTable(nil).hasTotals() {
		t.Fatal("an empty detail still prints a totals row")
	}
}

// Standby and breakdown answer the same question about the same shift, so they
// are worded together, longest first: the reason that cost the most is the one
// worth reading.
func TestA2BReadingWordsWhyTheShiftStopped(t *testing.T) {
	table := A2BReadingTable(sampleReadings())
	if got := table.Rows[0][12]; got != "ISTIRAHAT 60 m" {
		t.Fatalf("first row keterangan = %q", got)
	}
	if got := table.Rows[1][12]; got != "RUSAK 120 m · HUJAN 30 m" {
		t.Fatalf("second row keterangan = %q, want the longest first", got)
	}
	// A shift nobody stopped says nothing rather than "0 m".
	quiet := A2BReadingTable([]model.HourMeter{{HMID: "HM-3", IDUnit: "exc01", TotalHM: 8}})
	if got := quiet.Rows[0][12]; got != "" {
		t.Fatalf("a shift with no stoppage says %q", got)
	}
}

// The summary carries the detail with it, so both formats get it from one
// place rather than each assembling its own.
func TestA2BPerformanceCarriesItsDetail(t *testing.T) {
	table := a2bPerformanceWithDetail(sampleReport())
	if !table.hasDetail() {
		t.Fatal("the summary does not carry the shifts behind it")
	}
	if got := table.Detail.SheetName; got != "Rincian Shift" {
		t.Fatalf("detail sheet = %q", got)
	}
	// Nothing read means nothing to attach: an empty section is worse than none.
	if a2bPerformanceWithDetail(&service.A2BPerformanceReport{Units: samplePerformance()}).hasDetail() {
		t.Fatal("a report with no readings still attaches a detail")
	}
}

// The detail goes on a sheet of its own rather than under the summary: a
// second header halfway down a column breaks sorting and filtering, which is
// most of what a spreadsheet is for.
func TestA2BPerformanceWritesTheDetailToItsOwnSheet(t *testing.T) {
	payload, err := A2BPerformanceXLSX(sampleReport(), sampleMeta())
	if err != nil {
		t.Fatalf("build xlsx: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("read back the workbook: %v", err)
	}
	defer file.Close()

	sheets := file.GetSheetList()
	if len(sheets) != 2 || sheets[0] != "Performance Unit" || sheets[1] != "Rincian Shift" {
		t.Fatalf("sheets = %v, want the summary then its detail", sheets)
	}
	// The detail sheet holds the shifts, headed the way the section is.
	header, err := file.GetCellValue("Rincian Shift", "M5")
	if err != nil {
		t.Fatalf("read detail header: %v", err)
	}
	if header != "Keterangan" {
		t.Fatalf("detail header = %q, want the stoppage column", header)
	}
	reason, err := file.GetCellValue("Rincian Shift", "M7")
	if err != nil {
		t.Fatalf("read detail row: %v", err)
	}
	if reason != "RUSAK 120 m · HUJAN 30 m" {
		t.Fatalf("detail row = %q", reason)
	}

	// A report with nothing behind it is one sheet, not an empty second one.
	payload, err = A2BPerformanceXLSX(&service.A2BPerformanceReport{Units: samplePerformance()}, sampleMeta())
	if err != nil {
		t.Fatalf("build xlsx without a detail: %v", err)
	}
	plain, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	defer plain.Close()
	if got := plain.GetSheetList(); len(got) != 1 {
		t.Fatalf("sheets = %v, want the summary alone", got)
	}
}
