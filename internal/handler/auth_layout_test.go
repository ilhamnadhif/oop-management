package handler

import (
	"net/http"
	"strings"
	"testing"
)

func stylesheet(t *testing.T) string {
	t.Helper()
	testServer := newTestServer(t)
	response, err := http.Get(testServer.URL + "/static/css/style.css")
	if err != nil {
		t.Fatalf("get stylesheet: %v", err)
	}
	return readBody(t, response)
}

// The sign-in pages are framed by the viewport: the page itself never scrolls,
// on a desktop or a phone. A form too tall for the screen scrolls inside the
// card, so the brand, the tabs and the background art stay where they are.
func TestAuthPagesFitTheViewport(t *testing.T) {
	css := stylesheet(t)

	for _, rule := range []string{
		// The page is locked to the viewport height.
		"body:has(.auth-shell) { height: 100svh; overflow: hidden; }",
		// The card is a column that cannot outgrow its frame.
		"max-height: 100%",
		// Only the form scrolls.
		".auth-card .stack-form {",
		"overflow-y: auto",
		"overscroll-behavior: contain",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}

	// A phone gets the same treatment; letting the shell scroll would hand the
	// page back to the address bar.
	if strings.Contains(css, ".auth-shell { align-items: start; overflow-y: auto;") {
		t.Fatal("the auth shell still scrolls on a phone")
	}
}

// On a phone the whole card scrolls, not the form inside it. A swipe lands
// anywhere, and scrolling only the form left the submit button cut off at the
// card's edge for anyone who swiped over the logo or the tabs.
func TestAuthCardScrollsWholeOnASmallScreen(t *testing.T) {
	css := stylesheet(t)

	// The sheet declares the phone breakpoint twice; the later block wins.
	mobile := css[strings.LastIndex(css, "@media (max-width: 720px)"):]
	mobile = mobile[:strings.Index(mobile, "\n}")]
	for _, rule := range []string{
		".auth-card { overflow-y: auto; overscroll-behavior: contain; }",
		".auth-card .stack-form { margin: 0; padding: 0; overflow: visible; }",
		// Padding at the end of a scrolling flex column is dropped by the
		// engine, so the room under the button is a block of its own.
		`.auth-card .stack-form::after { display: block; height: 0.9rem; content: ""; }`,
	} {
		if !strings.Contains(mobile, rule) {
			t.Fatalf("the phone rules are missing %q", rule)
		}
	}

	// A short screen has the same problem for the same reason.
	if !strings.Contains(css, "@media (max-height: 700px) {") {
		t.Fatal("a short screen does not get the scrolling card")
	}
}

// The tick and its words are one line: the global input sizing would give the
// box the height of a text field and push the label onto its own row.
func TestRememberMeStaysOnOneLine(t *testing.T) {
	css := stylesheet(t)
	for _, rule := range []string{
		".checkbox-row { flex-wrap: nowrap; }",
		"width: 20px; height: 20px; min-height: 0;",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}
}

// A grid column sized to its content grows to the longest line of text, which
// pushed the card wider than a phone screen.
func TestAuthShellColumnIsBoundedByTheScreen(t *testing.T) {
	css := stylesheet(t)
	// .auth-shell is declared several times over the sheet; the one that lays
	// the page out is the block that stacks the background, so it is found by
	// that rather than by counting occurrences.
	anchor := strings.Index(css, "isolation: isolate")
	if anchor < 0 {
		t.Fatal("the auth shell no longer establishes its own stacking context")
	}
	end := strings.Index(css[anchor:], "}")
	if end < 0 {
		t.Fatal("the auth shell rule is not closed")
	}
	block := css[anchor : anchor+end]
	for _, rule := range []string{"grid-template-columns: minmax(0, 1fr)", "height: 100svh"} {
		if !strings.Contains(block, rule) {
			t.Fatalf("the auth shell is missing %q: %s", rule, block)
		}
	}
}

// Overlay scrollbars fade out, so a form that scrolls would look like it simply
// ends mid-field.
func TestAuthFormShowsThatItScrolls(t *testing.T) {
	css := stylesheet(t)
	for _, rule := range []string{
		"scrollbar-width: thin",
		".auth-card .stack-form::-webkit-scrollbar",
	} {
		if !strings.Contains(css, rule) {
			t.Fatalf("the stylesheet is missing %q", rule)
		}
	}
}

// The password pair shares a row so the whole form fits a laptop screen. Both
// fields keep their required marker, which the wrapper around the show/hide
// button would otherwise hide from the label rule.
func TestRegisterPasswordsSitInTheGrid(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchPage(t, testServer.URL+"/register")

	grid := page[strings.Index(page, `<div class="form-grid">`):]
	grid = grid[:strings.Index(grid, "</form>")]
	for _, field := range []string{`id="register-password"`, `id="register-password-confirm"`} {
		if !strings.Contains(grid, field) {
			t.Fatalf("%s is outside the form grid", field)
		}
	}
	if !strings.Contains(page, `data-confirm-for="register-password"`) {
		t.Fatal("the confirmation field no longer checks against the password")
	}

	css := stylesheet(t)
	if !strings.Contains(css, ".auth-card .form-grid label:has(+ .input-with-action > input:required)::after") {
		t.Fatal("a password label inside the grid loses its required marker")
	}
}
