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
	{Header: "No Transaksi", Width: 38},
	{Header: "Tanggal", Width: 24},
	{Header: "ID Unit", Width: 22},
	{Header: "Nama Unit", Width: 36},
	{Header: "HM Awal FM", Width: 23, Numeric: true, Decimals: 2},
	{Header: "HM Akhir FM", Width: 23, Numeric: true, Decimals: 2},
	{Header: "Fuel (L)", Width: 24, Numeric: true, Decimals: 2},
	{Header: "HM Alat Berat", Width: 26, Numeric: true, Decimals: 2},
	{Header: "Fuel Ratio (L/jam)", Width: 28, Numeric: true, Decimals: 2},
	{Header: "Operator", Width: 25},
}

// FuelKeluarTable describes the dispensing sheet for both formats. ratios is
// each row's fuel ratio, nil where it has none; a short or nil slice leaves the
// rest without one. The litres are totalled because a range's fuel is a figure
// worth having; the flowmeter, the machine's own hour meter and the ratio are
// not, since one fill's odometer added to the next means nothing and the
// ratios of different machines do not add.
func FuelKeluarTable(rows []model.FuelKeluar, ratios []*float64) Table {
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
		// No ratio prints a dash for the same reason.
		ratioText := "-"
		var ratioValue interface{}
		if i < len(ratios) && ratios[i] != nil {
			ratioText = FormatFloat(*ratios[i], 2)
			ratioValue = *ratios[i]
		}
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), row.FuelOutID, row.Tanggal, row.IDUnit, row.NamaUnit,
			FormatFloat(row.HMAwalFlowMeter, 2), FormatFloat(row.HMAkhirFlowMeter, 2),
			FormatFloat(row.Liter, 2), meterText, ratioText, row.Operator,
		})
		table.Values = append(table.Values, []interface{}{
			number, row.FuelOutID, row.Tanggal, row.IDUnit, row.NamaUnit,
			row.HMAwalFlowMeter, row.HMAkhirFlowMeter,
			row.Liter, meterValue, ratioValue, row.Operator,
		})
		liter += row.Liter
	}
	if len(rows) > 0 {
		table.Totals = map[int]float64{7: roundExport(liter)}
	}
	return table
}

// FuelKeluarXLSX writes the dispensing sheet as a spreadsheet.
func FuelKeluarXLSX(rows []model.FuelKeluar, ratios []*float64, meta Meta) ([]byte, error) {
	return RenderXLSX(FuelKeluarTable(rows, ratios), meta)
}

// FuelKeluarPDF prints the same sheet as the signable report.
func FuelKeluarPDF(rows []model.FuelKeluar, ratios []*float64, meta Meta) ([]byte, error) {
	return RenderPDF(FuelKeluarTable(rows, ratios), meta)
}
