package export

import (
	"testing"

	"github.com/go-pdf/fpdf"
)

// newSignatureCanvas is a page in the shape a report uses, with the cursor
// parked well above the bottom so the block draws where it is asked to rather
// than on a page of its own.
func newSignatureCanvas() *fpdf.Fpdf {
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(pageMargin, pageMargin, pageMargin)
	pdf.AddPage()
	pdf.SetY(30)
	return pdf
}

// Every column of the closing block starts from the same line. Drawing them one
// after another moves the cursor down, and forgetting to put it back walked
// each signature lower than the one before it.
func TestSignatureColumnsShareOneBaseline(t *testing.T) {
	heights := make(map[int]float64, 3)
	for _, count := range []int{1, 2, 3} {
		signatories := make([]Signatory, 0, count)
		for i := 0; i < count; i++ {
			signatories = append(signatories, Signatory{Name: "Budi Hartono", Title: "Direktur"})
		}
		pdf := newSignatureCanvas()
		before := pdf.GetY()
		drawSignature(pdf, Meta{Signatories: signatories})
		heights[count] = pdf.GetY() - before
	}
	if heights[1] != heights[2] || heights[2] != heights[3] {
		t.Fatalf("block heights = %v, want one baseline for every column count", heights)
	}
}

// The unnamed line and the named one are different branches, and the line has
// its own vertical step. Mixing them must still leave one baseline.
func TestSignatureBaselineHoldsForAnUnnamedColumn(t *testing.T) {
	pdf := newSignatureCanvas()
	before := pdf.GetY()
	drawSignature(pdf, Meta{Signatories: []Signatory{
		{Name: "Budi Hartono", Title: "PJO"},
		{Title: "Pembuat"},
	}})
	mixed := pdf.GetY() - before

	pdf = newSignatureCanvas()
	before = pdf.GetY()
	drawSignature(pdf, Meta{Signatories: []Signatory{{Name: "Budi Hartono", Title: "PJO"}}})
	single := pdf.GetY() - before

	// The unnamed branch steps 1mm rather than a 5mm cell, so the mixed block
	// is at most as tall as the named one - never taller, which is what the
	// drift looked like.
	if mixed > single {
		t.Fatalf("mixed block = %v, single = %v: the unnamed column pushed the baseline down", mixed, single)
	}
}
