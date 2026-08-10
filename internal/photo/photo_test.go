package photo

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestNormalizeAndDecodeDataURL(t *testing.T) {
	imageData := image.NewRGBA(image.Rect(0, 0, 900, 700))
	for y := 0; y < 700; y++ {
		for x := 0; x < 900; x++ {
			imageData.SetRGBA(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 180, A: 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, imageData); err != nil {
		t.Fatalf("encode source png: %v", err)
	}

	value, err := Normalize(source.Bytes(), MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize photo: %v", err)
	}
	if err := ValidateDataURL(value); err != nil {
		t.Fatalf("validate normalized photo: %v", err)
	}
	decoded, err := DecodeDataURL(value)
	if err != nil {
		t.Fatalf("decode data URL: %v", err)
	}
	if len(decoded) == 0 || len(value) > MaxOutputChars {
		t.Fatalf("unexpected normalized photo size: encoded=%d decoded=%d", len(value), len(decoded))
	}
}

func TestValidateDataURLRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "not-a-data-url", "data:image/png;base64,abc", DataURLPrefix + "not-base64"} {
		if err := ValidateDataURL(value); err == nil {
			t.Errorf("ValidateDataURL(%q) unexpectedly succeeded", value)
		}
	}
}

func TestNormalizeRejectsOversizedInput(t *testing.T) {
	_, err := Normalize(make([]byte, MaxInputBytes+1), MaxOutputChars)
	if err != ErrTooLarge {
		t.Fatalf("expected ErrTooLarge, got %v", err)
	}
}

func TestRawDataURLSanitizesSupportedFormats(t *testing.T) {
	const (
		metadataMarker = "private-receipt-exif-xmp-metadata"
		trailingMarker = "private-receipt-trailing-payload"
	)

	receipt := image.NewRGBA(image.Rect(0, 0, 2300, 1500))
	for y := 0; y < receipt.Bounds().Dy(); y++ {
		for x := 0; x < receipt.Bounds().Dx(); x++ {
			shade := uint8(235 + (x+y)%20)
			receipt.SetRGBA(x, y, color.RGBA{R: shade, G: shade, B: shade, A: 255})
		}
	}

	var jpegSource bytes.Buffer
	if err := jpeg.Encode(&jpegSource, receipt, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode source jpeg: %v", err)
	}
	var pngSource bytes.Buffer
	if err := png.Encode(&pngSource, receipt); err != nil {
		t.Fatalf("encode source png: %v", err)
	}
	webpSource, err := base64.StdEncoding.DecodeString("UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZlgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAXFOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8yZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wOK3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8Jkfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvshXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==")
	if err != nil {
		t.Fatalf("decode source webp fixture: %v", err)
	}

	tests := []struct {
		name       string
		raw        []byte
		sourceSize image.Point
	}{
		{
			name:       "jpeg",
			raw:        addJPEGMetadataAndTrailing(jpegSource.Bytes(), []byte(metadataMarker), []byte(trailingMarker)),
			sourceSize: image.Pt(2300, 1500),
		},
		{
			name:       "png",
			raw:        addPNGMetadataAndTrailing(pngSource.Bytes(), []byte(metadataMarker), []byte(trailingMarker)),
			sourceSize: image.Pt(2300, 1500),
		},
		{
			name:       "webp",
			raw:        addWebPMetadataAndTrailing(webpSource, []byte(metadataMarker), []byte(trailingMarker)),
			sourceSize: image.Pt(75, 100),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !bytes.Contains(test.raw, []byte(metadataMarker)) || !bytes.Contains(test.raw, []byte(trailingMarker)) {
				t.Fatal("test source is missing private marker data")
			}
			value, err := RawDataURL(test.raw)
			if err != nil {
				t.Fatalf("create sanitized data URL: %v", err)
			}
			if !strings.HasPrefix(value, DataURLPrefix) {
				t.Fatalf("expected prefix %q, got %.30q", DataURLPrefix, value)
			}
			if len(value) > maxOCRDataURLChars {
				t.Fatalf("sanitized data URL is too large: %d", len(value))
			}
			encoded := strings.TrimPrefix(value, DataURLPrefix)
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				t.Fatalf("decode data URL: %v", err)
			}
			if bytes.Contains(decoded, []byte(metadataMarker)) {
				t.Fatal("sanitized image still contains metadata marker")
			}
			if bytes.Contains(decoded, []byte(trailingMarker)) {
				t.Fatal("sanitized image still contains trailing payload")
			}
			if bytes.Equal(decoded, test.raw) {
				t.Fatal("raw upload bytes were forwarded unchanged")
			}

			config, format, err := image.DecodeConfig(bytes.NewReader(decoded))
			if err != nil {
				t.Fatalf("decode sanitized image config: %v", err)
			}
			if format != "jpeg" {
				t.Fatalf("sanitized format = %q, want jpeg", format)
			}
			wantSize := scaledSize(test.sourceSize, maxOCRDimension)
			if config.Width != wantSize.X || config.Height != wantSize.Y {
				t.Fatalf("sanitized dimensions = %dx%d, want %dx%d", config.Width, config.Height, wantSize.X, wantSize.Y)
			}
		})
	}
}

