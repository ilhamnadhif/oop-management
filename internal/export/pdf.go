package export

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// Landscape A4 throughout. The production table has twenty columns, which
// portrait cannot hold at a readable size, and one orientation for every report
// keeps the letterhead identical across the set.
const (
	pageWidth   = 297.0
	pageMargin  = 8.0
	headerFill  = "173F5F"
	stripeFill  = "F4F7F9"
	borderGrey  = "DCE4E9"
	signatureMM = 46.0
)

// pdfMetrics carries the type sizes a report is printed with. Most reports use
// the default; the attendance matrix, forty-odd columns wide, prints smaller so
// it still fits one landscape page, and unsigned because it is a data sheet
// rather than a document to sign.
type pdfMetrics struct {
	bodyFont    float64
	headerFont  float64
	rowHeight   float64
	lineHeight  float64
	cellPadding float64
	signed      bool
}

var (
	defaultPDFMetrics = pdfMetrics{
		bodyFont: 6, headerFont: 6.5, rowHeight: 4.6, lineHeight: 3.4, cellPadding: 1.2,
		signed: true,
	}
	compactPDFMetrics = pdfMetrics{
		bodyFont: 5, headerFont: 5.5, rowHeight: 4.0, lineHeight: 2.9, cellPadding: 0.8,
		signed: false,
	}
)

// The appendix prints the photos two to a row. One per row would waste half a
// landscape page on every receipt; three would shrink a handwritten kwitansi
// past reading. The cell is a caption block above a fixed frame, sized so two
// rows fit a page under the letterhead.
const (
	appendixColumns   = 2
	appendixGutter    = 8.0
	appendixCaptionMM = 9.0
	appendixImageMM   = 62.0
	appendixRowGap    = 6.0
	// appendixTopMM is where the first cell starts: the letterhead block plus
	// the section heading.
	appendixTopMM = pageMargin + 20 + 8.0
)

// RenderPDF writes the signable report: a letterhead on every page, a table
// with a repeating coloured header, and a signature block at the end.
func RenderPDF(table Table, meta Meta) ([]byte, error) {
	return renderPDF(table, meta, defaultPDFMetrics)
}

// RenderPDFCompact is the same report at the smaller type sizes the wide
// attendance matrix needs, and without the closing signature: a data sheet.
func RenderPDFCompact(table Table, meta Meta) ([]byte, error) {
	return renderPDF(table, meta, compactPDFMetrics)
}

