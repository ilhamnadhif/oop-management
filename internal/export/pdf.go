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
	bodyFont    = 6.0
	headerFont  = 6.5
	rowHeight   = 4.6
	headerFill  = "173F5F"
	stripeFill  = "F4F7F9"
	borderGrey  = "DCE4E9"
	signatureMM = 46.0
)

// RenderPDF writes the signable report: a letterhead on every page, a table
// with a repeating coloured header, and a signature block at the end.
func RenderPDF(table Table, meta Meta) ([]byte, error) {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.SetAutoPageBreak(true, 16)
	pdf.SetTitle(meta.Title, true)

	logoName := ""
	if len(meta.Logo) > 0 {
		logoName = "brand-logo"
		pdf.RegisterImageOptionsReader(logoName, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(meta.Logo))
	}

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

		drawTableHeader(pdf, table.Columns)
	})

	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(140, 150, 160)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("Halaman %d dari {nb}", pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AliasNbPages("{nb}")

	pdf.AddPage()

	pdf.SetFont("Helvetica", "", bodyFont)
	for i, cells := range table.Rows {
		drawRow(pdf, table.Columns, cells, i%2 == 1)
	}

	if len(table.Rows) == 0 {
		pdf.SetFont("Helvetica", "I", 9)
		pdf.SetTextColor(107, 119, 133)
		pdf.CellFormat(0, 10, tr("Tidak ada data pada periode ini."), "", 1, "C", false, 0, "")
	} else if table.hasTotals() {
		drawTotals(pdf, table)
	}

	drawSignature(pdf, meta)

	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("write pdf: %w", err)
	}
	return buffer.Bytes(), nil
}

func drawTableHeader(pdf *fpdf.Fpdf, columns []Column) {
	pdf.SetFont("Helvetica", "B", headerFont)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFillColor(hexToRGB(headerFill))
	pdf.SetDrawColor(hexToRGB(headerFill))
	pdf.SetLineWidth(0.1)
	for _, column := range columns {
		pdf.CellFormat(column.Width, 6, tr(column.Header), "1", 0, "C", true, 0, "")
	}
	pdf.Ln(-1)
	pdf.SetTextColor(28, 40, 51)
	pdf.SetDrawColor(hexToRGB(borderGrey))
}

func drawRow(pdf *fpdf.Fpdf, columns []Column, cells []string, stripe bool) {
	if stripe {
		pdf.SetFillColor(hexToRGB(stripeFill))
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	pdf.SetFont("Helvetica", "", bodyFont)
	for i, column := range columns {
		align := "L"
		if column.Numeric {
			align = "R"
		}
		if i == 0 {
			align = "C"
		}
		pdf.CellFormat(column.Width, rowHeight, tr(fit(pdf, cells[i], column.Width-1.6)), "1", 0, align, true, 0, "")
	}
	pdf.Ln(-1)
}

func drawTotals(pdf *fpdf.Fpdf, table Table) {
	pdf.SetFont("Helvetica", "B", headerFont)
	pdf.SetFillColor(hexToRGB(stripeFill))

	start := table.totalsStart()
	labelWidth := 0.0
	for _, column := range table.Columns[:start] {
		labelWidth += column.Width
	}
	pdf.CellFormat(labelWidth, 6, tr(fmt.Sprintf("TOTAL  (%d baris)", len(table.Rows))), "1", 0, "R", true, 0, "")
	for i := start; i < len(table.Columns); i++ {
		text := ""
		if value, ok := table.Totals[i]; ok {
			text = FormatFloat(value, table.Columns[i].Decimals)
		}
		pdf.CellFormat(table.Columns[i].Width, 6, tr(text), "1", 0, "R", true, 0, "")
	}
	pdf.Ln(-1)
}

// drawSignature prints the closing block. It starts a page when the remaining
// space is too small, so the signature never lands split across two pages.
func drawSignature(pdf *fpdf.Fpdf, meta Meta) {
	_, pageHeight := pdf.GetPageSize()
	_, _, _, bottomMargin := pdf.GetMargins()
	if pageHeight-bottomMargin-pdf.GetY() < signatureMM {
		pdf.AddPage()
	}

	pdf.Ln(8)
	blockWidth := 70.0
	pdf.SetX(pageWidth - pageMargin - blockWidth)
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(28, 40, 51)
	pdf.CellFormat(blockWidth, 5, tr(meta.signedOn()), "", 2, "C", false, 0, "")
	pdf.CellFormat(blockWidth, 5, tr("Tertanda,"), "", 2, "C", false, 0, "")

	// Room for a wet signature.
	pdf.Ln(18)

	pdf.SetX(pageWidth - pageMargin - blockWidth)
	name := strings.TrimSpace(meta.Signatory.Name)
	if name == "" {
		// An unnamed line, never a guessed name: the wrong name on a signed
		// document is worse than a blank one.
		pdf.SetDrawColor(28, 40, 51)
		pdf.SetLineWidth(0.2)
		y := pdf.GetY()
		pdf.Line(pageWidth-pageMargin-blockWidth+10, y, pageWidth-pageMargin-10, y)
		pdf.Ln(1)
		pdf.SetX(pageWidth - pageMargin - blockWidth)
	} else {
		pdf.SetFont("Helvetica", "BU", 9)
		pdf.CellFormat(blockWidth, 5, tr(name), "", 2, "C", false, 0, "")
	}

	if title := strings.TrimSpace(meta.Signatory.Title); title != "" {
		pdf.SetX(pageWidth - pageMargin - blockWidth)
		pdf.SetFont("Helvetica", "", 9)
		pdf.CellFormat(blockWidth, 5, tr(title), "", 2, "C", false, 0, "")
	}
}

// fit shortens text that would otherwise spill into the next column.
func fit(pdf *fpdf.Fpdf, text string, width float64) string {
	if text == "" || pdf.GetStringWidth(text) <= width {
		return text
	}
	for len(text) > 1 {
		text = text[:len(text)-1]
		if pdf.GetStringWidth(text+"…") <= width {
			return text + "…"
		}
	}
	return text
}

// tr keeps the output to characters the core PDF fonts can encode. Embedding a
// Unicode font for a handful of symbols would multiply the file size.
var replacer = strings.NewReplacer(
	"³", "3", "²", "2", "·", "-", "—", "-", "–", "-", "…", "...", "×", "x", "−", "-",
)

func tr(text string) string { return replacer.Replace(text) }

func hexToRGB(hex string) (int, int, int) {
	var r, g, b int
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}
