package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

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
	"created_by", "created_at", "foto_ada", "foto_akhir_fm",
}

// a2bBaruFuelKeluarListWidth is every column but the photo, which is read one
// row at a time when somebody opens it.
var a2bBaruFuelKeluarListWidth = len(a2bBaruFuelKeluarHeaders) - 1

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
	rows, err := r.readRows(ctx, a2bBaruFuelKeluarSheet, columnName(a2bBaruFuelKeluarListWidth))
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
			HasFoto: strings.TrimSpace(cellString(row[14])) != "",
		})
	}
	return fills, nil
}

// ReadA2BBaruFuelKeluarPhoto finds a dispense by its number and reads its
// photo, empty when it has none.
func (r *GoogleSheetsRepository) ReadA2BBaruFuelKeluarPhoto(ctx context.Context, fuelOutID string) (string, error) {
	ids, err := r.readRows(ctx, a2bBaruFuelKeluarSheet, "A")
	if err != nil {
		return "", err
	}
	for index, row := range ids {
		if index == 0 || len(row) == 0 || !strings.EqualFold(strings.TrimSpace(cellString(row[0])), strings.TrimSpace(fuelOutID)) {
			continue
		}
		column := columnName(len(a2bBaruFuelKeluarHeaders))
		values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID,
			fmt.Sprintf("%s!%s%d", quoteSheet(a2bBaruFuelKeluarSheet), column, index+1)).
			ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
		if err != nil {
			return "", fmt.Errorf("read a2b baru photo %s: %w", fuelOutID, err)
		}
		if len(values.Values) == 0 || len(values.Values[0]) == 0 {
			return "", nil
		}
		return cellString(values.Values[0][0]), nil
	}
	return "", ErrNotFound
}

func (r *GoogleSheetsRepository) AppendA2BBaruFuelKeluar(ctx context.Context, fills []model.A2BBaruFuelKeluar) error {
	rows := make([][]interface{}, 0, len(fills))
	for _, fill := range fills {
		rows = append(rows, []interface{}{
			fill.FuelOutID, fill.Tanggal, fill.IDUnit, fill.NamaUnit,
			numberOrBlank(fill.HMAwalA2B), numberOrBlank(fill.HMAkhirA2B),
			numberOrBlank(fill.HMAwalFM), numberOrBlank(fill.HMAkhirFM),
			fill.FuelLiter, numberOrBlank(fill.HMSaatIsi), fill.JamKerja, fill.Operator,
			fill.CreatedBy, formatDateTime(fill.CreatedAt), photoMark(fill.Foto), fill.Foto,
		})
	}
	return r.appendRows(ctx, a2bBaruFuelKeluarSheet, rows)
}

// photoMark fills the column the listing reads to know a photo is there.
func photoMark(foto string) string {
	if strings.TrimSpace(foto) == "" {
		return ""
	}
	return "ya"
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

// photoChunk is how many rows one photo read or write covers. A photo is up to
// a cell's worth of base64, so a chunk is a few megabytes.
const photoChunk = 40

// FuelKeluarPhotosByID reads every photo on the older Fuel Keluar sheet, by
// transaction number, a chunk of rows at a time. It is for copying them into
// A2B Baru once, not for a page.
func (r *GoogleSheetsRepository) FuelKeluarPhotosByID(ctx context.Context) (map[string]string, error) {
	ids, err := r.readRows(ctx, fuelKeluarSheet, "A")
	if err != nil {
		return nil, err
	}
	photos := make(map[string]string)
	for start := 2; start <= len(ids); start += photoChunk {
		end := start + photoChunk - 1
		if end > len(ids) {
			end = len(ids)
		}
		values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID,
			fmt.Sprintf("%s!%s%d:%s%d", quoteSheet(fuelKeluarSheet), fuelOutPhotoColumn, start, fuelOutPhotoColumn, end)).
			ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
		if err != nil {
			return nil, fmt.Errorf("read fuel keluar photos from row %d: %w", start, err)
		}
		for offset, row := range values.Values {
			index := start - 1 + offset
			if index >= len(ids) || len(ids[index]) == 0 || len(row) == 0 {
				continue
			}
			id := strings.TrimSpace(cellString(ids[index][0]))
			if foto := strings.TrimSpace(cellString(row[0])); id != "" && foto != "" {
				photos[id] = foto
			}
		}
	}
	return photos, nil
}

// FillA2BBaruFuelKeluarPhotos writes photos into the A2B Baru dispenses that
// have none yet, matched by transaction number, and reports how many it
// wrote. A row that already has a photo keeps it.
func (r *GoogleSheetsRepository) FillA2BBaruFuelKeluarPhotos(ctx context.Context, photos map[string]string) (int, error) {
	markColumn := columnName(len(a2bBaruFuelKeluarHeaders) - 1)
	photoColumn := columnName(len(a2bBaruFuelKeluarHeaders))
	rows, err := r.readRows(ctx, a2bBaruFuelKeluarSheet, columnName(a2bBaruFuelKeluarListWidth))
	if err != nil {
		return 0, err
	}
	var pending []*sheets.ValueRange
	for index, row := range rows {
		if index == 0 || len(row) == 0 {
			continue
		}
		row = padRow(row, a2bBaruFuelKeluarListWidth)
		foto := photos[strings.TrimSpace(cellString(row[0]))]
		if foto == "" || strings.TrimSpace(cellString(row[a2bBaruFuelKeluarListWidth-1])) != "" {
			continue
		}
		pending = append(pending, &sheets.ValueRange{
			Range:  fmt.Sprintf("%s!%s%d:%s%d", quoteSheet(a2bBaruFuelKeluarSheet), markColumn, index+1, photoColumn, index+1),
			Values: [][]interface{}{{"ya", foto}},
		})
	}
	written := 0
	for start := 0; start < len(pending); start += photoChunk {
		end := start + photoChunk
		if end > len(pending) {
			end = len(pending)
		}
		if _, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
			ValueInputOption: "RAW", Data: pending[start:end],
		}).Context(ctx).Do(); err != nil {
			return written, fmt.Errorf("write a2b baru photos: %w", err)
		}
		written = end
	}
	return written, nil
}
