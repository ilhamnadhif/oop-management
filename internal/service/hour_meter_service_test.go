package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func hourMeterTestInput() HourMeterInput {
	return HourMeterInput{
		Tanggal:   "2026-08-07",
		Shift:     "Shift 1",
		IDUnit:    "exc01",
		Operator:  "kadal",
		HMAwal:    "1200",
		HMAkhir:   "1208",
		FuelLiter: "245",
	}
}

func newHourMeterService(store repository.Store, now time.Time) *HourMeterService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewHourMeterService(store, location, func() time.Time { return now })
}

// Readings are numbered within the day they are recorded, and the total is the
// distance between the two.
func TestHourMeterNumbersReadingsWithinADay(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	user := fuelTestUser("Logistik")

	first, err := service.Create(context.Background(), user, hourMeterTestInput())
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	if first.HMID != "HM-20260807-0001" || first.TotalHM != 8 {
		t.Fatalf("first reading = %q, total %v", first.HMID, first.TotalHM)
	}

	second := hourMeterTestInput()
	second.HMAwal = "1208"
	second.HMAkhir = "1216"
	saved, err := service.Create(context.Background(), user, second)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if saved.HMID != "HM-20260807-0002" {
		t.Fatalf("second reading = %q", saved.HMID)
	}
}

// Each machine's meter runs on its own, so the picker carries the last closing
// reading per unit rather than one figure for the whole fleet.
func TestHourMeterUnitPicksCarryTheLastReadingPerUnit(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	seedFuelMachine(t, store, "bul02", "Bulldozer D6 CAT (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), hourMeterTestInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	picks, err := service.UnitPicks(context.Background())
	if err != nil {
		t.Fatalf("unit picks: %v", err)
	}
	if len(picks) != 2 {
		t.Fatalf("picks = %+v", picks)
	}
	byID := make(map[string]HourMeterUnitPick, len(picks))
	for _, pick := range picks {
		byID[pick.IDUnit] = pick
	}
	if got := byID["exc01"]; !got.HasRecord || got.HMAwal != 1208 {
		t.Fatalf("the read machine did not carry its closing reading: %+v", got)
	}
	// A machine with no history offers nothing rather than the register's own HM
	// column, which is kept on a different scale.
	if got := byID["bul02"]; got.HasRecord || got.HMAwal != 0 {
		t.Fatalf("a machine with no history was given a reading: %+v", got)
	}
	if picks[0].IDUnit != "bul02" {
		t.Fatalf("the picker is not sorted by id: %+v", picks)
	}
}

// The shift and operator lists grow from what has already been recorded.
func TestHourMeterOptionsComeFromTheSheet(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	options, err := service.Options(context.Background())
	if err != nil {
		t.Fatalf("options on an empty sheet: %v", err)
	}
	if len(options.Shifts) != 0 || len(options.Operators) != 0 {
		t.Fatalf("an empty sheet suggested %+v", options)
	}

	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), hourMeterTestInput()); err != nil {
		t.Fatalf("create: %v", err)
	}
	options, err = service.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if len(options.Shifts) != 1 || options.Shifts[0] != "Shift 1" {
		t.Fatalf("shifts = %+v", options.Shifts)
	}
	if len(options.Operators) != 1 || options.Operators[0] != "kadal" {
		t.Fatalf("operators = %+v", options.Operators)
	}

	// A second spelling of the same shift is adopted rather than stored twice.
	repeat := hourMeterTestInput()
	repeat.Shift = "shift 1"
	repeat.HMAwal = "1208"
	repeat.HMAkhir = "1216"
	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), repeat)
	if err != nil {
		t.Fatalf("create repeat: %v", err)
	}
	if saved.Shift != "Shift 1" {
		t.Fatalf("shift = %q, want the spelling already in use", saved.Shift)
	}
}

