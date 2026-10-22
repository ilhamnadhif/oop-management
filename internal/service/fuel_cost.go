package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// FuelMonthCost is one month's fuel bill: what came in, what was burned, and
// what the burning cost.
type FuelMonthCost struct {
	// Bulan is the month as "2006-01"; Label words it for a page.
	Bulan string
	Label string
	// MasukLiter is what arrived, and MasukBiaya what it was bought for. They
	// are here to be compared against the charge, not to be it: a month that
	// took a delivery it has not burned yet should not read as a month that
	// overspent.
	MasukLiter float64
	MasukBiaya float64
	// KeluarLiter is what was dispensed, and Biaya is what that cost at the
	// average price of the tank it came out of. This is the month's charge.
	KeluarLiter float64
	Biaya       float64
	// SisaLiter and SisaNilai are the tank at the end of the month, carried
	// into the next one.
	SisaLiter float64
	SisaNilai float64
}

// FuelCostReport is the fuel bill over a range, month by month.
type FuelCostReport struct {
	From string
	To   string
	// Months covers every month the range touches, including the ones nothing
	// happened in: a gap in a cost report reads as a month nobody looked at.
	Months []FuelMonthCost
	// The totals for the range, and the tank as it stands at the end of it.
	KeluarLiter float64
	Biaya       float64
	MasukLiter  float64
	MasukBiaya  float64
	SisaLiter   float64
	SisaNilai   float64
	// HargaRataRata is what a litre in the tank is worth at the end of the
	// range, which is the price the next dispense will be charged at.
	HargaRataRata float64
}

// BuildFuelCost prices the fuel that was burned rather than the fuel that was
// bought. A site that buys a month's fuel in one go and takes five weeks to
// burn it would otherwise read as a month that overspent followed by a month
// that spent nothing, when what happened was one steady month of work.
//
// The tank is one tank and the fuel in it is mixed, so there is no way to say
// which litre came from which delivery. It is costed at the running average:
// every delivery moves the average of what is in the tank, and every dispense
// is charged at the average as it stood when it was drawn.
func (s *UnitOverviewService) BuildFuelCost(ctx context.Context, from, to string) (*FuelCostReport, error) {
	from, to, err := s.normalizeA2BRange(from, to)
	if err != nil {
		return nil, err
	}

	deliveries, err := s.store.ListFuelMasuk(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel masuk: %w", err)
	}
	dispenses, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar: %w", err)
	}

	report := &FuelCostReport{From: from, To: to}
	events := fuelEvents(deliveries, dispenses, s.location)
	months := monthsInRange(from, to)
	if len(months) == 0 {
		return report, nil
	}

	tank := fuelTank{}
	next := 0
	// Fuel bought and burned before the range still decides what the tank holds
	// when the range opens, so those events are walked without being charged to
	// any month.
	for next < len(events) && monthOf(events[next].day) < months[0] {
		tank.apply(events[next])
		next++
	}

	for _, month := range months {
		entry := FuelMonthCost{Bulan: month, Label: monthName(month)}
		for next < len(events) && monthOf(events[next].day) == month {
			event := events[next]
			next++
			if event.delivery {
				entry.MasukLiter += event.liters
				entry.MasukBiaya += event.liters * event.price
				tank.apply(event)
				continue
			}
			entry.KeluarLiter += event.liters
			entry.Biaya += tank.draw(event.liters)
		}
		entry.MasukLiter = round2(entry.MasukLiter)
		entry.MasukBiaya = round2(entry.MasukBiaya)
		entry.KeluarLiter = round2(entry.KeluarLiter)
		entry.Biaya = round2(entry.Biaya)
		entry.SisaLiter = round2(tank.liters)
		entry.SisaNilai = round2(tank.value)

		report.Months = append(report.Months, entry)
		report.MasukLiter += entry.MasukLiter
		report.MasukBiaya += entry.MasukBiaya
		report.KeluarLiter += entry.KeluarLiter
		report.Biaya += entry.Biaya
	}

	report.MasukLiter = round2(report.MasukLiter)
	report.MasukBiaya = round2(report.MasukBiaya)
	report.KeluarLiter = round2(report.KeluarLiter)
	report.Biaya = round2(report.Biaya)
	// The tank as the range closes. Anything delivered after it belongs to a
	// later report, not to this one.
	report.SisaLiter = round2(tank.liters)
	report.SisaNilai = round2(tank.value)
	report.HargaRataRata = round2(tank.average())
	return report, nil
}

