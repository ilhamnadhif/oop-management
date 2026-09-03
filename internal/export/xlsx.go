package export

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// headerRow is where every sheet's column headings sit: three letterhead rows,
// one blank row, then the header.
const headerRow = 5

// RenderXLSX writes the spreadsheet: a short letterhead, then the table with a
// coloured, frozen header so the columns stay identifiable while scrolling
// thousands of rows.
func RenderXLSX(table Table, meta Meta) ([]byte, error) {
	file := excelize.NewFile()
	defer file.Close()

	sheet := table.SheetName
	index, err := file.NewSheet(sheet)
	if err != nil {
		return nil, fmt.Errorf("create sheet: %w", err)
	}
	file.SetActiveSheet(index)
	if err := file.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("remove default sheet: %w", err)
	}

	styles, err := newStyles(file)
	if err != nil {
		return nil, err
	}

	lastColumn, err := excelize.ColumnNumberToName(len(table.Columns))
	if err != nil {
		return nil, err
	}

	_ = file.SetCellValue(sheet, "A1", meta.Company)
	_ = file.SetCellStyle(sheet, "A1", "A1", styles.title)
	_ = file.MergeCell(sheet, "A1", lastColumn+"1")

	_ = file.SetCellValue(sheet, "A2", meta.Title)
	_ = file.SetCellStyle(sheet, "A2", "A2", styles.subtitle)
	_ = file.MergeCell(sheet, "A2", lastColumn+"2")

	_ = file.SetCellValue(sheet, "A3",
		fmt.Sprintf("Periode: %s  ·  Dicetak: %s  ·  %d baris",
			meta.periodLabel(), formatIndonesianDate(meta.Generated), len(table.Values)))
	_ = file.SetCellStyle(sheet, "A3", "A3", styles.subtitle)
	_ = file.MergeCell(sheet, "A3", lastColumn+"3")

	for i, column := range table.Columns {
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return nil, err
		}
		cell := fmt.Sprintf("%s%d", name, headerRow)
		_ = file.SetCellValue(sheet, cell, column.Header)
		_ = file.SetCellStyle(sheet, cell, cell, styles.header)
		// The PDF widths are millimetres; a spreadsheet counts characters.
		_ = file.SetColWidth(sheet, name, name, column.Width*0.85)
	}
	_ = file.SetRowHeight(sheet, headerRow, 26)

	for i, values := range table.Values {
		rowNumber := headerRow + 1 + i
		for j, value := range values {
			name, err := excelize.ColumnNumberToName(j + 1)
			if err != nil {
				return nil, err
			}
			cell := fmt.Sprintf("%s%d", name, rowNumber)
			if formula, ok := value.(Formula); ok {
				_ = file.SetCellFormula(sheet, cell, formula.Expression)
			} else {
				_ = file.SetCellValue(sheet, cell, value)
			}
			_ = file.SetCellStyle(sheet, cell, cell, styles.forColumn(table.Columns[j]))
		}
	}

	if len(table.Values) > 0 && table.hasTotals() {
		totalRow := headerRow + 1 + len(table.Values)
		start := table.totalsStart()
		labelEnd, err := excelize.ColumnNumberToName(start)
		if err != nil {
			return nil, err
		}
		first := fmt.Sprintf("A%d", totalRow)
		last := fmt.Sprintf("%s%d", labelEnd, totalRow)
		_ = file.SetCellValue(sheet, first, "TOTAL")
		_ = file.MergeCell(sheet, first, last)
		_ = file.SetCellStyle(sheet, first, last, styles.totalLabel)

		for column, value := range table.Totals {
			name, err := excelize.ColumnNumberToName(column + 1)
			if err != nil {
				return nil, err
			}
			cell := fmt.Sprintf("%s%d", name, totalRow)
			_ = file.SetCellValue(sheet, cell, value)
			_ = file.SetCellStyle(sheet, cell, cell, styles.total)
		}
	}

	// Freeze the letterhead and header so the columns stay named while scrolling.
	if err := file.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      headerRow,
		TopLeftCell: fmt.Sprintf("A%d", headerRow+1),
		ActivePane:  "bottomLeft",
	}); err != nil {
		return nil, fmt.Errorf("freeze header: %w", err)
	}
	if len(table.Values) > 0 {
		filterEnd := lastColumn
		if table.FilterColumns > 0 {
			if filterEnd, err = excelize.ColumnNumberToName(table.FilterColumns); err != nil {
				return nil, err
			}
		}
		_ = file.AutoFilter(sheet,
			fmt.Sprintf("A%d:%s%d", headerRow, filterEnd, headerRow+len(table.Values)), nil)
	}

	// A cell written as a formula has no cached value, so the workbook has to
	// recompute it the moment it opens, or the percentage would sit blank.
	fullCalc := true
	if err := file.SetCalcProps(&excelize.CalcPropsOptions{FullCalcOnLoad: &fullCalc}); err != nil {
		return nil, fmt.Errorf("set calc props: %w", err)
	}

	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buffer.Bytes(), nil
}

type sheetStyles struct {
	title      int
	subtitle   int
	header     int
	text       int
	whole      int
	number     int
	volume     int
	percent    int
	total      int
	totalLabel int
}

// forColumn picks the number format. Counts stay whole (no trailing ",00"),
// dimensions take two decimals, volumes four - the deviation is the figure
// people argue over, so it keeps its precision - and a percentage prints with
// its percent sign.
func (s sheetStyles) forColumn(column Column) int {
	if !column.Numeric {
		return s.text
	}
	if column.Percent {
		return s.percent
	}
	if column.Decimals >= 4 {
		return s.volume
	}
	if column.Decimals == 0 {
		return s.whole
	}
	return s.number
}

func newStyles(file *excelize.File) (sheetStyles, error) {
	var styles sheetStyles
	var err error

	if styles.title, err = file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 14, Color: "102F48"},
	}); err != nil {
		return styles, err
	}
	if styles.subtitle, err = file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Size: 10, Color: "6B7785"},
	}); err != nil {
		return styles, err
	}
	if styles.header, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"173F5F"}},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    cellBorder("2B5470"),
	}); err != nil {
		return styles, err
	}
	// Wrapped, so a long name grows its row instead of disappearing behind the
	// neighbouring cell.
	if styles.text, err = file.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
		Border:    cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.whole, err = file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPtr("#,##0"),
		Border:       cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.number, err = file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPtr("#,##0.00"),
		Border:       cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.volume, err = file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPtr("#,##0.0000"),
		Border:       cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.percent, err = file.NewStyle(&excelize.Style{
		CustomNumFmt: stringPtr("0.0%"),
		Border:       cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.total, err = file.NewStyle(&excelize.Style{
		Font:         &excelize.Font{Bold: true},
		Fill:         excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EEF3F5"}},
		CustomNumFmt: stringPtr("#,##0.0000"),
		Border:       cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	if styles.totalLabel, err = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EEF3F5"}},
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Border:    cellBorder("DCE4E9"),
	}); err != nil {
		return styles, err
	}
	return styles, nil
}

func cellBorder(colour string) []excelize.Border {
	return []excelize.Border{
		{Type: "left", Color: colour, Style: 1},
		{Type: "right", Color: colour, Style: 1},
		{Type: "top", Color: colour, Style: 1},
		{Type: "bottom", Color: colour, Style: 1},
	}
}

func stringPtr(value string) *string { return &value }
