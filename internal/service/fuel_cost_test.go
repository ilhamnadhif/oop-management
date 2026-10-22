package service

import (
	"context"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedDelivery(t *testing.T, store *repository.TestRepository, id, tanggal string, liter, harga float64) {
	t.Helper()
	day, err := time.Parse("2006-01-02", tanggal)
	if err != nil {
		t.Fatalf("seed delivery date: %v", err)
	}
	location := time.FixedZone("WIB", 7*60*60)
	if err := store.CreateFuelMasuk(context.Background(), &model.FuelMasuk{
		FuelID:       "FUEL-" + id,
		TanggalInput: time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, location),
		Vendor:       "Vendor", Driver: "Driver", Nopol: "B 1 A",
		JumlahLiter: liter, HargaPerLiter: harga,
		StatusApproval: model.FuelStatusDisetujui,
	}); err != nil {
		t.Fatalf("seed delivery %s: %v", id, err)
	}
}

func newFuelCostService(store repository.Store) *UnitOverviewService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewUnitOverviewService(store, location,
		func() time.Time { return time.Date(2026, 9, 30, 10, 0, 0, 0, location) })
}

// The client's own example. Ten thousand litres arrive at twenty thousand a
// litre and are budgeted to last the month; six thousand are burned. The month
// is charged for the six thousand, not for the purchase - and the rest is still
// in the tank, which is why the next month is not free.
func TestFuelCostChargesWhatWasBurnedNotWhatWasBought(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-01", 10000, 20000)
	seedDispense(t, store, "exc01", "2026-08-10", 6000, nil)
	seedDispense(t, store, "exc01", "2026-09-05", 4000, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-09-30")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(report.Months) != 2 {
		t.Fatalf("covered %d months, want August and September", len(report.Months))
	}
	august, september := report.Months[0], report.Months[1]
	if august.Biaya != 120_000_000 {
		t.Fatalf("august = %v, want the six thousand litres burned", august.Biaya)
	}
	if august.MasukBiaya != 200_000_000 {
		t.Fatalf("august purchase = %v, want the whole delivery", august.MasukBiaya)
	}
	if august.SisaLiter != 4000 || august.SisaNilai != 80_000_000 {
		t.Fatalf("august closes at %v litres worth %v, want the rest carried over",
			august.SisaLiter, august.SisaNilai)
	}
	// The month the fuel was actually burned in is charged for it, even though
	// nothing was bought that month.
	if september.Biaya != 80_000_000 || september.MasukBiaya != 0 {
		t.Fatalf("september = %v spent on %v bought, want the carried fuel charged",
			september.Biaya, september.MasukBiaya)
	}
	if report.Biaya != 200_000_000 {
		t.Fatalf("total = %v, want the whole delivery burned across the two months", report.Biaya)
	}
	if report.SisaLiter != 0 {
		t.Fatalf("the tank closes at %v litres, want empty", report.SisaLiter)
	}
}

// The tank is one tank and the fuel in it is mixed, so a dispense is charged at
// the average of what is in it - not at the price of the newest delivery.
func TestFuelCostUsesTheRunningAverageOfTheTank(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-01", 10000, 20000)
	seedDelivery(t, store, "2", "2026-08-02", 5000, 26000)
	seedDispense(t, store, "exc01", "2026-08-10", 3000, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// (10000 * 20000 + 5000 * 26000) / 15000 = 22000 a litre.
	if report.HargaRataRata != 22000 {
		t.Fatalf("average = %v, want 22000", report.HargaRataRata)
	}
	if report.Biaya != 66_000_000 {
		t.Fatalf("cost = %v, want 3000 litres at the mixed price", report.Biaya)
	}
}

// A delivery late in the month does not change what was burned before it
// arrived. This is the whole reason for the running average over the last
// price paid.
func TestFuelCostIsNotRewrittenByALaterDelivery(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-01", 10000, 20000)
	seedDispense(t, store, "exc01", "2026-08-10", 2000, nil)
	seedDelivery(t, store, "2", "2026-08-28", 5000, 30000)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.Biaya != 40_000_000 {
		t.Fatalf("cost = %v, want the two thousand litres at what they cost then", report.Biaya)
	}
}

// A rejected delivery never arrived, so it is neither stock nor cost.
func TestFuelCostIgnoresARejectedDelivery(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-01", 1000, 20000)
	if err := store.CreateFuelMasuk(context.Background(), &model.FuelMasuk{
		FuelID: "FUEL-REJECTED", TanggalInput: time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC),
		JumlahLiter: 9000, HargaPerLiter: 30000, StatusApproval: model.FuelStatusDitolak,
	}); err != nil {
		t.Fatalf("seed rejected delivery: %v", err)
	}

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.MasukLiter != 1000 || report.SisaLiter != 1000 {
		t.Fatalf("a rejected delivery reached the tank: %v in, %v left",
			report.MasukLiter, report.SisaLiter)
	}
}

