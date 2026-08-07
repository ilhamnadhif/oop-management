package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newRegisterService(t *testing.T) *AuthService {
	t.Helper()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	return NewAuthService(repository.NewTestRepository(), location, func() time.Time { return now })
}

func validRegisterInput() RegisterInput {
	return RegisterInput{
		TanggalGabung: "2026-08-07",
		NamaLengkap:   "Budi Santoso",
		NRP:           "123456",
		Jabatan:       "Produksi",
		Email:         "budi@example.com",
		Password:      "rahasia123",
		Status:        model.StatusAktif,
	}
}

// The dropdown is only a convenience; a direct POST must not be able to store
// an unlisted position.
func TestRegisterRejectsUnlistedJabatan(t *testing.T) {
	service := newRegisterService(t)
	input := validRegisterInput()
	input.Jabatan = "Direktur Utama"

	if _, err := service.Register(context.Background(), input); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// Casing differences would otherwise split one position into several values in
// the sheet and break any per-position recap.
func TestRegisterNormalizesJabatanCasing(t *testing.T) {
	service := newRegisterService(t)
	input := validRegisterInput()
	input.Jabatan = "spv"

	user, err := service.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Jabatan != "SPV" {
		t.Fatalf("Jabatan = %q, want SPV", user.Jabatan)
	}
}

func TestRegisterAcceptsEveryListedJabatan(t *testing.T) {
	for index, jabatan := range JabatanOptions {
		service := newRegisterService(t)
		input := validRegisterInput()
		input.Jabatan = jabatan

		if _, err := service.Register(context.Background(), input); err != nil {
			t.Fatalf("option %d (%q) rejected: %v", index, jabatan, err)
		}
	}
}

func TestRegisterRejectsNonNumericNRP(t *testing.T) {
	for _, nrp := range []string{"", "12A456", "12 456", "12-456", "١٢٣"} {
		service := newRegisterService(t)
		input := validRegisterInput()
		input.NRP = nrp

		if _, err := service.Register(context.Background(), input); !errors.Is(err, ErrValidation) {
			t.Fatalf("NRP %q: err = %v, want ErrValidation", nrp, err)
		}
	}
}

func TestRegisterAcceptsNumericNRP(t *testing.T) {
	service := newRegisterService(t)
	input := validRegisterInput()
	input.NRP = "000123"

	user, err := service.Register(context.Background(), input)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.NRP != "000123" {
		t.Fatalf("NRP = %q, want 000123 preserved", user.NRP)
	}
}