// Fuel is required but may be zero: a shift without a refuel is a real shift.
func TestHourMeterRequiresFuelAndAllowsZero(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	missing := hourMeterTestInput()
	missing.FuelLiter = ""
	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), missing); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}

	none := hourMeterTestInput()
	none.FuelLiter = "0"
	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), none)
	if err != nil {
		t.Fatalf("create with no fuel: %v", err)
	}
	if saved.FuelLiter != 0 {
		t.Fatalf("fuel = %v", saved.FuelLiter)
	}
}

// Standby is capped so a form post claiming hundreds of lines is refused, and
// the sheet is written with the whole list in order.
func TestHourMeterStandbyIsBoundedAndOrdered(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	// Every listed reason at once is still well under the cap, and past it the
	// only way to send more lines is to repeat one, which is refused first.
	// Seven and a half hours worked leaves thirty minutes, spread across every
	// reason on the list at once.
	tooMany := hourMeterTestInput()
	tooMany.HMAkhir = "1207.5"
	for index, variable := range StandbyVariables {
		menit := "1"
		if index == 0 {
			menit = "8"
		}
		tooMany.Standby = append(tooMany.Standby, HourMeterStandbyInput{Variable: variable.Nama, Menit: menit})
	}
	if len(tooMany.Standby) > standbyMaxRows {
		t.Fatalf("the whole list no longer fits under the cap: %d rows", len(tooMany.Standby))
	}
	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), tooMany); err != nil {
		t.Fatalf("create with every reason: %v", err)
	}

	// The same reason twice is refused, whichever case it was typed in.
	repeated := hourMeterTestInput()
	repeated.HMAwal = "1208"
	repeated.HMAkhir = "1216"
	repeated.Standby = []HourMeterStandbyInput{
		{Variable: "HUJAN", Menit: "30"},
		{Variable: "hujan", Menit: "20"},
	}
	_, err := service.Create(context.Background(), fuelTestUser("Logistik"), repeated)
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "HUJAN sudah dipakai") {
		t.Fatalf("error = %v, want a duplicate refusal", err)
	}

	// Blank rows between filled ones are dropped, and the remaining lines are
	// numbered from one without a gap.
	sparse := hourMeterTestInput()
	sparse.Standby = []HourMeterStandbyInput{
		{Variable: "P2H", Menit: "15"},
		{},
		{Variable: "ISTIRAHAT", Menit: "60"},
	}
	sparse.HMAwal = "1216"
	sparse.HMAkhir = "1222.75"
	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), sparse)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(saved.Standby) != 2 {
		t.Fatalf("stored %d lines: %+v", len(saved.Standby), saved.Standby)
	}
	if saved.Standby[0].Variable != "P2H" || saved.Standby[1].Variable != "ISTIRAHAT" {
		t.Fatalf("the lines lost their order: %+v", saved.Standby)
	}
	if saved.TotalStandby != 75 {
		t.Fatalf("total standby = %v, want 75", saved.TotalStandby)
	}
}

// The variable is stored in the site's own spelling whatever case was posted.
func TestHourMeterStandbyAdoptsTheListedSpelling(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	input := hourMeterTestInput()
	input.HMAkhir = "1207.25"
	input.Standby = []HourMeterStandbyInput{{Variable: "tunggu  alat", Menit: "45"}}

	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.Standby[0].Variable != "TUNGGU ALAT" {
		t.Fatalf("variable = %q, want the listed spelling", saved.Standby[0].Variable)
	}
}

// The timesheet files each reason under a code, so a post carrying the code
// lands on the same reason as one carrying the name.
func TestHourMeterStandbyAcceptsTheTimesheetCode(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	input := hourMeterTestInput()
	input.HMAkhir = "1207.5"
	input.Standby = []HourMeterStandbyInput{{Variable: "I15", Menit: "30"}}

	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.Standby[0].Variable != "HUJAN" {
		t.Fatalf("variable = %q, want the name the code stands for", saved.Standby[0].Variable)
	}
}

