package export

import (
	"strconv"

	"opp-management/internal/service"
)

// The performance report is the overview's per-unit table on paper: one row per
// machine, over the range asked for. The figures are the ones the overview
// shows, recomputed from each machine's totals rather than averaged, so a short
// shift does not weigh as much as a full one.
//
// The widths add up to the usable width of a landscape A4 page, so the table
// lines up with the letterhead rule above it.
var a2bPerformanceColumns = []Column{
	{Header: "No", Width: 12},
	{Header: "Unit ID", Width: 32},
	{Header: "Nama Unit", Width: 70},
	{Header: "Shift", Width: 22, Numeric: true},
	{Header: "Total HM (jam)", Width: 34, Numeric: true, Decimals: 2},
	{Header: "Fuel (L)", Width: 30, Numeric: true, Decimals: 2},
	{Header: "Fuel Ratio (L/jam)", Width: 38, Numeric: true, Decimals: 2},
	{Header: "PA (%)", Width: 22, Numeric: true, Decimals: 1},
	{Header: "UA (%)", Width: 21, Numeric: true, Decimals: 1},
}

// A2BPerformanceTable describes the machine performance report for both
// formats. Shifts, hours and fuel are totalled because the fleet's sums are
// worth having; PA, UA and the fuel ratio are not, since a column of
// percentages and ratios adds up to nothing.
func A2BPerformanceTable(units []service.A2BUnitPerformance) Table {
	table := Table{
		SheetName: "Performance Unit",
		Columns:   a2bPerformanceColumns,
		Rows:      make([][]string, 0, len(units)),
		Values:    make([][]interface{}, 0, len(units)),
		Totals:    map[int]float64{},
	}
	var shifts, hours, fuel float64
	for i, unit := range units {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), unit.IDUnit, unit.NamaUnit,
			strconv.Itoa(unit.Shifts),
			FormatFloat(unit.TotalHM, 2), FormatFloat(unit.Fuel, 2), FormatFloat(unit.FuelRatio, 2),
			FormatFloat(unit.PA, 1), FormatFloat(unit.UA, 1),
		})
		table.Values = append(table.Values, []interface{}{
			number, unit.IDUnit, unit.NamaUnit,
			unit.Shifts,
			unit.TotalHM, unit.Fuel, unit.FuelRatio,
			unit.PA, unit.UA,
		})
		shifts += float64(unit.Shifts)
		hours += unit.TotalHM
		fuel += unit.Fuel
	}
	table.Totals[3] = shifts
	table.Totals[4] = hours
	table.Totals[5] = fuel
	return table
}

// A2BPerformanceXLSX writes the performance table as a spreadsheet.
func A2BPerformanceXLSX(units []service.A2BUnitPerformance, meta Meta) ([]byte, error) {
	return RenderXLSX(A2BPerformanceTable(units), meta)
}

// A2BPerformancePDF prints the same table as the signable report.
func A2BPerformancePDF(units []service.A2BUnitPerformance, meta Meta) ([]byte, error) {
	return RenderPDF(A2BPerformanceTable(units), meta)
}
