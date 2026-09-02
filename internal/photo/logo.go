package photo

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// LogoPNGPrefix is the data URL a PNG logo is stored under. A logo usually has
// a transparent ground, and Normalize would flatten that onto white: the mark
// would then sit in a white box on every dark background it is put on. So a PNG
// stays a PNG here, and only a JPEG is stored as one.
const LogoPNGPrefix = "data:image/png;base64,"

// LogoICOPrefix is the data URL a Windows icon is stored under. A favicon is
// usually the one mark an organisation already has as a .ico, and nothing in
// the standard library decodes that format. Rather than take a dependency to
// read a file this app only ever passes through, an icon is stored exactly as
// it arrived and its structure is checked instead of its pixels.
const LogoICOPrefix = "data:image/x-icon;base64,"

// logoMaxDimension is the largest a stored logo is kept at. The mark is drawn
// at a few dozen pixels in the sidebar and at about a centimetre on a printed
// letterhead; anything beyond this is bytes nobody sees.
const logoMaxDimension = 512

// NormalizeLogo turns an uploaded file into the data URL the sheet stores. PNG
// is kept as PNG and JPEG as JPEG; anything else the decoders understand is
// re-encoded as PNG, which is the safe choice when transparency is unknown.
//
// It shrinks the image until the data URL fits, rather than refusing a file for
// being large: the person uploading has one logo, not five sizes of it.
func NormalizeLogo(raw []byte, maxOutputChars int) (string, error) {
	format, err := validateRawImageConfig(raw)
	if err != nil {
		return "", err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode image", ErrInvalid)
	}
	if maxOutputChars <= 0 {
		maxOutputChars = MaxOutputChars
	}

	for _, maxDimension := range []int{logoMaxDimension, 384, 256, 192, 128, 96, 64} {
		resized := resize(img, maxDimension)
		value, err := encodeLogo(resized, format)
		if err != nil {
			return "", err
		}
		if len(value) <= maxOutputChars {
			return value, nil
		}
	}
	return "", ErrTooLarge
}

// encodeLogo writes the image back in the form it arrived in, so a mark with a
// transparent ground keeps it.
func encodeLogo(img image.Image, format string) (string, error) {
	if format == "jpeg" {
		encoded, err := encodeJPEG(img, 85)
		if err != nil {
			return "", fmt.Errorf("encode logo: %w", err)
		}
		return DataURLPrefix + base64.StdEncoding.EncodeToString(encoded), nil
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		return "", fmt.Errorf("encode logo: %w", err)
	}
	return LogoPNGPrefix + base64.StdEncoding.EncodeToString(buffer.Bytes()), nil
}

// NormalizeFavicon turns an uploaded favicon into the data URL the sheet
// stores. A Windows icon is kept byte for byte; anything else goes through the
// ordinary path and is shrunk to fit the way the other marks are.
func NormalizeFavicon(raw []byte, maxOutputChars int) (string, error) {
	if !looksLikeICO(raw) {
		return NormalizeLogo(raw, maxOutputChars)
	}
	if err := validateICO(raw); err != nil {
		return "", err
	}
	if maxOutputChars <= 0 {
		maxOutputChars = MaxOutputChars
	}
	value := LogoICOPrefix + base64.StdEncoding.EncodeToString(raw)
	if len(value) > maxOutputChars {
		// An icon cannot be scaled down here, so one too big is refused rather
		// than stored truncated, which would serve a broken file forever.
		return "", ErrTooLarge
	}
	return value, nil
}

// looksLikeICO reads the six-byte header every icon file starts with: two zero
// bytes, a type of 1, and at least one image.
func looksLikeICO(raw []byte) bool {
	return len(raw) >= 6 && raw[0] == 0 && raw[1] == 0 && raw[2] == 1 && raw[3] == 0 &&
		(uint16(raw[4])|uint16(raw[5])<<8) >= 1
}

// validateICO walks the directory and checks every entry points at bytes the
// file actually holds. The result is served to browsers, so "the header looks
// right" is not enough on its own.
func validateICO(raw []byte) error {
	if int64(len(raw)) > MaxInputBytes {
		return ErrTooLarge
	}
	if !looksLikeICO(raw) {
		return fmt.Errorf("%w: not an icon file", ErrInvalid)
	}
	count := int(uint16(raw[4]) | uint16(raw[5])<<8)
	directoryEnd := 6 + count*16
	if len(raw) < directoryEnd {
		return fmt.Errorf("%w: icon directory is truncated", ErrInvalid)
	}
	for index := 0; index < count; index++ {
		entry := raw[6+index*16 : 6+(index+1)*16]
		size := int64(readU32(entry[8:12]))
		offset := int64(readU32(entry[12:16]))
		if size <= 0 || offset < int64(directoryEnd) || offset+size > int64(len(raw)) {
			return fmt.Errorf("%w: icon image %d falls outside the file", ErrInvalid, index+1)
		}
	}
	return nil
}

func readU32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// ValidateLogoDataURL checks a stored value before it is served. The sheet is
// editable by hand, so what comes back out of it is not trusted to be what this
// package put in.
func ValidateLogoDataURL(value string) error {
	_, _, err := decodeLogoDataURL(value)
	return err
}

// DecodeLogoDataURL returns the image bytes behind a stored logo.
func DecodeLogoDataURL(value string) ([]byte, error) {
	decoded, _, err := decodeLogoDataURL(value)
	return decoded, err
}

// LogoContentType is the type a stored logo is served as, or empty when the
// value is not a logo this package would have written.
func LogoContentType(value string) string {
	_, contentType, err := decodeLogoDataURL(value)
	if err != nil {
		return ""
	}
	return contentType
}

func decodeLogoDataURL(value string) ([]byte, string, error) {
	var prefix, contentType string
	switch {
	case strings.HasPrefix(value, LogoPNGPrefix):
		prefix, contentType = LogoPNGPrefix, "image/png"
	case strings.HasPrefix(value, DataURLPrefix):
		prefix, contentType = DataURLPrefix, "image/jpeg"
	case strings.HasPrefix(value, LogoICOPrefix):
		prefix, contentType = LogoICOPrefix, "image/x-icon"
	default:
		return nil, "", ErrInvalid
	}
	encoded := strings.TrimPrefix(value, prefix)
	if encoded == "" || len(value) > MaxOutputChars {
		return nil, "", ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 {
		return nil, "", ErrInvalid
	}
	if contentType == "image/x-icon" {
		// Nothing here decodes an icon, so its structure is what is checked.
		if err := validateICO(decoded); err != nil {
			return nil, "", err
		}
		return decoded, contentType, nil
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return nil, "", ErrInvalid
	}
	// The declared type has to be the actual one: a PNG served as a JPEG is a
	// mismatch a browser may resolve by sniffing, which is not a habit worth
	// relying on.
	if (contentType == "image/png" && format != "png") || (contentType == "image/jpeg" && format != "jpeg") {
		return nil, "", ErrInvalid
	}
	return decoded, contentType, nil
}

// Keep the JPEG encoder linked for builds that only reach it through here.
var _ = jpeg.Encode
