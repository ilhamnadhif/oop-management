package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

var ErrDuplicateUnitA2B = fmt.Errorf("unit a2b already exists")

type UnitA2BService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex

	optionsMu sync.Mutex
	options   UnitA2BOptions
	optionsAt time.Time
}

// UnitA2BOptions is what the pickers suggest. These are suggestions, not a
// closed set: a machine of an unfamiliar make must still be registerable.
type UnitA2BOptions struct {
	MerekType []string
	Lokasi    []string
}

// Options lists the values already used in the register.
func (s *UnitA2BService) Options(ctx context.Context) (UnitA2BOptions, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.options, nil
	}
	units, err := s.store.ListUnitA2B(ctx)
	if err != nil {
		return UnitA2BOptions{}, fmt.Errorf("read unit a2b options: %w", err)
	}
	s.options = UnitA2BOptions{
		MerekType: distinctValues(nil, units, func(u model.UnitA2B) string { return u.MerekType }),
		Lokasi:    distinctValues(nil, units, func(u model.UnitA2B) string { return u.Lokasi }),
	}
	s.optionsAt = s.now()
	return s.options, nil
}

func (s *UnitA2BService) invalidateOptions() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
}

type UnitA2BInput struct {
	Tanggal     string
	IDUnit      string
	NamaUnit    string
	MerekType   string
	FuelStorage string
	FRUnit      string
	Lokasi      string
	HMAwal      string
	Foto        string
}

func NewUnitA2BService(store repository.Store, location *time.Location, now NowFunc) *UnitA2BService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &UnitA2BService{store: store, location: location, now: now}
}

// Today is the date the form preselects.
func (s *UnitA2BService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// NextNumber reports the running number the next unit would receive. The form
// shows it as a preview; Create assigns the authoritative one under the lock.
func (s *UnitA2BService) NextNumber(ctx context.Context) (int, error) {
	highest, err := s.store.MaxUnitA2BNumber(ctx)
	if err != nil {
		return 0, err
	}
	return highest + 1, nil
}

func (s *UnitA2BService) Create(ctx context.Context, user *model.User, input UnitA2BInput) (*model.UnitA2B, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	tanggal := strings.TrimSpace(input.Tanggal)
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return nil, fmt.Errorf("%w: tanggal input wajib valid", ErrValidation)
	}
	idUnit := strings.ToUpper(strings.Join(strings.Fields(input.IDUnit), " "))
	if idUnit == "" {
		return nil, fmt.Errorf("%w: ID unit wajib diisi", ErrValidation)
	}
	namaUnit := strings.TrimSpace(input.NamaUnit)
	if namaUnit == "" {
		return nil, fmt.Errorf("%w: nama unit wajib diisi", ErrValidation)
	}
	// Merek and lokasi suggest what the register already holds but accept new
	// values; adopting an existing spelling keeps one make from appearing twice.
	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	merekType, err := adoptOption("Merek/type", input.MerekType, options.MerekType)
	if err != nil {
		return nil, err
	}
	lokasi, err := adoptOption("Lokasi unit", input.Lokasi, options.Lokasi)
	if err != nil {
		return nil, err
	}
	fuelStorage, err := parsePositive("Fuel storage", input.FuelStorage)
	if err != nil {
		return nil, err
	}
	frUnit, err := parsePositive("FR unit", input.FRUnit)
	if err != nil {
		return nil, err
	}
	// A brand new unit legitimately reads zero hours, so this one may be zero
	// while the others may not.
	hmAwal, err := parseNonNegative("HM awal", input.HMAwal)
	if err != nil {
		return nil, err
	}
	if input.Foto != "" {
		if err := photo.ValidateDataURL(input.Foto); err != nil {
			return nil, fmt.Errorf("%w: foto unit tidak valid", ErrInvalidPhoto)
		}
	}

	// Serialise the check, the numbering and the write, so two submissions
	// cannot claim the same identifier or the same running number.
	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.store.UnitA2BExists(ctx, idUnit)
	if err != nil {
		return nil, fmt.Errorf("check unit a2b uniqueness: %w", err)
	}
	if exists {
		return nil, ErrDuplicateUnitA2B
	}
	highest, err := s.store.MaxUnitA2BNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("read last unit a2b number: %w", err)
	}

	now := s.now().In(s.location)
	unit := &model.UnitA2B{
		NoUrut:      highest + 1,
		TanggalIn:   tanggal,
		IDUnit:      idUnit,
		NamaUnit:    namaUnit,
		MerekType:   merekType,
		FuelStorage: fuelStorage,
		FRUnit:      frUnit,
		Lokasi:      lokasi,
		HMAwal:      hmAwal,
		Foto:        input.Foto,
		CreatedBy:   user.NamaLengkap,
		CreatedByID: user.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateUnitA2B(ctx, unit); err != nil {
		return nil, fmt.Errorf("create unit a2b: %w", err)
	}
	// A value typed just now must reach the next submission, or the same make
	// gets stored again under a different spelling.
	s.invalidateOptions()
	return unit, nil
}

func parseNumber(label, value string) (float64, error) {
	// Indonesian keyboards produce a decimal comma; accept it rather than
	// rejecting a number the user considers well-formed.
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	if cleaned == "" {
		return 0, fmt.Errorf("%w: %s wajib diisi", ErrValidation, label)
	}
	parsed, err := strconv.ParseFloat(cleaned, 64)
	// ParseFloat accepts "NaN" and "Inf", and NaN slips past every comparison
	// because they are all false.
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%w: %s harus berupa angka", ErrValidation, label)
	}
	return parsed, nil
}

func parsePositive(label, value string) (float64, error) {
	parsed, err := parseNumber(label, value)
	if err != nil {
		return 0, err
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%w: %s harus lebih dari 0", ErrValidation, label)
	}
	return parsed, nil
}

func parseNonNegative(label, value string) (float64, error) {
	parsed, err := parseNumber(label, value)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%w: %s tidak boleh minus", ErrValidation, label)
	}
	return parsed, nil
}