// Every reason carries a code, they are unique, and the column each one is
// written to follows from the pair. A duplicate code would silently overwrite
// another reason's minutes.
func TestStandbyCodesAndColumnsAreUnique(t *testing.T) {
	codes := make(map[string]string, len(model.StandbyVariables))
	columns := make(map[string]string, len(model.StandbyVariables))
	for _, variable := range model.StandbyVariables {
		if variable.Kode == "" || variable.Nama == "" {
			t.Fatalf("incomplete standby variable: %+v", variable)
		}
		if previous, clash := codes[variable.Kode]; clash {
			t.Fatalf("code %s is shared by %s and %s", variable.Kode, previous, variable.Nama)
		}
		codes[variable.Kode] = variable.Nama
		column := variable.StandbyColumn()
		if previous, clash := columns[column]; clash {
			t.Fatalf("column %s is shared by %s and %s", column, previous, variable.Nama)
		}
		columns[column] = variable.Nama
	}
	if got := model.StandbyVariables[0]; got.Kode != "D01" || got.Nama != "P2H" {
		t.Fatalf("the list no longer starts at D01 P2H: %+v", got)
	}
	if got := model.StandbyVariables[len(model.StandbyVariables)-1]; got.Kode != "I20" {
		t.Fatalf("the list no longer ends at I20: %+v", got)
	}
	if got := model.StandbyVariables[0].StandbyColumn(); got != "d01_p2h" {
		t.Fatalf("column name = %q, want d01_p2h", got)
	}
}

// The shift is either worked or accounted for. A machine that ran the whole
// shift has nothing left to explain; anything short of it has to be split
// across standby and breakdown, to the minute.
func TestHourMeterBalancesTheShift(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	t.Run("a full shift takes no standby", func(t *testing.T) {
		input := hourMeterTestInput()
		input.Standby = []HourMeterStandbyInput{{Variable: "P2H", Menit: "15"}}
		_, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "harus kosong") {
			t.Fatalf("error = %v, want a refusal to record standby against a full shift", err)
		}
	})

	t.Run("a machine that ran longer than the shift is still full", func(t *testing.T) {
		input := hourMeterTestInput()
		input.HMAkhir = "1210"
		saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if saved.TotalHM != 10 || saved.TotalStandby != 0 || saved.TotalBreakdown != 0 {
			t.Fatalf("a ten hour shift was not stored clean: %+v", saved)
		}
	})

	t.Run("short of the shift the remainder is required", func(t *testing.T) {
		input := hourMeterTestInput()
		input.HMAwal = "1210"
		input.HMAkhir = "1217"
		_, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "sisa 60 menit") {
			t.Fatalf("error = %v, want the remaining hour to be demanded", err)
		}
	})

	t.Run("the remainder may be split across both sections", func(t *testing.T) {
		input := hourMeterTestInput()
		input.HMAwal = "1210"
		input.HMAkhir = "1217"
		input.Standby = []HourMeterStandbyInput{{Variable: "ISTIRAHAT", Menit: "45"}}
		input.Breakdown = []HourMeterBreakdownInput{{Variable: "SCM", Menit: "15"}}
		saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if saved.TotalStandby != 45 || saved.TotalBreakdown != 15 {
			t.Fatalf("the split was not stored: %+v", saved)
		}
	})

	t.Run("too much accounted for is refused too", func(t *testing.T) {
		input := hourMeterTestInput()
		input.HMAwal = "1217"
		input.HMAkhir = "1224"
		input.Standby = []HourMeterStandbyInput{{Variable: "ISTIRAHAT", Menit: "90"}}
		_, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
		if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "saat ini 90 menit") {
			t.Fatalf("error = %v, want an over-account refusal", err)
		}
	})
}

