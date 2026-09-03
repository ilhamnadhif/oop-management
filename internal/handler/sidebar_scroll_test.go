package handler

import (
	"strings"
	"testing"
)

// Every page here is a fresh server render, so the sidebar is rebuilt from the
// top on every click. The shell script is what keeps the place somebody had
// scrolled to, and it has to be on the page for that to happen at all.
func TestTheShellScriptIsOnEveryPage(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/dashboard", "/absensi", "/hr/overview"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		if !strings.Contains(page, `src="/static/js/shell.js"`) {
			t.Fatalf("%s does not load the shell script", path)
		}
	}
}

// The script remembers the position per tab and puts it back on the next page.
// It also has to survive a browser that refuses storage: a menu that will not
// scroll is a worse failure than a menu that forgets where it was.
func TestTheShellScriptRestoresTheSidebarScroll(t *testing.T) {
	testServer := newTestServer(t)
	script := fetchPage(t, testServer.URL+"/static/js/shell.js")

	for _, want := range []string{
		// Both the nav and the sidebar can be the scrolling element, depending
		// on the width, so both are remembered.
		"sidebar-scroll",
		"sidebar-nav-scroll",
		// Per tab, not per browser: two tabs must not drag each other about.
		"sessionStorage",
		"scrollTop",
		// Written on the way out as well, since a click may leave before the
		// next animation frame runs.
		`addEventListener("pagehide"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("the shell script is missing %q", want)
		}
	}
	if !strings.Contains(script, "catch") {
		t.Fatal("the script does not guard against storage being unavailable")
	}
}
