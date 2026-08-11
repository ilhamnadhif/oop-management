package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// FuelUnitOption is one machine the dispensing form may be pointed at. The name
// travels with the id so the page can fill it in without a second request, and
// the service looks it up again rather than trusting what came back.
type FuelUnitOption struct {
	IDUnit   string
	NamaUnit string
}

// FuelKeluarInput is what the person dispensing supplies. The litres are absent
// on purpose: they are the distance between the two flowmeter readings, not a
// figure anyone gets to type.
type FuelKeluarInput struct {
	Tanggal          string
	IDUnit           string
	HMAwalFlowMeter  string
	HMAkhirFlowMeter string
	HMAlatBerat      string
	Operator         string
	Foto             string
}

// FuelKeluarService owns the dispensing log. The mutex makes sequential ID
// allocation atomic within a process.
type FuelKeluarService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex

	optionsMu sync.Mutex
	operators []string
	optionsAt time.Time
}

func NewFuelKeluarService(store repository.Store, location *time.Location, now NowFunc) *FuelKeluarService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &FuelKeluarService{store: store, location: location, now: now}
}

func (s *FuelKeluarService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// fuelOutIDPrefix numbers dispenses within a day, so the transaction number
// says when the fuel left the tank.
func fuelOutIDPrefix(now time.Time) string {
	return "FUELOUT-" + now.Format("20060102") + "-"
}

// NextFuelOutID reports the number the next saved dispense would receive. The
// form shows it as a preview; Create assigns the real one under the lock.
func (s *FuelKeluarService) NextFuelOutID(ctx context.Context) (string, error) {
	prefix := fuelOutIDPrefix(s.now().In(s.location))
	highest, err := s.store.MaxFuelKeluarSequence(ctx, prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, highest+1), nil
}

// UnitOptions is the A2B register, which is where the machine list comes from.
// The picker is closed: fuel cannot be booked against a machine nobody
// registered, or its consumption belongs to nothing.
func (s *FuelKeluarService) UnitOptions(ctx context.Context) ([]FuelUnitOption, error) {
	units, err := s.store.ListUnitA2B(ctx)
	if err != nil {
		return nil, fmt.Errorf("read unit a2b for fuel keluar: %w", err)
	}
	options := make([]FuelUnitOption, 0, len(units))
	for _, unit := range units {
		id := strings.TrimSpace(unit.IDUnit)
		if id == "" {
			continue
		}
		options = append(options, FuelUnitOption{IDUnit: id, NamaUnit: strings.TrimSpace(unit.NamaUnit)})
	}
	sort.SliceStable(options, func(i, j int) bool {
		return strings.ToLower(options[i].IDUnit) < strings.ToLower(options[j].IDUnit)
	})
	return options, nil
}

// Operators lists the names already used, so the same person is not recorded
// under three spellings. A new hire must still be typeable.
func (s *FuelKeluarService) Operators(ctx context.Context) ([]string, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.operators, nil
	}
	rows, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar operators: %w", err)
	}
	s.operators = distinctValues(nil, rows, func(f model.FuelKeluar) string { return f.Operator })
	s.optionsAt = s.now()
	return s.operators, nil
}

func (s *FuelKeluarService) invalidateOperators() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
}

// LastFlowMeter reports where the pump's flowmeter finished last time, which is
// where the next dispense starts. A totaliser only goes forwards, so the most
// recently recorded end reading is the highest one.
func (s *FuelKeluarService) LastFlowMeter(ctx context.Context) (float64, error) {
	rows, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return 0, fmt.Errorf("read fuel keluar flow meter: %w", err)
	}
	highest := 0.0
	for _, row := range rows {
		if row.HMAkhirFlowMeter > highest {
			highest = row.HMAkhirFlowMeter
		}
	}
	return highest, nil
}