// The length of a shift comes from configuration, so a site working other hours
// is judged against its own day.
func TestHourMeterShiftLengthIsConfigurable(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)).WithWorkMinutes(600)

	if got := service.WorkMinutes(); got != 600 {
		t.Fatalf("work minutes = %d", got)
	}
	if got := service.IdleMinutesFor(8); got != 120 {
		t.Fatalf("idle after eight hours = %v, want 120", got)
	}
	if got := service.IdleMinutesFor(11); got != 0 {
		t.Fatalf("idle after eleven hours = %v, want 0", got)
	}

	// A nonsense value leaves the default in place rather than making every
	// reading unaccountable.
	if got := service.WithWorkMinutes(0).WorkMinutes(); got != 600 {
		t.Fatalf("work minutes after a zero = %d", got)
	}

	input := hourMeterTestInput()
	input.Standby = []HourMeterStandbyInput{{Variable: "ISTIRAHAT", Menit: "120"}}
	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.TotalStandby != 120 {
		t.Fatalf("standby = %v, want the two hours a ten hour day leaves", saved.TotalStandby)
	}
}

// A shift reads as three figures: how much of it the machine was fit to work,
// how much was lost to breakdown, and how much of the fit time it used.
func TestHourMeterSummaryReadsTheShift(t *testing.T) {
	store := repository.NewTestRepository()
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	// 390 worked, 60 standby, 30 breakdown out of 480.
	summary := service.SummaryFor(390, 30)
	if summary.PA != 93.75 {
		t.Fatalf("PA = %v, want 93.75", summary.PA)
	}
	if summary.BDPersen != 6.25 {
		t.Fatalf("BD%% = %v, want 6.25", summary.BDPersen)
	}
	if summary.UA != 86.67 {
		t.Fatalf("UA = %v, want 86.67", summary.UA)
	}
	// Availability and its mirror are the same rule stated twice.
	if got := summary.PA + summary.BDPersen; got != 100 {
		t.Fatalf("PA + BD%% = %v, want 100", got)
	}

	// A machine that worked the whole shift is fully available and fully used.
	full := service.SummaryFor(480, 0)
	if full.PA != 100 || full.BDPersen != 0 || full.UA != 100 {
		t.Fatalf("a full shift read as %+v", full)
	}

	// Broken all shift: never available, so there is nothing to have used.
	broken := service.SummaryFor(0, 480)
	if broken.PA != 0 || broken.BDPersen != 100 || broken.UA != 0 {
		t.Fatalf("a broken shift read as %+v", broken)
	}
}

// The figures are stored with the reading, and the remark travels with them.
func TestHourMeterStoresItsFiguresAndRemark(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	input := hourMeterTestInput()
	input.HMAkhir = "1206.5"
	input.Standby = []HourMeterStandbyInput{{Variable: "ISTIRAHAT", Menit: "60"}}
	input.Breakdown = []HourMeterBreakdownInput{{Variable: "SCM", Menit: "30"}}
	input.Remark = "  Ganti hose hidrolik  "

	saved, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if saved.PA != 93.75 || saved.BDPersen != 6.25 || saved.UA != 86.67 {
		t.Fatalf("figures = %v / %v / %v", saved.PA, saved.BDPersen, saved.UA)
	}
	if saved.Remark != "Ganti hose hidrolik" {
		t.Fatalf("remark = %q", saved.Remark)
	}

	// The remark is bounded, because it is the one free-text field on the form.
	long := hourMeterTestInput()
	long.Remark = strings.Repeat("a", HourMeterRemarkMaxLength+1)
	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), long); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want a validation error", err)
	}
}

// Use of availability stops at a hundred. A machine that ran past the end of
// the shift used all of the shift and no more; 125% is not a reading.
func TestHourMeterUseOfAvailabilityStopsAtFull(t *testing.T) {
	store := repository.NewTestRepository()
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	for name, tc := range map[string]struct {
		worked    float64
		breakdown float64
		wantUA    float64
	}{
		"ten hours worked":            {600, 0, 100},
		"exactly the shift":           {480, 0, 100},
		"past the shift with a break": {600, 60, 100},
		"short of the shift":          {390, 30, 86.67},
	} {
		t.Run(name, func(t *testing.T) {
			if got := service.SummaryFor(tc.worked, tc.breakdown).UA; got != tc.wantUA {
				t.Fatalf("UA = %v, want %v", got, tc.wantUA)
			}
		})
	}
}

