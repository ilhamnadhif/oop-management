package export

import (
	"math"
	"strconv"
	"strings"

	"opp-management/internal/model"
)

// The hour meter report is one row per reading. The operator's own inputs - the
// machine, what it worked on and where, and the reading itself - are what it
// exists to print; the derived figures (PA and UA) ride along because they are
// what a reading is judged against. Fuel and the standby/breakdown minutes are
// left out: the shift's fuel goes in the fuel sheets and the delay breakdown
// belongs to the overview, not a per-reading log.
var hourMeterColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "Tanggal", Width: 30},
	{Header: "ID Unit", Width: 28},
	{Header: "Pekerjaan", Width: 50},
	{Header: "Lokasi", Width: 38},
	{Header: "HM Awal", Width: 28, Numeric: true, Decimals: 2},
	{Header: "HM Akhir", Width: 28, Numeric: true, Decimals: 2},
	{Header: "Total HM", Width: 25, Numeric: true, Decimals: 2},
	{Header: "PA (%)", Width: 21, Numeric: true, Decimals: 1},
	{Header: "UA (%)", Width: 21, Numeric: true, Decimals: 1},
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
		pekerjaan, lokasi := orDash(row.Pekerjaan), orDash(row.Lokasi)
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), row.Tanggal, row.IDUnit, pekerjaan, lokasi,
			FormatFloat(row.HMAwal, 2), FormatFloat(row.HMAkhir, 2), FormatFloat(row.TotalHM, 2),
			FormatFloat(row.PA, 1), FormatFloat(row.UA, 1),
		})
		table.Values = append(table.Values, []interface{}{
			number, row.Tanggal, row.IDUnit, pekerjaan, lokasi,
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
			7: roundExport(totalHM),
			8: roundExport(sumPA / count),
			9: roundExport(sumUA / count),
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

// orDash prints an empty text cell as a dash, so a reading from before a column
// was asked for reads as not recorded rather than as a cell somebody missed.
func orDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

// roundExport trims the drift a running sum picks up, so a total of hours reads
// as the hours rather than as 13.999999999999998.
func roundExport(value float64) float64 {
	return math.Round(value*100) / 100
}
