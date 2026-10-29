package export

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/xuri/excelize/v2"
)

// RowKind is how a line of a sectioned report is set. The flat reports print
// every row alike; a summary laid out the way the site's own sheets are -
// vendors heading their trucks, a title over each block, a total at the foot -
// needs each line to say which it is.
type RowKind int

const (
	// RowPlain is an ordinary line.
	RowPlain RowKind = iota
	// RowGroup heads the lines under it: a vendor, or a section of a summary.
	RowGroup
	// RowSub is a bold line inside a group, heading lines of its own: a truck
	// over its loads.
	RowSub
	// RowTotal is a total at the foot of a block.
	RowTotal
	// RowCaption is a title spanning the whole table, over a block of lines.
	RowCaption
	// RowMuted is a line with nothing in it worth reading, set in grey so the
	// lines that do carry figures stand out.
	RowMuted
)

// Tone colours one cell the way the overview does: a vendor that hauled in
// green, a class with nothing in red, the busiest hour in amber. Group and
// Plain exist for the legend's chips, which show what a vendor line and an
// ordinary line look like.
type Tone string

const (
	ToneNone    Tone = ""
	ToneGood    Tone = "good"
	ToneBad     Tone = "bad"
	TonePeak    Tone = "peak"
	ToneAccent  Tone = "accent"
	ToneMuted   Tone = "muted"
	ToneBadText Tone = "bad-text"
	ToneGroup   Tone = "group"
	TonePlain   Tone = "plain"
)

// tonePaint is a tone's colours: the text, the fill behind it (empty to keep
// the row's), and whether it is set bold.
type tonePaint struct {
	text string
	fill string
	bold bool
}

var tonePaints = map[Tone]tonePaint{
	ToneGood:    {text: "2F7D32", fill: "E8F5E4", bold: true},
	ToneBad:     {text: "B42318", fill: "FDECEC", bold: true},
	TonePeak:    {text: "9A5B0B", fill: "FDF1DE", bold: true},
	ToneAccent:  {text: "2563EB", bold: true},
	ToneMuted:   {text: "8A94A0"},
	ToneBadText: {text: "B42318", bold: true},
	ToneGroup:   {text: "102F48", fill: "F3F1EC", bold: true},
	TonePlain:   {text: "6B7785", fill: "FFFFFF", bold: true},
}

// LegendItem is one line of a section's key: a chip in the tone it explains,
// and what that tone means.
type LegendItem struct {
	Tone  Tone
	Label string
	Text  string
}

// ReportRow is one line of a section. Cells is what the PDF prints; Values is
// what the spreadsheet stores, numbers where the figure is a number. Indent
// steps the first cell in, for a line that belongs to the one above it.
type ReportRow struct {
	Kind   RowKind
	Indent int
	Cells  []string
	Values []interface{}
	// SpanTail merges SpanTail columns at the end into one cell, which takes
	// the row's last value: a note running across the per-rit columns, say.
	// Zero leaves every column its own cell.
	SpanTail int
	// SpanKeep leaves the last SpanKeep columns out of the span, each its own
	// cell after it: a note across the per-rit columns with the interval
	// still in its column at the end. The span's value then comes before
	// theirs.
	SpanKeep int
	// Tones colours cells one by one, by column; a missing or empty tone
	// leaves the cell as its row sets it.
	Tones []Tone
}

// span is the first and last column the row merges, of width in all, or ok
// false when it merges none.
func (r ReportRow) span(width int) (first, last int, ok bool) {
	if r.SpanTail <= 1 || r.SpanKeep < 0 || r.SpanTail+r.SpanKeep > width {
		return 0, 0, false
	}
	last = width - r.SpanKeep - 1
	return last - r.SpanTail + 1, last, true
}

func (r ReportRow) toneAt(index int) Tone {
	if index < len(r.Tones) {
		return r.Tones[index]
	}
	return ToneNone
}

// Section is one table of a sectioned report: a sheet of the workbook, and a
// run of pages of the PDF under its own title.
type Section struct {
	// SheetName names the sheet; Excel allows 31 characters.
	SheetName string
	// Title heads the section: the sheet's second line and the band over the
	// section's pages.
	Title   string
	Columns []Column
	Rows    []ReportRow
	// Legend is the key printed under the table, and Notes the lines saying
	// where the figures come from and how they were counted.
	Legend []LegendItem
	Notes  []string
}

