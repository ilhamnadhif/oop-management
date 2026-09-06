package service

import (
	"context"
	"fmt"
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
	// IDUnit is the machine picked, empty meaning every machine on site.
	IDUnit string
	Rows   []model.FuelKeluar
}

// ExportRows returns the dispensing sheet for a report, newest first, narrowed
// to a date range and to one machine. Every filter left empty means all of it,
// which is how the export page defaults.
func (s *FuelKeluarService) ExportRows(ctx context.Context, from, to, idUnit string) (*FuelKeluarExport, error) {
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
	idUnit = strings.TrimSpace(idUnit)

	rows, err := s.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel keluar: %w", err)
	}
	report := &FuelKeluarExport{From: from, To: to, IDUnit: idUnit, Rows: make([]model.FuelKeluar, 0, len(rows))}
	for _, row := range rows {
		tanggal := strings.TrimSpace(row.Tanggal)
		if from != "" && tanggal < from {
			continue
		}
		if to != "" && tanggal > to {
			continue
		}
		if idUnit != "" && !strings.EqualFold(strings.TrimSpace(row.IDUnit), idUnit) {
			continue
		}
		report.Rows = append(report.Rows, row)
	}
	return report, nil
}