// fuelTank is the site tank as two running figures: how much is in it and what
// that is worth. The average of the two is what the next dispense costs.
type fuelTank struct {
	liters float64
	value  float64
	// lastAverage is what the tank was worth per litre the last time it held
	// anything, kept for the dispenses that have no stock behind them.
	lastAverage float64
}

// apply moves the tank by one event. Only deliveries go through here; a
// dispense is drawn instead, because it reports what it cost.
func (t *fuelTank) apply(event fuelEvent) {
	if event.delivery {
		t.receive(event.liters, event.price)
		return
	}
	t.draw(event.liters)
}

func (t *fuelTank) receive(liters, price float64) {
	if liters <= 0 {
		return
	}
	t.liters += liters
	t.value += liters * price
	t.lastAverage = t.average()
}

// draw takes fuel out and reports what it cost.
//
// A dispense with no stock behind it is not an error to refuse: the dispensing
// sheet was kept before the deliveries were, so the early months hold fuel that
// was really burned out of a tank this app never saw filled. It is charged at
// the last price the tank was worth, or at the default when there has never
// been a delivery at all - which is closer to the truth than charging nothing.
func (t *fuelTank) draw(liters float64) float64 {
	if liters <= 0 {
		return 0
	}
	price := t.average()
	if price <= 0 {
		price = t.lastAverage
	}
	if price <= 0 {
		price = DefaultHargaPerLiter
	}
	cost := liters * price
	t.liters -= liters
	t.value -= cost
	if t.liters <= 0 {
		// An empty tank has no average of its own; the last one it had is what
		// the next dispense without stock will be charged at.
		t.liters = 0
		t.value = 0
	}
	t.lastAverage = price
	return cost
}

func (t fuelTank) average() float64 {
	if t.liters <= 0 {
		return 0
	}
	return t.value / t.liters
}

// fuelEvent is one movement of the tank, which is all the costing cares about.
type fuelEvent struct {
	day      string
	delivery bool
	liters   float64
	price    float64
	// order breaks a tie within a day so the same sheet always costs the same.
	order string
}

// fuelEvents puts every delivery and dispense on one timeline. A delivery that
// was rejected never arrived, so it is left off.
//
// Within a day the deliveries come first: fuel is put in the tank before it is
// drawn out of it, and a day that receives and dispenses the same litres should
// not read as dispensing from an empty tank.
func fuelEvents(deliveries []model.FuelMasuk, dispenses []model.FuelKeluar, location *time.Location) []fuelEvent {
	events := make([]fuelEvent, 0, len(deliveries)+len(dispenses))
	for _, delivery := range deliveries {
		if delivery.StatusApproval == model.FuelStatusDitolak {
			continue
		}
		events = append(events, fuelEvent{
			day:      delivery.TanggalInput.In(location).Format("2006-01-02"),
			delivery: true,
			liters:   delivery.JumlahLiter,
			price:    HargaPerLiterOf(delivery),
			order:    delivery.FuelID,
		})
	}
	for _, dispense := range dispenses {
		events = append(events, fuelEvent{
			day:    strings.TrimSpace(dispense.Tanggal),
			liters: dispense.Liter,
			order:  dispense.FuelOutID,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].day != events[j].day {
			return events[i].day < events[j].day
		}
		if events[i].delivery != events[j].delivery {
			return events[i].delivery
		}
		return events[i].order < events[j].order
	})
	return events
}

// monthsInRange lists every month the range touches, in order.
func monthsInRange(from, to string) []string {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return nil
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return nil
	}
	months := make([]string, 0, 12)
	for cursor := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC); !cursor.After(end); cursor = cursor.AddDate(0, 1, 0) {
		months = append(months, cursor.Format("2006-01"))
	}
	return months
}

// monthName words a month for a page: "Agustus 2026".
func monthName(month string) string {
	parsed, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return indonesianMonthLabel(parsed)
}

// monthOf is the month a day belongs to, as "2006-01".
func monthOf(day string) string {
	if len(day) < 7 {
		return day
	}
	return day[:7]
}
