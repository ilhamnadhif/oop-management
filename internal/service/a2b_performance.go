package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A2BPerformanceReport is the machine performance table as an export asks for
// it: the same figures the overview draws, over a range the person picked and
// narrowed to one machine if they picked one.
type A2BPerformanceReport struct {
	// From and To are the range actually used, which is what the report header
	// prints. They are filled in when the person left a side open.
	From string
	To   string
	// IDUnit is the filter as it was given, echoed back so the page can put the
	// dropdown back where it was. Empty means the whole fleet.
	IDUnit string
	Units  []A2BUnitPerformance
}

// A2BPerformance builds the per-machine table for an export. Unlike the
// overview, a range left open means every reading ever taken rather than the
// last week: an export names its own period, and somebody asking for a report
// without saying when is asking for all of it.
func (s *UnitOverviewService) A2BPerformance(ctx context.Context, from, to, idUnit string, workMinutes int) (*A2BPerformanceReport, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	idUnit = strings.TrimSpace(idUnit)

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

	report := &A2BPerformanceReport{From: overview.From, To: overview.To, IDUnit: idUnit}
	if idUnit == "" {
		report.Units = overview.Units
		return report, nil
	}
	for _, unit := range overview.Units {
		if strings.EqualFold(strings.TrimSpace(unit.IDUnit), idUnit) {
			report.Units = append(report.Units, unit)
		}
	}
	return report, nil
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
