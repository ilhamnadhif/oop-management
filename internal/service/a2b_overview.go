package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// A2BUnitPerformance is one machine's shifts added up. The figures are the same
// ones a single reading carries, recomputed from the totals rather than
// averaged: a one-hour shift must not weigh as much as a full one.
type A2BUnitPerformance struct {
	IDUnit   string
	NamaUnit string
	Shifts   int
	// TotalHM is hours; the rest of the durations are minutes.
	TotalHM          float64
	Fuel             float64
	FuelRatio        float64
	StandbyMinutes   float64
	BreakdownMinutes float64
	PA               float64
	UA               float64
}

// A2BDelayShare is one standby reason and how much of the fleet's idle time it
// accounts for.
type A2BDelayShare struct {
	Variable string
	Menit    float64
	Jam      float64
	Percent  float64
}

// A2BDayPoint is one day of the range: how the fleet was employed, and how
// much fuel moved either way. The fuel figures are that day's movement, not the
// running level the stock card reports.
type A2BDayPoint struct {
	Tanggal       string
	Label         string
	UnitActive    int
	UnitBreakdown int
	FuelMasuk     float64
	FuelKeluar    float64
}

// A2BOverview is the machine dashboard: what the fleet did over a range,
// rather than what the register holds.
type A2BOverview struct {
	From        string
	To          string
	LastUpdated string

	TotalUnit int
	// UnitActive is machines that actually worked in the range. A machine whose
	// every shift was standby was not used, so it is counted as standby.
	UnitActive    int
	UnitStandby   int
	UnitBreakdown int

	// StockFuel is what is left in the tank at the end of the range: everything
	// delivered up to that day less everything dispensed. Stock is a running
	// level, so a past range shows the level as it stood then.
	FuelMasuk  float64
	FuelKeluar float64
	StockFuel  float64

	Units []A2BUnitPerformance

	// Series is the range day by day, which is what the two charts draw.
	Series []A2BDayPoint

	TopDelay          []A2BDelayShare
	TotalDelayMinutes float64
	TotalDelayJam     float64
}

// a2bTopDelayCount is how many reasons the delay panel names before the rest
// become "lainnya".
const a2bTopDelayCount = 5

