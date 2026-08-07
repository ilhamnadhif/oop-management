package photo

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
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
