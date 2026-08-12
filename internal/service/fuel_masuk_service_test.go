package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

func fuelTestPhotos(t *testing.T) [4]string {
	t.Helper()
	value := testPhoto(t)
	return [4]string{value, value, value, value}
}

func fuelTestInput(t *testing.T) FuelMasukInput {
	t.Helper()
	return FuelMasukInput{
		TanggalInput: "2026-08-07T09:30",
		Vendor:       "PT Sumber Energi",
		Driver:       "Slamet",
		Nopol:        "B 1234 ABC",
		JumlahLiter:  "8010",
		Keterangan:   model.FuelKeteranganSesuai,
		Photos:       fuelTestPhotos(t),
	}
}

func newFuelService(store repository.Store, now time.Time) *FuelMasukService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewFuelMasukService(store, location, func() time.Time { return now })
}

func fuelTestUser(jabatan string) *model.User {
	return &model.User{
		UserID:         "user-1",
		NamaLengkap:    "Budi Santoso",
		Jabatan:        jabatan,
		StatusPengguna: model.StatusAktif,
	}
}

// A delivery is approved the moment it is recorded: the four photos are the
// check, and a status that only ever had one value is not a decision.
func TestFuelMasukIsApprovedOnSave(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), fuelTestInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.StatusApproval != model.FuelStatusDisetujui {
		t.Fatalf("status = %q, want %q", saved.StatusApproval, model.FuelStatusDisetujui)
	}
	// Nobody signed it off, so nobody's name is against it.
	if saved.DiprosesOleh != "" || saved.DiprosesPada != nil || saved.CatatanApproval != "" {
		t.Fatalf("an automatic status carries a decision trail: %+v", saved)
	}
}

// The list reads newest first, which is the order the page wants.
func TestFuelMasukListsNewestFirst(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, location)
	service := NewFuelMasukService(store, location, func() time.Time { return now })
	user := fuelTestUser("Logistik")

	older := fuelTestInput(t)
	older.TanggalInput = "2026-08-05T08:00"
	if _, err := service.Create(context.Background(), user, older); err != nil {
		t.Fatalf("create older: %v", err)
	}
	newer := fuelTestInput(t)
	newer.TanggalInput = "2026-08-06T08:00"
	second, err := service.Create(context.Background(), user, newer)
	if err != nil {
		t.Fatalf("create newer: %v", err)
	}

	rows, err := service.List(context.Background(), FuelMasukFilters{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 2 || rows[0].FuelID != second.FuelID {
		t.Fatalf("the newest delivery is not first: %+v", rows)
	}
}

// The transaction number says which day the delivery was recorded and counts
// within it, so two deliveries on one day cannot collide.
func TestFuelMasukNumbersDeliveriesWithinADay(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, location)
	service := NewFuelMasukService(store, location, func() time.Time { return now })
	user := fuelTestUser("Logistik")

	first, err := service.Create(context.Background(), user, fuelTestInput(t))
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := service.Create(context.Background(), user, fuelTestInput(t))
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.FuelID != "FUEL-20260807-0001" || second.FuelID != "FUEL-20260807-0002" {
		t.Fatalf("numbers = %q, %q", first.FuelID, second.FuelID)
	}

	now = now.AddDate(0, 0, 1)
	nextDay, err := service.Create(context.Background(), user, fuelTestInput(t))
	if err != nil {
		t.Fatalf("create next day: %v", err)
	}
	if nextDay.FuelID != "FUEL-20260808-0001" {
		t.Fatalf("the count did not restart with the day: %q", nextDay.FuelID)
	}
}

// A shortfall larger than the delivery itself is a typo, not a reading.
func TestFuelMasukRefusesAShortfallLargerThanTheDelivery(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	input := fuelTestInput(t)
	input.Keterangan = model.FuelKeteranganTidakSesuai
	input.LiterTidakSesuai = "9000"

	_, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
	if !strings.Contains(err.Error(), "melebihi jumlah fuel masuk") {
		t.Fatalf("the message does not say why: %v", err)
	}
}

// Keterangan is a closed set, so a direct call cannot store a third word.
func TestFuelMasukRefusesAnUnknownKeterangan(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	input := fuelTestInput(t)
	input.Keterangan = "kurang lebih"

	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), input); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// The stored photo comes back by transaction number and kind, and an unknown
// kind is a miss rather than a guess.
func TestFuelMasukPhotoIsFoundByKind(t *testing.T) {
	store := repository.NewTestRepository()
	service := newFuelService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	created, err := service.Create(context.Background(), fuelTestUser("Logistik"), fuelTestInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	value, err := service.Photo(context.Background(), created.FuelID, "flowmeter")
	if err != nil {
		t.Fatalf("read photo: %v", err)
	}
	if err := photo.ValidateDataURL(value); err != nil {
		t.Fatalf("the stored photo is not a usable image: %v", err)
	}
	if _, err := service.Photo(context.Background(), created.FuelID, "tangki-tengah"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("error = %v, want not found", err)
	}
}
