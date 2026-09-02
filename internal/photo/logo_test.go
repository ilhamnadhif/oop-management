package photo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func pngBytes(t *testing.T, width, height int, transparent bool) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			alpha := uint8(255)
			if transparent && x < width/2 {
				alpha = 0
			}
			img.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: 90, A: alpha})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}

func jpegBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 251), G: uint8(y % 241), B: 40, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buffer.Bytes()
}

// A logo is usually a PNG with a transparent ground. Re-encoding it as JPEG
// would fill that ground with white, which shows as a box around the logo on
// every dark background it sits on.
func TestNormalizeLogoKeepsPNGAsPNG(t *testing.T) {
	value, err := NormalizeLogo(pngBytes(t, 300, 200, true), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize png: %v", err)
	}
	if !strings.HasPrefix(value, LogoPNGPrefix) {
		t.Fatalf("value starts %q, want a png data url", value[:40])
	}
	decoded, err := DecodeLogoDataURL(value)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(decoded))
	if err != nil || format != "png" {
		t.Fatalf("decoded as %q: %v", format, err)
	}
	// The transparent half survived the round trip.
	if _, _, _, alpha := img.At(1, 1).RGBA(); alpha != 0 {
		t.Fatalf("alpha = %d, want the transparent ground kept", alpha)
	}
}

// A JPEG logo stays a JPEG: there is no transparency to protect, and PNG would
// only make the file bigger.
func TestNormalizeLogoKeepsJPEGAsJPEG(t *testing.T) {
	value, err := NormalizeLogo(jpegBytes(t, 300, 200), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize jpeg: %v", err)
	}
	if !strings.HasPrefix(value, DataURLPrefix) {
		t.Fatalf("value starts %q, want a jpeg data url", value[:40])
	}
}

// A logo far too big for a spreadsheet cell is scaled down until it fits,
// rather than refused: the person uploading it has one file, not five.
func TestNormalizeLogoShrinksUntilItFits(t *testing.T) {
	value, err := NormalizeLogo(pngBytes(t, 1400, 1400, false), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize large png: %v", err)
	}
	if len(value) > MaxOutputChars {
		t.Fatalf("value is %d characters, over the %d a cell holds", len(value), MaxOutputChars)
	}
}

// Anything that is not an image the browsers agree on is refused.
func TestNormalizeLogoRefusesWhatIsNotAnImage(t *testing.T) {
	if _, err := NormalizeLogo([]byte("bukan gambar"), MaxOutputChars); err == nil {
		t.Fatal("a text file was accepted as a logo")
	}
	if _, err := NormalizeLogo(nil, MaxOutputChars); err == nil {
		t.Fatal("an empty upload was accepted as a logo")
	}
}

// The stored value is read back on every page, so it is validated rather than
// trusted: a row typed straight into the sheet is not a logo.
func TestValidateLogoDataURL(t *testing.T) {
	good, err := NormalizeLogo(pngBytes(t, 64, 64, false), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := ValidateLogoDataURL(good); err != nil {
		t.Fatalf("a value this package produced was refused: %v", err)
	}
	for _, bad := range []string{
		"", "https://example.test/logo.png",
		"data:image/png;base64,bukan-base64!!",
		"data:text/html;base64,PHNjcmlwdD4=",
	} {
		if err := ValidateLogoDataURL(bad); err == nil {
			t.Fatalf("%q was accepted as a logo", bad)
		}
	}
}

// icoBytes writes a minimal but structurally real .ico: a header, one directory
// entry, and the PNG that entry points at. Windows icon files may embed PNGs,
// which is what a modern favicon usually is.
func icoBytes(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writeU16 := func(v uint16) { buffer.Write([]byte{byte(v), byte(v >> 8)}) }
	writeU32 := func(v uint32) {
		buffer.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
	}
	writeU16(0)                        // reserved
	writeU16(1)                        // type: icon
	writeU16(1)                        // one image
	buffer.Write([]byte{32, 32, 0, 0}) // width, height, colours, reserved
	writeU16(1)                        // planes
	writeU16(32)                       // bits per pixel
	writeU32(uint32(len(payload)))
	writeU32(22) // offset: 6 header + 16 directory
	buffer.Write(payload)
	return buffer.Bytes()
}

// A favicon is the one mark people already have as a .ico, and Go's image
// package cannot decode that format. It is stored as it arrived, after its
// structure is checked.
func TestNormalizeFaviconAcceptsAnIcoFile(t *testing.T) {
	value, err := NormalizeFavicon(icoBytes(t, pngBytes(t, 32, 32, true)), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize ico: %v", err)
	}
	if !strings.HasPrefix(value, LogoICOPrefix) {
		t.Fatalf("value starts %q, want an icon data url", value[:40])
	}
	if got := LogoContentType(value); got != "image/x-icon" {
		t.Fatalf("content type = %q", got)
	}
	if _, err := DecodeLogoDataURL(value); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// A PNG or JPEG favicon still goes through the ordinary path, so it is shrunk
// to fit the way the other marks are.
func TestNormalizeFaviconStillTakesPNG(t *testing.T) {
	value, err := NormalizeFavicon(pngBytes(t, 512, 512, false), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize png favicon: %v", err)
	}
	if !strings.HasPrefix(value, LogoPNGPrefix) {
		t.Fatalf("value starts %q, want a png data url", value[:40])
	}
}

// The structure is checked rather than trusted: the bytes are served back to
// browsers, and "it ends in .ico" is not a fact about the file.
func TestNormalizeFaviconRefusesAMalformedIco(t *testing.T) {
	good := icoBytes(t, pngBytes(t, 32, 32, false))
	for name, broken := range map[string][]byte{
		"header only":       good[:6],
		"truncated entry":   good[:16],
		"claims no images":  append(append([]byte{}, 0, 0, 1, 0, 0, 0), good[6:]...),
		"entry runs past":   append(append([]byte{}, good[:12]...), []byte{0xff, 0xff, 0xff, 0x7f, 22, 0, 0, 0}...),
		"not an icon type":  append(append([]byte{}, 0, 0, 9, 0, 1, 0), good[6:]...),
		"reserved not zero": append(append([]byte{}, 1, 0, 1, 0, 1, 0), good[6:]...),
	} {
		if _, err := NormalizeFavicon(broken, MaxOutputChars); err == nil {
			t.Fatalf("%s was accepted as an icon", name)
		}
	}
}

// An icon cannot be scaled down here - nothing in the standard library decodes
// it - so one too big for a cell is refused rather than silently truncated.
func TestNormalizeFaviconRefusesAnIcoTooBigForACell(t *testing.T) {
	huge := icoBytes(t, bytes.Repeat([]byte{0x41}, MaxOutputChars))
	if _, err := NormalizeFavicon(huge, MaxOutputChars); err == nil {
		t.Fatal("an icon larger than a cell was accepted")
	}
}
