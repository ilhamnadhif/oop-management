package export

import (
	"bytes"
	"fmt"

	"github.com/xuri/excelize/v2"
)

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

	const headerRow = 5
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
			_ = file.SetCellValue(sheet, cell, value)
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
		_ = file.AutoFilter(sheet,
			fmt.Sprintf("A%d:%s%d", headerRow, lastColumn, headerRow+len(table.Values)), nil)
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
	number     int
	volume     int
	total      int
	totalLabel int
}

// forColumn picks the number format. Two decimals for dimensions, four for
// volumes: the deviation is the figure people argue over, so it keeps its
// precision.
func (s sheetStyles) forColumn(column Column) int {
	if !column.Numeric {
		return s.text
	}
	if column.Decimals >= 4 {
		return s.volume
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
	if styles.text, err = file.NewStyle(&excelize.Style{Border: cellBorder("DCE4E9")}); err != nil {
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
		Alignment: &excelize.Alignment{Horizontal: "right"},
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
