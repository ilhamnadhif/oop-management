// Package receipt extracts editable purchase items from receipt images.
package receipt

import (
	"context"
	"errors"
)

const (
	// DefaultBaseURL is the official Xiaomi MiMo OpenAI-compatible API base URL.
	DefaultBaseURL = "https://api.xiaomimimo.com/v1"
	// DefaultModel is the MiMo model that supports image understanding.
	DefaultModel = "mimo-v2.5"
)

var (
	// ErrInvalidInput means the image data URL is missing or malformed.
	ErrInvalidInput = errors.New("receipt scanner input is invalid")
	// ErrNoItems means the model could not find a product row in the receipt.
	ErrNoItems = errors.New("receipt scanner found no items")
	// ErrInvalidResponse means the upstream response could not be safely used.
	ErrInvalidResponse = errors.New("receipt scanner response is invalid")
	// ErrUnavailable means the scanner is not configured or temporarily unavailable.
	ErrUnavailable = errors.New("receipt scanner is unavailable")
	// ErrTimeout means the scan did not finish before its deadline.
	ErrTimeout = errors.New("receipt scanner timed out")
	// ErrRateLimited means the upstream service rejected the request rate.
	ErrRateLimited = errors.New("receipt scanner is rate limited")
	// ErrUpstream means the upstream service rejected the request.
	ErrUpstream = errors.New("receipt scanner upstream request failed")
)

// Item is one editable product row extracted from a receipt. Harga is the unit
// price in whole Indonesian Rupiah.
type Item struct {
	NamaProduk string  `json:"nama_produk"`
	Satuan     string  `json:"satuan"`
	Volume     float64 `json:"volume"`
	Harga      int64   `json:"harga"`
}

// Result is a validated extraction result. TotalTerbaca is optional because
// some receipts do not show a reliable grand total.
type Result struct {
	Items        []Item   `json:"items"`
	TotalTerbaca *int64   `json:"total_terbaca,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Scanner extracts validated purchase items from an image data URL.
type Scanner interface {
	Scan(ctx context.Context, imageDataURL string) (Result, error)
}

// UpstreamError safely exposes only an HTTP status code. It never includes an
// API key, receipt content, or upstream response body.
type UpstreamError struct {
	StatusCode int
	kind       error
}

func (e *UpstreamError) Error() string {
	return "receipt scanner upstream request failed"
}

func (e *UpstreamError) Unwrap() error {
	if e == nil || e.kind == nil {
		return ErrUpstream
	}
	return e.kind
}

func (e *UpstreamError) Is(target error) bool {
	if target == ErrUpstream {
		return true
	}
	return e != nil && target == e.kind
}
