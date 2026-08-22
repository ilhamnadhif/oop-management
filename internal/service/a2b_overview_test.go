package service

import (
	"context"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newA2BOverviewService(store repository.Store, now time.Time) *UnitOverviewService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewUnitOverviewService(store, location, func() time.Time { return now })
}

func seedReading(t *testing.T, store *repository.TestRepository, id, tanggal string, hours, fuel float64, standby []model.HourMeterStandby, breakdown float64) {
	t.Helper()
	total := 0.0
	for _, line := range standby {
		total += line.Menit
	}
	reading := &model.HourMeter{
		HMID: id + "-" + tanggal, Tanggal: tanggal, Shift: "Shift 1",
		IDUnit: id, NamaUnit: "Excavator " + id, Operator: "kadal",
		TotalHM: hours, FuelLiter: fuel,
		TotalStandby: total, Standby: standby,
		TotalBreakdown: breakdown,
	}
	if err := store.CreateHourMeter(context.Background(), reading); err != nil {
		t.Fatalf("seed hour meter: %v", err)
	}
}

// A machine that worked is active; one that only stood still, or was never
// read at all, is standby.
func TestA2BOverviewCountsWhatTheFleetDid(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	seedFuelMachine(t, store, "bul02", "Bulldozer D6")
	seedFuelMachine(t, store, "gen03", "Genset 5 KVA")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-05", 7, 245, []model.HourMeterStandby{{Variable: "ISTIRAHAT", Menit: 60}}, 0)
	// Worked nothing all shift, so it was never used.
	seedReading(t, store, "bul02", "2026-08-06", 0, 0, []model.HourMeterStandby{{Variable: "HUJAN", Menit: 240}}, 480)
	// Outside the range: it must not make gen03 look active.
	seedReading(t, store, "gen03", "2026-07-30", 8, 100, nil, 0)

	overview, err := service.BuildA2B(context.Background(), "2026-08-01", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if overview.TotalUnit != 3 {
		t.Fatalf("total unit = %d, want 3", overview.TotalUnit)
	}
	if overview.UnitActive != 1 {
		t.Fatalf("unit active = %d, want 1", overview.UnitActive)
	}
	if overview.UnitStandby != 2 {
		t.Fatalf("unit standby = %d, want 2", overview.UnitStandby)
	}
	if overview.UnitBreakdown != 1 {
		t.Fatalf("unit breakdown = %d, want 1", overview.UnitBreakdown)
	}
	if len(overview.Units) != 2 {
		t.Fatalf("the table lists %d units, want the two read in range", len(overview.Units))
	}
	if overview.Units[0].IDUnit != "exc01" {
		t.Fatalf("the busiest machine is not first: %+v", overview.Units)
	}
}

// Each machine's figures are recomputed from its totals, so a short shift does
// not weigh as much as a full one.
func TestA2BOverviewRecomputesPerUnitFigures(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	// Three shifts of 720 minutes: 1410 minutes worked, 240 lost to breakdown.
	seedReading(t, store, "exc01", "2026-08-05", 8, 245, nil, 0)
	seedReading(t, store, "exc01", "2026-08-06", 8, 180, nil, 0)
	seedReading(t, store, "exc01", "2026-08-07", 7.5, 200, nil, 240)

	overview, err := service.BuildA2B(context.Background(), "2026-08-01", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unit := overview.Units[0]
	if unit.Shifts != 3 || unit.TotalHM != 23.5 {
		t.Fatalf("totals = %+v", unit)
	}
	// The readings carry a fuel figure of their own, and the column no longer
	// reads it: fuel is what the tank dispensed, and none was dispensed here.
	if unit.Fuel != 0 || unit.FuelRatio != 0 {
		t.Fatalf("fuel = %v ratio = %v, want the dispensing sheet to be the source", unit.Fuel, unit.FuelRatio)
	}
	// 2160 shift minutes, 240 of them breakdown.
	if unit.PA != 88.89 {
		t.Fatalf("PA = %v, want 88.89", unit.PA)
	}
	if unit.UA != 73.44 {
		t.Fatalf("UA = %v, want 73.44", unit.UA)
	}
}

// Stock is a level: everything delivered less everything dispensed, up to the
// end of the range. A rejected delivery never arrived.
func TestA2BOverviewReadsStockAsALevel(t *testing.T) {
	store := repository.NewTestRepository()
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))
	location := time.FixedZone("WIB", 7*60*60)

	deliveries := []struct {
		day    int
		litres float64
		status string
	}{
		{1, 8000, model.FuelStatusDisetujui},
		{9, 8000, model.FuelStatusDisetujui},
		{9, 5000, model.FuelStatusDitolak},
		{20, 4000, model.FuelStatusDisetujui},
	}
	for index, delivery := range deliveries {
		row := &model.FuelMasuk{
			FuelID:         "FUEL-" + time.Date(2026, 8, delivery.day, 8, 0, 0, 0, location).Format("20060102"),
			TanggalInput:   time.Date(2026, 8, delivery.day, 8, 0, 0, 0, location),
			JumlahLiter:    delivery.litres,
			StatusApproval: delivery.status,
		}
		row.FuelID = row.FuelID + string(rune('a'+index))
		if err := store.CreateFuelMasuk(context.Background(), row); err != nil {
			t.Fatalf("seed fuel masuk: %v", err)
		}
	}
	for index, dispense := range []struct {
		day    int
		litres float64
	}{{2, 5400}, {8, 4000}, {25, 1000}} {
		row := &model.FuelKeluar{
			FuelOutID: "FUELOUT-" + string(rune('a'+index)),
			Tanggal:   time.Date(2026, 8, dispense.day, 0, 0, 0, 0, location).Format("2006-01-02"),
			Liter:     dispense.litres,
		}
		if err := store.CreateFuelKeluar(context.Background(), row); err != nil {
			t.Fatalf("seed fuel keluar: %v", err)
		}
	}

	overview, err := service.BuildA2B(context.Background(), "2026-08-01", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if overview.FuelMasuk != 16000 {
		t.Fatalf("fuel masuk = %v, want 16000", overview.FuelMasuk)
	}
	if overview.FuelKeluar != 9400 {
		t.Fatalf("fuel keluar = %v, want 9400", overview.FuelKeluar)
	}
	if overview.StockFuel != 6600 {
		t.Fatalf("stock = %v, want 6600", overview.StockFuel)
	}
}

// The delay panel ranks the reasons and gathers the tail, so the slices still
// add up to the whole.
func TestA2BOverviewRanksTheDelayReasons(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-05", 1, 0, []model.HourMeterStandby{
		{Variable: "HUJAN", Menit: 300},
		{Variable: "ISTIRAHAT", Menit: 120},
		{Variable: "P2H", Menit: 60},
		{Variable: "DEBU", Menit: 30},
		{Variable: "LICIN", Menit: 20},
		{Variable: "SHOLAT", Menit: 10},
		{Variable: "KABUT", Menit: 5},
	}, 0)

	overview, err := service.BuildA2B(context.Background(), "2026-08-01", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if overview.TotalDelayMinutes != 545 {
		t.Fatalf("total delay = %v, want 545", overview.TotalDelayMinutes)
	}
	if len(overview.TopDelay) != 6 {
		t.Fatalf("the panel names %d reasons, want five plus the rest", len(overview.TopDelay))
	}
	if overview.TopDelay[0].Variable != "HUJAN" || overview.TopDelay[0].Menit != 300 {
		t.Fatalf("the worst reason is %+v", overview.TopDelay[0])
	}
	last := overview.TopDelay[len(overview.TopDelay)-1]
	if last.Variable != "LAINNYA" || last.Menit != 15 {
		t.Fatalf("the tail was not gathered: %+v", last)
	}
	share := 0.0
	for _, reason := range overview.TopDelay {
		share += reason.Percent
	}
	if share < 99.9 || share > 100.1 {
		t.Fatalf("the slices add to %v%%", share)
	}
}

