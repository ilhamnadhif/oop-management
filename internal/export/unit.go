package export

import (
	"strconv"

	"opp-management/internal/model"
)

// The photo column is left out of the register on purpose: it holds a base64
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

func UnitDTXLSX(units []model.UnitDT, meta Meta) ([]byte, error) {
	return RenderXLSX(UnitDTTable(units), meta)
}

func UnitDTPDF(units []model.UnitDT, meta Meta) ([]byte, error) {
	return RenderPDF(UnitDTTable(units), meta)
}
