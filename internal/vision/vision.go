// Package vision talks to Xiaomi MiMo's OpenAI-compatible chat completions API
// for reading one image into JSON.
//
// It knows how to send an image and get validated JSON back, and nothing about
// what is in the picture. Callers own their prompt, their schema, and their
// validation; two of them reading two different documents share this transport
// rather than a copy of it, so a fix to a timeout or a leaking error message
// lands in one place instead of being made twice and remembered once.
package vision

import (
	"context"
	"encoding/json"
	"errors"
	"io"
)

const (
	// DefaultBaseURL is the official Xiaomi MiMo OpenAI-compatible API base URL.
	DefaultBaseURL = "https://api.xiaomimimo.com/v1"
	// DefaultModel is the MiMo model that supports image understanding.
	DefaultModel = "mimo-v2.5"
	// MaxResponseBytes caps both the upstream body and the JSON the model wrote
	// inside it, so a runaway completion cannot be read into memory whole.
	MaxResponseBytes = 1 << 20
)

var (
	// ErrInvalidInput means the image data URL is missing or malformed.
	ErrInvalidInput = errors.New("vision request input is invalid")
	// ErrInvalidResponse means the upstream response could not be safely used.
	ErrInvalidResponse = errors.New("vision response is invalid")
	// ErrUnavailable means the client is not configured or temporarily unavailable.
	ErrUnavailable = errors.New("vision service is unavailable")
	// ErrTimeout means the read did not finish before its deadline.
	ErrTimeout = errors.New("vision request timed out")
	// ErrRateLimited means the upstream service rejected the request rate.
	ErrRateLimited = errors.New("vision service is rate limited")
	// ErrUpstream means the upstream service rejected the request.
	ErrUpstream = errors.New("vision upstream request failed")
)

// reasonError attaches a fixed keyword to one of the package errors so a failure
// can be diagnosed from a log without any of the document reaching it. The
// keyword comes from a closed set written here; nothing read from the image, the
// prompt, or the provider's body is ever part of it.
type reasonError struct {
	reason string
	kind   error
}

func (e *reasonError) Error() string {
	return e.kind.Error() + " (" + e.reason + ")"
}

func (e *reasonError) Unwrap() error { return e.kind }

// Reasoned pairs one of this package's errors with a fixed keyword.
func Reasoned(kind error, reason string) error {
	return &reasonError{reason: reason, kind: kind}
}

// Reason reports the keyword a failure was tagged with, or "" when it carries
// none. It is safe to log: the set of keywords is written in the source.
func Reason(err error) string {
	var reasoned *reasonError
	if errors.As(err, &reasoned) {
		return reasoned.reason
	}
	return ""
}

// UpstreamError safely exposes only an HTTP status code. It never includes an
// API key, document content, or upstream response body.
type UpstreamError struct {
	StatusCode int
	kind       error
}

func (e *UpstreamError) Error() string {
	return "vision upstream request failed"
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

// DecodeStrictJSON reads exactly one JSON value and rejects unknown fields.
// Model-authored JSON is decoded this way: a field nobody asked for is a model
// inventing structure, and reading around it is how a wrong value gets stored
// as if it had been checked.
func DecodeStrictJSON(reader io.Reader, destination any) error {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	return decodeSingle(decoder, destination)
}

// DecodeLooseJSON reads exactly one JSON value and allows unknown fields. The
// OpenAI-compatible envelope may legitimately gain metadata, so only the
// envelope is read this way.
func DecodeLooseJSON(reader io.Reader, destination any) error {
	return decodeSingle(json.NewDecoder(reader), destination)
}

func decodeSingle(decoder *json.Decoder, destination any) error {
	if err := decoder.Decode(destination); err != nil {
		// The decoder's own message quotes the offending JSON, which is content
		// from the image. Only the shape of the failure survives.
		return Reasoned(ErrInvalidResponse, "json-shape")
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return Reasoned(ErrInvalidResponse, "json-trailing")
	}
	return nil
}

// contextError translates a finished context into the package's own errors, so
// a caller can tell a deadline from a cancellation without importing context
// semantics it does not otherwise care about.
func contextError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return ErrTimeout
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return nil
}
