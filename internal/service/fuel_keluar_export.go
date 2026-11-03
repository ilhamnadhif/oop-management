package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"opp-management/internal/model"
)

// FuelKeluarExport is the dispensing sheet a report asks for: the range and
// machine as they were given, echoed back so the page can put its own filters
// back, and the rows that survived them.
type FuelKeluarExport struct {
	// From and To are the range as it was asked for, swapped when it was typed
	// the wrong way round. Either may be empty, which does not bound that side.
	From string
	To   string
	// IDUnits are the machines picked, empty meaning every machine on site.
	IDUnits []string
	Rows    []model.FuelKeluar
	// Ratios is each row's fuel ratio in litres an hour, nil where the fill
	// has no ratio: see fillFuelRatios.
	Ratios []*float64
}

// ExportRows returns the dispensing sheet for a report, newest first, narrowed
// to a date range and to one machine. Every filter left empty means all of it,
// which is how the export page defaults.
func (s *FuelKeluarService) ExportRows(ctx context.Context, from, to string, idUnits []string) (*FuelKeluarExport, error) {
	from, err := normalizeExportDate("tanggal awal", from)
	if err != nil {
		return nil, err
	}
	to, err = normalizeExportDate("tanggal akhir", to)
	if err != nil {
		return nil, err
	}
	if from != "" && to != "" && from > to {
		// Typing the range backwards is a slip, not a request for nothing.
		from, to = to, from
	}
	units := newUnitFilter(idUnits)

	rows, err := s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar: %w", err)
	}
	report := &FuelKeluarExport{From: from, To: to, IDUnits: units.given, Rows: make([]model.FuelKeluar, 0, len(rows))}
	// Measured over the whole sheet, so the first fill in the range is still
	// measured from the last one before it.
	ratios := fillFuelRatios(rows)
	for index, row := range rows {
		tanggal := strings.TrimSpace(row.Tanggal)
		if from != "" && tanggal < from {
			continue
		}
		if to != "" && tanggal > to {
			continue
		}
		if !units.matches(row.IDUnit) {
			continue
		}
		report.Rows = append(report.Rows, row)
		report.Ratios = append(report.Ratios, ratios[index])
	}
	return report, nil
}

// fillFuelRatios is the fuel ratio of every fill, by its place in rows, worked
// the way the A2B overview works a machine's: the litres put in divided by the
// hours the machine's meter ran since the fill before. A fill tops up what was
// burned since the last one, so the litres of a fill nobody read the meter at
// are carried to the next fill that was read. A fill without a reading, the
// first reading a machine has, and a meter that did not move forward have no
// ratio.
func fillFuelRatios(rows []model.FuelKeluar) []*float64 {
	order := make([]int, len(rows))
	for i := range order {
		order[i] = i
	}
	// Oldest first, ties broken as the overview breaks them, so both read the
	// sheet in the same order.
	sort.SliceStable(order, func(a, b int) bool {
		left, right := rows[order[a]], rows[order[b]]
		if left.Tanggal != right.Tanggal {
			return left.Tanggal < right.Tanggal
		}
		if !left.CreatedAt.Equal(right.CreatedAt) {
			return left.CreatedAt.Before(right.CreatedAt)
		}
		return left.FuelOutID < right.FuelOutID
	})

	type mark struct {
		meter   float64
		hasMark bool
		liters  float64
	}
	marks := make(map[string]*mark)
	ratios := make([]*float64, len(rows))
	for _, index := range order {
		row := rows[index]
		key := strings.ToUpper(strings.TrimSpace(row.IDUnit))
		if key == "" {
			continue
		}
		unit, seen := marks[key]
		if !seen {
			unit = &mark{}
			marks[key] = unit
		}
		unit.liters += row.Liter
		if row.HMAlatBerat == nil {
			continue
		}
		meter := *row.HMAlatBerat
		if unit.hasMark {
			if hours := meter - unit.meter; hours > 0 {
				ratio := round2(unit.liters / hours)
				ratios[index] = &ratio
			}
		}
		unit.meter, unit.hasMark, unit.liters = meter, true, 0
	}
	return ratios
}
