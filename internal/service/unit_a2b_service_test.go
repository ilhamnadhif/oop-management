package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newUnitA2BFixture(t *testing.T) (*UnitA2BService, *repository.TestRepository, *model.User) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	service := NewUnitA2BService(store, location, func() time.Time { return now })
	user := &model.User{UserID: "usr_1", NamaLengkap: "Budi", StatusPengguna: model.StatusAktif}
	return service, store, user
}

func validUnitA2BInput(t *testing.T) UnitA2BInput {
	t.Helper()
	return UnitA2BInput{
		Tanggal:     "2026-08-07",
		IDUnit:      "EXCA-01",
		NamaUnit:    "Excavator",
		MerekType:   "Komatsu PC200",
		FuelStorage: "400",
		FRUnit:      "8.5",
		Lokasi:      "Blok A",
		HMAwal:      "1200.5",
		Foto:        testPhoto(t),
	}
}

func TestCreateUnitA2BStoresTheUnit(t *testing.T) {
	service, store, user := newUnitA2BFixture(t)

	unit, err := service.Create(context.Background(), user, validUnitA2BInput(t))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unit.NoUrut != 1 {
		t.Fatalf("NoUrut = %d, want 1", unit.NoUrut)
	}
	if unit.IDUnit != "EXCA-01" || unit.NamaUnit != "Excavator" || unit.MerekType != "Komatsu PC200" {
		t.Fatalf("unexpected unit: %+v", unit)
	}
	if unit.FuelStorage != 400 || unit.FRUnit != 8.5 || unit.HMAwal != 1200.5 {
		t.Fatalf("numeric fields wrong: %+v", unit)
	}
	if unit.CreatedByID != "usr_1" {
		t.Fatalf("CreatedByID = %q", unit.CreatedByID)
	}
	if len(store.UnitA2BList()) != 1 {
		t.Fatalf("stored %d units, want 1", len(store.UnitA2BList()))
	}
}

// The identifier is the key operators use, so one unit must not end up as two
// rows under different spellings.
func TestCreateUnitA2BRejectsDuplicateID(t *testing.T) {
	service, store, user := newUnitA2BFixture(t)
	if _, err := service.Create(context.Background(), user, validUnitA2BInput(t)); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := validUnitA2BInput(t)
	second.IDUnit = "  exca-01  "
	if _, err := service.Create(context.Background(), user, second); !errors.Is(err, ErrDuplicateUnitA2B) {
		t.Fatalf("err = %v, want ErrDuplicateUnitA2B", err)
	}
	if len(store.UnitA2BList()) != 1 {
		t.Fatalf("stored %d units, want 1", len(store.UnitA2BList()))
	}
}

func TestUnitA2BNumbersRunSequentially(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)

	next, err := service.NextNumber(context.Background())
	if err != nil || next != 1 {
		t.Fatalf("NextNumber = %d (err %v), want 1", next, err)
	}
	for i, id := range []string{"EXCA-01", "DT-05", "DOZER-02"} {
		input := validUnitA2BInput(t)
		input.IDUnit = id
		unit, err := service.Create(context.Background(), user, input)
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if unit.NoUrut != i+1 {
			t.Fatalf("%s got number %d, want %d", id, unit.NoUrut, i+1)
		}
	}
	if next, err = service.NextNumber(context.Background()); err != nil || next != 4 {
		t.Fatalf("NextNumber = %d (err %v), want 4", next, err)
	}
}