func renderPDF(table Table, meta Meta, metrics pdfMetrics) ([]byte, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetTitle(meta.Title, true)

	logoName := ""
	if len(meta.Logo) > 0 {
		logoName = "brand-logo"
		pdf.RegisterImageOptionsReader(logoName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(meta.Logo))
	}

	// The appendix pages carry the same letterhead but no table header: there
	// is no table on them to name.
	appendix := false

	// The header and footer run on every page, so a page torn out of the stack
	// still says what it is and where it came from.
	pdf.SetHeaderFunc(func() {
		top := pageMargin
		if logoName != "" {
			pdf.ImageOptions(logoName, pageMargin, top-1, 12, 0, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
		}
		left := pageMargin + 15

		pdf.SetXY(left, top)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(16, 47, 72)
		pdf.CellFormat(150, 5, tr(meta.Company), "", 1, "L", false, 0, "")

		pdf.SetX(left)
		pdf.SetFont("Helvetica", "", 9)
		pdf.SetTextColor(70, 85, 100)
		pdf.CellFormat(150, 4.5, tr(meta.Title), "", 1, "L", false, 0, "")

		pdf.SetX(left)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.SetTextColor(107, 119, 133)
		pdf.CellFormat(150, 4, tr("Periode: "+meta.periodLabel()), "", 1, "L", false, 0, "")

		pdf.SetXY(pageWidth-pageMargin-70, top)
		pdf.SetFont("Helvetica", "", 7.5)
		pdf.CellFormat(70, 4, tr("Dicetak: "+formatIndonesianDate(meta.Generated)), "", 2, "R", false, 0, "")
		pdf.CellFormat(70, 4, tr(fmt.Sprintf("%d baris", len(table.Rows))), "", 2, "R", false, 0, "")

		pdf.SetDrawColor(23, 63, 95)
		pdf.SetLineWidth(0.4)
		pdf.Line(pageMargin, top+17, pageWidth-pageMargin, top+17)
		pdf.SetY(top + 20)

		if appendix {
			drawAppendixHeading(pdf)
			return
		}
		drawTableHeader(pdf, table.Columns, metrics)
	})

	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(140, 150, 160)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Halaman %d dari {nb}", pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AliasNbPages("{nb}")

	pdf.AddPage()

	pdf.SetFont("Helvetica", "", metrics.bodyFont)
	for i, cells := range table.Rows {
		drawRow(pdf, table.Columns, cells, i%2 == 1, metrics)
	}

	if len(table.Rows) == 0 {
		pdf.SetFont("Helvetica", "I", 9)
		pdf.SetTextColor(107, 119, 133)
		pdf.CellFormat(0, 10, tr("Tidak ada data pada periode ini."), "", 1, "C", false, 0, "")
	} else if table.hasTotals() {
		drawTotals(pdf, table, metrics)
	}

	if metrics.signed {
		drawSignature(pdf, meta)
	}

	// The photos come after the signature: the signed figures are the report,
	// and the evidence backs them up rather than interrupting them.
	if len(table.Attachments) > 0 {
		appendix = true
		drawAttachments(pdf, table.Attachments)
	}

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return buffer.Bytes(), nil
}

func drawTableHeader(pdf *fpdf.Fpdf, columns []Column, metrics pdfMetrics) {
	pdf.SetFont("Helvetica", "B", metrics.headerFont)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFillColor(hexToRGB(headerFill))
	pdf.SetDrawColor(hexToRGB(headerFill))
	pdf.SetLineWidth(0.1)

	headers := make([]string, len(columns))
	for i, column := range columns {
		headers[i] = column.Header
	}
	drawCells(pdf, columns, headers, alignHeader, 6, metrics)

	pdf.SetTextColor(28, 40, 51)
	pdf.SetDrawColor(hexToRGB(borderGrey))
}

func drawRow(pdf *fpdf.Fpdf, columns []Column, cells []string, stripe bool, metrics pdfMetrics) {
	if stripe {
		pdf.SetFillColor(hexToRGB(stripeFill))
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.SetFont("Helvetica", "", metrics.bodyFont)
	drawCells(pdf, columns, cells, alignBody, metrics.rowHeight, metrics)
}

type cellAlign func(index int, column Column) string

func alignHeader(int, Column) string { return "C" }

func alignBody(index int, column Column) string {
	switch {
	case index == 0:
		return "C"
	case column.Numeric:
		return "R"
	default:
		return "L"
	}
}

// drawCells writes one band of the table. Text too long for its column wraps
// onto further lines and the whole band grows to fit: a report that quietly
// shortened a name would be read as if that were the name.
func drawCells(pdf *fpdf.Fpdf, columns []Column, cells []string, align cellAlign, minHeight float64, metrics pdfMetrics) {
	wrapped := make([][]string, len(columns))
	longest := 1
	for i, column := range columns {
		text := ""
		if i < len(cells) {
			text = cells[i]
		}
		wrapped[i] = wrapCell(pdf, tr(text), column.Width-2*metrics.cellPadding)
		if len(wrapped[i]) > longest {
			longest = len(wrapped[i])
		}
	}
	height := float64(longest)*metrics.lineHeight + metrics.cellPadding
	if height < minHeight {
		height = minHeight
	}

	// A band is drawn as one piece, so the page break has to happen before it
	// starts rather than in the middle of a wrapped cell.
	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pdf.GetY()+height > pageHeight-bottomMargin {
		pdf.AddPage()
	}

	top := pdf.GetY()
	left := pageMargin
	for i, column := range columns {
		pdf.Rect(left, top, column.Width, height, "FD")
		textTop := top + (height-float64(len(wrapped[i]))*metrics.lineHeight)/2
		for j, line := range wrapped[i] {
			pdf.SetXY(left, textTop+float64(j)*metrics.lineHeight)
			pdf.CellFormat(column.Width, metrics.lineHeight, line, "", 0, align(i, column), false, 0, "")
		}
		left += column.Width
	}
	pdf.SetXY(pageMargin, top+height)
}

// wrapCell breaks text to fit a column width, on spaces where it can and
// mid-word where a single word is wider than the column.
func wrapCell(pdf *fpdf.Fpdf, text string, width float64) []string {
	if text == "" || pdf.GetStringWidth(text) <= width {
		return []string{text}
	}
	lines := make([]string, 0, 2)
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if pdf.GetStringWidth(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
		// A word longer than the column has to be cut somewhere; cutting it
		// across lines keeps every character, which dropping the tail would not.
		for pdf.GetStringWidth(word) > width {
			cut := len(word)
			for cut > 1 && pdf.GetStringWidth(word[:cut]) > width {
				cut--
			}
			lines = append(lines, word[:cut])
			word = word[cut:]
		}
		current = word
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func drawTotals(pdf *fpdf.Fpdf, table Table, metrics pdfMetrics) {
	pdf.SetFont("Helvetica", "B", metrics.headerFont)
	pdf.SetFillColor(hexToRGB(stripeFill))

	start := table.totalsStart()
	labelWidth := 0.0
	for _, column := range table.Columns[:start] {
		labelWidth += column.Width
	}

	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pdf.GetY()+6 > pageHeight-bottomMargin {
		pdf.AddPage()
	}
	pdf.CellFormat(labelWidth, 6, tr(fmt.Sprintf("TOTAL  (%d baris)", len(table.Rows))), "1", 0, "R", true, 0, "")
	for i := start; i < len(table.Columns); i++ {
		text := ""
		if value, ok := table.Totals[i]; ok {
			text = FormatCell(value, table.Columns[i])
		}
		pdf.CellFormat(table.Columns[i].Width, 6, tr(text), "1", 0, "R", true, 0, "")
	}
	pdf.Ln(-1)
}

// drawAppendixHeading names the section on every appendix page, so a page torn
// from the stack is not mistaken for a loose photocopy.
func drawAppendixHeading(pdf *fpdf.Fpdf) {
	pdf.SetFont("Helvetica", "B", 8)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFillColor(hexToRGB(headerFill))
	pdf.CellFormat(pageWidth-2*pageMargin, 6,
		tr("LAMPIRAN - FOTO KWITANSI"), "", 1, "L", true, 0, "")
	pdf.SetTextColor(28, 40, 51)
	pdf.Ln(2)
}

// drawAttachments lays the photos out in a fixed grid. Every cell is the same
// height whatever the photo's shape, so the rows stay aligned and a page break
// falls between rows rather than through a picture.
func drawAttachments(pdf *fpdf.Fpdf, attachments []Attachment) {
	columnWidth := (pageWidth - 2*pageMargin - appendixGutter*(appendixColumns-1)) / appendixColumns
	cellHeight := appendixCaptionMM + appendixImageMM + appendixRowGap

	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()

	pdf.AddPage()
	top := pdf.GetY()
	for index, attachment := range attachments {
		column := index % appendixColumns
		if column == 0 && index > 0 {
			top += cellHeight
			if top+cellHeight > pageHeight-bottomMargin {
				pdf.AddPage()
				top = pdf.GetY()
			}
		}
		left := pageMargin + float64(column)*(columnWidth+appendixGutter)
		drawAttachment(pdf, attachment, index, left, top, columnWidth)
	}
}

func drawAttachment(pdf *fpdf.Fpdf, attachment Attachment, index int, left, top, width float64) {
	name := fmt.Sprintf("lampiran-%d", index)
	options := fpdf.ImageOptions{ImageType: attachment.Format}
	pdf.RegisterImageOptionsReader(name, options, bytes.NewReader(attachment.Image))

	pdf.SetXY(left, top)
	pdf.SetFont("Helvetica", "B", 7.5)
	pdf.SetTextColor(28, 40, 51)
	pdf.CellFormat(width, 4, tr(attachment.Caption), "", 2, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 6.5)
	pdf.SetTextColor(107, 119, 133)
	pdf.CellFormat(width, 3.4, tr(attachment.Detail), "", 2, "L", false, 0, "")
	pdf.SetTextColor(28, 40, 51)

	imageWidth, imageHeight := attachment.fit(width, appendixImageMM)
	x := left + (width-imageWidth)/2
	y := top + appendixCaptionMM

	// A thin frame, because a photographed receipt is often paper on paper and
	// its own edges are not always visible.
	pdf.SetDrawColor(hexToRGB(borderGrey))
	pdf.SetLineWidth(0.2)
	pdf.Rect(x-0.8, y-0.8, imageWidth+1.6, imageHeight+1.6, "D")
	pdf.ImageOptions(name, x, y, imageWidth, imageHeight, false, options, 0, "")
}

// drawSignature prints the closing block. It starts a page when the remaining
// space is too small, so the signature never lands split across two pages.
//
// The block lays out one, two or three signatories. A single signature keeps
// its place on the right, as every report has always signed; with more, the
// columns spread evenly across the page with the place-and-date line centred
// above them.
func drawSignature(pdf *fpdf.Fpdf, meta Meta) {
	signatories := meta.signatories()
	if len(signatories) == 0 {
		return
	}
	if len(signatories) > 3 {
		signatories = signatories[:3]
	}

	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pageHeight-bottomMargin-pdf.GetY() < signatureMM {
		pdf.AddPage()
	}

	pdf.Ln(8)

	blockWidth := 70.0
	blockGap := 22.0

	// One signature stays where every report has always put it: on the right.
	var xPositions []float64
	if len(signatories) == 1 {
		xPositions = []float64{pageWidth - pageMargin - blockWidth}
	} else {
		total := float64(len(signatories))*blockWidth + float64(len(signatories)-1)*blockGap
		start := (pageWidth - total) / 2
		for i := range signatories {
			xPositions = append(xPositions, start+float64(i)*(blockWidth+blockGap))
		}
	}

	// The place-and-date line sits centred across the whole block, once: over
	// the single column on the right, or over the span the columns cover.
	headerX := xPositions[0]
	headerWidth := xPositions[len(xPositions)-1] + blockWidth - headerX
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(28, 40, 51)
	pdf.SetX(headerX)
	pdf.CellFormat(headerWidth, 5, tr(meta.signedOn()), "", 2, "C", false, 0, "")
	pdf.SetX(headerX)
	pdf.CellFormat(headerWidth, 5, tr("Tertanda,"), "", 2, "C", false, 0, "")

	// Room for the wet signatures before any names are printed.
	pdf.Ln(18)

	// Every column starts from the same line. Writing a cell moves the cursor
	// down, so without putting it back each column would begin lower than the
	// one before it and the block would walk down the page.
	baseY := pdf.GetY()
	lowestY := baseY
	for i, signatory := range signatories {
		x := xPositions[i]
		pdf.SetY(baseY)
		name := strings.TrimSpace(signatory.Name)
		if name == "" {
			// An unnamed line, never a guessed name: the wrong name on a signed
			// document is worse than a blank one.
			pdf.SetDrawColor(28, 40, 51)
			pdf.SetLineWidth(0.2)
			pdf.Line(x+10, baseY, x+blockWidth-10, baseY)
			pdf.Ln(1)
		} else {
			pdf.SetX(x)
			pdf.SetFont("Helvetica", "BU", 9)
			pdf.CellFormat(blockWidth, 5, tr(name), "", 2, "C", false, 0, "")
		}
		if title := strings.TrimSpace(signatory.Title); title != "" {
			pdf.SetX(x)
			pdf.SetFont("Helvetica", "", 9)
			pdf.CellFormat(blockWidth, 5, tr(title), "", 2, "C", false, 0, "")
		}
		if y := pdf.GetY(); y > lowestY {
			lowestY = y
		}
	}
	// Whatever follows starts below the tallest column, not below the last one
	// drawn.
	pdf.SetY(lowestY)
}

// tr keeps the output to characters the core PDF fonts can encode. Embedding a
// Unicode font for a handful of symbols would multiply the file size.
var replacer = strings.NewReplacer(
	"³", "3", "²", "2", "·", "-", "—", "-", "–", "-", "…", "...", "×", "x", "−", "-",
	// The attendance matrix marks a day present with a check; the core fonts
	// cannot draw it, so it prints as the "v" the user reads as the same thing.
	"✓", "v",
)

func tr(text string) string { return replacer.Replace(text) }

func hexToRGB(hex string) (int, int, int) {
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
