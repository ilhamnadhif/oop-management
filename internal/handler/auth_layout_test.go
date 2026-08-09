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

// On a desktop the sign-in pages are framed by the viewport: the page itself
// never scrolls, and a form too tall for the window scrolls inside the card so
// the brand, the tabs and the background art stay where they are.
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

	// A phone is the exception, and has its own test: a locked viewport there
	// fights the browser's toolbar.
}

// A phone scrolls the page itself. Locking the viewport and nesting a scroll
// container inside it fights the browser's own toolbar: the bar slides away,
// the locked height keeps its old value, and the submit button ends up under
// the fold with nothing willing to scroll to it.
func TestAuthPagesScrollThePageOnAPhone(t *testing.T) {
	css := stylesheet(t)

	// The sheet declares the phone breakpoint twice; the later block wins.
	mobile := css[strings.LastIndex(css, "@media (max-width: 720px)"):]
	mobile = mobile[:strings.Index(mobile, "\n}")]
	for _, rule := range []string{
		"body:has(.auth-shell) { height: auto; overflow: visible; }",
		"min-height: 100svh",
		".auth-card { max-height: none; overflow: visible; }",
		// The decorative circles hang below the shell. Clipped, they stop
		// adding a screenful of empty page under the button.
		"overflow: clip",
	} {
		if !strings.Contains(mobile, rule) {
			t.Fatalf("the phone rules are missing %q: %s", rule, mobile)
		}
	}

	// A short window has the same problem for the same reason.
	if !strings.Contains(css, "@media (max-height: 700px) {") {
		t.Fatal("a short window still locks the viewport")
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
