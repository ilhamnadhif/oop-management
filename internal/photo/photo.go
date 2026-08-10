package photo

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	stddraw "image/draw"
	"image/jpeg"
	"image/png"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxInputBytes      int64 = 2 * 1024 * 1024
	MaxOutputChars           = 45000
	DataURLPrefix            = "data:image/jpeg;base64,"
	maxDecodeDimension       = 4096
	maxOCRDimension          = 2048
	// A 2 MiB JPEG produces a data URL below the scanner's 4 MiB request limit.
	maxOCRImageBytes   = 2 * 1024 * 1024
	maxOCRDataURLChars = len(DataURLPrefix) + 4*((maxOCRImageBytes+2)/3)
)

var (
	ErrInvalid  = errors.New("invalid photo")
	ErrTooLarge = errors.New("photo too large")
)

func Normalize(raw []byte, maxOutputChars int) (string, error) {
	if _, err := validateRawImageConfig(raw); err != nil {
		return "", err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode image", ErrInvalid)
	}
	if maxOutputChars <= 0 {
		maxOutputChars = MaxOutputChars
	}

	for _, maxDimension := range []int{640, 560, 480, 400, 320} {
		resized := resize(img, maxDimension)
		for _, quality := range []int{75, 65, 55, 45, 35} {
			encoded, err := encodeJPEG(resized, quality)
			if err != nil {
				return "", fmt.Errorf("encode photo: %w", err)
			}
			value := DataURLPrefix + base64.StdEncoding.EncodeToString(encoded)
			if len(value) <= maxOutputChars {
				return value, nil
			}
		}
	}
	return "", ErrTooLarge
}

// RawDataURL validates an uploaded image and returns a sanitized JPEG data URL
// suitable for OCR. Decoding and re-encoding intentionally strips EXIF, XMP,
// container metadata, and trailing payloads instead of forwarding upload bytes
// to the external OCR provider.
func RawDataURL(raw []byte) (string, error) {
	if _, err := validateRawImageConfig(raw); err != nil {
		return "", err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode image", ErrInvalid)
	}

	for _, maxDimension := range []int{maxOCRDimension, 1792, 1536, 1280, 1024} {
		resized := resize(img, maxDimension)
		flattened := flattenOnWhite(resized)
		for _, quality := range []int{90, 82, 74, 66, 58} {
			encoded, err := encodeJPEG(flattened, quality)
			if err != nil {
				return "", fmt.Errorf("encode OCR photo: %w", err)
			}
			if len(encoded) <= maxOCRImageBytes {
				value := DataURLPrefix + base64.StdEncoding.EncodeToString(encoded)
				if len(value) <= maxOCRDataURLChars {
					return value, nil
				}
			}
		}
	}
	return "", ErrTooLarge
}

func validateRawImageConfig(raw []byte) (string, error) {
	if len(raw) == 0 {
		return "", ErrInvalid
	}
	if int64(len(raw)) > MaxInputBytes {
		return "", ErrTooLarge
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%w: decode image config", ErrInvalid)
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxDecodeDimension || config.Height > maxDecodeDimension {
		return "", fmt.Errorf("%w: image dimensions are not supported", ErrInvalid)
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return "", fmt.Errorf("%w: unsupported image format", ErrInvalid)
	}
	return format, nil
}

func ValidateDataURL(value string) error {
	if !strings.HasPrefix(value, DataURLPrefix) {
		return ErrInvalid
	}
	encoded := strings.TrimPrefix(value, DataURLPrefix)
	if encoded == "" || len(value) > MaxOutputChars {
		return ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || int64(len(decoded)) > MaxInputBytes {
		return ErrInvalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
	if err != nil || format != "jpeg" || config.Width <= 0 || config.Height <= 0 {
		return ErrInvalid
	}
	return nil
}

func DecodeDataURL(value string) ([]byte, error) {
	if err := ValidateDataURL(value); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(strings.TrimPrefix(value, DataURLPrefix))
}

func resize(source image.Image, maxDimension int) image.Image {
	bounds := source.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	if width <= maxDimension && height <= maxDimension {
		return source
	}
	scale := float64(maxDimension) / float64(width)
	if height > width {
		scale = float64(maxDimension) / float64(height)
	}
	newWidth := int(float64(width) * scale)
	newHeight := int(float64(height) * scale)
	if newWidth < 1 {
		newWidth = 1
	}
	if newHeight < 1 {
		newHeight = 1
	}
	destination := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.CatmullRom.Scale(destination, destination.Bounds(), source, bounds, draw.Over, nil)
	return destination
}

func flattenOnWhite(source image.Image) image.Image {
	bounds := source.Bounds()
	destination := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	stddraw.Draw(destination, destination.Bounds(), image.White, image.Point{}, stddraw.Src)
	stddraw.Draw(destination, destination.Bounds(), source, bounds.Min, stddraw.Over)
	return destination
}

func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Keep the standard PNG decoder linked explicitly for builds where only this
// package is imported through tests.
var _ = png.Decode
