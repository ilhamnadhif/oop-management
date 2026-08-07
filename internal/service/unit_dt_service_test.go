package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func TestNormalizeNopolAcceptsValidPlates(t *testing.T) {
	cases := map[string]string{
		"B 1234 ABC":     "B 1234 ABC",
		"b 1234 abc":     "B 1234 ABC",
		"BK 1 A":         "BK 1 A",
		"  D  567  XY  ": "D 567 XY",
		"DK 9999 ZZZ":    "DK 9999 ZZZ",
	}
	for input, want := range cases {
		got, err := NormalizeNopol(input)
		if err != nil {
			t.Fatalf("NormalizeNopol(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeNopol(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeNopolRejectsBadPlates(t *testing.T) {
	for _, input := range []string{
		"",
		"B1234ABC",    // no separators
		"ABC 1234 AB", // 3 leading letters
		"B 12345 ABC", // 5 digits
		"B 1234 ABCD", // 4 trailing letters
		"B 1234",      // missing suffix
		"1234 B ABC",  // wrong order
		"B 1234 AB1",  // digit in suffix
	} {
		if _, err := NormalizeNopol(input); !errors.Is(err, ErrValidation) {
			t.Fatalf("NormalizeNopol(%q) accepted the plate", input)
		}
	}
}

func newUnitDTFixture(t *testing.T) (*UnitDTService, *repository.TestRepository, *model.User) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	service := NewUnitDTService(store, location, func() time.Time { return now })
	user := &model.User{UserID: "usr_1", NamaLengkap: "Budi", StatusPengguna: model.StatusAktif}
	return service, store, user
}

func validUnitInput(t *testing.T) UnitDTInput {
	t.Helper()
	return UnitDTInput{
		Nopol:      "B 1234 ABC",
		Panjang:    "7.5",
		Lebar:      "2.4",
		Tinggi:     "1.8",
		Driver:     "Slamet",
		Keterangan: "DT KECIL",
		Foto:       testPhoto(t),
	}
}

func TestCreateUnitDTStoresNormalizedValues(t *testing.T) {
	service, store, user := newUnitDTFixture(t)
	input := validUnitInput(t)
	input.Nopol = "b 1234 abc"
	// Indonesian locales type a decimal comma.
	input.Lebar = "2,4"

	unit, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unit.Nopol != "B 1234 ABC" {
		t.Fatalf("Nopol = %q, want B 1234 ABC", unit.Nopol)
	}
	if unit.Lebar != 2.4 {
		t.Fatalf("Lebar = %v, want 2.4", unit.Lebar)
	}
	if unit.CreatedByID != "usr_1" {
		t.Fatalf("CreatedByID = %q, want usr_1", unit.CreatedByID)
	}

	stored := store.UnitDTList()
	if len(stored) != 1 || stored[0].Nopol != "B 1234 ABC" {
		t.Fatalf("stored units = %+v", stored)
	}
}

// One truck must not end up as several rows with conflicting dimensions.
func TestCreateUnitDTRejectsDuplicateNopol(t *testing.T) {
	service, _, user := newUnitDTFixture(t)
	if _, err := service.Create(context.Background(), user, validUnitInput(t)); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := validUnitInput(t)
	second.Nopol = "b  1234  abc" // same plate, sloppier spelling
	if _, err := service.Create(context.Background(), user, second); !errors.Is(err, ErrDuplicateUnitDT) {
		t.Fatalf("err = %v, want ErrDuplicateUnitDT", err)
	}
}

func TestCreateUnitDTRejectsBadDimensions(t *testing.T) {
	service, _, user := newUnitDTFixture(t)

	for name, mutate := range map[string]func(*UnitDTInput){
		"zero":          func(in *UnitDTInput) { in.Panjang = "0" },
		"negative":      func(in *UnitDTInput) { in.Lebar = "-1" },
		"negative zero": func(in *UnitDTInput) { in.Tinggi = "-0.0" },
		"not a number":  func(in *UnitDTInput) { in.Tinggi = "tinggi" },
		"empty":         func(in *UnitDTInput) { in.Lebar = "" },
		// ParseFloat accepts these, and NaN defeats a plain `<= 0` test.
		"infinity": func(in *UnitDTInput) { in.Panjang = "Inf" },
		"nan":      func(in *UnitDTInput) { in.Lebar = "NaN" },
	} {
		input := validUnitInput(t)
		mutate(&input)
		if _, err := service.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

// There is no upper bound: an oversized unit is a data-entry question, not
// something the app should refuse to record.
func TestCreateUnitDTAcceptsLargeDimensions(t *testing.T) {
	service, _, user := newUnitDTFixture(t)
	input := validUnitInput(t)
	input.Panjang = "700"
	input.Lebar = "12.75"
	input.Tinggi = "9999"

	unit, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unit.Panjang != 700 || unit.Tinggi != 9999 {
		t.Fatalf("unexpected dimensions: %+v", unit)
	}
}

func TestUnitIDNumbersSequentiallyWithinTheYear(t *testing.T) {
	service, store, user := newUnitDTFixture(t)

	next, err := service.NextUnitID(context.Background())
	if err != nil {
		t.Fatalf("next id: %v", err)
	}
	if next != "UNT-2026-0001" {
		t.Fatalf("first id = %q, want UNT-2026-0001", next)
	}

	plates := []string{"B 1234 ABC", "B 4321 XYZ", "D 777 QQ"}
	for index, plate := range plates {
		input := validUnitInput(t)
		input.Nopol = plate
		unit, err := service.Create(context.Background(), user, input)
		if err != nil {
			t.Fatalf("create %s: %v", plate, err)
		}
		want := fmt.Sprintf("UNT-2026-%04d", index+1)
		if unit.UnitID != want {
			t.Fatalf("unit %d id = %q, want %q", index+1, unit.UnitID, want)
		}
	}

	if next, err = service.NextUnitID(context.Background()); err != nil || next != "UNT-2026-0004" {
		t.Fatalf("next id = %q (err %v), want UNT-2026-0004", next, err)
	}
	if len(store.UnitDTList()) != len(plates) {
		t.Fatalf("stored %d units, want %d", len(store.UnitDTList()), len(plates))
	}
}

// The counter is the highest sequence in use, not a row count, so removing a
// row cannot make the next ID collide with a surviving one.
func TestUnitIDUsesHighestSequenceNotRowCount(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	service := NewUnitDTService(store, location, func() time.Time { return now })

	for _, unitID := range []string{"UNT-2026-0001", "UNT-2026-0009"} {
		if err := store.CreateUnitDT(context.Background(), &model.UnitDT{UnitID: unitID}); err != nil {
			t.Fatalf("seed %s: %v", unitID, err)
		}
	}

	next, err := service.NextUnitID(context.Background())
	if err != nil {
		t.Fatalf("next id: %v", err)
	}
	if next != "UNT-2026-0010" {
		t.Fatalf("next id = %q, want UNT-2026-0010", next)
	}
}

// The year is in the ID so the sequence can restart each year.
func TestUnitIDRestartsEachYear(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	current := time.Date(2026, 12, 31, 23, 0, 0, 0, location)
	service := NewUnitDTService(store, location, func() time.Time { return current })

	input := validUnitInput(t)
	unit, err := service.Create(context.Background(), &model.User{UserID: "usr_1"}, input)
	if err != nil {
		t.Fatalf("create in 2026: %v", err)
	}
	if unit.UnitID != "UNT-2026-0001" {
		t.Fatalf("2026 id = %q", unit.UnitID)
	}

	current = time.Date(2027, 1, 1, 0, 30, 0, 0, location)
	next := validUnitInput(t)
	next.Nopol = "B 4321 XYZ"
	unit, err = service.Create(context.Background(), &model.User{UserID: "usr_1"}, next)
	if err != nil {
		t.Fatalf("create in 2027: %v", err)
	}
	if unit.UnitID != "UNT-2027-0001" {
		t.Fatalf("2027 id = %q, want UNT-2027-0001", unit.UnitID)
	}
}

func TestCreateUnitDTRequiresDriver(t *testing.T) {
	service, _, user := newUnitDTFixture(t)

	noDriver := validUnitInput(t)
	noDriver.Driver = "   "
	if _, err := service.Create(context.Background(), user, noDriver); !errors.Is(err, ErrValidation) {
		t.Fatalf("missing driver: err = %v, want ErrValidation", err)
	}
}

// The photo is optional, but a value that is present must be one we produced.
func TestCreateUnitDTPhotoIsOptionalButValidated(t *testing.T) {
	service, store, user := newUnitDTFixture(t)

	noPhoto := validUnitInput(t)
	noPhoto.Foto = ""
	unit, err := service.Create(context.Background(), user, noPhoto)
	if err != nil {
		t.Fatalf("create without photo: %v", err)
	}
	if unit.Foto != "" {
		t.Fatalf("Foto = %.40q, want empty", unit.Foto)
	}

	junk := validUnitInput(t)
	junk.Nopol = "B 4321 XYZ"
	junk.Foto = "not-a-data-url"
	if _, err := service.Create(context.Background(), user, junk); !errors.Is(err, ErrInvalidPhoto) {
		t.Fatalf("junk photo: err = %v, want ErrInvalidPhoto", err)
	}
	if len(store.UnitDTList()) != 1 {
		t.Fatalf("stored %d units, want 1", len(store.UnitDTList()))
	}
}

func TestCreateUnitDTKeteranganDefaultsAndValidates(t *testing.T) {
	service, _, user := newUnitDTFixture(t)

	empty := validUnitInput(t)
	empty.Keterangan = ""
	unit, err := service.Create(context.Background(), user, empty)
	if err != nil {
		t.Fatalf("create without keterangan: %v", err)
	}
	if unit.Keterangan != DefaultKeterangan {
		t.Fatalf("Keterangan = %q, want %q", unit.Keterangan, DefaultKeterangan)
	}

	lower := validUnitInput(t)
	lower.Nopol = "B 4321 XYZ"
	lower.Keterangan = "dt besar"
	unit, err = service.Create(context.Background(), user, lower)
	if err != nil {
		t.Fatalf("create with lowercase keterangan: %v", err)
	}
	if unit.Keterangan != "DT BESAR" {
		t.Fatalf("Keterangan = %q, want DT BESAR", unit.Keterangan)
	}

	unlisted := validUnitInput(t)
	unlisted.Nopol = "B 5555 QQQ"
	unlisted.Keterangan = "DT SEDANG"
	if _, err := service.Create(context.Background(), user, unlisted); !errors.Is(err, ErrValidation) {
		t.Fatalf("unlisted keterangan: err = %v, want ErrValidation", err)
	}
}
