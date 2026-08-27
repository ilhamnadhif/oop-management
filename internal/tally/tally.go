// Package tally reads a handwritten production tally sheet into rows.
//
// The sheet is a printed grid filled in by hand through the day: one line per
// load, ten columns, and a date written once at the head. What this package
// returns is what the paper appears to say, cleaned of stray whitespace and
// bounded in size. It resolves nothing: a nopol that no unit register knows is
// still handed back. Which row is usable is a question about the business, and
// it is answered where the register and the options live, not here.
//
// The date is not read at all. It is filled in by hand when the sheet is
// confirmed, because the column is too often left blank on the paper and a page
// that arrived without one used to fail at the last step.
package tally

import (
	"context"
	"errors"

	"opp-management/internal/vision"
)

// MaxRows bounds one sheet. A tally sheet is a page; something claiming more
// lines than a page holds is a misread photograph, and a generous limit is how
// one strange image spends a token budget.
const MaxRows = 200

var (
	// ErrInvalidInput means the image data URL is missing or malformed.
	ErrInvalidInput = vision.ErrInvalidInput
	// ErrInvalidResponse means the reading could not be safely used.
	ErrInvalidResponse = vision.ErrInvalidResponse
	// ErrUnavailable means the scanner is not configured or temporarily unavailable.
	ErrUnavailable = vision.ErrUnavailable
	// ErrTimeout means the scan did not finish before its deadline.
	ErrTimeout = vision.ErrTimeout
	// ErrRateLimited means the upstream service rejected the request rate.
	ErrRateLimited = vision.ErrRateLimited
	// ErrUpstream means the upstream service rejected the request.
	ErrUpstream = vision.ErrUpstream

	// ErrNoRows means the picture held no filled line. It is separate from a
	// transport failure because the answer to it is a better photograph, not a
	// retry.
	ErrNoRows = errors.New("tally scanner found no rows")
)

// Row is one line of the sheet as read. Every field is what the paper appears
// to say: nothing here has been looked up, matched, or filled in.
type Row struct {
	// Nomor is the No column, which is how a rejected row is pointed at later.
	Nomor    int     `json:"no"`
	Project  string  `json:"project"`
	Supplier string  `json:"supplier"`
	Quary    string  `json:"quary"`
	Kategori string  `json:"kategori"`
	Lokasi   string  `json:"lokasi"`
	Layer    string  `json:"layer"`
	Nopol    string  `json:"nopol"`
	TT       float64 `json:"tt"`

	// Alasan is set when a cell came back unusable - a top-up height that is not
	// a height. The row is carried rather than dropped so the page can point at
	// the line that needs looking at, and one bad cell does not throw away the
	// ninety-nine rows read correctly.
	//
	// It is not part of the schema the model answers to: the field is written
	// here, never read from the completion.
	Alasan string `json:"-"`
}

// Sheet is one photographed page.
type Sheet struct {
	Rows     []Row    `json:"rows"`
	Warnings []string `json:"warnings,omitempty"`
}

// Reason reports the fixed keyword a failure was tagged with, or "" when it
// carries none. Safe to log: the keywords are written in the source.
func Reason(err error) string { return vision.Reason(err) }

// Scanner reads one tally sheet image into rows.
type Scanner interface {
	Scan(ctx context.Context, imageDataURL string) (Sheet, error)
}
