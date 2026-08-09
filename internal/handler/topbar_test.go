package handler

import (
	"strings"
	"testing"
	"time"
)

// The full date does not fit a phone header beside the menu button and the
// avatar, and a date cut off mid-month reads as a rendering fault.
func TestTopbarCarriesBothDateForms(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")

	topbar := sectionBetween(t, page, `<header class="topbar">`, "</header>")
	for _, fragment := range []string{
		`<span class="date-long">Jumat, 7 Agustus 2026</span>`,
		`<span class="date-short">7 Agu 2026</span>`,
	} {
		if !strings.Contains(topbar, fragment) {
			t.Fatalf("the topbar is missing %q", fragment)
		}
	}

	css := stylesheet(t)
	// One form is shown at a time, and the avatar never gives up its width.
	for _, rule := range []string{
		".topbar-date .date-short { display: none; }",
		".topbar-date .date-long { display: none; }",
		".topbar-date .date-short { display: inline; }",
		".topbar-meta { display: flex; align-items: center; gap: 1rem; flex-shrink: 0; }",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}
}

func TestShortIndonesianDate(t *testing.T) {
	cases := map[string]string{
		"2026-08-07": "7 Agu 2026",
		"2026-05-01": "1 Mei 2026",
		"2026-01-31": "31 Jan 2026",
		"2026-12-25": "25 Des 2026",
	}
	for date, want := range cases {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			t.Fatalf("parse %s: %v", date, err)
		}
		if got := formatShortIndonesianDate(parsed); got != want {
			t.Fatalf("formatShortIndonesianDate(%s) = %q, want %q", date, got, want)
		}
	}
}
