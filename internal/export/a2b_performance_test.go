package export

import (
	"testing"

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

func TestA2BPerformanceRendersBothFormats(t *testing.T) {
	meta := sampleMeta()
	if _, err := A2BPerformanceXLSX(samplePerformance(), meta); err != nil {
		t.Fatalf("build xlsx: %v", err)
	}
	if _, err := A2BPerformancePDF(samplePerformance(), meta); err != nil {
		t.Fatalf("build pdf: %v", err)
	}
	// An export of a range nothing was read in is a page saying so, not a
	// failure.
	if _, err := A2BPerformancePDF(nil, meta); err != nil {
		t.Fatalf("build empty pdf: %v", err)
	}
}
