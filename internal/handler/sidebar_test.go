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

		// Look inside the nav itself. Matching on the whole page would also hit
		// the breadcrumb's aria-current, which would keep this passing even if
		// the sidebar stopped marking anything.
		nav := navSection(t, page)
		activeAt := strings.Index(nav, `aria-current="page"`)
		if activeAt < 0 {
			t.Fatalf("%s: no menu item is marked active", path)
		}
		linkEnd := strings.Index(nav[activeAt:], "</a>")
		if linkEnd < 0 || !strings.Contains(nav[activeAt:activeAt+linkEnd], ">"+label+"<") {
			t.Fatalf("%s: active menu item is not %q", path, label)
		}
		if strings.Count(nav, `aria-current="page"`) != 1 {
			t.Fatalf("%s: %d menu items marked active, want 1", path, strings.Count(nav, `aria-current="page"`))
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

// The signed-in user's name and position sit at the bottom of the sidebar,
// alongside the logout control - not in the topbar.
func TestUserIdentityLivesAtTheBottomOfTheSidebar(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/dashboard", "/produksi", "/unit-dt"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)

		sidebar := sectionBetween(t, page, `<aside class="sidebar"`, "</aside>")
		if !strings.Contains(sidebar, "Budi Santoso") {
			t.Fatalf("%s: sidebar is missing the user name", path)
		}
		if !strings.Contains(sidebar, `class="account-role"`) {
			t.Fatalf("%s: sidebar is missing the user position", path)
		}
		// The avatar shows the first letter of the name.
		if !strings.Contains(sidebar, `class="account-avatar" aria-hidden="true">B<`) {
			t.Fatalf("%s: avatar initial is wrong or missing", path)
		}
		// It belongs below every menu link.
		accountAt := strings.Index(sidebar, `class="sidebar-account"`)
		lastLinkAt := strings.LastIndex(sidebar, `class="sidebar-link`)
		if accountAt < 0 || accountAt < lastLinkAt {
			t.Fatalf("%s: the account card is not below the menu", path)
		}

		topbar := sectionBetween(t, page, `<header class="topbar">`, "</header>")
		if strings.Contains(topbar, "Budi Santoso") {
			t.Fatalf("%s: the user name is still rendered in the topbar", path)
		}
	}
}

// The breadcrumb sits under the page title, inside the content area rather than
// the topbar, and every page gets exactly one of each.
func TestBreadcrumbSitsBelowThePageTitle(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	titles := map[string]string{
		"/dashboard":         "Absensi",
		"/produksi":          "Produksi",
		"/produksi/overview": "Produksi Overview",
		"/unit-dt":           "Unit DT",
		"/unit-a2b":          "Unit A2B",
	}
	for path, title := range titles {
		page := fetchAuthedPage(t, client, testServer.URL+path)

		main := sectionBetween(t, page, `<main class="app-shell">`, "</main>")
		titleAt := strings.Index(main, `<h1 class="page-title">`+title+`</h1>`)
		crumbAt := strings.Index(main, `<nav class="breadcrumb"`)
		if titleAt < 0 {
			t.Fatalf("%s: page title %q not found in the content area", path, title)
		}
		if crumbAt < 0 {
			t.Fatalf("%s: breadcrumb not found in the content area", path)
		}
		if crumbAt < titleAt {
			t.Fatalf("%s: breadcrumb appears above the page title", path)
		}

		topbar := sectionBetween(t, page, `<header class="topbar">`, "</header>")
		if strings.Contains(topbar, `class="breadcrumb"`) {
			t.Fatalf("%s: breadcrumb is still in the topbar", path)
		}

		// Moving the title into the shell must not leave a page rendering its
		// own copy as well.
		if got := strings.Count(page, `class="page-title"`); got != 1 {
			t.Fatalf("%s: %d page titles rendered, want 1", path, got)
		}
		if got := strings.Count(page, `class="breadcrumb"`); got != 1 {
			t.Fatalf("%s: %d breadcrumbs rendered, want 1", path, got)
		}
	}
}

// Where the hamburger appears the sidebar is hidden, so the topbar carries the
// logo instead of the open page's name. Both swap together in the same query:
// showing one without hiding the other leaves the topbar cluttered.
func TestTopbarShowsTheLogoBesideTheHamburger(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	topbar := sectionBetween(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"), `<header class="topbar">`, "</header>")
	toggleAt := strings.Index(topbar, `id="menuToggle"`)
	logoAt := strings.Index(topbar, `class="topbar-logo"`)
	if toggleAt < 0 || logoAt < 0 {
		t.Fatal("topbar is missing the hamburger or the logo")
	}
	if logoAt < toggleAt {
		t.Fatal("the logo is rendered before the hamburger")
	}
	if !strings.Contains(topbar, `src="/static/img/opp-logo.png"`) {
		t.Fatal("the topbar logo does not point at the brand image")
	}

	stylesheet := fetchPage(t, testServer.URL+"/static/css/style.css")
	mobile := sectionBetween(t, stylesheet, "@media (max-width: 900px) {", "}\n\n")
	for _, rule := range []string{
		".menu-toggle { display: grid",
		".topbar-logo { display: block; }",
		".topbar-title { display: none; }",
	} {
		if !strings.Contains(mobile, rule) {
			t.Fatalf("the hamburger breakpoint is missing %q", rule)
		}
	}
	if !strings.Contains(stylesheet, ".topbar-logo { display: none; }") {
		t.Fatal("the logo is not hidden at desktop width, where the sidebar already shows one")
	}
}

// Logout must stay a CSRF-protected POST even though it now looks like an icon.
func TestSidebarLogoutIsAProtectedPost(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	sidebar := sectionBetween(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"), `<aside class="sidebar"`, "</aside>")

	if !strings.Contains(sidebar, `method="post" action="/logout"`) {
		t.Fatal("logout is no longer a POST form")
	}
	if !strings.Contains(sidebar, `name="csrf_token"`) {
		t.Fatal("logout form lost its CSRF token")
	}
	// An icon-only control still has to announce itself.
	if !strings.Contains(sidebar, `aria-label="Keluar"`) {
		t.Fatal("the logout icon has no accessible name")
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
