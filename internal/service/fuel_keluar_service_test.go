package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedFuelMachine(t *testing.T, store *repository.TestRepository, idUnit, namaUnit string) {
	t.Helper()
	unit := &model.UnitA2B{
		NoUrut: 1, TanggalIn: "2026-08-01", IDUnit: idUnit, NamaUnit: namaUnit,
		MerekType: "Kobelco", FuelStorage: 300, FRUnit: 19.3, Lokasi: "PIT A", HMAwal: 100,
	}
	if err := store.CreateUnitA2B(context.Background(), unit); err != nil {
		t.Fatalf("seed unit a2b: %v", err)
	}
}

func fuelKeluarTestInput(t *testing.T) FuelKeluarInput {
	t.Helper()
	return FuelKeluarInput{
		Tanggal:          "2026-08-03",
		IDUnit:           "exc01",
		HMAwalFlowMeter:  "20",
		HMAkhirFlowMeter: "30",
		Operator:         "kadal",
		Foto:             testPhoto(t),
	}
}

func newFuelKeluarService(store repository.Store, now time.Time) *FuelKeluarService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewFuelKeluarService(store, location, func() time.Time { return now })
}

// The transaction number counts within the day it was recorded.
func TestFuelKeluarNumbersDispensesWithinADay(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 3, 10, 0, 0, 0, location)
	service := NewFuelKeluarService(store, location, func() time.Time { return now })
	user := fuelTestUser("Logistik")

	first, err := service.Create(context.Background(), user, fuelKeluarTestInput(t))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second := fuelKeluarTestInput(t)
	second.HMAwalFlowMeter = "30"
	second.HMAkhirFlowMeter = "45.5"
	saved, err := service.Create(context.Background(), user, second)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.FuelOutID != "FUELOUT-20260803-0001" || saved.FuelOutID != "FUELOUT-20260803-0002" {
		t.Fatalf("numbers = %q, %q", first.FuelOutID, saved.FuelOutID)
	}
	if saved.Liter != 15.5 {
		t.Fatalf("litres = %v, want 15.5", saved.Liter)
	}
}

// The next dispense starts where the pump stopped.
func TestFuelKeluarLastFlowMeterReportsTheClosingReading(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newFuelKeluarService(store, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC))

	last, err := service.LastFlowMeter(context.Background())
	if err != nil {
		t.Fatalf("last flow meter on an empty log: %v", err)
	}
	if last != 0 {
		t.Fatalf("an empty log reported %v", last)
	}

	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), fuelKeluarTestInput(t)); err != nil {
		t.Fatalf("create: %v", err)
	}
	last, err = service.LastFlowMeter(context.Background())
	if err != nil {
		t.Fatalf("last flow meter: %v", err)
	}
	if last != 30 {
		t.Fatalf("last flow meter = %v, want 30", last)
	}
}

// The hour meter of the machine is optional, and a reading of zero is a real
// reading rather than an absent one.
func TestFuelKeluarKeepsTheMachineHourMeterOptional(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newFuelKeluarService(store, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC))
	input := fuelKeluarTestInput(t)
	input.HMAlatBerat = "0"

	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.HMAlatBerat == nil || *saved.HMAlatBerat != 0 {
		t.Fatalf("an explicit zero was dropped: %+v", saved.HMAlatBerat)
	}

	input = fuelKeluarTestInput(t)
	input.HMAwalFlowMeter = "30"
	input.HMAkhirFlowMeter = "40"
	saved, err = service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create without hour meter: %v", err)
	}
	if saved.HMAlatBerat != nil {
		t.Fatalf("an unentered hour meter became %v", *saved.HMAlatBerat)
	}
}

// A negative reading is not something a meter can show.
func TestFuelKeluarRefusesANegativeReading(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newFuelKeluarService(store, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC))
	input := fuelKeluarTestInput(t)
	input.HMAwalFlowMeter = "-5"

	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), input); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// The picker is the A2B register, sorted so the same machine sits in the same
// place every time the form is opened.
func TestFuelKeluarUnitOptionsComeFromTheRegister(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc02", "Bulldozer D6 CAT (Rent)")
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newFuelKeluarService(store, time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC))

	options, err := service.UnitOptions(context.Background())
	if err != nil {
		t.Fatalf("unit options: %v", err)
	}
	if len(options) != 2 {
		t.Fatalf("options = %+v", options)
	}
	if options[0].IDUnit != "exc01" || options[0].NamaUnit != "Excavator PC200 Kobelco (Rent)" {
		t.Fatalf("the list is not sorted by id, or lost the name: %+v", options)
	}
}