// ReportXLSX writes a sectioned report as a workbook with a sheet per section,
// each under the same letterhead.
func ReportXLSX(sections []Section, meta Meta) ([]byte, error) {
	file := excelize.NewFile()
	defer file.Close()

	styles := newReportStyles(file)
	for i, section := range sections {
		index, err := file.NewSheet(section.SheetName)
		if err != nil {
			return nil, fmt.Errorf("create sheet %s: %w", section.SheetName, err)
		}
		if i == 0 {
			file.SetActiveSheet(index)
		}
		if err := writeReportSheet(file, styles, section, meta); err != nil {
			return nil, err
		}
	}
	if err := file.DeleteSheet("Sheet1"); err != nil {
		return nil, fmt.Errorf("remove default sheet: %w", err)
	}

	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		return nil, fmt.Errorf("write xlsx: %w", err)
	}
	return buffer.Bytes(), nil
}

func writeReportSheet(file *excelize.File, styles *reportStyles, section Section, meta Meta) error {
	sheet := section.SheetName
	lastColumn, err := excelize.ColumnNumberToName(len(section.Columns))
	if err != nil {
		return err
	}
	title := section.Title
	if title == "" {
		title = meta.Title
	}
	_ = file.SetCellValue(sheet, "A1", meta.Company)
	_ = file.SetCellStyle(sheet, "A1", "A1", styles.base.title)
	_ = file.MergeCell(sheet, "A1", lastColumn+"1")
	_ = file.SetCellValue(sheet, "A2", title)
	_ = file.SetCellStyle(sheet, "A2", "A2", styles.sectionTitle)
	_ = file.MergeCell(sheet, "A2", lastColumn+"2")
	_ = file.SetCellValue(sheet, "A3", fmt.Sprintf("%s  ·  Periode: %s  ·  Dicetak: %s",
		meta.Title, meta.periodLabel(), formatIndonesianDate(meta.Generated)))
	_ = file.SetCellStyle(sheet, "A3", "A3", styles.base.subtitle)
	_ = file.MergeCell(sheet, "A3", lastColumn+"3")

	for i, column := range section.Columns {
		name, err := excelize.ColumnNumberToName(i + 1)
		if err != nil {
			return err
		}
		cell := fmt.Sprintf("%s%d", name, headerRow)
		_ = file.SetCellValue(sheet, cell, column.Header)
		_ = file.SetCellStyle(sheet, cell, cell, styles.base.header)
		_ = file.SetColWidth(sheet, name, name, column.Width*0.85)
	}
	_ = file.SetRowHeight(sheet, headerRow, 30)

	for i, row := range section.Rows {
		rowNumber := headerRow + 1 + i
		if row.Kind == RowCaption {
			first := fmt.Sprintf("A%d", rowNumber)
			_ = file.SetCellValue(sheet, first, firstText(row))
			_ = file.MergeCell(sheet, first, fmt.Sprintf("%s%d", lastColumn, rowNumber))
			_ = file.SetCellStyle(sheet, first, fmt.Sprintf("%s%d", lastColumn, rowNumber), styles.caption)
			_ = file.SetRowHeight(sheet, rowNumber, 22)
			continue
		}
		// Cell j sits in column j up to the span, which takes one cell over
		// several columns; the cells after it move along by the columns it
		// swallowed.
		cells := len(section.Columns)
		first, last, spans := row.span(cells)
		if spans {
			cells -= last - first
		}
		for j := 0; j < cells; j++ {
			index := j
			if spans && j > first {
				index = j + last - first
			}
			column := section.Columns[index]
			name, err := excelize.ColumnNumberToName(index + 1)
			if err != nil {
				return err
			}
			cell := fmt.Sprintf("%s%d", name, rowNumber)
			if spans && j == first {
				// The spanning cell reads as text, left to right.
				column = Column{Header: column.Header, Width: column.Width}
				end, err := excelize.ColumnNumberToName(last + 1)
				if err != nil {
					return err
				}
				_ = file.MergeCell(sheet, cell, fmt.Sprintf("%s%d", end, rowNumber))
			}
			var value interface{} = ""
			if j < len(row.Values) && row.Values[j] != nil {
				value = row.Values[j]
			} else if j < len(row.Cells) {
				value = row.Cells[j]
			}
			_ = file.SetCellValue(sheet, cell, value)
			indent := 0
			if j == 0 {
				indent = row.Indent
			}
			_ = file.SetCellStyle(sheet, cell, cell, styles.forCell(row.Kind, column, indent, row.toneAt(j)))
		}
	}
	writeReportLegend(file, styles, section, headerRow+len(section.Rows)+2, lastColumn)

	// The header stays put while scrolling. There is no autofilter: a report
	// whose lines head and total one another falls apart when sorted.
	if err := file.SetPanes(sheet, &excelize.Panes{
		Freeze: true, YSplit: headerRow, TopLeftCell: fmt.Sprintf("A%d", headerRow+1), ActivePane: "bottomLeft",
	}); err != nil {
		return fmt.Errorf("freeze header: %w", err)
	}
	return nil
}

