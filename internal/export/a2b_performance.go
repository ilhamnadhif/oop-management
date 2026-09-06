package export

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"opp-management/internal/model"
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

// The detail behind the summary: one row per shift, in the order the machine
// worked them. The machine's name is left out - the id names it, and the
// summary above says which machine that is - to leave the width for the reasons
// a shift was not spent working, which is what the availability figures are
// actually explained by.
var a2bReadingColumns = []Column{
	{Header: "No", Width: 10},
	{Header: "ID Unit", Width: 24},
	{Header: "Tanggal", Width: 24},
	{Header: "Shift", Width: 20},
	{Header: "Operator", Width: 30},
	{Header: "HM Awal", Width: 20, Numeric: true, Decimals: 2},
	{Header: "HM Akhir", Width: 20, Numeric: true, Decimals: 2},
	{Header: "Total HM", Width: 22, Numeric: true, Decimals: 2},
	{Header: "PA (%)", Width: 18, Numeric: true, Decimals: 1},
	{Header: "UA (%)", Width: 18, Numeric: true, Decimals: 1},
	{Header: "Standby (m)", Width: 20, Numeric: true, Decimals: 0},
	{Header: "Breakdown (m)", Width: 22, Numeric: true, Decimals: 0},
	{Header: "Keterangan", Width: 33},
}

// A2BReadingTable describes the shifts the summary was built from. Hours and
// lost minutes add up; PA and UA do not, and are left out of the summary line
// rather than printed as a sum that means nothing - the figures per machine are
// in the table this is attached to.
func A2BReadingTable(readings []model.HourMeter) Table {
	table := Table{
		SheetName: "Rincian Shift",
		Columns:   a2bReadingColumns,
		Rows:      make([][]string, 0, len(readings)),
		Values:    make([][]interface{}, 0, len(readings)),
	}
	var hours, standby, breakdown float64
	for i, row := range readings {
		number := i + 1
		reasons := stoppageReasons(row)
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), row.IDUnit, row.Tanggal, row.Shift, row.Operator,
			FormatFloat(row.HMAwal, 2), FormatFloat(row.HMAkhir, 2), FormatFloat(row.TotalHM, 2),
			FormatFloat(row.PA, 1), FormatFloat(row.UA, 1),
			FormatFloat(row.TotalStandby, 0), FormatFloat(row.TotalBreakdown, 0),
			reasons,
		})
		table.Values = append(table.Values, []interface{}{
			number, row.IDUnit, row.Tanggal, row.Shift, row.Operator,
			row.HMAwal, row.HMAkhir, row.TotalHM,
			row.PA, row.UA,
			row.TotalStandby, row.TotalBreakdown,
			reasons,
		})
		hours += row.TotalHM
		standby += row.TotalStandby
		breakdown += row.TotalBreakdown
	}
	if len(readings) > 0 {
		table.Totals = map[int]float64{
			7:  roundExport(hours),
			10: roundExport(standby),
			11: roundExport(breakdown),
		}
	}
	return table
}

// stoppageReasons words why a shift was not spent working, standby and
// breakdown together: they are the same question asked of the same shift, and
// splitting them across two columns would leave both half empty.
func stoppageReasons(row model.HourMeter) string {
	minutes := make(map[string]float64)
	order := make([]string, 0, len(row.Standby)+len(row.Breakdown))
	add := func(variable string, menit float64) {
		variable = strings.TrimSpace(variable)
		if variable == "" || menit <= 0 {
			return
		}
		if _, seen := minutes[variable]; !seen {
			order = append(order, variable)
		}
		minutes[variable] += menit
	}
	for _, line := range row.Standby {
		add(line.Variable, line.Menit)
	}
	for _, line := range row.Breakdown {
		add(line.Variable, line.Menit)
	}
	// Longest first: the reason that cost the most is the one worth reading.
	sort.SliceStable(order, func(i, j int) bool { return minutes[order[i]] > minutes[order[j]] })

	parts := make([]string, 0, len(order))
	for _, variable := range order {
		parts = append(parts, fmt.Sprintf("%s %s m", variable, FormatFloat(minutes[variable], 0)))
	}
	return strings.Join(parts, " · ")
}

// a2bPerformanceWithDetail is the summary with the shifts behind it attached,
// which is what both formats render.
func a2bPerformanceWithDetail(report *service.A2BPerformanceReport) Table {
	table := A2BPerformanceTable(report.Units)
	if len(report.Readings) > 0 {
		detail := A2BReadingTable(report.Readings)
		table.Detail = &detail
	}
	return table
}

// A2BPerformanceXLSX writes the performance table as a spreadsheet, with the
// shifts behind it on a second sheet.
func A2BPerformanceXLSX(report *service.A2BPerformanceReport, meta Meta) ([]byte, error) {
	return RenderXLSX(a2bPerformanceWithDetail(report), meta)
}

// A2BPerformancePDF prints the same table as the signable report, with the
// shifts behind it after the signature.
func A2BPerformancePDF(report *service.A2BPerformanceReport, meta Meta) ([]byte, error) {
	return RenderPDF(a2bPerformanceWithDetail(report), meta)
}
