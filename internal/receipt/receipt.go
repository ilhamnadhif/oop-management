// Package receipt extracts editable purchase items from receipt images.
//
// The talking to the model lives in internal/vision. What is left here is what
// a receipt is: the prompt that describes one, the shape of a product row, and
// the rules a row has to survive before anyone is shown it.
package receipt

import (
	"context"
	"errors"

	"opp-management/internal/vision"
)

const (
	// DefaultBaseURL is the official Xiaomi MiMo OpenAI-compatible API base URL.
	DefaultBaseURL = vision.DefaultBaseURL
	// DefaultModel is the MiMo model that supports image understanding.
	DefaultModel = vision.DefaultModel
)

// The transport's failures are the scanner's failures. They are named again
// here so a caller that only scans receipts keeps importing one package, and
// errors.Is keeps working across both.
var (
	// ErrInvalidInput means the image data URL is missing or malformed.
	ErrInvalidInput = vision.ErrInvalidInput
	// ErrInvalidResponse means the upstream response could not be safely used.
	ErrInvalidResponse = vision.ErrInvalidResponse
	// ErrUnavailable means the scanner is not configured or temporarily unavailable.
	ErrUnavailable = vision.ErrUnavailable
	// ErrTimeout means the scan did not finish before its deadline.
	ErrTimeout = vision.ErrTimeout
	// ErrRateLimited means the upstream service rejected the request rate.
	ErrRateLimited = vision.ErrRateLimited
	// ErrUpstream means the upstream service rejected the request.
	ErrUpstream = vision.ErrUpstream

	// ErrNoItems means the model could not find a product row in the receipt.
	// Unlike the rest it is the receipt's own: a picture that is not a receipt
	// is not a transport failure.
	ErrNoItems = errors.New("receipt scanner found no items")
)

// UpstreamError safely exposes only an HTTP status code. It never includes an
// API key, receipt content, or upstream response body.
type UpstreamError = vision.UpstreamError

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