// BuildA2B reads the machine fleet over a date range. workMinutes is one
// shift's length, which is what the availability figures are judged against.
func (s *UnitOverviewService) BuildA2B(ctx context.Context, from, to string, workMinutes int) (*A2BOverview, error) {
	from, to, err := s.normalizeA2BRange(from, to)
	if err != nil {
		return nil, err
	}
	if workMinutes <= 0 {
		workMinutes = defaultWorkMinutes
	}

	machines, err := s.store.ListUnitA2B(ctx)
	if err != nil {
		return nil, fmt.Errorf("read unit a2b: %w", err)
	}
	readings, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return nil, fmt.Errorf("read hour meter: %w", err)
	}
	deliveries, err := s.store.ListFuelMasuk(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel masuk: %w", err)
	}
	dispenses, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar: %w", err)
	}

	overview := &A2BOverview{
		From:        from,
		To:          to,
		LastUpdated: s.now().In(s.location).Format("02 Jan 2006 15:04"),
		TotalUnit:   len(machines),
	}

	names := make(map[string]string, len(machines))
	for _, machine := range machines {
		id := strings.TrimSpace(machine.IDUnit)
		if id == "" {
			continue
		}
		names[strings.ToUpper(id)] = strings.TrimSpace(machine.NamaUnit)
	}

	days := dateStringsInRange(from, to)
	series := make(map[string]*A2BDayPoint, len(days))
	for _, day := range days {
		point := &A2BDayPoint{Tanggal: day, Label: hrDateLabel(day)}
		series[day] = point
		overview.Series = append(overview.Series, *point)
	}
	// A machine counts once a day however many shifts it ran.
	activeOn := make(map[string]map[string]bool, len(days))
	brokenOn := make(map[string]map[string]bool, len(days))

	// One pass over the readings in range: per-machine totals, and the fleet's
	// standby broken down by reason.
	perUnit := make(map[string]*A2BUnitPerformance)
	order := make([]string, 0, len(machines))
	delay := make(map[string]float64)
	for _, reading := range readings {
		if reading.Tanggal < from || reading.Tanggal > to {
			continue
		}
		id := strings.TrimSpace(reading.IDUnit)
		if id == "" {
			continue
		}
		key := strings.ToUpper(id)
		unit, seen := perUnit[key]
		if !seen {
			name := names[key]
			if name == "" {
				// A reading whose machine has since left the register still
				// happened, and its own snapshot names it.
				name = strings.TrimSpace(reading.NamaUnit)
			}
			unit = &A2BUnitPerformance{IDUnit: id, NamaUnit: name}
			perUnit[key] = unit
			order = append(order, key)
		}
		unit.Shifts++
		unit.TotalHM += reading.TotalHM
		unit.StandbyMinutes += reading.TotalStandby
		unit.BreakdownMinutes += reading.TotalBreakdown

		for _, standby := range reading.Standby {
			variable := strings.TrimSpace(standby.Variable)
			if variable == "" {
				continue
			}
			delay[variable] += standby.Menit
			overview.TotalDelayMinutes += standby.Menit
		}

		if reading.TotalHM > 0 {
			markUnitDay(activeOn, reading.Tanggal, key)
		}
		if reading.TotalBreakdown > 0 {
			markUnitDay(brokenOn, reading.Tanggal, key)
		}
	}

	burn := fuelBurnedPerUnit(dispenses, from, to)

	for _, key := range order {
		unit := perUnit[key]
		unit.TotalHM = round2(unit.TotalHM)
		unit.StandbyMinutes = round2(unit.StandbyMinutes)
		unit.BreakdownMinutes = round2(unit.BreakdownMinutes)
		if tally := burn[key]; tally != nil {
			unit.Fuel = round2(tally.liters)
			if hours := tally.hours(); hours > 0 {
				unit.FuelRatio = round2(tally.liters / hours)
			}
		}

		shift := float64(unit.Shifts * workMinutes)
		available := shift - unit.BreakdownMinutes
		if available < 0 {
			available = 0
		}
		if shift > 0 {
			unit.PA = round2(available / shift * 100)
		}
		if available > 0 {
			used := unit.TotalHM * 60 / available * 100
			if used > 100 {
				used = 100
			}
			unit.UA = round2(used)
		}

		if unit.TotalHM > 0 {
			overview.UnitActive++
		}
		if unit.BreakdownMinutes > 0 {
			overview.UnitBreakdown++
		}
		overview.Units = append(overview.Units, *unit)
	}
	// Busiest first: the machine that ran the most is the one being asked about.
	sort.SliceStable(overview.Units, func(i, j int) bool {
		if overview.Units[i].TotalHM != overview.Units[j].TotalHM {
			return overview.Units[i].TotalHM > overview.Units[j].TotalHM
		}
		return strings.ToLower(overview.Units[i].IDUnit) < strings.ToLower(overview.Units[j].IDUnit)
	})

	// A machine nobody worked is standby, whether it has readings full of
	// standby or no readings at all.
	overview.UnitStandby = overview.TotalUnit - overview.UnitActive
	if overview.UnitStandby < 0 {
		overview.UnitStandby = 0
	}

	// Stock is a level, not a flow: everything in less everything out, up to the
	// end of the range. A rejected delivery never arrived, so it is left out.
	// The chart wants the flow, so each day's movement is tallied on the way past.
	for _, delivery := range deliveries {
		if delivery.StatusApproval == model.FuelStatusDitolak {
			continue
		}
		day := delivery.TanggalInput.In(s.location).Format("2006-01-02")
		if day > to {
			continue
		}
		overview.FuelMasuk += delivery.JumlahLiter
		if point, inRange := series[day]; inRange {
			point.FuelMasuk += delivery.JumlahLiter
		}
	}
	for _, dispense := range dispenses {
		if dispense.Tanggal > to {
			continue
		}
		overview.FuelKeluar += dispense.Liter
		if point, inRange := series[dispense.Tanggal]; inRange {
			point.FuelKeluar += dispense.Liter
		}
	}

	for index, day := range days {
		point := series[day]
		point.UnitActive = len(activeOn[day])
		point.UnitBreakdown = len(brokenOn[day])
		point.FuelMasuk = round2(point.FuelMasuk)
		point.FuelKeluar = round2(point.FuelKeluar)
		overview.Series[index] = *point
	}
	overview.FuelMasuk = round2(overview.FuelMasuk)
	overview.FuelKeluar = round2(overview.FuelKeluar)
	overview.StockFuel = round2(overview.FuelMasuk - overview.FuelKeluar)

	overview.TotalDelayMinutes = round2(overview.TotalDelayMinutes)
	overview.TotalDelayJam = round2(overview.TotalDelayMinutes / 60)
	overview.TopDelay = topDelayShares(delay, overview.TotalDelayMinutes)
	return overview, nil
}

