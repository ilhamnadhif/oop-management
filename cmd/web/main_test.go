package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

// The whole point of these messages is that somebody reading one knows what to
// go and do. A test on the wording is a test on that.
func TestSpreadsheetProblemSaysWhatToDoAboutIt(t *testing.T) {
	for name, testCase := range map[string]struct {
		err  error
		want string
	}{
		"wrong id": {
			err:  &googleapi.Error{Code: http.StatusNotFound},
			want: "tidak ditemukan",
		},
		"never shared": {
			err:  &googleapi.Error{Code: http.StatusForbidden},
			want: "belum dibagikan ke service account",
		},
		"bad credentials": {
			err:  &googleapi.Error{Code: http.StatusUnauthorized},
			want: "Kredensial service account",
		},
		"quota": {
			err:  &googleapi.Error{Code: http.StatusTooManyRequests},
			want: "Kuota",
		},
		"google is down": {
			err:  &googleapi.Error{Code: http.StatusBadGateway},
			want: "sedang bermasalah",
		},
		"no route to google": {
			err:  &url.Error{Op: "Post", URL: "https://sheets.googleapis.com", Err: errors.New("dial tcp: lookup failed")},
			want: "Periksa koneksi internet server",
		},
		"google stopped answering": {
			err:  context.DeadlineExceeded,
			want: "tidak merespons",
		},
	} {
		got := spreadsheetProblem(testCase.err)
		if !strings.Contains(got.Error(), testCase.want) {
			t.Errorf("%s: message = %q, want it to mention %q", name, got, testCase.want)
		}
	}
}

// Anything this function has no advice for is passed through as itself, so the
// real reason reaches the log instead of being replaced by a guess.
func TestSpreadsheetProblemPassesThroughWhatItCannotExplain(t *testing.T) {
	original := errors.New("something nobody anticipated")
	if got := spreadsheetProblem(original); !errors.Is(got, original) {
		t.Fatalf("got %v, want the original error", got)
	}
}
