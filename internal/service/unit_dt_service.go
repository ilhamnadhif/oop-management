package service

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// nopolPattern is the Indonesian plate layout requested for this register:
// 1-2 letters, 1-4 digits, 1-3 letters, separated by single spaces.
var nopolPattern = regexp.MustCompile(`^[A-Z]{1,2} [0-9]{1,4} [A-Z]{1,3}$`)

// unitIDPrefix builds "UNT-2026-". The sequence restarts each year, which is
// the only reason the year is part of the ID at all.
func unitIDPrefix(year int) string {
	return fmt.Sprintf("UNT-%04d-", year)
}

func formatUnitID(year, sequence int) string {
	return fmt.Sprintf("%s%04d", unitIDPrefix(year), sequence)
}

// NextUnitID reports the ID the next saved unit would receive. The form shows
// it as a preview; the authoritative value is assigned inside Create, under the
// same lock as the uniqueness check.
func (s *UnitDTService) NextUnitID(ctx context.Context) (string, error) {
	year := s.now().In(s.location).Year()
	highest, err := s.store.MaxUnitDTSequence(ctx, unitIDPrefix(year))
	if err != nil {
		return "", err
	}
	return formatUnitID(year, highest+1), nil
}

// KeteranganOptions is the closed set of unit classes. The form renders it and
// the service enforces it, so a direct POST cannot store a free-text value.
var KeteranganOptions = []string{"DT KECIL", "DT BESAR"}

// DefaultKeterangan is preselected in the form and used when the field arrives
// empty.
const DefaultKeterangan = "DT KECIL"

func canonicalKeterangan(value string) (string, bool) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return DefaultKeterangan, true
	}
	for _, option := range KeteranganOptions {
		if strings.EqualFold(option, value) {
			return option, true
		}
	}
	return "", false
}

type UnitDTService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex
}

type UnitDTInput struct {
	Nopol      string
	Panjang    string
	Lebar      string
	Tinggi     string
	Driver     string
	Keterangan string
	Foto       string
}

func NewUnitDTService(store repository.Store, location *time.Location, now NowFunc) *UnitDTService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &UnitDTService{store: store, location: location, now: now}
}

func (s *UnitDTService) Create(ctx context.Context, user *model.User, input UnitDTInput) (*model.UnitDT, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	nopol, err := NormalizeNopol(input.Nopol)
	if err != nil {
		return nil, err
	}
	panjang, err := parseDimension("Panjang", input.Panjang)
	if err != nil {
		return nil, err
	}
	lebar, err := parseDimension("Lebar", input.Lebar)
	if err != nil {
		return nil, err
	}
	tinggi, err := parseDimension("Tinggi", input.Tinggi)
	if err != nil {
		return nil, err
	}
	driver := strings.TrimSpace(input.Driver)
	if driver == "" {
		return nil, fmt.Errorf("%w: driver wajib diisi", ErrValidation)
	}
	keterangan, ok := canonicalKeterangan(input.Keterangan)
	if !ok {
		return nil, fmt.Errorf("%w: keterangan tidak terdaftar", ErrValidation)
	}
	// The photo is optional, but anything that is sent must still be a real
	// image we produced, not an arbitrary string.
	if input.Foto != "" {
		if err := photo.ValidateDataURL(input.Foto); err != nil {
			return nil, fmt.Errorf("%w: foto unit tidak valid", ErrInvalidPhoto)
		}
	}

	// Serialise the check-then-write so two submissions of the same plate
	// cannot both pass the existence check.
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.store.UnitDTExists(ctx, nopol)
	if err != nil {
		return nil, fmt.Errorf("check unit uniqueness: %w", err)
	}
	if exists {
		return nil, ErrDuplicateUnitDT
	}

	now := s.now().In(s.location)
	highest, err := s.store.MaxUnitDTSequence(ctx, unitIDPrefix(now.Year()))
	if err != nil {
		return nil, fmt.Errorf("read last unit id: %w", err)
	}
	unitID := formatUnitID(now.Year(), highest+1)
	unit := &model.UnitDT{
		UnitID:      unitID,
		Nopol:       nopol,
		Panjang:     panjang,
		Lebar:       lebar,
		Tinggi:      tinggi,
		Driver:      driver,
		Keterangan:  keterangan,
		Foto:        input.Foto,
		CreatedBy:   user.NamaLengkap,
		CreatedByID: user.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateUnitDT(ctx, unit); err != nil {
		return nil, fmt.Errorf("create unit dt: %w", err)
	}
	return unit, nil
}

// NormalizeNopol upper-cases the plate and collapses runs of whitespace, so
// "b  1234 abc" and "B 1234 ABC" cannot become two rows for one truck.
func NormalizeNopol(value string) (string, error) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(value), " "))
	if !nopolPattern.MatchString(normalized) {
		return "", fmt.Errorf("%w: format nopol harus seperti B 1234 ABC", ErrValidation)
	}
	return normalized, nil
}

func parseDimension(label, value string) (float64, error) {
	// Indonesian keyboards and locales produce a decimal comma; accept it
	// rather than rejecting a number the user considers well-formed.
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parsed, err := strconv.ParseFloat(cleaned, 64)
	// ParseFloat accepts "NaN" and "Inf". NaN also slips past a `<= 0` check
	// because every comparison with NaN is false, so reject non-finite values
	// explicitly rather than relying on the range test.
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%w: %s harus berupa angka", ErrValidation, label)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%w: %s harus lebih dari 0", ErrValidation, label)
	}
	return parsed, nil
}