// writeReportLegend puts the key and the notes under a sheet's table: a chip
// in each tone with what it means beside it, then the notes, each across the
// sheet. The chip takes the first two columns when the first alone is too
// narrow to hold a word.
func writeReportLegend(file *excelize.File, styles *reportStyles, section Section, row int, lastColumn string) {
	sheet := section.SheetName
	if len(section.Legend) == 0 && len(section.Notes) == 0 {
		return
	}
	chipEnd := "A"
	textStart := "B"
	if len(section.Columns) > 2 && section.Columns[0].Width < 25 {
		chipEnd, textStart = "B", "C"
	}
	if len(section.Legend) > 0 {
		cell := fmt.Sprintf("A%d", row)
		_ = file.SetCellValue(sheet, cell, "Keterangan")
		_ = file.SetCellStyle(sheet, cell, cell, styles.legendTitle)
		row++
		for _, item := range section.Legend {
			chip := fmt.Sprintf("A%d", row)
			_ = file.SetCellValue(sheet, chip, item.Label)
			_ = file.MergeCell(sheet, chip, fmt.Sprintf("%s%d", chipEnd, row))
			_ = file.SetCellStyle(sheet, chip, fmt.Sprintf("%s%d", chipEnd, row), styles.chip(item.Tone))
			text := fmt.Sprintf("%s%d", textStart, row)
			_ = file.SetCellValue(sheet, text, item.Text)
			if textStart != lastColumn {
				_ = file.MergeCell(sheet, text, fmt.Sprintf("%s%d", lastColumn, row))
			}
			_ = file.SetCellStyle(sheet, text, text, styles.legendText)
			row++
		}
		row++
	}
	for _, note := range section.Notes {
		cell := fmt.Sprintf("A%d", row)
		_ = file.SetCellValue(sheet, cell, note)
		_ = file.MergeCell(sheet, cell, fmt.Sprintf("%s%d", lastColumn, row))
		_ = file.SetCellStyle(sheet, cell, cell, styles.note)
		_ = file.SetRowHeight(sheet, row, 30)
		row++
	}
}

// chip is a legend chip's style: the tone's colours, centred and bordered.
func (s *reportStyles) chip(tone Tone) int {
	key := "chip|" + string(tone)
	if style, ok := s.made[key]; ok {
		return style
	}
	paint := tonePaints[tone]
	style := &excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: paint.text},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Border:    cellBorder("DCE4E9"),
	}
	if paint.fill != "" {
		style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{paint.fill}}
	}
	id, err := s.file.NewStyle(style)
	if err != nil {
		id = s.base.text
	}
	s.made[key] = id
	return id
}

func firstText(row ReportRow) string {
	if len(row.Values) > 0 {
		if text, ok := row.Values[0].(string); ok {
			return text
		}
	}
	if len(row.Cells) > 0 {
		return row.Cells[0]
	}
	return ""
}

// reportStyles builds a cell style per row kind, column format and indent the
// first time a sheet asks for it. The combinations a report uses are few; the
// ones it does not are never made.
type reportStyles struct {
	file         *excelize.File
	base         sheetStyles
	sectionTitle int
	caption      int
	legendTitle  int
	legendText   int
	note         int
	made         map[string]int
}

