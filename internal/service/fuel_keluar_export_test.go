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

	all, err := service.ExportRows(context.Background(), "", "", nil)
	if err != nil {
		t.Fatalf("export all: %v", err)
	}
	if len(all.Rows) != 2 {
		t.Fatalf("export all returned %d rows, want 2", len(all.Rows))
	}

	august, err := service.ExportRows(context.Background(), "2026-08-01", "2026-08-31", nil)
	if err != nil {
		t.Fatalf("export august: %v", err)
	}
	if len(august.Rows) != 1 || august.Rows[0].Tanggal != "2026-08-07" {
		t.Fatalf("export august returned %+v", august.Rows)
	}

	open, err := service.ExportRows(context.Background(), "2026-09-01", "", nil)
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

	report, err := service.ExportRows(context.Background(), "2026-08-31", "2026-08-01", nil)
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

	if _, err := service.ExportRows(context.Background(), "bukan-tanggal", "", nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("an invalid start returned %v, want a validation error", err)
	}
	if _, err := service.ExportRows(context.Background(), "", "2026-13-40", nil); !errors.Is(err, ErrValidation) {
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
	one, err := service.ExportRows(context.Background(), "", "", []string{"BLD03"})
	if err != nil {
		t.Fatalf("export one unit: %v", err)
	}
	if len(one.Rows) != 1 || !strings.EqualFold(one.Rows[0].IDUnit, "bld03") {
		t.Fatalf("the unit filter returned %+v", one.Rows)
	}
	if one.IDUnits[0] != "BLD03" {
		t.Fatalf("IDUnits = %v, want the filter echoed back for the page", one.IDUnits)
	}
}

// The dispensing export narrows to several machines at once.
func TestFuelKeluarExportFiltersBySeveralUnits(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)
	seedDispenseFor(t, store, "FO-1", "2026-09-02", "exc01", 150)
	seedDispenseFor(t, store, "FO-2", "2026-09-02", "bld03", 200)
	seedDispenseFor(t, store, "FO-3", "2026-09-02", "svd05", 90)

	report, err := service.ExportRows(context.Background(), "", "", []string{"BLD03", "SVD05"})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("returned %d rows, want the two machines asked for", len(report.Rows))
	}
	for _, row := range report.Rows {
		if strings.EqualFold(row.IDUnit, "exc01") {
			t.Fatalf("a machine outside the filter was returned: %+v", report.Rows)
		}
	}
}

// A fill's ratio is the litres put in since the last read fill over the hours
// the meter ran between them, so an unread fill's litres go to the next read
// one; the first reading and a meter that did not move have none.
func TestFillFuelRatiosMeasureFromTheFillBefore(t *testing.T) {
	meter := func(value float64) *float64 { return &value }
	rows := []model.FuelKeluar{
		// Listed newest first, as the sheet reads, to show the order is the
		// dates' and not the list's.
		{FuelOutID: "F-5", Tanggal: "2026-09-05", IDUnit: "EXC-01", Liter: 50, HMAlatBerat: meter(1120)},
		{FuelOutID: "F-4", Tanggal: "2026-09-04", IDUnit: "EXC-01", Liter: 30, HMAlatBerat: meter(1120)},
		{FuelOutID: "F-3", Tanggal: "2026-09-03", IDUnit: "EXC-01", Liter: 40},
		{FuelOutID: "F-B", Tanggal: "2026-09-02", IDUnit: "BLD-03", Liter: 90, HMAlatBerat: meter(500)},
		{FuelOutID: "F-2", Tanggal: "2026-09-02", IDUnit: "exc-01", Liter: 100, HMAlatBerat: meter(1110)},
		{FuelOutID: "F-1", Tanggal: "2026-09-01", IDUnit: "EXC-01", Liter: 120, HMAlatBerat: meter(1100)},
	}
	ratios := fillFuelRatios(rows)
	want := map[string]float64{
		"F-2": 10, // 100 L over 10 hours
		"F-4": 7,  // F-3's unread 40 L and its own 30 L over 10 hours
		// F-5: the meter stood still since F-4, so none.
	}
	for i, row := range rows {
		expected, has := want[row.FuelOutID]
		switch {
		case has && (ratios[i] == nil || *ratios[i] != expected):
			t.Fatalf("%s ratio = %v, want %v", row.FuelOutID, ratios[i], expected)
		case !has && ratios[i] != nil:
			t.Fatalf("%s ratio = %v, want none", row.FuelOutID, *ratios[i])
		}
	}
}

// The first fill in a range is measured from the last fill before it, so the
// export's ratio matches the one the whole sheet gives.
func TestFuelKeluarExportRatioReachesBeforeTheRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelKeluarExportService(store)
	meter := func(value float64) *float64 { return &value }
	for _, row := range []model.FuelKeluar{
		{FuelOutID: "F-1", Tanggal: "2026-08-31", IDUnit: "EXC-01", Liter: 100, HMAlatBerat: meter(1000)},
		{FuelOutID: "F-2", Tanggal: "2026-09-01", IDUnit: "EXC-01", Liter: 160, HMAlatBerat: meter(1008)},
		{FuelOutID: "F-3", Tanggal: "2026-09-02", IDUnit: "EXC-01", Liter: 60, HMAlatBerat: meter(1013)},
	} {
		row := row
		if err := store.CreateFuelKeluar(context.Background(), &row); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	report, err := service.ExportRows(context.Background(), "2026-09-01", "2026-09-30", nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(report.Rows) != 2 || len(report.Ratios) != 2 {
		t.Fatalf("rows %d, ratios %d", len(report.Rows), len(report.Ratios))
	}
	got := map[string]float64{}
	for i, row := range report.Rows {
		if report.Ratios[i] != nil {
			got[row.FuelOutID] = *report.Ratios[i]
		}
	}
	if got["F-2"] != 20 || got["F-3"] != 12 {
		t.Fatalf("ratios = %v, want F-2 20 and F-3 12", got)
	}
}
