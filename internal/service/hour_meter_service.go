package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// HourMeterUnitPick is one machine the reading may be booked against, carrying
// what the form needs to fill itself in: the name that belongs to the id, and
// where this machine's hour meter finished last time.
type HourMeterUnitPick struct {
	IDUnit   string
	NamaUnit string
	// HMAwal is the machine's last closing reading in minutes, and zero when it
	// has none yet. The A2B register's own HM column is deliberately not used as
	// a fallback: it is recorded on a different scale, and seeding minutes from
	// it would put a wrong number in front of the operator.
	HMAwal    float64
	HasRecord bool
}

// StandbyVariables is the closed set of reasons, re-exported so handlers and
// templates do not have to reach past this package for it.
var StandbyVariables = model.StandbyVariables

// BreakdownVariables is the closed set of breakdown reasons, re-exported for
// the same reason StandbyVariables is.
var BreakdownVariables = model.BreakdownVariables

// standbyMaxRows is a backstop, not a rule: every reason at once is a full
// list, and a form post claiming more than that is not a shift anyone worked.
var standbyMaxRows = len(model.StandbyVariables)

// HourMeterStandbyInput is one standby line as the form submits it.
type HourMeterStandbyInput struct {
	Variable string
	Menit    string
}

// IsBlank reports a row nobody filled in. The form always renders one empty
// line, and an untouched line is not an error.
func (i HourMeterStandbyInput) IsBlank() bool {
	return strings.TrimSpace(i.Variable) == "" && strings.TrimSpace(i.Menit) == ""
}

// HourMeterBreakdownInput is one breakdown line as the form submits it.
type HourMeterBreakdownInput struct {
	Variable string
	Menit    string
}

func (i HourMeterBreakdownInput) IsBlank() bool {
	return strings.TrimSpace(i.Variable) == "" && strings.TrimSpace(i.Menit) == ""
}

// HourMeterInput is what the form supplies. The totals are absent: one is the
// distance between the two readings and the other is the sum of the standby
// lines, and neither is a figure anyone types.
type HourMeterInput struct {
	Tanggal   string
	Shift     string
	IDUnit    string
	Operator  string
	HMAwal    string
	HMAkhir   string
	FuelLiter string
	Standby   []HourMeterStandbyInput
	Breakdown []HourMeterBreakdownInput
	Remark    string
}

// HourMeterRemarkMaxLength bounds the one free-text field on the form.
const HourMeterRemarkMaxLength = 500

// HourMeterSummary is the shift read as three figures.
type HourMeterSummary struct {
	// PA is physical availability: the share of the shift the machine was fit
	// to work, whatever it then did with the time.
	PA float64
	// BDPersen is the share lost to breakdown, which is PA seen from the other
	// side: the two always add to a hundred.
	BDPersen float64
	// UA is use of availability: of the time the machine was fit to work, how
	// much it actually worked. A machine broken all shift was never available,
	// so there is nothing to have used and this reads zero. A machine that ran
	// past the shift used all of it and no more, so this stops at a hundred.
	UA float64
}

// SummaryFor reads a shift as its three figures. Minutes in, percentages out.
func (s *HourMeterService) SummaryFor(workedMinutes, breakdownMinutes float64) HourMeterSummary {
	shift := float64(s.workMinutes)
	if shift <= 0 {
		return HourMeterSummary{}
	}
	available := shift - breakdownMinutes
	if available < 0 {
		available = 0
	}
	summary := HourMeterSummary{
		PA:       round2(available / shift * 100),
		BDPersen: round2(breakdownMinutes / shift * 100),
	}
	if available > 0 {
		used := workedMinutes / available * 100
		// Running past the end of the shift is still one shift's use of it.
		if used > 100 {
			used = 100
		}
		summary.UA = round2(used)
	}
	return summary
}

// HourMeterOptions are the open lists the form suggests from. Both grow as the
// sheet does: a shift or an operator seen for the first time is typeable.
type HourMeterOptions struct {
	Shifts    []string
	Operators []string
}

// defaultWorkMinutes is one shift, used when nothing was configured. It is the
// same eight hours the sheet is built around.
const defaultWorkMinutes = 480

type HourMeterService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	// workMinutes is one shift. Time the machine did not spend working has to be
	// accounted for as standby or breakdown, and this is what "all of it" means.
	workMinutes int
	mu          sync.Mutex

	optionsMu sync.Mutex
	options   HourMeterOptions
	optionsAt time.Time
}

