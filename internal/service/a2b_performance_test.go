package service

import (
	"context"
	"testing"
	"time"

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

	report, err := service.A2BPerformance(context.Background(), "", "", "", 720)
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

	report, err := service.A2BPerformance(context.Background(), "2026-06-01", "", "", 720)
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

	all, err := service.A2BPerformance(context.Background(), "", "", "", 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(all.Units) != 2 {
		t.Fatalf("listed %d units, want the whole fleet", len(all.Units))
	}

	// The dropdown sends the id as the register holds it; matching must not
	// turn on the case it was typed in.
	one, err := service.A2BPerformance(context.Background(), "", "", "BUL02", 720)
	if err != nil {
		t.Fatalf("build performance for one unit: %v", err)
	}
	if len(one.Units) != 1 || one.Units[0].IDUnit != "bul02" {
		t.Fatalf("filtered to %+v, want bul02 alone", one.Units)
	}
	if one.IDUnit != "BUL02" {
		t.Fatalf("IDUnit = %q, want the filter echoed back for the page", one.IDUnit)
	}
}

// Nothing read at all is an empty report, not a failure: the page says so and
// offers no download.
func TestA2BPerformanceWithoutAnyReadings(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	report, err := service.A2BPerformance(context.Background(), "", "", "", 720)
	if err != nil {
		t.Fatalf("build performance: %v", err)
	}
	if len(report.Units) != 0 {
		t.Fatalf("listed %d units, want none", len(report.Units))
	}
}
