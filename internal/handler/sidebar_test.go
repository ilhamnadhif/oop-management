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

	for _, path := range []string{"/absensi", "/produksi", "/unit-dt"} {
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
		"/absensi":           "Absensi",
		"/produksi":          "Input Data",
		"/produksi/overview": "Overview",
		"/unit-dt":           "Unit DT",
		"/unit-a2b":          "Unit A2B",
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

		// The sidebar ends with support and the copyright, below every link.
		footerAt := strings.Index(page, `class="sidebar-footer"`)
		lastLinkAt := strings.LastIndex(page, `class="sidebar-link`)
		if footerAt < 0 || lastLinkAt < 0 {
			t.Fatalf("%s: sidebar is missing links or its footer", path)
		}
		if footerAt < lastLinkAt {
			t.Fatalf("%s: the footer appears before the menu links", path)
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

// The signed-in user's name, position and logout sit in the top right, beside
// the date - not in the sidebar.
func TestUserIdentityLivesInTheTopbar(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for _, path := range []string{"/absensi", "/produksi", "/unit-dt"} {
		page := fetchAuthedPage(t, client, testServer.URL+path)

		topbar := sectionBetween(t, page, `<header class="topbar">`, "</header>")
		if !strings.Contains(topbar, "Budi Santoso") {
			t.Fatalf("%s: topbar is missing the user name", path)
		}
		if !strings.Contains(topbar, `class="account-role"`) {
			t.Fatalf("%s: topbar is missing the user position", path)
		}
		// The avatar shows the first letter of the name.
		if !strings.Contains(topbar, `class="account-avatar" aria-hidden="true">B<`) {
			t.Fatalf("%s: avatar initial is wrong or missing", path)
		}
		if !strings.Contains(topbar, `action="/logout"`) {
			t.Fatalf("%s: logout is not in the account menu", path)
		}

		sidebar := sectionBetween(t, page, `<aside class="sidebar"`, "</aside>")
		if strings.Contains(sidebar, "Budi Santoso") {
			t.Fatalf("%s: the user name is still rendered in the sidebar", path)
		}
		if strings.Contains(sidebar, `action="/logout"`) {
			t.Fatalf("%s: logout is still rendered in the sidebar", path)
		}
	}
}

// The sidebar ends with who to ask for help and who owns the app.
func TestSidebarFooterOffersSupportAndCopyright(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	sidebar := sectionBetween(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"),
		`<aside class="sidebar"`, "</aside>")

	// The number is dialled from a phone, so the link opens the chat directly.
	if !strings.Contains(sidebar, `href="https://wa.me/6285393464812"`) {
		t.Fatal("the support link does not open WhatsApp")
	}
	// A link leaving the app must not hand the new tab a window opener.
	if !strings.Contains(sidebar, `rel="noopener noreferrer"`) {
		t.Fatal("the support link opens a tab that can reach back into the app")
	}
	// The template writes the entity, which is what the browser renders as ©.
	if !strings.Contains(sidebar, "&copy; 2026 PT Orecon Putra Perkasa") {
		t.Fatalf("the copyright line is missing or wrong: %s", sidebar)
	}
}

// The breadcrumb sits under the page title, inside the content area rather than
// the topbar, and every page gets exactly one of each.
func TestBreadcrumbSitsBelowThePageTitle(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	titles := map[string]string{
		"/absensi":           "Absensi",
		"/produksi":          "Input Data",
		"/produksi/overview": "Overview",
		"/produksi/export":   "Export Data",
		"/unit-dt":           "Unit DT",
		"/unit-a2b":          "Unit A2B",
		"/unit/export":       "Export Data",
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

// The account panel hangs below the topbar, so the topbar has to sit above the
// page content and the panel has to close when attention moves elsewhere.
func TestAccountMenuLayersAndCloses(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	stylesheet := fetchAuthedPage(t, client, testServer.URL+"/static/css/style.css")
	if !strings.Contains(stylesheet, ".topbar { position: relative; z-index: 900;") {
		t.Fatal("the topbar does not sit above the page, so the panel is drawn under it")
	}
	if !strings.Contains(stylesheet, "max-width: calc(100vw - 1.5rem)") {
		t.Fatal("the panel can grow past the edge of a narrow screen")
	}

	script := fetchAuthedPage(t, client, testServer.URL+"/static/js/shell.js")
	if !strings.Contains(script, "details.account-menu") {
		t.Fatal("nothing closes the account menu")
	}
	// A menu that only closes by clicking its own summary stays over the page
	// while you work elsewhere.
	for _, behaviour := range []string{`addEventListener("click"`, `event.key !== "Escape"`} {
		if !strings.Contains(script, behaviour) {
			t.Fatalf("the account menu is missing %q", behaviour)
		}
	}
}

// Logout must stay a CSRF-protected POST now that it lives inside a menu.
func TestLogoutIsAProtectedPost(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	topbar := sectionBetween(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"), `<header class="topbar">`, "</header>")

	if !strings.Contains(topbar, `method="post" action="/logout"`) {
		t.Fatal("logout is no longer a POST form")
	}
	if !strings.Contains(topbar, `name="csrf_token"`) {
		t.Fatal("logout form lost its CSRF token")
	}
	if !strings.Contains(topbar, ">Keluar<") {
		t.Fatal("the logout control has no label")
	}
}

// Pages under a group are nested beneath its heading, and the heading itself is
// not a link: it has no page of its own.
func TestSidebarGroupsPagesUnderHeadings(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	for _, group := range []string{"Produksi", "Unit"} {
		heading := `class="sidebar-group-label"`
		if !strings.Contains(nav, heading) {
			t.Fatalf("no group headings rendered")
		}
		if !strings.Contains(nav, ">"+group+"<") {
			t.Fatalf("group %q is missing", group)
		}
		// A heading must not be a link, or clicking it leads nowhere.
		if strings.Contains(nav, `href="/produksi"><`) {
			t.Fatal("a group heading was rendered as a link")
		}
	}

	// Every page sits inside a sublist.
	for _, page := range []string{"Overview", "Input Data", "Export Data", "Unit DT", "Unit A2B"} {
		at := strings.Index(nav, ">"+page+"<")
		if at < 0 {
			t.Fatalf("page %q is missing from the menu", page)
		}
		if strings.LastIndex(nav[:at], `class="sidebar-sublist"`) < 0 {
			t.Fatalf("page %q is not nested under a group", page)
		}
	}

	// Absensi stays top level and first.
	absensiAt := strings.Index(nav, ">Absensi<")
	firstGroupAt := strings.Index(nav, `class="sidebar-group`)
	if absensiAt < 0 || firstGroupAt < 0 || absensiAt > firstGroupAt {
		t.Fatal("Absensi is not the first, ungrouped entry")
	}
}

// Groups expand and collapse. The one holding the open page arrives expanded
// from the server, so the menu never hides where you are - and it works before
// any script runs.
func TestSidebarGroupsAreCollapsible(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/produksi/overview"))

	if !strings.Contains(nav, "<details class=\"sidebar-group") {
		t.Fatal("groups are not collapsible")
	}
	if !strings.Contains(nav, "<summary class=\"sidebar-group-label\"") {
		t.Fatal("group headings are not summaries, so they cannot be toggled")
	}

	produksi := sectionBetween(t, nav, `data-group="produksi"`, "</details>")
	if !strings.Contains(produksi, "open") {
		t.Fatal("the group holding the open page is not expanded")
	}

	// Every other group arrives collapsed: that is the default, and the reason
	// for collapsing at all.
	unit := sectionBetween(t, nav, `data-group="unit"`, "</summary>")
	if strings.Contains(unit, " open") {
		t.Fatal("an unrelated group arrived expanded")
	}

	// On a page that belongs to no group, nothing is expanded.
	dashboard := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/absensi"))
	for _, group := range []string{"produksi", "unit"} {
		opening := sectionBetween(t, dashboard, `data-group="`+group+`"`, "</summary>")
		if strings.Contains(opening, " open") {
			t.Fatalf("group %q is expanded on a page outside it", group)
		}
	}

	// Nothing is remembered between page loads, so a fresh sign-in always finds
	// the menu tidy rather than however it was left days ago.
	shell := fetchPage(t, testServer.URL+"/static/js/shell.js")
	for _, stale := range []string{`"opp.sidebar.expanded"`, `"opp.sidebar.collapsed"`} {
		if strings.Contains(shell, stale) {
			t.Fatalf("shell.js still stores %s, which survives a sign-out", stale)
		}
	}
}

// Opening a group closes the others: a stack of open groups pushes the lower
// ones off a phone screen, and the sidebar is for finding the next page rather
// than for keeping unfolded.
func TestSidebarGroupsOpenOneAtATime(t *testing.T) {
	testServer := newTestServer(t)
	shell := fetchPage(t, testServer.URL+"/static/js/shell.js")

	for _, behaviour := range []string{
		// The click is intercepted, or the browser toggles before anything can
		// animate.
		"event.preventDefault()",
		"groups.forEach((other) => other !== group && setOpen(other, false))",
		// A closed <details> renders nothing, so the collapse has to finish
		// before the element is closed.
		"group.open = true;",
		`animation.addEventListener("finish"`,
		// Motion is a preference, not a given.
		`matchMedia("(prefers-reduced-motion: reduce)")`,
	} {
		if !strings.Contains(shell, behaviour) {
			t.Fatalf("shell.js is missing %q", behaviour)
		}
	}
}

// The group holding the open page is marked, so the sidebar shows which section
// you are in even when the page name alone is ambiguous - both groups end in
// "Export Data".
func TestSidebarMarksTheActiveGroup(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for path, section := range map[string]string{
		"/produksi/overview": "Produksi",
		"/unit-a2b":          "Unit",
	} {
		nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+path))
		activeAt := strings.Index(nav, `class="sidebar-group active"`)
		if activeAt < 0 {
			t.Fatalf("%s: no group is marked active", path)
		}
		labelAt := strings.Index(nav[activeAt:], ">"+section+"<")
		if labelAt < 0 {
			t.Fatalf("%s: the active group is not %q", path, section)
		}
		if strings.Count(nav, `class="sidebar-group active"`) != 1 {
			t.Fatalf("%s: more than one group marked active", path)
		}
	}
}

// The breadcrumb names the section, since two pages share the label
// "Export Data".
func TestBreadcrumbNamesTheSection(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for path, section := range map[string]string{
		"/produksi/export": "Produksi",
		"/unit/export":     "Unit",
	} {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		crumb := sectionBetween(t, page, `<nav class="breadcrumb"`, "</nav>")
		if !strings.Contains(crumb, "<li>"+section+"</li>") {
			t.Fatalf("%s: breadcrumb does not name the section %q", path, section)
		}
		sectionAt := strings.Index(crumb, "<li>"+section+"</li>")
		pageAt := strings.Index(crumb, `aria-current="page"`)
		if sectionAt > pageAt {
			t.Fatalf("%s: the section comes after the page name", path)
		}
	}

	// An ungrouped page has no section segment.
	crumb := sectionBetween(t, fetchAuthedPage(t, client, testServer.URL+"/absensi"), `<nav class="breadcrumb"`, "</nav>")
	if strings.Count(crumb, "<li") != 2 {
		t.Fatalf("dashboard breadcrumb has %d segments, want 2", strings.Count(crumb, "<li"))
	}
}

// Both export pages are real pages behind the session guard, not 404s.
func TestExportPagesExistAndAreGuarded(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-08-01", "B 1234 ABC", "DT KECIL", 10.5, 10)
	client := loggedInClient(t, testServer)

	produksi := fetchAuthedPage(t, client, testServer.URL+"/produksi/export")
	if !strings.Contains(produksi, "format=xlsx") || !strings.Contains(produksi, "format=pdf") {
		t.Fatal("the produksi export page offers no downloads")
	}
	// The registers are empty here, so the page names them without offering
	// buttons; the downloads themselves are covered in unit_export_test.go.
	unit := fetchAuthedPage(t, client, testServer.URL+"/unit/export")
	if !strings.Contains(unit, "UNIT DT") || !strings.Contains(unit, "UNIT A2B") {
		t.Fatal("the unit export page does not list both registers")
	}

	anonymous := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/produksi/export", "/unit/export"} {
		response, err := anonymous.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("%s: anonymous request went to %q, want /login", path, location)
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