func TestCreateUnitA2BRequiresText(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)

	for name, mutate := range map[string]func(*UnitA2BInput){
		"id unit":    func(in *UnitA2BInput) { in.IDUnit = "  " },
		"nama unit":  func(in *UnitA2BInput) { in.NamaUnit = "" },
		"merek type": func(in *UnitA2BInput) { in.MerekType = " " },
		"lokasi":     func(in *UnitA2BInput) { in.Lokasi = "" },
		"tanggal":    func(in *UnitA2BInput) { in.Tanggal = "07/08/2026" },
	} {
		input := validUnitA2BInput(t)
		mutate(&input)
		if _, err := service.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

func TestCreateUnitA2BValidatesNumbers(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)

	for name, mutate := range map[string]func(*UnitA2BInput){
		"fuel zero":     func(in *UnitA2BInput) { in.FuelStorage = "0" },
		"fuel negative": func(in *UnitA2BInput) { in.FuelStorage = "-1" },
		"fuel empty":    func(in *UnitA2BInput) { in.FuelStorage = "" },
		"fuel text":     func(in *UnitA2BInput) { in.FuelStorage = "penuh" },
		"fr zero":       func(in *UnitA2BInput) { in.FRUnit = "0" },
		"fr code":       func(in *UnitA2BInput) { in.FRUnit = "FR-01" },
		"hm negative":   func(in *UnitA2BInput) { in.HMAwal = "-5" },
		"hm empty":      func(in *UnitA2BInput) { in.HMAwal = "" },
		// ParseFloat accepts these, and NaN defeats every comparison.
		"fuel infinity": func(in *UnitA2BInput) { in.FuelStorage = "Inf" },
		"hm nan":        func(in *UnitA2BInput) { in.HMAwal = "NaN" },
	} {
		input := validUnitA2BInput(t)
		mutate(&input)
		if _, err := service.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

// A brand new machine legitimately reads zero hours, unlike the other figures.
func TestCreateUnitA2BAcceptsZeroHourMeter(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)
	input := validUnitA2BInput(t)
	input.HMAwal = "0"

	unit, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unit.HMAwal != 0 {
		t.Fatalf("HMAwal = %v, want 0", unit.HMAwal)
	}
}

func TestCreateUnitA2BAcceptsDecimalComma(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)
	input := validUnitA2BInput(t)
	input.FRUnit = "8,5"

	unit, err := service.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if unit.FRUnit != 8.5 {
		t.Fatalf("FRUnit = %v, want 8.5", unit.FRUnit)
	}
}

// The photo is optional, but a value that is present must be one we produced.
func TestCreateUnitA2BPhotoIsOptionalButValidated(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)

	noPhoto := validUnitA2BInput(t)
	noPhoto.Foto = ""
	unit, err := service.Create(context.Background(), user, noPhoto)
	if err != nil {
		t.Fatalf("create without photo: %v", err)
	}
	if unit.Foto != "" {
		t.Fatalf("Foto = %.40q, want empty", unit.Foto)
	}

	junk := validUnitA2BInput(t)
	junk.IDUnit = "DT-09"
	junk.Foto = "not-a-data-url"
	if _, err := service.Create(context.Background(), user, junk); !errors.Is(err, ErrInvalidPhoto) {
		t.Fatalf("junk photo: err = %v, want ErrInvalidPhoto", err)
	}
}

// Merek and lokasi suggest what the register holds, accept new values, and
// settle on one spelling.
func TestUnitA2BMerekSuggestionsComeFromTheRegister(t *testing.T) {
	service, _, user := newUnitA2BFixture(t)

	if _, err := service.Create(context.Background(), user, validUnitA2BInput(t)); err != nil {
		t.Fatalf("first create: %v", err)
	}

	options, err := service.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if !containsValue(options.MerekType, "Komatsu PC200") {
		t.Fatalf("merek suggestions %v do not include the value just used", options.MerekType)
	}
	if !containsValue(options.Lokasi, "Blok A") {
		t.Fatalf("lokasi suggestions %v do not include the value just used", options.Lokasi)
	}

	sameMake := validUnitA2BInput(t)
	sameMake.IDUnit = "EXCA-02"
	sameMake.MerekType = "  komatsu   pc200 "
	unit, err := service.Create(context.Background(), user, sameMake)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if unit.MerekType != "Komatsu PC200" {
		t.Fatalf("MerekType = %q, want the existing spelling", unit.MerekType)
	}

	newMake := validUnitA2BInput(t)
	newMake.IDUnit = "DZR-01"
	newMake.MerekType = "Sakai SV526D"
	unit, err = service.Create(context.Background(), user, newMake)
	if err != nil {
		t.Fatalf("third create: %v", err)
	}
	if unit.MerekType != "Sakai SV526D" {
		t.Fatalf("MerekType = %q, want Sakai SV526D", unit.MerekType)
	}
}

func containsValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