// An unreadable range is refused rather than quietly widened.
func TestA2BOverviewRefusesABadRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	if _, err := service.BuildA2B(context.Background(), "kemarin", "2026-08-10", 720); err == nil {
		t.Fatal("a nonsense start date was accepted")
	}
	// Back to front is a slip, not an error: the two dates are swapped.
	overview, err := service.BuildA2B(context.Background(), "2026-08-10", "2026-08-01", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if overview.From != "2026-08-01" || overview.To != "2026-08-10" {
		t.Fatalf("range = %s to %s", overview.From, overview.To)
	}
}

// The two charts read the range day by day: how many machines worked or broke
// that day, and how much fuel moved either way.
func TestA2BOverviewBuildsADailySeries(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	seedFuelMachine(t, store, "bul02", "Bulldozer D6")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))
	location := time.FixedZone("WIB", 7*60*60)

	// Two machines on the 5th, one of them twice; one of them broken.
	seedReading(t, store, "exc01", "2026-08-05", 7, 0, nil, 0)
	seedReading(t, store, "exc01", "2026-08-05", 4, 0, nil, 60)
	seedReading(t, store, "bul02", "2026-08-05", 6, 0, nil, 0)
	seedReading(t, store, "exc01", "2026-08-06", 8, 0, nil, 0)

	if err := store.CreateFuelMasuk(context.Background(), &model.FuelMasuk{
		FuelID: "FUEL-1", TanggalInput: time.Date(2026, 8, 5, 8, 0, 0, 0, location),
		JumlahLiter: 8000, StatusApproval: model.FuelStatusDisetujui,
	}); err != nil {
		t.Fatalf("seed fuel masuk: %v", err)
	}
	if err := store.CreateFuelKeluar(context.Background(), &model.FuelKeluar{
		FuelOutID: "FUELOUT-1", Tanggal: "2026-08-06", Liter: 240,
	}); err != nil {
		t.Fatalf("seed fuel keluar: %v", err)
	}

	overview, err := service.BuildA2B(context.Background(), "2026-08-04", "2026-08-06", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(overview.Series) != 3 {
		t.Fatalf("the series covers %d days, want 3", len(overview.Series))
	}
	// A day with nothing on it still appears, or the chart would close the gap.
	if overview.Series[0].Tanggal != "2026-08-04" || overview.Series[0].UnitActive != 0 {
		t.Fatalf("the quiet day is missing or occupied: %+v", overview.Series[0])
	}
	// A machine read twice in a day counts once.
	fifth := overview.Series[1]
	if fifth.UnitActive != 2 || fifth.UnitBreakdown != 1 {
		t.Fatalf("the busy day = %+v", fifth)
	}
	if fifth.FuelMasuk != 8000 || fifth.FuelKeluar != 0 {
		t.Fatalf("the delivery landed on the wrong day: %+v", fifth)
	}
	sixth := overview.Series[2]
	if sixth.UnitActive != 1 || sixth.FuelKeluar != 240 {
		t.Fatalf("the last day = %+v", sixth)
	}
}

