package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedDispenseFor files one dispense straight into the store, so the export has a
// sheet to read without going through the form's photo and meter rules.
func seedDispenseFor(t *testing.T, store *repository.TestRepository, id, tanggal, idUnit string, liter float64) {
	t.Helper()
	fuel := &model.FuelKeluar{
		FuelOutID: id, Tanggal: tanggal, IDUnit: idUnit, NamaUnit: "Excavator " + idUnit,
		HMAwalFlowMeter: 100, HMAkhirFlowMeter: 100 + liter, Liter: liter,
		Operator: "kadal",
	}
	if err := store.CreateFuelKeluar(context.Background(), fuel); err != nil {
		t.Fatalf("seed dispense %s: %v", id, err)
	}
}

func newFuelKeluarExportService(store repository.Store) *FuelKeluarService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewFuelKeluarService(store, location,
		func() time.Time { return time.Date(2026, 9, 6, 10, 0, 0, 0, location) })
}

// The export narrows the sheet to a date range, and an empty side does not
// bound that side at all.
func TestFuelKeluarExportFiltersByRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)
	seedDispenseFor(t, store, "FO-1", "2026-08-07", "exc01", 150)
	seedDispenseFor(t, store, "FO-2", "2026-09-02", "exc01", 200)

	all, err := service.ExportRows(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("export all: %v", err)
	}
	if len(all.Rows) != 2 {
		t.Fatalf("export all returned %d rows, want 2", len(all.Rows))
	}

	august, err := service.ExportRows(context.Background(), "2026-08-01", "2026-08-31", "")
	if err != nil {
		t.Fatalf("export august: %v", err)
	}
	if len(august.Rows) != 1 || august.Rows[0].Tanggal != "2026-08-07" {
		t.Fatalf("export august returned %+v", august.Rows)
	}

	open, err := service.ExportRows(context.Background(), "2026-09-01", "", "")
	if err != nil {
		t.Fatalf("export from september: %v", err)
	}
	if len(open.Rows) != 1 || open.Rows[0].Tanggal != "2026-09-02" {
		t.Fatalf("an open end returned %+v", open.Rows)
	}
}

// A range typed the wrong way round is the person's slip, not a refusal.
func TestFuelKeluarExportSwapsAReversedRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)
	seedDispenseFor(t, store, "FO-1", "2026-08-07", "exc01", 150)

	report, err := service.ExportRows(context.Background(), "2026-08-31", "2026-08-01", "")
	if err != nil {
		t.Fatalf("export reversed: %v", err)
	}
	if report.From != "2026-08-01" || report.To != "2026-08-31" {
		t.Fatalf("range = %s..%s, want it swapped", report.From, report.To)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("a swapped range returned %d rows, want 1", len(report.Rows))
	}
}

// A date the calendar does not have cannot filter anything, so it is refused
// rather than quietly ignored.
func TestFuelKeluarExportRefusesAnInvalidDate(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)

	if _, err := service.ExportRows(context.Background(), "bukan-tanggal", "", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("an invalid start returned %v, want a validation error", err)
	}
	if _, err := service.ExportRows(context.Background(), "", "2026-13-40", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("an invalid end returned %v, want a validation error", err)
	}
}

// The unit filter is what the dropdown sends, and an empty one means every
// machine on the site.
func TestFuelKeluarExportFiltersByUnit(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)
	seedDispenseFor(t, store, "FO-1", "2026-09-02", "exc01", 150)
	seedDispenseFor(t, store, "FO-2", "2026-09-02", "bld03", 200)

	// The dropdown sends the id as the register holds it; matching must not
	// turn on the case it was typed in.
	one, err := service.ExportRows(context.Background(), "", "", "BLD03")
	if err != nil {
		t.Fatalf("export one unit: %v", err)
	}
	if len(one.Rows) != 1 || !strings.EqualFold(one.Rows[0].IDUnit, "bld03") {
		t.Fatalf("the unit filter returned %+v", one.Rows)
	}
	if one.IDUnit != "BLD03" {
		t.Fatalf("IDUnit = %q, want the filter echoed back for the page", one.IDUnit)
	}
}