func (s *FuelKeluarService) Create(ctx context.Context, user *model.User, input FuelKeluarInput) (*model.FuelKeluar, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}

	tanggal, err := normalizeFuelDate(input.Tanggal)
	if err != nil {
		return nil, err
	}
	// The name is taken from the register rather than the form: the page fills
	// it in for the reader's benefit, and a posted name is not evidence.
	unit, err := s.resolveUnit(ctx, input.IDUnit)
	if err != nil {
		return nil, err
	}
	awal, err := parseNonNegative("HM awal flow meter", input.HMAwalFlowMeter)
	if err != nil {
		return nil, err
	}
	akhir, err := parseNonNegative("HM akhir flow meter", input.HMAkhirFlowMeter)
	if err != nil {
		return nil, err
	}
	if akhir <= awal {
		return nil, fmt.Errorf("%w: HM akhir flow meter harus lebih besar dari HM awal", ErrValidation)
	}
	hmAlatBerat, err := optionalHourMeter(input.HMAlatBerat)
	if err != nil {
		return nil, err
	}
	operators, err := s.Operators(ctx)
	if err != nil {
		return nil, err
	}
	operator, err := adoptOption("Operator", input.Operator, operators)
	if err != nil {
		return nil, err
	}
	// The photo of the closing reading is the only evidence the numbers typed
	// above match the pump.
	foto := strings.TrimSpace(input.Foto)
	if foto == "" {
		return nil, fmt.Errorf("%w: foto akhir flow meter wajib dilampirkan", ErrValidation)
	}
	if err := photo.ValidateDataURL(foto); err != nil {
		return nil, fmt.Errorf("%w: foto akhir flow meter tidak valid", ErrInvalidPhoto)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	prefix := fuelOutIDPrefix(now)
	sequence, err := s.store.MaxFuelKeluarSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar sequence: %w", err)
	}
	fuel := &model.FuelKeluar{
		FuelOutID:          fmt.Sprintf("%s%04d", prefix, sequence+1),
		Tanggal:            tanggal,
		IDUnit:             unit.IDUnit,
		NamaUnit:           unit.NamaUnit,
		HMAwalFlowMeter:    round2(awal),
		HMAkhirFlowMeter:   round2(akhir),
		Liter:              round2(akhir - awal),
		HMAlatBerat:        hmAlatBerat,
		Operator:           operator,
		FotoAkhirFlowMeter: foto,
		CreatedBy:          strings.TrimSpace(user.NamaLengkap),
		CreatedByID:        user.UserID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.store.CreateFuelKeluar(ctx, fuel); err != nil {
		return nil, fmt.Errorf("create fuel keluar: %w", err)
	}
	s.invalidateOperators()
	return fuel, nil
}

// List returns the dispenses newest first, which is the order the page wants:
// the row just saved is the one being checked.
func (s *FuelKeluarService) List(ctx context.Context) ([]model.FuelKeluar, error) {
	rows, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Tanggal != rows[j].Tanggal {
			return rows[i].Tanggal > rows[j].Tanggal
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].FuelOutID > rows[j].FuelOutID
	})
	return rows, nil
}

// Photo reads the stored flowmeter photo for one dispense.
func (s *FuelKeluarService) Photo(ctx context.Context, fuelOutID string) (string, error) {
	fuelOutID = strings.TrimSpace(fuelOutID)
	if fuelOutID == "" {
		return "", fmt.Errorf("%w: nomor transaksi wajib diisi", ErrValidation)
	}
	_, rowNumber, err := s.store.FindFuelKeluarRow(ctx, fuelOutID)
	if err != nil {
		return "", fmt.Errorf("find fuel keluar: %w", err)
	}
	value, err := s.store.ReadFuelKeluarPhoto(ctx, rowNumber)
	if err != nil {
		return "", fmt.Errorf("read fuel keluar photo: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", repository.ErrNotFound
	}
	if err := photo.ValidateDataURL(value); err != nil {
		return "", fmt.Errorf("stored fuel keluar photo: %w", ErrInvalidPhoto)
	}
	return value, nil
}

func (s *FuelKeluarService) resolveUnit(ctx context.Context, idUnit string) (FuelUnitOption, error) {
	idUnit = strings.Join(strings.Fields(idUnit), " ")
	if idUnit == "" {
		return FuelUnitOption{}, fmt.Errorf("%w: ID unit wajib dipilih", ErrValidation)
	}
	options, err := s.UnitOptions(ctx)
	if err != nil {
		return FuelUnitOption{}, err
	}
	for _, option := range options {
		if strings.EqualFold(option.IDUnit, idUnit) {
			return option, nil
		}
	}
	return FuelUnitOption{}, fmt.Errorf("%w: ID unit tidak terdaftar di Unit A2B", ErrValidation)
}

func normalizeFuelDate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%w: tanggal input wajib diisi", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("%w: tanggal input tidak valid", ErrValidation)
	}
	return value, nil
}

// optionalHourMeter accepts an empty reading. An hour meter nobody wrote down
// is not the same as one reading zero, so the empty case stays nil.
func optionalHourMeter(value string) (*float64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseNonNegative("HM alat berat pengisian", value)
	if err != nil {
		return nil, err
	}
	rounded := round2(parsed)
	return &rounded, nil
}