func NewHourMeterService(store repository.Store, location *time.Location, now NowFunc) *HourMeterService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &HourMeterService{store: store, location: location, now: now, workMinutes: defaultWorkMinutes}
}

// WithWorkMinutes sets the length of a shift. A value of zero or less leaves
// the default in place rather than making every reading unaccountable.
func (s *HourMeterService) WithWorkMinutes(minutes int) *HourMeterService {
	if minutes > 0 {
		s.workMinutes = minutes
	}
	return s
}

// WorkMinutes is what the form judges a reading against.
func (s *HourMeterService) WorkMinutes() int { return s.workMinutes }

// IdleMinutesFor reports how many minutes of the shift the machine did not
// spend working, which is what standby and breakdown together have to add up
// to. A machine that ran the whole shift or longer leaves nothing to account
// for, and the two sections have nothing to ask.
func (s *HourMeterService) IdleMinutesFor(totalHours float64) float64 {
	idle := float64(s.workMinutes) - totalHours*60
	if idle <= 0 {
		return 0
	}
	return round2(idle)
}

func (s *HourMeterService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// hourMeterIDPrefix numbers readings within a day.
func hourMeterIDPrefix(now time.Time) string {
	return "HM-" + now.Format("20060102") + "-"
}

// NextHMID reports the number the next saved reading would receive. The form
// shows it as a preview; Create assigns the real one under the lock.
func (s *HourMeterService) NextHMID(ctx context.Context) (string, error) {
	prefix := hourMeterIDPrefix(s.now().In(s.location))
	highest, err := s.store.MaxHourMeterSequence(ctx, prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, highest+1), nil
}

// Options lists the shifts and operators already recorded.
func (s *HourMeterService) Options(ctx context.Context) (HourMeterOptions, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.options, nil
	}
	rows, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return HourMeterOptions{}, fmt.Errorf("read hour meter options: %w", err)
	}
	s.options = HourMeterOptions{
		Shifts:    distinctValues(nil, rows, func(h model.HourMeter) string { return h.Shift }),
		Operators: distinctValues(nil, rows, func(h model.HourMeter) string { return h.Operator }),
	}
	s.optionsAt = s.now()
	return s.options, nil
}

func (s *HourMeterService) invalidateOptions() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
}

// UnitPicks is the A2B register with each machine's last closing reading
// attached, so choosing a unit can fill the opening reading without a second
// request. The list is closed: an hour meter booked against a machine nobody
// registered belongs to nothing.
func (s *HourMeterService) UnitPicks(ctx context.Context) ([]HourMeterUnitPick, error) {
	units, err := s.store.ListUnitA2B(ctx)
	if err != nil {
		return nil, fmt.Errorf("read unit a2b for hour meter: %w", err)
	}
	readings, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return nil, fmt.Errorf("read hour meter history: %w", err)
	}
	// An hour meter only goes forwards, so the highest reading a machine has is
	// also its most recent one.
	last := make(map[string]float64, len(readings))
	for _, reading := range readings {
		key := strings.ToUpper(strings.TrimSpace(reading.IDUnit))
		if key == "" {
			continue
		}
		if reading.HMAkhir > last[key] {
			last[key] = reading.HMAkhir
		}
	}

	picks := make([]HourMeterUnitPick, 0, len(units))
	for _, unit := range units {
		id := strings.TrimSpace(unit.IDUnit)
		if id == "" {
			continue
		}
		hmAwal, found := last[strings.ToUpper(id)]
		picks = append(picks, HourMeterUnitPick{
			IDUnit:    id,
			NamaUnit:  strings.TrimSpace(unit.NamaUnit),
			HMAwal:    hmAwal,
			HasRecord: found,
		})
	}
	sort.SliceStable(picks, func(i, j int) bool {
		return strings.ToLower(picks[i].IDUnit) < strings.ToLower(picks[j].IDUnit)
	})
	return picks, nil
}

