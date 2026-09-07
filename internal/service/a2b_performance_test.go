package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// An export names its own range. Leaving the dates empty asks for everything
// ever read, not for the week the overview happens to open on.
func TestA2BPerformanceWithoutDatesCoversEveryReading(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	// Months apart, and both outside the seven days the overview defaults to.
	seedReading(t, store, "exc01", "2026-05-02", 8, 245, nil, 0)
	seedReading(t, store, "exc01", "2026-07-19", 7, 180, nil, 0)

	report, err := service.A2BPerformance(context.Background(), "", "", nil, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if report.From != "2026-05-02" || report.To != "2026-07-19" {
		t.Fatalf("range = %s..%s, want the first and last reading", report.From, report.To)
	}
	if len(report.Units) != 1 {
		t.Fatalf("listed %d units, want 1", len(report.Units))
	}
	if report.Units[0].Shifts != 2 {
		t.Fatalf("shifts = %d, want both readings counted", report.Units[0].Shifts)
	}
}

// One side left open runs to the edge of what has been read.
func TestA2BPerformanceFillsTheOpenSideOfTheRange(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-05-02", 8, 245, nil, 0)
	seedReading(t, store, "exc01", "2026-07-19", 7, 180, nil, 0)

	report, err := service.A2BPerformance(context.Background(), "2026-06-01", "", nil, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if report.From != "2026-06-01" || report.To != "2026-07-19" {
		t.Fatalf("range = %s..%s, want the given start and the last reading", report.From, report.To)
	}
	if report.Units[0].Shifts != 1 {
		t.Fatalf("shifts = %d, want only the reading inside the range", report.Units[0].Shifts)
	}
}

// The unit filter is what the dropdown sends. Empty means the whole fleet.
func TestA2BPerformanceFiltersToOneUnit(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	seedFuelMachine(t, store, "bul02", "Bulldozer D6")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-05", 8, 245, nil, 0)
	seedReading(t, store, "bul02", "2026-08-06", 7, 180, nil, 0)

	all, err := service.A2BPerformance(context.Background(), "", "", nil, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(all.Units) != 2 {
		t.Fatalf("listed %d units, want the whole fleet", len(all.Units))
	}

	// The dropdown sends the id as the register holds it; matching must not
	// turn on the case it was typed in.
	one, err := service.A2BPerformance(context.Background(), "", "", []string{"BUL02"}, 720)
	if err != nil {
		t.Fatalf("build performance for one unit: %v", err)
	}
	if len(one.Units) != 1 || one.Units[0].IDUnit != "bul02" {
		t.Fatalf("filtered to %+v, want bul02 alone", one.Units)
	}
	if one.IDUnits[0] != "BUL02" {
		t.Fatalf("IDUnits = %v, want the filter echoed back for the page", one.IDUnits)
	}
}

// Nothing read at all is an empty report, not a failure: the page says so and
// offers no download.
func TestA2BPerformanceWithoutAnyReadings(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	report, err := service.A2BPerformance(context.Background(), "", "", nil, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(report.Units) != 0 {
		t.Fatalf("listed %d units, want none", len(report.Units))
	}
}

// The summary says a machine ran fifty hours; the detail says which shifts
// those were. Without it the figure has to be taken on trust.
func TestA2BPerformanceCarriesTheReadingsBehindIt(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	seedFuelMachine(t, store, "bld02", "Bulldozer D6")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-05", 7, 245,
		[]model.HourMeterStandby{{Variable: "ISTIRAHAT", Menit: 60}}, 0)
	seedReading(t, store, "exc01", "2026-08-06", 6, 200, nil, 120)
	seedReading(t, store, "bld02", "2026-08-06", 8, 300, nil, 0)

	report, err := service.A2BPerformance(context.Background(), "", "", nil, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(report.Readings) != 3 {
		t.Fatalf("carried %d readings, want all three", len(report.Readings))
	}
	// Grouped by machine and then by day, so the detail reads down each unit's
	// own run rather than jumping between them.
	if report.Readings[0].IDUnit != "bld02" {
		t.Fatalf("readings are not grouped by unit: %+v", report.Readings)
	}
	if report.Readings[1].Tanggal > report.Readings[2].Tanggal {
		t.Fatalf("one unit's readings are not in date order: %+v", report.Readings[1:])
	}
}

// The detail answers the same filters the summary does, or the two would
// describe different months.
func TestA2BPerformanceReadingsFollowTheFilters(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	seedFuelMachine(t, store, "bld02", "Bulldozer D6")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-07-30", 8, 100, nil, 0)
	seedReading(t, store, "exc01", "2026-08-05", 7, 245, nil, 0)
	seedReading(t, store, "bld02", "2026-08-06", 8, 300, nil, 0)

	ranged, err := service.A2BPerformance(context.Background(), "2026-08-01", "2026-08-31", nil, 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(ranged.Readings) != 2 {
		t.Fatalf("carried %d readings, want the two inside the range", len(ranged.Readings))
	}

	one, err := service.A2BPerformance(context.Background(), "2026-08-01", "2026-08-31", []string{"EXC01"}, 720)
	if err != nil {
		t.Fatalf("build for one unit: %v", err)
	}
	if len(one.Readings) != 1 || !strings.EqualFold(one.Readings[0].IDUnit, "exc01") {
		t.Fatalf("the unit filter did not narrow the detail: %+v", one.Readings)
	}
}

// A site asking about two machines gets both, and nothing else.
func TestA2BPerformanceFiltersToSeveralUnits(t *testing.T) {
	store := repository.NewTestRepository()
	for _, id := range []string{"exc01", "bld02", "svd03"} {
		seedFuelMachine(t, store, id, "Alat "+id)
		seedReading(t, store, id, "2026-08-05", 8, 200, nil, 0)
	}
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	report, err := service.A2BPerformance(context.Background(), "", "", []string{"EXC01", "svd03"}, 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(report.Units) != 2 {
		t.Fatalf("listed %d units, want the two asked for: %+v", len(report.Units), report.Units)
	}
	for _, unit := range report.Units {
		if strings.EqualFold(unit.IDUnit, "bld02") {
			t.Fatalf("a machine outside the filter was listed: %+v", report.Units)
		}
	}
	// The detail follows the same filter, or the two halves of the report would
	// describe different fleets.
	if len(report.Readings) != 2 {
		t.Fatalf("carried %d readings, want one per machine asked for", len(report.Readings))
	}
	// The page gets its ticks back exactly as it sent them.
	if len(report.IDUnits) != 2 || report.IDUnits[0] != "EXC01" {
		t.Fatalf("IDUnits = %v, want the filter echoed back", report.IDUnits)
	}
}
