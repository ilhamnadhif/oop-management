package handler

import (
	"strings"
	"testing"
)

// Icons are decorative labels' companions, so they must be hidden from screen
// readers; announcing them would just repeat the adjacent text.
func TestIconsAreDecorativeAndSelfContained(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	pages := map[string]string{
		"login":     fetchPage(t, testServer.URL+"/login"),
		"register":  fetchPage(t, testServer.URL+"/register"),
		"dashboard": fetchAuthedPage(t, client, testServer.URL+"/dashboard"),
		"produksi":  fetchAuthedPage(t, client, testServer.URL+"/produksi"),
		"unit-dt":   fetchAuthedPage(t, client, testServer.URL+"/unit-dt"),
	}

	for name, page := range pages {
		count := strings.Count(page, `class="icon"`)
		if count == 0 {
			t.Fatalf("%s: no icons rendered", name)
		}
		if aria := strings.Count(page, `class="icon" viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"`); aria != count {
			t.Fatalf("%s: %d of %d icons are not aria-hidden", name, count-aria, count)
		}
		// An unknown icon name still renders the <svg> shell but with an empty
		// path, which is invisible and easy to miss by eye.
		if strings.Contains(page, `<path d=""></path>`) {
			t.Fatalf("%s: an icon name did not match any shape", name)
		}
	}
}

func TestSidebarAndSectionHeadingsCarryIcons(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")

	// Every menu item gets one.
	nav := navSection(t, page)
	if got := strings.Count(nav, `class="icon"`); got != len(navItems) {
		t.Fatalf("sidebar has %d icons for %d menu items", got, len(navItems))
	}

	// So does every section heading.
	for _, eyebrow := range []string{"INFORMASI UMUM", "DATA UNIT", "KALKULASI OTOMATIS"} {
		at := strings.Index(page, eyebrow)
		if at < 0 {
			t.Fatalf("heading %q not found", eyebrow)
		}
		opening := strings.LastIndex(page[:at], `<p class="eyebrow">`)
		if opening < 0 || !strings.Contains(page[opening:at], `class="icon"`) {
			t.Fatalf("heading %q has no icon", eyebrow)
		}
	}
}
