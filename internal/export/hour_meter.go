package export

import (
	"math"
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
	{Header: "PA (%)", Width: 35, Numeric: true, Decimals: 1},
	{Header: "UA (%)", Width: 35, Numeric: true, Decimals: 1},
}

// HourMeterTable describes the hour meter readings for both formats. It closes
// with a summary row: the hours worked add up, while PA and UA are averaged,
// because a column of percentages added together is a number about nothing.
func HourMeterTable(rows []model.HourMeter) Table {
	table := Table{
		SheetName: "Input HM",
		Columns:   hourMeterColumns,
		Rows:      make([][]string, 0, len(rows)),
		Values:    make([][]interface{}, 0, len(rows)),
	}
	var totalHM, sumPA, sumUA float64
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
		totalHM += row.TotalHM
		sumPA += row.PA
		sumUA += row.UA
	}
	// Nothing read means nothing to summarise. A row of noughts would read as a
	// fleet that did no work rather than as an empty range.
	if len(rows) > 0 {
		count := float64(len(rows))
		table.Totals = map[int]float64{
			4: roundExport(totalHM),
			5: roundExport(sumPA / count),
			6: roundExport(sumUA / count),
		}
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

// roundExport trims the drift a running sum picks up, so a total of hours reads
// as the hours rather than as 13.999999999999998.
func roundExport(value float64) float64 {
	return math.Round(value*100) / 100
}
