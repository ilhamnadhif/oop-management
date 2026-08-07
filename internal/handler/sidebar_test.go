package handler

import (
	"net/http"
	"strings"
	"testing"
)

func fetchAuthedPage(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d", url, response.StatusCode)
	}
	return body
}

// navSection narrows a page to the sidebar nav. The user's own jabatan is
// printed elsewhere in the sidebar and would otherwise match a menu label.
func navSection(t *testing.T, page string) string {
	t.Helper()
	start := strings.Index(page, `<nav class="sidebar-nav"`)
	if start < 0 {
		t.Fatal("sidebar nav not found")
	}
	end := strings.Index(page[start:], "</nav>")
	if end < 0 {
		t.Fatal("sidebar nav is not closed")
	}
	return page[start : start+end]
}

// Absensi must lead the menu on every page; it is the one item every role
// needs.
func TestSidebarOrderPutsAbsensiFirst(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/dashboard", "/produksi", "/unit-dt"} {
		page := navSection(t, fetchAuthedPage(t, client, testServer.URL+path))

		positions := []struct {
			label string
			at    int
		}{
			{"Absensi", strings.Index(page, `>Absensi<`)},
			{"Produksi", strings.Index(page, `>Produksi<`)},
			{"Unit DT", strings.Index(page, `>Unit DT<`)},
		}
		for _, item := range positions {
			if item.at < 0 {
				t.Fatalf("%s: menu item %q missing", path, item.label)
			}
		}
		if !(positions[0].at < positions[1].at && positions[1].at < positions[2].at) {
			t.Fatalf("%s: menu order is Absensi=%d Produksi=%d UnitDT=%d",
				path, positions[0].at, positions[1].at, positions[2].at)
		}
	}
}

func TestSidebarMarksActivePageAndKeepsLogoutLast(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	active := map[string]string{
		"/dashboard": "Absensi",
		"/produksi":  "Produksi",
		"/unit-dt":   "Unit DT",
	}
	for path, label := range active {
		page := fetchAuthedPage(t, client, testServer.URL+path)

		marker := `aria-current="page">` + label + `<`
		if !strings.Contains(page, marker) {
			t.Fatalf("%s: active menu item is not %q", path, label)
		}
		// Breadcrumb names the current page.
		if !strings.Contains(page, `<li aria-current="page">`+label+`</li>`) {
			t.Fatalf("%s: breadcrumb does not show %q", path, label)
		}

		// Logout is pinned to the bottom, so it must come after every link.
		logoutAt := strings.Index(page, `action="/logout"`)
		lastLinkAt := strings.LastIndex(page, `class="sidebar-link`)
		if logoutAt < 0 || lastLinkAt < 0 {
			t.Fatalf("%s: sidebar is missing links or logout", path)
		}
		if logoutAt < lastLinkAt {
			t.Fatalf("%s: logout appears before the menu links", path)
		}
	}
}

func sectionBetween(t *testing.T, page, openTag, closeTag string) string {
	t.Helper()
	start := strings.Index(page, openTag)
	if start < 0 {
		t.Fatalf("%q not found", openTag)
	}
	end := strings.Index(page[start:], closeTag)
	if end < 0 {
		t.Fatalf("%q is not closed", openTag)
	}
	return page[start : start+end]
}

// The signed-in user's name and position belong in the topbar next to the date,
// not in the sidebar.
func TestUserIdentityLivesInTheTopbar(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/dashboard", "/produksi", "/unit-dt"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)

		topbar := sectionBetween(t, page, `<header class="topbar">`, "</header>")
		if !strings.Contains(topbar, "Budi Santoso") {
			t.Fatalf("%s: topbar is missing the user name", path)
		}
		if !strings.Contains(topbar, `class="topbar-user-role"`) {
			t.Fatalf("%s: topbar is missing the user position", path)
		}

		sidebar := sectionBetween(t, page, `<aside class="sidebar"`, "</aside>")
		if strings.Contains(sidebar, "Budi Santoso") {
			t.Fatalf("%s: the user name is still rendered in the sidebar", path)
		}
	}
}

// Attendance controls belong to the Absensi page only.
func TestOtherPagesDoNotLeakAttendanceMarkup(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/produksi", "/unit-dt"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		for _, absensiOnly := range []string{"data-attendance-action", `id="attendanceMap"`, `id="cameraModal"`} {
			if strings.Contains(page, absensiOnly) {
				t.Fatalf("%s: leaked attendance markup %q", path, absensiOnly)
			}
		}
	}
}

func TestNewPagesRequireASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for _, path := range []string{"/produksi", "/unit-dt"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
			t.Fatalf("%s: anonymous status %d, want a redirect", path, response.StatusCode)
		}
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("%s: redirected to %q, want /login", path, location)
		}
	}
}

func TestShellScriptIsServed(t *testing.T) {
	testServer := newTestServer(t)
	for _, path := range []string{"/static/js/shell.js", "/static/js/clock.js"} {
		body := fetchPage(t, testServer.URL+path)
		if len(body) == 0 {
			t.Fatalf("%s is empty", path)
		}
	}
}
