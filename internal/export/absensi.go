package export

import (
	"fmt"
	"strconv"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/service"
)

// AbsensiXLSX writes the monthly attendance matrix: a column for every day of
// the month (✓ for hadir, S/I/C for an approved leave), then the weekly and
// monthly totals per employee. Excel only, because the point is a workable
// sheet someone can sort and sum, not a signed page.
func AbsensiXLSX(report *service.MonthlyAbsensi, meta Meta) ([]byte, error) {
	return RenderXLSX(AbsensiTable(report), meta)
}

// AbsensiTable describes the matrix in the table model both renderers take.
func AbsensiTable(report *service.MonthlyAbsensi) Table {
	columns := []Column{
		{Header: "No", Width: 7},
		{Header: "Nama", Width: 36},
		{Header: "Jabatan", Width: 22},
	}
	for day := 1; day <= report.Days; day++ {
		columns = append(columns, Column{Header: strconv.Itoa(day), Width: 7})
	}
	columns = append(columns,
		Column{Header: "Total M1", Width: 12, Numeric: true},
		Column{Header: "Total M2", Width: 12, Numeric: true},
		Column{Header: "Total M3", Width: 12, Numeric: true},
		Column{Header: "Total M4", Width: 12, Numeric: true},
		Column{Header: "Total Kehadiran", Width: 17, Numeric: true},
		Column{Header: "Total Sakit", Width: 12, Numeric: true},
		Column{Header: "Total Izin", Width: 12, Numeric: true},
		Column{Header: "Total Cuti", Width: 12, Numeric: true},
		Column{Header: "Total Tidak absen", Width: 17, Numeric: true},
		Column{Header: "Presentase", Width: 12, Numeric: true, Decimals: 1, Percent: true},
	)

	table := Table{
		SheetName:     "Absensi",
		Columns:       columns,
		Rows:          make([][]string, 0, len(report.Rows)),
		Values:        make([][]interface{}, 0, len(report.Rows)),
		FilterColumns: 3,
	}
	for i, row := range report.Rows {
		cells := []string{strconv.Itoa(row.No), row.Nama, row.Jabatan}
		values := []interface{}{row.No, row.Nama, row.Jabatan}
		for _, mark := range row.Hari {
			cells = append(cells, mark)
			values = append(values, mark)
		}
		cells = append(cells,
			FormatFloat(float64(row.M1), 0),
			FormatFloat(float64(row.M2), 0),
			FormatFloat(float64(row.M3), 0),
			FormatFloat(float64(row.M4), 0),
			FormatFloat(float64(row.Hadir), 0),
			FormatFloat(float64(row.Sakit), 0),
			FormatFloat(float64(row.Izin), 0),
			FormatFloat(float64(row.Cuti), 0),
			FormatFloat(float64(row.TidakAbsen), 0),
			fmt.Sprintf("%.1f%%", row.Persentase),
		)
		values = append(values,
			row.M1, row.M2, row.M3, row.M4,
			row.Hadir, row.Sakit, row.Izin, row.Cuti, row.TidakAbsen,
			absensiPercentFormula(report, i),
		)
		table.Rows = append(table.Rows, cells)
		table.Values = append(table.Values, values)
	}
	return table
}

// absensiPercentFormula is the live attendance rate: every day that was not
// "tidak absen" (hadir plus any approved leave) over the days the employee
// actually owed. The owed days are the sum of the five total columns, because
// each active day lands in exactly one of them. A formula, not the precomputed
// number, so the sheet still reads correctly when someone corrects a day by
// hand. The guard keeps a row with no owed days from showing #DIV/0!.
func absensiPercentFormula(report *service.MonthlyAbsensi, rowIndex int) Formula {
	hadir, _ := excelize.ColumnNumberToName(3 + report.Days + 4 + 1)
	sakit, _ := excelize.ColumnNumberToName(3 + report.Days + 5 + 1)
	izin, _ := excelize.ColumnNumberToName(3 + report.Days + 6 + 1)
	cuti, _ := excelize.ColumnNumberToName(3 + report.Days + 7 + 1)
	tidakAbsen, _ := excelize.ColumnNumberToName(3 + report.Days + 8 + 1)
	row := headerRow + 1 + rowIndex
	return Formula{Expression: fmt.Sprintf(
		"IF(%s%d+%s%d+%s%d+%s%d+%s%d=0,0,(%s%d+%s%d+%s%d+%s%d)/(%s%d+%s%d+%s%d+%s%d+%s%d))",
		hadir, row, sakit, row, izin, row, cuti, row, tidakAbsen, row,
		hadir, row, sakit, row, izin, row, cuti, row,
		hadir, row, sakit, row, izin, row, cuti, row, tidakAbsen, row)}
}