func addJPEGMetadataAndTrailing(raw, metadata, trailing []byte) []byte {
	payload := append([]byte("Exif\x00\x00"), metadata...)
	segment := []byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	segment = append(segment, payload...)

	result := append([]byte{}, raw[:2]...)
	result = append(result, segment...)
	result = append(result, raw[2:]...)
	return append(result, trailing...)
}

func addPNGMetadataAndTrailing(raw, metadata, trailing []byte) []byte {
	data := append([]byte("ReceiptMetadata\x00"), metadata...)
	typeAndData := append([]byte("tEXt"), data...)
	chunk := make([]byte, 4, 12+len(data))
	binary.BigEndian.PutUint32(chunk, uint32(len(data)))
	chunk = append(chunk, typeAndData...)
	checksum := make([]byte, 4)
	binary.BigEndian.PutUint32(checksum, crc32.ChecksumIEEE(typeAndData))
	chunk = append(chunk, checksum...)

	const iendSize = 12
	result := append([]byte{}, raw[:len(raw)-iendSize]...)
	result = append(result, chunk...)
	result = append(result, raw[len(raw)-iendSize:]...)
	return append(result, trailing...)
}

func addWebPMetadataAndTrailing(raw, metadata, trailing []byte) []byte {
	chunk := append([]byte("XMP "), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(metadata)))
	chunk = append(chunk, metadata...)
	if len(metadata)%2 != 0 {
		chunk = append(chunk, 0)
	}

	result := append([]byte{}, raw...)
	result = append(result, chunk...)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(result)-8))
	return append(result, trailing...)
}

func scaledSize(source image.Point, maxDimension int) image.Point {
	if source.X <= maxDimension && source.Y <= maxDimension {
		return source
	}
	scale := float64(maxDimension) / float64(source.X)
	if source.Y > source.X {
		scale = float64(maxDimension) / float64(source.Y)
	}
	return image.Pt(int(float64(source.X)*scale), int(float64(source.Y)*scale))
}

func TestRawDataURLRejectsInvalidImages(t *testing.T) {
	oversizedDimension := image.NewRGBA(image.Rect(0, 0, maxDecodeDimension+1, 1))
	var oversizedDimensionSource bytes.Buffer
	if err := png.Encode(&oversizedDimensionSource, oversizedDimension); err != nil {
		t.Fatalf("encode oversized-dimension png: %v", err)
	}

	unsupported := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var unsupportedSource bytes.Buffer
	if err := gif.Encode(&unsupportedSource, unsupported, nil); err != nil {
		t.Fatalf("encode unsupported gif: %v", err)
	}

	tests := []struct {
		name string
		raw  []byte
		err  error
	}{
		{name: "empty", raw: nil, err: ErrInvalid},
		{name: "oversized bytes", raw: make([]byte, MaxInputBytes+1), err: ErrTooLarge},
		{name: "oversized dimensions", raw: oversizedDimensionSource.Bytes(), err: ErrInvalid},
		{name: "unsupported format", raw: unsupportedSource.Bytes(), err: ErrInvalid},
		{name: "not an image", raw: []byte("not-an-image"), err: ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RawDataURL(test.raw); !errors.Is(err, test.err) {
				t.Fatalf("expected %v, got %v", test.err, err)
			}
		})
	}
}