func seedDispense(t *testing.T, store *repository.TestRepository, id, tanggal string, liter float64, hm *float64) {
	t.Helper()
	row := &model.FuelKeluar{
		FuelOutID: "FUELOUT-" + id + "-" + tanggal, Tanggal: tanggal,
		IDUnit: id, NamaUnit: "Excavator " + id, Liter: liter, HMAlatBerat: hm,
	}
	if err := store.CreateFuelKeluar(context.Background(), row); err != nil {
		t.Fatalf("seed fuel keluar: %v", err)
	}
}

func hmAt(value float64) *float64 { return &value }

// The litres a machine burned are what came out of the tank into it, and the
// hours they bought are the distance its own meter moved between fills. The
// last fill before the range is the mark the first fill in range is measured
// from: the fuel in view was burned over those hours, not over the ones between
// the fills that happen to fall inside the dates.
func TestA2BOverviewReadsFuelFromWhatWasDispensed(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-06", 8, 0, nil, 0)
	seedDispense(t, store, "exc01", "2026-08-03", 90, hmAt(1195))
	seedDispense(t, store, "exc01", "2026-08-06", 70, hmAt(1201))
	seedDispense(t, store, "exc01", "2026-08-08", 50, hmAt(1205))
	seedDispense(t, store, "exc01", "2026-08-10", 60, hmAt(1210.4))

	overview, err := service.BuildA2B(context.Background(), "2026-08-05", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unit := overview.Units[0]
	// The 90 litres from before the range are not in view; the meter reading
	// they were taken at is.
	if unit.Fuel != 180 {
		t.Fatalf("fuel = %v, want 180", unit.Fuel)
	}
	// 180 litres over 1210.4 - 1195 = 15.4 hours.
	if unit.FuelRatio != 11.69 {
		t.Fatalf("fuel ratio = %v, want 11.69", unit.FuelRatio)
	}
}

// Nothing before the range means nothing to measure the first fill from, so it
// becomes the mark itself rather than being thrown away.
func TestA2BOverviewMeasuresFromTheFirstFillWithoutAnEarlierOne(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-06", 8, 0, nil, 0)
	seedDispense(t, store, "exc01", "2026-08-06", 70, hmAt(1201))
	seedDispense(t, store, "exc01", "2026-08-10", 60, hmAt(1210.4))

	overview, err := service.BuildA2B(context.Background(), "2026-08-05", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unit := overview.Units[0]
	if unit.Fuel != 130 {
		t.Fatalf("fuel = %v, want 130", unit.Fuel)
	}
	// 130 litres over 1210.4 - 1201 = 9.4 hours.
	if unit.FuelRatio != 13.83 {
		t.Fatalf("fuel ratio = %v, want 13.83", unit.FuelRatio)
	}
}

// The meter reading is optional on the dispensing form. A fill nobody read the
// machine's meter at still emptied litres into it, so it counts toward the fuel
// while the hours are measured across the fills that do carry a reading.
func TestA2BOverviewCountsFuelDispensedWithoutAMeterReading(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-06", 8, 0, nil, 0)
	seedDispense(t, store, "exc01", "2026-08-06", 40, nil)
	seedDispense(t, store, "exc01", "2026-08-07", 70, hmAt(1201))
	seedDispense(t, store, "exc01", "2026-08-10", 60, hmAt(1210.4))

	overview, err := service.BuildA2B(context.Background(), "2026-08-05", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unit := overview.Units[0]
	if unit.Fuel != 170 {
		t.Fatalf("fuel = %v, want 170", unit.Fuel)
	}
	// 170 litres over 1210.4 - 1201 = 9.4 hours.
	if unit.FuelRatio != 18.09 {
		t.Fatalf("fuel ratio = %v, want 18.09", unit.FuelRatio)
	}
}

// One fill and nothing to measure it from leaves no hours to divide by. The
// litres are still reported; the ratio waits for the next fill.
func TestA2BOverviewLeavesTheRatioAtZeroWithoutTwoMeterReadings(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200")
	service := newA2BOverviewService(store, time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC))

	seedReading(t, store, "exc01", "2026-08-06", 8, 0, nil, 0)
	seedDispense(t, store, "exc01", "2026-08-06", 70, hmAt(1201))

	overview, err := service.BuildA2B(context.Background(), "2026-08-05", "2026-08-10", 720)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	unit := overview.Units[0]
	if unit.Fuel != 70 {
		t.Fatalf("fuel = %v, want 70", unit.Fuel)
	}
	if unit.FuelRatio != 0 {
		t.Fatalf("fuel ratio = %v, want 0", unit.FuelRatio)
	}
}
