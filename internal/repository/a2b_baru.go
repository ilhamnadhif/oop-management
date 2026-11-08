package repository

import (
	"context"
	"strings"
	"time"

	"opp-management/internal/model"
)

// The A2B Baru sheets. Numbers are written as numbers rather than as text, so
// somebody reading the sheet can sum a column.
const (
	a2bBaruUnitSheet       = "A2B Baru Unit"
	a2bBaruFuelMasukSheet  = "A2B Baru Fuel Masuk"
	a2bBaruFuelKeluarSheet = "A2B Baru Fuel Keluar"
)

var a2bBaruUnitHeaders = []string{"id_unit", "nama_unit", "created_by", "created_at"}

var a2bBaruFuelMasukHeaders = []string{
	"no_transaksi", "phase", "tanggal", "no_po",
	"po_volume", "po_harga_satuan", "site_volume", "site_harga_satuan",
	"created_by", "created_at",
}

var a2bBaruFuelKeluarHeaders = []string{
	"no_transaksi", "tanggal", "id_unit", "nama_unit",
	"hm_awal_a2b", "hm_akhir_a2b", "hm_awal_fm", "hm_akhir_fm",
	"fuel_liter", "hm_saat_isi", "jam_kerja", "operator",
	"created_by", "created_at",
}

func (r *GoogleSheetsRepository) ListA2BBaruUnits(ctx context.Context) ([]model.A2BBaruUnit, error) {
	rows, err := r.readRows(ctx, a2bBaruUnitSheet, columnName(len(a2bBaruUnitHeaders)))
	if err != nil {
		return nil, err
	}
	units := make([]model.A2BBaruUnit, 0, len(rows))
	for index, row := range rows {
		if index == 0 || isBlankRow(row) {
			continue
		}
		row = padRow(row, len(a2bBaruUnitHeaders))
		units = append(units, model.A2BBaruUnit{
			IDUnit: strings.TrimSpace(cellString(row[0])), NamaUnit: strings.TrimSpace(cellString(row[1])),
			CreatedBy: cellString(row[2]), CreatedAt: lenientDateTime(row[3], r.location),
		})
	}
	return units, nil
}

func (r *GoogleSheetsRepository) AppendA2BBaruUnits(ctx context.Context, units []model.A2BBaruUnit) error {
	rows := make([][]interface{}, 0, len(units))
	for _, unit := range units {
		rows = append(rows, []interface{}{unit.IDUnit, unit.NamaUnit, unit.CreatedBy, formatDateTime(unit.CreatedAt)})
	}
	return r.appendRows(ctx, a2bBaruUnitSheet, rows)
}

func (r *GoogleSheetsRepository) ListA2BBaruFuelMasuk(ctx context.Context) ([]model.A2BBaruFuelMasuk, error) {
	rows, err := r.readRows(ctx, a2bBaruFuelMasukSheet, columnName(len(a2bBaruFuelMasukHeaders)))
	if err != nil {
		return nil, err
	}
	orders := make([]model.A2BBaruFuelMasuk, 0, len(rows))
	for index, row := range rows {
		if index == 0 || isBlankRow(row) {
			continue
		}
		row = padRow(row, len(a2bBaruFuelMasukHeaders))
		orders = append(orders, model.A2BBaruFuelMasuk{
			FuelInID: cellString(row[0]), Phase: cellString(row[1]), Tanggal: cellString(row[2]), NoPO: cellString(row[3]),
			POVolume: parseFloatCell(row[4]), POHargaSatuan: parseFloatCell(row[5]),
			SiteVolume: parseFloatCell(row[6]), SiteHargaSatuan: parseFloatCell(row[7]),
			CreatedBy: cellString(row[8]), CreatedAt: lenientDateTime(row[9], r.location),
		})
	}
	return orders, nil
}

func (r *GoogleSheetsRepository) AppendA2BBaruFuelMasuk(ctx context.Context, orders []model.A2BBaruFuelMasuk) error {
	rows := make([][]interface{}, 0, len(orders))
	for _, order := range orders {
		rows = append(rows, []interface{}{
			order.FuelInID, order.Phase, order.Tanggal, order.NoPO,
			order.POVolume, order.POHargaSatuan, order.SiteVolume, order.SiteHargaSatuan,
			order.CreatedBy, formatDateTime(order.CreatedAt),
		})
	}
	return r.appendRows(ctx, a2bBaruFuelMasukSheet, rows)
}

func (r *GoogleSheetsRepository) ListA2BBaruFuelKeluar(ctx context.Context) ([]model.A2BBaruFuelKeluar, error) {
	rows, err := r.readRows(ctx, a2bBaruFuelKeluarSheet, columnName(len(a2bBaruFuelKeluarHeaders)))
	if err != nil {
		return nil, err
	}
	fills := make([]model.A2BBaruFuelKeluar, 0, len(rows))
	for index, row := range rows {
		if index == 0 || isBlankRow(row) {
			continue
		}
		row = padRow(row, len(a2bBaruFuelKeluarHeaders))
		fills = append(fills, model.A2BBaruFuelKeluar{
			FuelOutID: cellString(row[0]), Tanggal: cellString(row[1]),
			IDUnit: strings.TrimSpace(cellString(row[2])), NamaUnit: cellString(row[3]),
			HMAwalA2B: lenientOptionalFloat(row[4]), HMAkhirA2B: lenientOptionalFloat(row[5]),
			HMAwalFM: lenientOptionalFloat(row[6]), HMAkhirFM: lenientOptionalFloat(row[7]),
			FuelLiter: parseFloatCell(row[8]), HMSaatIsi: lenientOptionalFloat(row[9]),
			JamKerja: parseFloatCell(row[10]), Operator: cellString(row[11]),
			CreatedBy: cellString(row[12]), CreatedAt: lenientDateTime(row[13], r.location),
		})
	}
	return fills, nil
}

func (r *GoogleSheetsRepository) AppendA2BBaruFuelKeluar(ctx context.Context, fills []model.A2BBaruFuelKeluar) error {
	rows := make([][]interface{}, 0, len(fills))
	for _, fill := range fills {
		rows = append(rows, []interface{}{
			fill.FuelOutID, fill.Tanggal, fill.IDUnit, fill.NamaUnit,
			numberOrBlank(fill.HMAwalA2B), numberOrBlank(fill.HMAkhirA2B),
			numberOrBlank(fill.HMAwalFM), numberOrBlank(fill.HMAkhirFM),
			fill.FuelLiter, numberOrBlank(fill.HMSaatIsi), fill.JamKerja, fill.Operator,
			fill.CreatedBy, formatDateTime(fill.CreatedAt),
		})
	}
	return r.appendRows(ctx, a2bBaruFuelKeluarSheet, rows)
}

// numberOrBlank writes a reading nobody took as an empty cell.
func numberOrBlank(value *float64) interface{} {
	if value == nil {
		return ""
	}
	return *value
}

// lenientOptionalFloat reads a cell typed by hand: anything that is not a
// number is a reading nobody took, rather than a page that will not open.
func lenientOptionalFloat(cell interface{}) *float64 {
	value, err := parseOptionalFloatCell(cell)
	if err != nil {
		return nil
	}
	return value
}

func lenientDateTime(cell interface{}, location *time.Location) time.Time {
	value, err := parseDateTime(cellString(cell), location)
	if err != nil {
		return time.Time{}
	}
	return value
}
