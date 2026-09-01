package export

import (
	"strconv"

	"opp-management/internal/model"
)

// The hour meter report is one row per reading. The operator's own inputs - the
// reading itself - are what it exists to print; the derived figures (PA and UA)
// ride along because they are what a reading is judged against. Fuel and the
// standby/breakdown minutes are left out: the shift's fuel goes in the fuel
// sheets and the delay breakdown belongs to the overview, not a per-reading
// log.
var hourMeterColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "Tanggal", Width: 55},
	{Header: "HM Awal", Width: 48, Numeric: true, Decimals: 2},
	{Header: "HM Akhir", Width: 48, Numeric: true, Decimals: 2},
	{Header: "Total HM", Width: 48, Numeric: true, Decimals: 2},
	{Header: "PA", Width: 35, Numeric: true, Decimals: 1},
	{Header: "UA", Width: 35, Numeric: true, Decimals: 1},
}

// HourMeterTable describes the hour meter readings for both formats.
func HourMeterTable(rows []model.HourMeter) Table {
	table := Table{
		SheetName: "Input HM",
		Columns:   hourMeterColumns,
		Rows:      make([][]string, 0, len(rows)),
		Values:    make([][]interface{}, 0, len(rows)),
	}
	for i, row := range rows {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), row.Tanggal,
			FormatFloat(row.HMAwal, 2), FormatFloat(row.HMAkhir, 2), FormatFloat(row.TotalHM, 2),
			FormatFloat(row.PA, 1), FormatFloat(row.UA, 1),
		})
		table.Values = append(table.Values, []interface{}{
			number, row.Tanggal,
			row.HMAwal, row.HMAkhir, row.TotalHM,
			row.PA, row.UA,
		})
	}
	return table
}

// HourMeterXLSX writes the readings as a spreadsheet.
func HourMeterXLSX(rows []model.HourMeter, meta Meta) ([]byte, error) {
	return RenderXLSX(HourMeterTable(rows), meta)
}

// HourMeterPDF prints the same readings as the signable report.
func HourMeterPDF(rows []model.HourMeter, meta Meta) ([]byte, error) {
	return RenderPDF(HourMeterTable(rows), meta)
}
