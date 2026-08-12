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
		unit.Fuel += reading.FuelLiter
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

	for _, key := range order {
		unit := perUnit[key]
		unit.TotalHM = round2(unit.TotalHM)
		unit.Fuel = round2(unit.Fuel)
		unit.StandbyMinutes = round2(unit.StandbyMinutes)
		unit.BreakdownMinutes = round2(unit.BreakdownMinutes)
		if unit.TotalHM > 0 {
			unit.FuelRatio = round2(unit.Fuel / unit.TotalHM)
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