// topDelayShares ranks the reasons and keeps the leading few. What falls off
// the end is gathered rather than dropped, so the slices still add to the
// whole.
func topDelayShares(tally map[string]float64, total float64) []A2BDelayShare {
	if len(tally) == 0 || total <= 0 {
		return nil
	}
	shares := make([]A2BDelayShare, 0, len(tally))
	for variable, minutes := range tally {
		shares = append(shares, A2BDelayShare{Variable: variable, Menit: round2(minutes)})
	}
	sort.SliceStable(shares, func(i, j int) bool {
		if shares[i].Menit != shares[j].Menit {
			return shares[i].Menit > shares[j].Menit
		}
		return shares[i].Variable < shares[j].Variable
	})

	if len(shares) > a2bTopDelayCount {
		rest := 0.0
		for _, share := range shares[a2bTopDelayCount:] {
			rest += share.Menit
		}
		shares = append(shares[:a2bTopDelayCount:a2bTopDelayCount], A2BDelayShare{
			Variable: "LAINNYA",
			Menit:    round2(rest),
		})
	}
	for index := range shares {
		shares[index].Jam = round2(shares[index].Menit / 60)
		shares[index].Percent = round2(shares[index].Menit / total * 100)
	}
	return shares
}

// DefaultA2BRange is the week the dashboard opens on.
func (s *UnitOverviewService) DefaultA2BRange() (string, string) {
	today := s.now().In(s.location)
	return today.AddDate(0, 0, -6).Format("2006-01-02"), today.Format("2006-01-02")
}

func (s *UnitOverviewService) normalizeA2BRange(from, to string) (string, string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	defaultFrom, defaultTo := s.DefaultA2BRange()
	if from == "" && to == "" {
		return defaultFrom, defaultTo, nil
	}
	if to == "" {
		to = defaultTo
	}
	if from == "" {
		from = defaultFrom
	}
	if _, err := time.Parse("2006-01-02", from); err != nil {
		return "", "", fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
	}
	if _, err := time.Parse("2006-01-02", to); err != nil {
		return "", "", fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
	}
	if from > to {
		from, to = to, from
	}
	return from, to, nil
}

func markUnitDay(days map[string]map[string]bool, day, unit string) {
	if days[day] == nil {
		days[day] = make(map[string]bool)
	}
	days[day][unit] = true
}

// unitFuelBurn is one machine's fuel over a range: the litres dispensed into it
// while the range was open, and the two meter readings the hours between them
// are measured from.
//
// The litres a machine burned are what came out of the tank into it, and the
// hours they bought are the distance its own meter moved. That distance is not
// the hour meter timesheet: fuel is dispensed between fills, so the hours the
// fuel paid for run from one fill to the next, which is what the operator reads
// off the machine when he fills it.
type unitFuelBurn struct {
	liters float64
	// firstHM is the reading the range is measured from and lastHM the reading
	// it is measured to. The mark is the last fill before the range where there
	// is one: the fuel in view was burned over those hours, not over the ones
	// between the fills that happen to fall inside the dates.
	firstHM float64
	lastHM  float64
	hasMark bool
	hasLast bool
}

// hours is the distance the meter moved, or zero when there is not yet a second
// reading to measure against. A meter that went backwards is a typo rather than
// a machine that un-worked, and reports no hours instead of negative ones.
func (b *unitFuelBurn) hours() float64 {
	if !b.hasMark || !b.hasLast {
		return 0
	}
	return b.lastHM - b.firstHM
}

// fuelBurnedPerUnit tallies the dispensing sheet by machine. Readings are
// optional on the dispensing form, so a fill nobody read the meter at counts
// toward the litres while the hours are measured across the fills that do carry
// one.
func fuelBurnedPerUnit(dispenses []model.FuelKeluar, from, to string) map[string]*unitFuelBurn {
	ordered := make([]model.FuelKeluar, len(dispenses))
	copy(ordered, dispenses)
	// Oldest first, so one pass can keep the last fill before the range as the
	// mark and then walk forward through the range. The id breaks a tie between
	// two fills written up at the same moment, so the same sheet always reads
	// the same way.
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Tanggal != ordered[j].Tanggal {
			return ordered[i].Tanggal < ordered[j].Tanggal
		}
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].FuelOutID < ordered[j].FuelOutID
	})

	burn := make(map[string]*unitFuelBurn)
	for _, dispense := range ordered {
		if dispense.Tanggal > to {
			continue
		}
		id := strings.TrimSpace(dispense.IDUnit)
		if id == "" {
			continue
		}
		key := strings.ToUpper(id)
		tally, seen := burn[key]
		if !seen {
			tally = &unitFuelBurn{}
			burn[key] = tally
		}

		inRange := dispense.Tanggal >= from
		if inRange {
			tally.liters += dispense.Liter
		}
		if dispense.HMAlatBerat == nil {
			continue
		}
		reading := *dispense.HMAlatBerat
		if !inRange {
			// Before the range every reading replaces the last, leaving the
			// nearest one to it as the mark.
			tally.firstHM = reading
			tally.hasMark = true
			tally.hasLast = false
			continue
		}
		if !tally.hasMark {
			// Nothing before the range means nothing to measure the first fill
			// from, so it becomes the mark itself rather than being thrown away.
			tally.firstHM = reading
			tally.hasMark = true
			continue
		}
		tally.lastHM = reading
		tally.hasLast = true
	}
	return burn
}
