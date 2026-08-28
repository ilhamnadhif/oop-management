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

// AbsensiPDF prints the same matrix, but the forty-odd columns cannot hold
// their Excel widths on paper, so they are squeezed into the one landscape page
// and set at the compact type size, without a signature block.
func AbsensiPDF(report *service.MonthlyAbsensi, meta Meta) ([]byte, error) {
	return RenderPDFCompact(absensiPDFTable(report), meta)
}

// AbsensiTable describes the matrix for the spreadsheet.
func AbsensiTable(report *service.MonthlyAbsensi) Table {
	columns := absensiColumns(report.Days,
		7, 36, 22, 7,
		[]float64{12, 12, 12, 12, 17, 12, 12, 12, 17, 12})
	rows, values := absensiRows(report)
	return Table{
		SheetName:     "Absensi",
		Columns:       columns,
		Rows:          rows,
		Values:        values,
		FilterColumns: 3,
	}
}

// absensiPDFTable is the same matrix at the widths that fit the usable width of
// a landscape A4 page (297 - 2x8 = 281mm) whatever the month's length.
func absensiPDFTable(report *service.MonthlyAbsensi) Table {
	columns := absensiColumns(report.Days,
		6, 30, 15, 4.5,
		[]float64{8, 8, 8, 8, 10, 7, 7, 7, 10, 8})
	rows, values := absensiRows(report)
	return Table{
		SheetName: "Absensi",
		Columns:   columns,
		Rows:      rows,
		Values:    values,
	}
}

// absensiColumns lays out the matrix. The spreadsheet and the PDF share the
// structure - identity, one column per day, then the totals - but not the
// widths, since the PDF has to fit the whole thing on one landscape page.
func absensiColumns(days int, no, nama, jabatan, day float64, totals []float64) []Column {
	columns := []Column{
		{Header: "No", Width: no},
		{Header: "Nama", Width: nama},
		{Header: "Jabatan", Width: jabatan},
	}
	for number := 1; number <= days; number++ {
		columns = append(columns, Column{Header: strconv.Itoa(number), Width: day})
	}
	totalNames := []string{"Total M1", "Total M2", "Total M3", "Total M4", "Total Kehadiran",
		"Total Sakit", "Total Izin", "Total Cuti", "Total Tidak absen", "Presentase"}
	for i, name := range totalNames {
		if name == "Presentase" {
			columns = append(columns, Column{Header: name, Width: totals[i], Numeric: true, Decimals: 1, Percent: true})
		} else {
			columns = append(columns, Column{Header: name, Width: totals[i], Numeric: true})
		}
	}
	return columns
}

// absensiRows builds the shared cell data: the identity, the day marks and the
// totals, with the percentage kept live as a formula for the spreadsheet.
func absensiRows(report *service.MonthlyAbsensi) (rows [][]string, values [][]interface{}) {
	rows = make([][]string, 0, len(report.Rows))
	values = make([][]interface{}, 0, len(report.Rows))
	for i, row := range report.Rows {
		cells := []string{strconv.Itoa(row.No), row.Nama, row.Jabatan}
		rowValues := []interface{}{row.No, row.Nama, row.Jabatan}
		for _, mark := range row.Hari {
			cells = append(cells, mark)
			rowValues = append(rowValues, mark)
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
		rowValues = append(rowValues,
			row.M1, row.M2, row.M3, row.M4,
			row.Hadir, row.Sakit, row.Izin, row.Cuti, row.TidakAbsen,
			absensiPercentFormula(report, i),
		)
		rows = append(rows, cells)
		values = append(values, rowValues)
	}
	return rows, values
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
