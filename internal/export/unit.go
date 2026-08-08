package export

import (
	"strconv"

	"opp-management/internal/model"
)

// The photo column is left out of both registers on purpose: it holds a base64
// data URL tens of thousands of characters long, which no spreadsheet cell or
// printed page can show and which would bloat the file for nothing.

// The widths add up to the usable width of a landscape A4 page, so the table
// lines up with the letterhead rule above it.
var unitDTColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "Unit ID", Width: 38},
	{Header: "Nopol", Width: 38},
	{Header: "Panjang", Width: 28, Numeric: true, Decimals: 2},
	{Header: "Lebar", Width: 28, Numeric: true, Decimals: 2},
	{Header: "Tinggi", Width: 28, Numeric: true, Decimals: 2},
	{Header: "Driver", Width: 65},
	{Header: "Keterangan", Width: 44},
}

// UnitDTTable describes the dump truck register.
func UnitDTTable(units []model.UnitDT) Table {
	table := Table{
		SheetName: "Unit DT",
		Columns:   unitDTColumns,
		Rows:      make([][]string, 0, len(units)),
		Values:    make([][]interface{}, 0, len(units)),
	}
	for i, unit := range units {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), unit.UnitID, unit.Nopol,
			FormatFloat(unit.Panjang, 2), FormatFloat(unit.Lebar, 2), FormatFloat(unit.Tinggi, 2),
			unit.Driver, unit.Keterangan,
		})
		table.Values = append(table.Values, []interface{}{
			number, unit.UnitID, unit.Nopol,
			unit.Panjang, unit.Lebar, unit.Tinggi,
			unit.Driver, unit.Keterangan,
		})
	}
	return table
}

var unitA2BColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "No Urut", Width: 16},
	{Header: "Tanggal Input", Width: 26},
	{Header: "ID Unit", Width: 24},
	{Header: "Nama Unit", Width: 60},
	{Header: "Merek / Type", Width: 40},
	{Header: "Fuel Storage (L)", Width: 26, Numeric: true, Decimals: 2},
	{Header: "FR (L/jam)", Width: 23, Numeric: true, Decimals: 2},
	{Header: "Lokasi", Width: 30},
	{Header: "HM Awal", Width: 24, Numeric: true, Decimals: 2},
}

// UnitA2BTable describes the A2B register. Fuel capacity is totalled because
// the sum is the fleet's tank capacity, a figure worth having; the hour meters
// are not, since adding hour readings together means nothing.
func UnitA2BTable(units []model.UnitA2B) Table {
	table := Table{
		SheetName: "Unit A2B",
		Columns:   unitA2BColumns,
		Rows:      make([][]string, 0, len(units)),
		Values:    make([][]interface{}, 0, len(units)),
		Totals:    map[int]float64{},
	}
	var fuel float64
	for i, unit := range units {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), strconv.Itoa(unit.NoUrut), unit.TanggalIn, unit.IDUnit,
			unit.NamaUnit, unit.MerekType,
			FormatFloat(unit.FuelStorage, 2), FormatFloat(unit.FRUnit, 2),
			unit.Lokasi, FormatFloat(unit.HMAwal, 2),
		})
		table.Values = append(table.Values, []interface{}{
			number, unit.NoUrut, unit.TanggalIn, unit.IDUnit,
			unit.NamaUnit, unit.MerekType,
			unit.FuelStorage, unit.FRUnit, unit.Lokasi, unit.HMAwal,
		})
		fuel += unit.FuelStorage
	}
	table.Totals[6] = fuel
	return table
}

func UnitDTXLSX(units []model.UnitDT, meta Meta) ([]byte, error) {
	return RenderXLSX(UnitDTTable(units), meta)
}

func UnitDTPDF(units []model.UnitDT, meta Meta) ([]byte, error) {
	return RenderPDF(UnitDTTable(units), meta)
}

func UnitA2BXLSX(units []model.UnitA2B, meta Meta) ([]byte, error) {
	return RenderXLSX(UnitA2BTable(units), meta)
}

func UnitA2BPDF(units []model.UnitA2B, meta Meta) ([]byte, error) {
	return RenderPDF(UnitA2BTable(units), meta)
}
