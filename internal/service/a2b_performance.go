package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// A2BPerformanceReport is the machine performance table as an export asks for
// it: the same figures the overview draws, over a range the person picked and
// narrowed to one machine if they picked one.
type A2BPerformanceReport struct {
	// From and To are the range actually used, which is what the report header
	// prints. They are filled in when the person left a side open.
	From string
	To   string
	// IDUnits are the machines picked, echoed back so the page can put the
	// ticks back where they were. Empty means the whole fleet.
	IDUnits []string
	Units   []A2BUnitPerformance
	// Readings are the shifts the summary was built from, grouped by machine
	// and then by day. A report that says a machine ran fifty hours without
	// saying which shifts those were has to be taken on trust.
	Readings []model.HourMeter
}

// A2BPerformance builds the per-machine table for an export. Unlike the
// overview, a range left open means every reading ever taken rather than the
// last week: an export names its own period, and somebody asking for a report
// without saying when is asking for all of it.
func (s *UnitOverviewService) A2BPerformance(ctx context.Context, from, to string, idUnits []string, workMinutes int) (*A2BPerformanceReport, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	units := newUnitFilter(idUnits)

	if from == "" || to == "" {
		earliest, latest, err := s.a2bReadingRange(ctx)
		if err != nil {
			return nil, err
		}
		if earliest == "" {
			// Nothing has ever been read. Any range would do; today's keeps the
			// report header printing a date rather than a blank.
			today := s.now().In(s.location).Format("2006-01-02")
			earliest, latest = today, today
		}
		if from == "" {
			from = earliest
		}
		if to == "" {
			to = latest
		}
	}

	overview, err := s.BuildA2B(ctx, from, to, workMinutes)
	if err != nil {
		return nil, err
	}

	report := &A2BPerformanceReport{From: overview.From, To: overview.To, IDUnits: units.given}
	for _, unit := range overview.Units {
		if units.matches(unit.IDUnit) {
			report.Units = append(report.Units, unit)
		}
	}

	readings, err := s.a2bReadingsIn(ctx, report.From, report.To, units)
	if err != nil {
		return nil, err
	}
	report.Readings = readings
	return report, nil
}

// a2bReadingsIn is the detail behind the summary: the shifts that fell inside
// the range, for the machine asked for or for all of them. It answers the same
// filters the summary does, or the two would describe different months.
func (s *UnitOverviewService) a2bReadingsIn(ctx context.Context, from, to string, units unitFilter) ([]model.HourMeter, error) {
	rows, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return nil, fmt.Errorf("read hour meter: %w", err)
	}
	readings := make([]model.HourMeter, 0, len(rows))
	for _, row := range rows {
		tanggal := strings.TrimSpace(row.Tanggal)
		if tanggal < from || tanggal > to {
			continue
		}
		if !units.matches(row.IDUnit) {
			continue
		}
		readings = append(readings, row)
	}
	// Grouped by machine, then by day, so the detail reads down each unit's own
	// run rather than jumping between them.
	sort.SliceStable(readings, func(i, j int) bool {
		left, right := strings.ToLower(readings[i].IDUnit), strings.ToLower(readings[j].IDUnit)
		if left != right {
			return left < right
		}
		if readings[i].Tanggal != readings[j].Tanggal {
			return readings[i].Tanggal < readings[j].Tanggal
		}
		return readings[i].HMID < readings[j].HMID
	})
	return readings, nil
}

// a2bReadingRange is the first and last day anything was read. Both are empty
// when no reading has been taken.
func (s *UnitOverviewService) a2bReadingRange(ctx context.Context) (string, string, error) {
	readings, err := s.store.ListHourMeter(ctx)
	if err != nil {
		return "", "", fmt.Errorf("read hour meter: %w", err)
	}
	earliest, latest := "", ""
	for _, reading := range readings {
		day := strings.TrimSpace(reading.Tanggal)
		if _, err := time.Parse("2006-01-02", day); err != nil {
			// A row typed straight into the sheet may hold anything. It cannot
			// widen a range it does not sort into.
			continue
		}
		if earliest == "" || day < earliest {
			earliest = day
		}
		if latest == "" || day > latest {
			latest = day
		}
	}
	return earliest, latest, nil
}