func (s *HourMeterService) Create(ctx context.Context, user *model.User, input HourMeterInput) (*model.HourMeter, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}

	tanggal, err := normalizeFuelDate(input.Tanggal)
	if err != nil {
		return nil, err
	}
	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	shift, err := adoptOption("Shift", input.Shift, options.Shifts)
	if err != nil {
		return nil, err
	}
	operator, err := adoptOption("Operator", input.Operator, options.Operators)
	if err != nil {
		return nil, err
	}
	// The name is taken from the register rather than the form: the page fills
	// it in for the reader's benefit, and a posted name is not evidence.
	unit, err := s.resolveUnit(ctx, input.IDUnit)
	if err != nil {
		return nil, err
	}
	awal, err := parseNonNegative("HM awal", input.HMAwal)
	if err != nil {
		return nil, err
	}
	akhir, err := parseNonNegative("HM akhir", input.HMAkhir)
	if err != nil {
		return nil, err
	}
	if akhir < awal {
		return nil, fmt.Errorf("%w: HM akhir tidak boleh lebih kecil dari HM awal", ErrValidation)
	}
	totalHM := round2(akhir - awal)
	// A machine that stood idle all shift reads the same at both ends, so an
	// equal pair is a real reading rather than a mistake.
	fuel, err := parseNonNegative("Fuel", input.FuelLiter)
	if err != nil {
		return nil, err
	}
	standby, totalStandby, err := normalizeStandby(input.Standby)
	if err != nil {
		return nil, err
	}
	breakdown, totalBreakdown, err := normalizeBreakdown(input.Breakdown)
	if err != nil {
		return nil, err
	}
	// The shift is either worked or accounted for. A machine that ran the whole
	// shift has nothing left to explain; anything short of it has to be split
	// across standby and breakdown, down to the minute.
	idle := s.IdleMinutesFor(totalHM)
	accounted := round2(totalStandby + totalBreakdown)
	switch {
	case idle == 0 && accounted > 0:
		return nil, fmt.Errorf("%w: HM sudah memenuhi %d menit kerja, standby dan breakdown harus kosong",
			ErrValidation, s.workMinutes)
	case accounted != idle:
		return nil, fmt.Errorf("%w: sisa %s menit dari %d menit kerja harus terisi di standby atau breakdown, saat ini %s menit",
			ErrValidation, trimFloat(idle), s.workMinutes, trimFloat(accounted))
	}

	remark := strings.TrimSpace(input.Remark)
	if utf8.RuneCountInString(remark) > HourMeterRemarkMaxLength {
		return nil, fmt.Errorf("%w: remark maksimal %d karakter", ErrValidation, HourMeterRemarkMaxLength)
	}
	summary := s.SummaryFor(totalHM*60, totalBreakdown)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	prefix := hourMeterIDPrefix(now)
	sequence, err := s.store.MaxHourMeterSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read hour meter sequence: %w", err)
	}
	reading := &model.HourMeter{
		HMID:           fmt.Sprintf("%s%04d", prefix, sequence+1),
		Tanggal:        tanggal,
		Shift:          shift,
		IDUnit:         unit.IDUnit,
		NamaUnit:       unit.NamaUnit,
		Operator:       operator,
		HMAwal:         round2(awal),
		HMAkhir:        round2(akhir),
		TotalHM:        round2(akhir - awal),
		FuelLiter:      round2(fuel),
		TotalStandby:   totalStandby,
		Standby:        standby,
		TotalBreakdown: totalBreakdown,
		Breakdown:      breakdown,
		PA:             summary.PA,
		BDPersen:       summary.BDPersen,
		UA:             summary.UA,
		Remark:         remark,
		CreatedBy:      strings.TrimSpace(user.NamaLengkap),
		CreatedByID:    user.UserID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateHourMeter(ctx, reading); err != nil {
		return nil, fmt.Errorf("create hour meter: %w", err)
	}
	// A shift or operator typed just now must be offered to the next reading, or
	// the same value gets stored again under a different spelling.
	s.invalidateOptions()
	return reading, nil
}

// List returns the readings newest first, which is the order the page wants.
func (s *HourMeterService) List(ctx context.Context) ([]model.HourMeter, error) {
	rows, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return nil, fmt.Errorf("read hour meter: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Tanggal != rows[j].Tanggal {
			return rows[i].Tanggal > rows[j].Tanggal
		}
		if !rows[i].CreatedAt.Equal(rows[j].CreatedAt) {
			return rows[i].CreatedAt.After(rows[j].CreatedAt)
		}
		return rows[i].HMID > rows[j].HMID
	})
	return rows, nil
}

