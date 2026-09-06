package export

import (
	"strconv"

	"opp-management/internal/model"
)

// The dispensing report is one row per fill. The photo of the closing flowmeter
// is left out for the same reason the registers leave a machine's photo out: it
// is a base64 data URL tens of thousands of characters long, which no cell or
// printed page can show and which would bloat the file for nothing.
//
// The widths add up to the usable width of a landscape A4 page, so the table
// lines up with the letterhead rule above it.
var fuelKeluarColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "No Transaksi", Width: 42},
	{Header: "Tanggal", Width: 26},
	{Header: "ID Unit", Width: 24},
	{Header: "Nama Unit", Width: 46},
	{Header: "HM Awal FM", Width: 24, Numeric: true, Decimals: 2},
	{Header: "HM Akhir FM", Width: 24, Numeric: true, Decimals: 2},
	{Header: "Fuel (L)", Width: 26, Numeric: true, Decimals: 2},
	{Header: "HM Alat Berat", Width: 30, Numeric: true, Decimals: 2},
	{Header: "Operator", Width: 27},
}

// FuelKeluarTable describes the dispensing sheet for both formats. The litres
// are totalled because a range's fuel is a figure worth having; the flowmeter
// and the machine's own hour meter are not, since one fill's odometer added to
// the next means nothing.
func FuelKeluarTable(rows []model.FuelKeluar) Table {
	table := Table{
		SheetName: "Fuel Keluar",
		Columns:   fuelKeluarColumns,
		Rows:      make([][]string, 0, len(rows)),
		Values:    make([][]interface{}, 0, len(rows)),
	}
	var liter float64
	for i, row := range rows {
		number := i + 1
		// A fill nobody read the machine's meter at prints a dash and stores an
		// empty cell. A nought would say the meter read zero, which is a
		// different fact about the machine.
		meterText := "-"
		var meterValue interface{}
		if row.HMAlatBerat != nil {
			meterText = FormatFloat(*row.HMAlatBerat, 2)
			meterValue = *row.HMAlatBerat
		}
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), row.FuelOutID, row.Tanggal, row.IDUnit, row.NamaUnit,
			FormatFloat(row.HMAwalFlowMeter, 2), FormatFloat(row.HMAkhirFlowMeter, 2),
			FormatFloat(row.Liter, 2), meterText, row.Operator,
		})
		table.Values = append(table.Values, []interface{}{
			number, row.FuelOutID, row.Tanggal, row.IDUnit, row.NamaUnit,
			row.HMAwalFlowMeter, row.HMAkhirFlowMeter,
			row.Liter, meterValue, row.Operator,
		})
		liter += row.Liter
	}
	if len(rows) > 0 {
		table.Totals = map[int]float64{7: roundExport(liter)}
	}
	return table
}

// FuelKeluarXLSX writes the dispensing sheet as a spreadsheet.
func FuelKeluarXLSX(rows []model.FuelKeluar, meta Meta) ([]byte, error) {
	return RenderXLSX(FuelKeluarTable(rows), meta)
}

// FuelKeluarPDF prints the same sheet as the signable report.
func FuelKeluarPDF(rows []model.FuelKeluar, meta Meta) ([]byte, error) {
	return RenderPDF(FuelKeluarTable(rows), meta)
}