// The export narrows the readings to a date range, and an empty side does not
// bound that side at all.
func TestHourMeterExportRowsFiltersByRange(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	user := fuelTestUser("Logistik")

	input := hourMeterTestInput() // 2026-08-07
	if _, err := service.Create(context.Background(), user, input); err != nil {
		t.Fatalf("create august: %v", err)
	}
	september := hourMeterTestInput()
	september.Tanggal = "2026-09-02"
	september.HMAwal = "1208"
	september.HMAkhir = "1216"
	if _, err := service.Create(context.Background(), user, september); err != nil {
		t.Fatalf("create september: %v", err)
	}

	all, err := service.ExportRows(context.Background(), "", "", "")
	if err != nil {
		t.Fatalf("export all: %v", err)
	}
	if len(all.Rows) != 2 {
		t.Fatalf("export all returned %d rows, want 2", len(all.Rows))
	}

	august, err := service.ExportRows(context.Background(), "2026-08-01", "2026-08-31", "")
	if err != nil {
		t.Fatalf("export august: %v", err)
	}
	if len(august.Rows) != 1 || august.Rows[0].Tanggal != "2026-08-07" {
		t.Fatalf("export august returned %+v", august.Rows)
	}

	// One side open runs to the edge of the sheet rather than to a default.
	fromSeptember, err := service.ExportRows(context.Background(), "2026-09-01", "", "")
	if err != nil {
		t.Fatalf("export from september: %v", err)
	}
	if len(fromSeptember.Rows) != 1 || fromSeptember.Rows[0].Tanggal != "2026-09-02" {
		t.Fatalf("an open end returned %+v", fromSeptember.Rows)
	}
}

// A range typed the wrong way round is the person's slip, not a refusal.
func TestHourMeterExportRowsSwapsAReversedRange(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	if _, err := service.Create(context.Background(), fuelTestUser("Logistik"), hourMeterTestInput()); err != nil {
		t.Fatalf("create: %v", err)
	}

	report, err := service.ExportRows(context.Background(), "2026-08-31", "2026-08-01", "")
	if err != nil {
		t.Fatalf("export reversed: %v", err)
	}
	if report.From != "2026-08-01" || report.To != "2026-08-31" {
		t.Fatalf("range = %s..%s, want it swapped", report.From, report.To)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("a swapped range returned %d rows, want 1", len(report.Rows))
	}
}

// A date the calendar does not have cannot filter anything, so it is refused
// rather than quietly ignored.
func TestHourMeterExportRowsRefusesAnInvalidDate(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))

	if _, err := service.ExportRows(context.Background(), "bukan-tanggal", "", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("an invalid start returned %v, want a validation error", err)
	}
	if _, err := service.ExportRows(context.Background(), "", "2026-13-40", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("an invalid end returned %v, want a validation error", err)
	}
}

// The unit filter is what the dropdown sends, and an empty one means the whole
// fleet.
func TestHourMeterExportRowsFiltersByUnit(t *testing.T) {
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	seedFuelMachine(t, store, "bld03", "Bulldozer D65 Komatsu 2 (Rent)")
	service := newHourMeterService(store, time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC))
	user := fuelTestUser("Logistik")

	if _, err := service.Create(context.Background(), user, hourMeterTestInput()); err != nil {
		t.Fatalf("create excavator: %v", err)
	}
	bulldozer := hourMeterTestInput()
	bulldozer.IDUnit = "bld03"
	if _, err := service.Create(context.Background(), user, bulldozer); err != nil {
		t.Fatalf("create bulldozer: %v", err)
	}

	// The dropdown sends the id as the register holds it; matching must not
	// turn on the case it was typed in.
	one, err := service.ExportRows(context.Background(), "", "", "BLD03")
	if err != nil {
		t.Fatalf("export one unit: %v", err)
	}
	if len(one.Rows) != 1 || !strings.EqualFold(one.Rows[0].IDUnit, "bld03") {
		t.Fatalf("the unit filter returned %+v", one.Rows)
	}
	if one.IDUnit != "BLD03" {
		t.Fatalf("IDUnit = %q, want the filter echoed back for the page", one.IDUnit)
	}
}