func newReportStyles(file *excelize.File) *reportStyles {
	base, _ := newStyles(file)
	styles := &reportStyles{file: file, base: base, made: make(map[string]int)}
	styles.sectionTitle, _ = file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 12, Color: "173F5F"},
	})
	styles.caption, _ = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Size: 11, Color: "102F48"},
		Alignment: &excelize.Alignment{Vertical: "center"},
		Border:    []excelize.Border{{Type: "bottom", Color: "173F5F", Style: 1}},
	})
	styles.legendTitle, _ = file.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Size: 10, Color: "C8102E"},
	})
	styles.legendText, _ = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Color: "6B7785"},
		Alignment: &excelize.Alignment{Vertical: "center"},
	})
	styles.note, _ = file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Italic: true, Size: 9, Color: "6B7785"},
		Alignment: &excelize.Alignment{Vertical: "top", WrapText: true},
	})
	return styles
}

func (s *reportStyles) forCell(kind RowKind, column Column, indent int, tone Tone) int {
	format := ""
	if column.Numeric {
		switch {
		case column.Percent:
			format = "0.0%"
		case column.Decimals == 0:
			format = "#,##0"
		default:
			format = "#,##0." + strings.Repeat("0", column.Decimals)
		}
	}
	key := fmt.Sprintf("%d|%s|%d|%t|%t|%t|%s", kind, format, indent, column.Numeric, column.Center, column.Width < 20, tone)
	if style, ok := s.made[key]; ok {
		return style
	}
	style := &excelize.Style{
		Border:    cellBorder("DCE4E9"),
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true, Indent: indent * 2},
		Font:      &excelize.Font{Color: "1C2833"},
	}
	if column.Numeric {
		style.Alignment.Horizontal = "right"
		style.CustomNumFmt = stringPtr(format)
	} else if indent == 0 && (column.Center || column.Width < 20) {
		style.Alignment.Horizontal = "center"
	}
	switch kind {
	case RowGroup:
		style.Font.Bold = true
		style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F3F1EC"}}
	case RowSub:
		style.Font.Bold = true
	case RowTotal:
		style.Font.Bold = true
		style.Font.Color = "2563EB"
		style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"EAF1FB"}}
	case RowMuted:
		style.Font.Color = "A3ACB6"
	}
	if paint, ok := tonePaints[tone]; ok {
		style.Font.Color = paint.text
		style.Font.Bold = style.Font.Bold || paint.bold
		if paint.fill != "" {
			style.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{paint.fill}}
		}
	}
	id, err := s.file.NewStyle(style)
	if err != nil {
		id = s.base.text
	}
	s.made[key] = id
	return id
}