// A delivery written before the price was asked for is not free fuel. It is
// costed at what the site was paying then.
func TestFuelCostPricesADeliveryWithNoPriceAtTheDefault(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-01", 1000, 0)
	seedDispense(t, store, "exc01", "2026-08-10", 1000, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.Biaya != 1000*DefaultHargaPerLiter {
		t.Fatalf("cost = %v, want the default price", report.Biaya)
	}
}

// The dispensing sheet was kept before the deliveries were, so the early months
// hold fuel burned out of a tank this app never saw filled. Charging nothing
// for it would be further from the truth than charging the going rate.
func TestFuelCostChargesADispenseWithNoStockBehindIt(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDispense(t, store, "exc01", "2026-08-10", 500, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.Biaya != 500*DefaultHargaPerLiter {
		t.Fatalf("cost = %v, want the fuel charged rather than given away", report.Biaya)
	}
	if report.SisaLiter != 0 {
		t.Fatalf("the tank reads %v litres, want it floored at empty", report.SisaLiter)
	}
}

// Fuel bought before the range is still in the tank when the range opens.
func TestFuelCostCarriesStockIntoTheRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-07-01", 5000, 20000)
	seedDispense(t, store, "exc01", "2026-08-10", 1000, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.MasukLiter != 0 {
		t.Fatalf("a delivery outside the range was counted as arriving in it: %v", report.MasukLiter)
	}
	if report.Biaya != 20_000_000 {
		t.Fatalf("cost = %v, want the carried stock charged at what it cost", report.Biaya)
	}
	if report.SisaLiter != 4000 {
		t.Fatalf("closing tank = %v, want what is left of the carried stock", report.SisaLiter)
	}
}

// A month nothing happened in is still a month, and a gap in a cost report
// reads as a month nobody looked at.
func TestFuelCostCoversEveryMonthInTheRange(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-07-01", 1000, 20000)

	report, err := service.BuildFuelCost(context.Background(), "2026-07-01", "2026-09-30")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(report.Months) != 3 {
		t.Fatalf("covered %d months, want July, August and September", len(report.Months))
	}
	if report.Months[1].Label != "Agustus 2026" {
		t.Fatalf("second month = %q", report.Months[1].Label)
	}
	if report.Months[2].SisaLiter != 1000 {
		t.Fatalf("a quiet month lost the tank: %v", report.Months[2].SisaLiter)
	}
}

// Fuel is put in the tank before it is drawn out of it, so a day that receives
// and dispenses the same litres is not a day dispensing from an empty tank.
func TestFuelCostPutsADeliveryBeforeADispenseOnTheSameDay(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelCostService(store)
	seedDelivery(t, store, "1", "2026-08-10", 1000, 25000)
	seedDispense(t, store, "exc01", "2026-08-10", 1000, nil)

	report, err := service.BuildFuelCost(context.Background(), "2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.Biaya != 25_000_000 {
		t.Fatalf("cost = %v, want the fuel delivered that day charged at its own price", report.Biaya)
	}
}