func (s *HourMeterService) resolveUnit(ctx context.Context, idUnit string) (HourMeterUnitPick, error) {
	idUnit = strings.Join(strings.Fields(idUnit), " ")
	if idUnit == "" {
		return HourMeterUnitPick{}, fmt.Errorf("%w: ID unit wajib dipilih", ErrValidation)
	}
	picks, err := s.UnitPicks(ctx)
	if err != nil {
		return HourMeterUnitPick{}, err
	}
	for _, pick := range picks {
		if strings.EqualFold(pick.IDUnit, idUnit) {
			return pick, nil
		}
	}
	return HourMeterUnitPick{}, fmt.Errorf("%w: ID unit tidak terdaftar di Unit A2B", ErrValidation)
}

// durationLine is one reason and its minutes, whichever block it came from.
type durationLine struct {
	Variable string
	Menit    float64
}

// normalizeDurationLines is the rule both blocks follow. The list is optional:
// a shift where nothing happened has none. Each reason may be given once,
// because each has a column of its own on the sheet, so a shift that stopped
// twice for the same reason is recorded as the minutes added up.
func normalizeDurationLines(label string, count int, blank func(int) bool, variable func(int) string, menit func(int) string, canonical func(string) (string, bool), maxRows int) ([]durationLine, float64, error) {
	lines := make([]durationLine, 0, count)
	seen := make(map[string]bool, count)
	total := 0.0
	for index := 0; index < count; index++ {
		if blank(index) {
			continue
		}
		if len(lines) >= maxRows {
			return nil, 0, fmt.Errorf("%w: %s maksimal %d baris", ErrValidation, label, maxRows)
		}
		position := len(lines) + 1
		name, ok := canonical(variable(index))
		if !ok {
			return nil, 0, fmt.Errorf("%w: variable %s baris %d tidak terdaftar", ErrValidation, label, position)
		}
		if seen[name] {
			return nil, 0, fmt.Errorf("%w: variable %s %s sudah dipakai, gabungkan menitnya jadi satu baris", ErrValidation, label, name)
		}
		seen[name] = true
		minutes, err := parsePositive(fmt.Sprintf("Menit %s baris %d", label, position), menit(index))
		if err != nil {
			return nil, 0, err
		}
		lines = append(lines, durationLine{Variable: name, Menit: round2(minutes)})
		total += minutes
	}
	return lines, round2(total), nil
}

func normalizeStandby(inputs []HourMeterStandbyInput) ([]model.HourMeterStandby, float64, error) {
	lines, total, err := normalizeDurationLines("standby", len(inputs),
		func(i int) bool { return inputs[i].IsBlank() },
		func(i int) string { return inputs[i].Variable },
		func(i int) string { return inputs[i].Menit },
		canonicalStandbyVariable, standbyMaxRows)
	if err != nil {
		return nil, 0, err
	}
	rows := make([]model.HourMeterStandby, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, model.HourMeterStandby{Variable: line.Variable, Menit: line.Menit})
	}
	return rows, total, nil
}

func normalizeBreakdown(inputs []HourMeterBreakdownInput) ([]model.HourMeterBreakdown, float64, error) {
	lines, total, err := normalizeDurationLines("breakdown", len(inputs),
		func(i int) bool { return inputs[i].IsBlank() },
		func(i int) string { return inputs[i].Variable },
		func(i int) string { return inputs[i].Menit },
		canonicalBreakdownVariable, len(model.BreakdownVariables))
	if err != nil {
		return nil, 0, err
	}
	rows := make([]model.HourMeterBreakdown, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, model.HourMeterBreakdown{Variable: line.Variable, Menit: line.Menit})
	}
	return rows, total, nil
}

func canonicalBreakdownVariable(value string) (string, bool) {
	value = strings.Join(strings.Fields(value), " ")
	for _, option := range model.BreakdownVariables {
		if strings.EqualFold(option, value) {
			return option, true
		}
	}
	return "", false
}

// canonicalStandbyVariable accepts either the name an operator picked or the
// code the timesheet files it under, and answers with the name.
func canonicalStandbyVariable(value string) (string, bool) {
	value = strings.Join(strings.Fields(value), " ")
	for _, option := range model.StandbyVariables {
		if strings.EqualFold(option.Nama, value) || strings.EqualFold(option.Kode, value) {
			return option.Nama, true
		}
	}
	return "", false
}

// trimFloat prints a figure the way the form shows it: whole minutes stay
// whole, and a fraction keeps its decimals.
func trimFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
