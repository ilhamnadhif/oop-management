package export

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"opp-management/internal/model"
)

// jpegDataURL builds the same shape the app stores: a base64 JPEG data URL.
func jpegDataURL(t *testing.T, width, height int) string {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, width, height))
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			picture.Set(x, y, color.RGBA{R: 210, G: 215, B: 220, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, picture, nil); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

// The appendix carries the receipt and only the receipt. The transfer and
// settlement proofs say how the money moved afterwards, which is reconciliation
// rather than the expense this report accounts for.
func TestNotaAttachmentsCarryOnlyTheReceipt(t *testing.T) {
	rows := []model.Nota{{
		NotaID: "NTA-0001", Tanggal: "2026-08-07", PIC: "Budi", Total: 230000,
		FotoKwitansi:  jpegDataURL(t, 48, 64),
		BuktiTransfer: jpegDataURL(t, 64, 48),
		BuktiBayar:    jpegDataURL(t, 40, 40),
	}}

	attachments := NotaTable(rows).Attachments
	if len(attachments) != 1 {
		t.Fatalf("carried %d attachments, want 1", len(attachments))
	}
	attachment := attachments[0]
	if attachment.Caption != "NTA-0001" {
		t.Fatalf("attachment caption %q does not name its nota", attachment.Caption)
	}
	if attachment.Format != "JPEG" || len(attachment.Image) == 0 {
		t.Fatalf("attachment decoded as %q with %d bytes", attachment.Format, len(attachment.Image))
	}
	// The 48x64 receipt is the one carried; the other two have other shapes.
	if attachment.Width != 48 || attachment.Height != 64 {
		t.Fatalf("the appendix carried a %dx%d photo, want the 48x64 kwitansi",
			attachment.Width, attachment.Height)
	}
	// The caption alone does not say which expense the photo belongs to.
	if detail := attachment.Detail; !strings.Contains(detail, "Budi") || !strings.Contains(detail, "230.000") {
		t.Fatalf("attachment detail = %q", detail)
	}
}

// A nota whose only photo is a payment proof brings nothing to the appendix.
func TestNotaAttachmentsIgnorePaymentProofs(t *testing.T) {
	rows := []model.Nota{{
		NotaID: "NTA-0001", Tanggal: "2026-08-07", PIC: "Budi", Total: 230000,
		BuktiTransfer: jpegDataURL(t, 64, 48),
		BuktiBayar:    jpegDataURL(t, 40, 40),
	}}
	if attachments := NotaTable(rows).Attachments; len(attachments) != 0 {
		t.Fatalf("carried %d payment proofs into the appendix", len(attachments))
	}
}

// A nota without a photo yet contributes nothing: an appendix page holding an
// empty frame reads as a missing receipt rather than one not taken.
func TestNotaAttachmentsSkipWhatIsNotThere(t *testing.T) {
	rows := []model.Nota{
		{NotaID: "NTA-0001", FotoKwitansi: jpegDataURL(t, 32, 32)},
		{NotaID: "NTA-0002"},
		{NotaID: "NTA-0003", FotoKwitansi: "   "},
	}
	attachments := NotaTable(rows).Attachments
	if len(attachments) != 1 {
		t.Fatalf("carried %d attachments, want 1", len(attachments))
	}
	if !strings.HasPrefix(attachments[0].Caption, "NTA-0001") {
		t.Fatalf("wrong attachment carried: %q", attachments[0].Caption)
	}
}

// Stored text that is not a picture has to be dropped, not rendered: a report
// that fails to build hides the rows that were fine.
func TestNotaAttachmentsDropUnreadableValues(t *testing.T) {
	rows := []model.Nota{
		// Base64 that decodes but is not a picture.
		{NotaID: "NTA-0001", FotoKwitansi: "data:image/jpeg;base64," + strings.Repeat("x", 4000)},
		// A link rather than the image itself.
		{NotaID: "NTA-0002", FotoKwitansi: "https://example.com/kwitansi.jpg"},
		// A real image in a format the renderer cannot embed.
		{NotaID: "NTA-0003", FotoKwitansi: "data:image/gif;base64,R0lGODlhAQABAAAAACw="},
	}
	if attachments := NotaTable(rows).Attachments; len(attachments) != 0 {
		t.Fatalf("carried %d unreadable attachments", len(attachments))
	}
}

// A receipt printed at the wrong aspect ratio is a receipt nobody can check the
// figures on, so the fit shrinks rather than stretches.
func TestAttachmentFitKeepsAspectRatio(t *testing.T) {
	portrait := Attachment{Width: 480, Height: 640}
	width, height := portrait.fit(100, 50)
	if height > 50.0001 || width > 100.0001 {
		t.Fatalf("fit overflowed the cell: %.2f x %.2f", width, height)
	}
	if ratio := width / height; ratio < 0.74 || ratio > 0.76 {
		t.Fatalf("aspect ratio became %.3f, want 0.75", ratio)
	}

	landscape := Attachment{Width: 640, Height: 480}
	width, height = landscape.fit(100, 200)
	if width != 100 {
		t.Fatalf("a wide photo did not use the full column: %.2f", width)
	}
	if ratio := width / height; ratio < 1.32 || ratio > 1.35 {
		t.Fatalf("aspect ratio became %.3f, want 1.333", ratio)
	}
}

// The appendix is the point of the change: the photos have to reach the file.
func TestNotaPDFPrintsAppendixPages(t *testing.T) {
	rows := notaFixtures()
	rows[0].FotoKwitansi = jpegDataURL(t, 480, 640)
	rows[1].FotoKwitansi = jpegDataURL(t, 640, 480)

	withPhotos, err := NotaPDF(rows, notaMeta())
	if err != nil {
		t.Fatalf("render pdf with attachments: %v", err)
	}
	if !bytes.Contains(withPhotos, []byte("/Subtype /Image")) {
		t.Fatal("the pdf carries no image, so the appendix never rendered")
	}

	// Unreadable photos leave the document exactly as it was.
	plain, err := NotaPDF(notaFixtures(), notaMeta())
	if err != nil {
		t.Fatalf("render pdf without attachments: %v", err)
	}
	if bytes.Contains(plain, []byte("/Subtype /Image")) {
		t.Fatal("the pdf embedded an image that is not a readable photo")
	}
	if len(withPhotos) <= len(plain) {
		t.Fatalf("the appendix added nothing: %d bytes against %d", len(withPhotos), len(plain))
	}
}

// The cell height is tuned so two rows sit under the letterhead. Growing the
// frame past that would silently drop the page to a single row and double the
// appendix, so the arithmetic is guarded rather than left to the eye.
func TestAppendixFitsTwoRowsPerPage(t *testing.T) {
	const pageHeight, bottomMargin = 210.0, 16.0
	cellHeight := appendixCaptionMM + appendixImageMM + appendixRowGap
	available := pageHeight - bottomMargin - appendixTopMM

	if rows := int(available / cellHeight); rows != 2 {
		t.Fatalf("%.1fmm of page fits %d rows of %.1fmm, want 2", available, rows, cellHeight)
	}
	width := (pageWidth - 2*pageMargin - appendixGutter*(appendixColumns-1)) / appendixColumns
	if width <= 0 || width*appendixColumns > pageWidth-2*pageMargin {
		t.Fatalf("a column of %.1fmm does not fit the page", width)
	}
}

// Reports without photos share the renderer and must keep their old shape.
func TestOtherReportsHaveNoAppendix(t *testing.T) {
	if attachments := NotaTable(nil).Attachments; len(attachments) != 0 {
		t.Fatalf("an empty period carried %d attachments", len(attachments))
	}
}