// ReportPDF prints a sectioned report: each section from a fresh page under a
// band naming it, the column header repeated on every page, and the signature
// once, after the last section.
func ReportPDF(sections []Section, meta Meta) ([]byte, error) {
	metrics := defaultPDFMetrics
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetTitle(meta.Title, true)

	logoName := ""
	if len(meta.Logo) > 0 {
		logoName = "brand-logo"
		pdf.RegisterImageOptionsReader(logoName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(meta.Logo))
	}

	var current *Section
	pdf.SetHeaderFunc(func() {
		top := pageMargin
		if logoName != "" {
			pdf.ImageOptions(logoName, pageMargin, top-1, 12, 0, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		}
		left := pageMargin + 15
		pdf.SetXY(left, top)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(16, 47, 72)
		pdf.CellFormat(170, 5, tr(meta.Company), "", 1, "L", false, 0, "")
		pdf.SetX(left)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(70, 85, 100)
		pdf.CellFormat(170, 4.5, tr(meta.Title), "", 1, "L", false, 0, "")
		pdf.SetX(left)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.SetTextColor(107, 119, 133)
		pdf.CellFormat(170, 4, tr("Periode: "+meta.periodLabel()), "", 1, "L", false, 0, "")

		pdf.SetXY(pageWidth-pageMargin-70, top)
		pdf.CellFormat(70, 4, tr("Dicetak: "+formatIndonesianDate(meta.Generated)), "", 2, "R", false, 0, "")

		pdf.SetDrawColor(23, 63, 95)
		pdf.SetLineWidth(0.4)
		pdf.Line(pageMargin, top+17, pageWidth-pageMargin, top+17)
		pdf.SetY(top + 20)

		if current == nil {
			return
		}
		drawSectionBand(pdf, current.Title)
		drawTableHeader(pdf, current.Columns, metrics)
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(140, 150, 160)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Halaman %d dari {nb}", pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AliasNbPages("{nb}")

	for i := range sections {
		current = &sections[i]
		pdf.AddPage()
		if len(current.Rows) == 0 {
			pdf.SetFont("Helvetica", "I", 9)
			pdf.SetTextColor(107, 119, 133)
			pdf.CellFormat(0, 10, tr("Tidak ada data pada periode ini."), "", 1, "C", false, 0, "")
			continue
		}
		for _, row := range current.Rows {
			drawReportRow(pdf, current.Columns, row, metrics)
		}
		drawReportLegend(pdf, *current)
	}
	if metrics.signed {
		drawSignature(pdf, meta)
	}

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return buffer.Bytes(), nil
}

// drawSectionBand names the section at the top of each of its pages.
func drawSectionBand(pdf *fpdf.Fpdf, title string) {
	pdf.SetFont("Helvetica", "B", 9.5)
	pdf.SetTextColor(16, 47, 72)
	pdf.CellFormat(pageWidth-2*pageMargin, 6, tr(title), "", 1, "L", false, 0, "")
	pdf.Ln(1)
}

// drawReportRow sets one line by its kind. A caption is one cell across the
// table; every other kind is the ordinary band of cells, filled and weighted
// to say what it is.
func drawReportRow(pdf *fpdf.Fpdf, columns []Column, row ReportRow, metrics pdfMetrics) {
	if row.Kind == RowCaption {
		width := 0.0
		for _, column := range columns {
			width += column.Width
		}
		_, pageHeight := pdf.GetPageSize()
		_, _, _, bottomMargin := pdf.GetMargins()
		// A caption alone at the foot of a page, with its block on the next,
		// heads nothing; it moves over with the block.
		if pdf.GetY()+7+3*metrics.rowHeight > pageHeight-bottomMargin {
			pdf.AddPage()
		}
		pdf.Ln(1.5)
		pdf.SetFont("Helvetica", "B", metrics.headerFont+1)
		pdf.SetTextColor(16, 47, 72)
		pdf.SetDrawColor(23, 63, 95)
		pdf.SetLineWidth(0.3)
		pdf.CellFormat(width, 5.5, tr(firstText(row)), "B", 1, "L", false, 0, "")
		pdf.SetLineWidth(0.1)
		pdf.SetDrawColor(hexToRGB(borderGrey))
		pdf.SetTextColor(28, 40, 51)
		return
	}

	cells := append([]string(nil), row.Cells...)
	if len(cells) > 0 && row.Indent > 0 {
		cells[0] = strings.Repeat("   ", row.Indent) + cells[0]
	}
	if first, last, ok := row.span(len(columns)); ok {
		// The spanned columns print as one cell as wide as all of them.
		merged := Column{}
		for _, column := range columns[first : last+1] {
			merged.Width += column.Width
		}
		spanned := append(append([]Column(nil), columns[:first]...), merged)
		columns = append(spanned, columns[last+1:]...)
		if len(cells) > len(columns) {
			cells = cells[:len(columns)]
		}
	}
	// The row's own colours, which a cell's tone may override.
	fill, text, bold := "FFFFFF", "1C2833", false
	switch row.Kind {
	case RowGroup:
		fill, text, bold = "F3F1EC", "102F48", true
	case RowSub:
		text, bold = "102F48", true
	case RowTotal:
		fill, text, bold = "EAF1FB", "2563EB", true
	case RowMuted:
		text = "A3ACB6"
	}
	// Set on every row: fpdf puts the line colour back after each page header
	// runs, so the grey the header leaves behind does not survive to the rows.
	pdf.SetDrawColor(hexToRGB(borderGrey))
	pdf.SetLineWidth(0.1)
	drawToneCells(pdf, columns, cells, row, fill, text, bold, metrics)
	pdf.SetTextColor(28, 40, 51)
}

// drawToneCells is drawCells with each cell painted on its own: the row's
// fill and text, or its tone's where it has one.
func drawToneCells(pdf *fpdf.Fpdf, columns []Column, cells []string, row ReportRow, fill, text string, bold bool, metrics pdfMetrics) {
	font := metrics.bodyFont + 0.5
	style := func(index int) (string, string, string) {
		cellFill, cellText, cellBold := fill, text, bold
		if paint, ok := tonePaints[row.toneAt(index)]; ok {
			cellText = paint.text
			cellBold = cellBold || paint.bold
			if paint.fill != "" {
				cellFill = paint.fill
			}
		}
		weight := ""
		if cellBold {
			weight = "B"
		}
		return cellFill, cellText, weight
	}

	wrapped := make([][]string, len(columns))
	longest := 1
	for i, column := range columns {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		_, _, weight := style(i)
		pdf.SetFont("Helvetica", weight, font)
		wrapped[i] = wrapCell(pdf, tr(cell), column.Width-2*metrics.cellPadding)
		if len(wrapped[i]) > longest {
			longest = len(wrapped[i])
		}
	}
	height := float64(longest)*metrics.lineHeight + metrics.cellPadding
	if minimum := metrics.rowHeight + 0.4; height < minimum {
		height = minimum
	}
	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pdf.GetY()+height > pageHeight-bottomMargin {
		pdf.AddPage()
		pdf.SetDrawColor(hexToRGB(borderGrey))
		pdf.SetLineWidth(0.1)
	}

	top := pdf.GetY()
	left := pageMargin
	for i, column := range columns {
		cellFill, cellText, weight := style(i)
		pdf.SetFillColor(hexToRGB(cellFill))
		pdf.SetTextColor(hexToRGB(cellText))
		pdf.SetFont("Helvetica", weight, font)
		pdf.Rect(left, top, column.Width, height, "FD")
		textTop := top + (height-float64(len(wrapped[i]))*metrics.lineHeight)/2
		for j, line := range wrapped[i] {
			pdf.SetXY(left, textTop+float64(j)*metrics.lineHeight)
			pdf.CellFormat(column.Width, metrics.lineHeight, line, "", 0, alignReport(i, column), false, 0, "")
		}
		left += column.Width
	}
	pdf.SetXY(pageMargin, top+height)
}

// drawReportLegend prints a section's key and notes under its table: a chip
// in each tone with its meaning beside it, then the notes in small italics.
func drawReportLegend(pdf *fpdf.Fpdf, section Section) {
	if len(section.Legend) == 0 && len(section.Notes) == 0 {
		return
	}
	width := pageWidth - 2*pageMargin
	needed := 8 + 6*float64(len(section.Legend)) + 8*float64(len(section.Notes))
	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pdf.GetY()+needed > pageHeight-bottomMargin {
		pdf.AddPage()
	}
	pdf.Ln(4)
	if len(section.Legend) > 0 {
		pdf.SetFont("Helvetica", "B", 7.5)
		pdf.SetTextColor(200, 16, 46)
		pdf.CellFormat(width, 4.5, tr("KETERANGAN"), "", 1, "L", false, 0, "")
		pdf.Ln(0.5)
		pdf.SetDrawColor(hexToRGB(borderGrey))
		pdf.SetLineWidth(0.1)
		for _, item := range section.Legend {
			paint := tonePaints[item.Tone]
			fillColor := paint.fill
			if fillColor == "" {
				fillColor = "FFFFFF"
			}
			pdf.SetFillColor(hexToRGB(fillColor))
			pdf.SetTextColor(hexToRGB(paint.text))
			pdf.SetFont("Helvetica", "B", 7)
			pdf.CellFormat(28, 5, tr(item.Label), "1", 0, "C", true, 0, "")
			pdf.SetX(pdf.GetX() + 3)
			pdf.SetTextColor(107, 119, 133)
			pdf.SetFont("Helvetica", "", 7)
			pdf.CellFormat(width-31, 5, tr(item.Text), "", 1, "L", false, 0, "")
			pdf.Ln(1)
		}
		pdf.Ln(1.5)
	}
	pdf.SetFont("Helvetica", "I", 6.8)
	pdf.SetTextColor(107, 119, 133)
	for _, note := range section.Notes {
		pdf.MultiCell(width, 3.4, tr(note), "", "L", false)
		pdf.Ln(0.8)
	}
	pdf.SetTextColor(28, 40, 51)
}

// alignReport puts labels on the left, figures on the right, and the narrow
// coded columns - a truck class, a clock time - in the middle.
func alignReport(index int, column Column) string {
	switch {
	case column.Numeric:
		return "R"
	case index > 0 && (column.Center || column.Width < 20):
		return "C"
	default:
		return "L"
	}
}
