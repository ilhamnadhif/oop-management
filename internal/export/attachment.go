package export

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

// Attachment is one photo printed on the appendix pages of a PDF. The table
// itself cannot hold these: they are base64 images tens of thousands of
// characters long, which no cell can show. Printed at the end of the report
// they are the evidence the figures in the table stand on.
type Attachment struct {
	// Caption names what the photo proves; Detail says which expense it
	// belongs to, so a page torn from the stack still identifies itself.
	Caption string
	Detail  string

	Image []byte
	// Format is the image type as fpdf names it: "JPEG" or "PNG".
	Format string
	// Width and Height are pixels, kept only to scale without distortion.
	Width  int
	Height int
}

// fit scales the photo to the largest size that stays inside the cell without
// stretching. A receipt read at the wrong aspect ratio is a receipt nobody can
// check the figures on.
func (a Attachment) fit(maxWidth, maxHeight float64) (float64, float64) {
	if a.Width <= 0 || a.Height <= 0 {
		return maxWidth, maxHeight
	}
	width := maxWidth
	height := width * float64(a.Height) / float64(a.Width)
	if height > maxHeight {
		height = maxHeight
		width = height * float64(a.Width) / float64(a.Height)
	}
	return width, height
}

// decodeAttachment reads a stored base64 data URL. It reports false rather than
// an error for anything it cannot print - an empty field, a link, a format the
// renderer does not carry, bytes that do not decode as a picture - because a
// report that refuses to build hides every row that was fine.
func decodeAttachment(caption, detail, value string) (Attachment, bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "data:image/") {
		return Attachment{}, false
	}
	const marker = ";base64,"
	index := strings.Index(value, marker)
	if index < 0 {
		return Attachment{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(value[index+len(marker):])
	if err != nil || len(raw) == 0 {
		return Attachment{}, false
	}

	// Decoding the header both proves the bytes are a picture fpdf can embed
	// and gives the pixel size the layout needs.
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return Attachment{}, false
	}
	name := ""
	switch format {
	case "jpeg":
		name = "JPEG"
	case "png":
		name = "PNG"
	default:
		return Attachment{}, false
	}

	return Attachment{
		Caption: caption,
		Detail:  detail,
		Image:   raw,
		Format:  name,
		Width:   config.Width,
		Height:  config.Height,
	}, true
}
