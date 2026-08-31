package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/service"
)

const secondProjectName = "KENDAL"

// newTwoProjectServer is the arrangement this whole feature exists for: the
// project that was already here, and a new one with an empty spreadsheet of its
// own and nobody in it.
func newTwoProjectServer(t *testing.T) (*httptest.Server, *testStores, *service.ProjectService) {
	t.Helper()
	stores := newTestStores(repository.NewTestRepository())
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }

	deps := testDepsWithStores(t, stores, location, nowFunc, defaultTestBranding())
	if _, err := deps.Projects.Create(context.Background(), secondProjectName, "kendal-spreadsheet", nil, nil); err != nil {
		t.Fatalf("create second project: %v", err)
	}
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)
	return testServer, stores, deps.Projects
}

// switchProject drives the switcher the way the dropdown does.
func switchProject(t *testing.T, client *http.Client, testServer *httptest.Server, csrf, project string) {
	t.Helper()
	response, err := client.PostForm(testServer.URL+"/project/switch", urlValues(map[string]string{
		"csrf_token": csrf,
		"project":    project,
	}))
	if err != nil {
		t.Fatalf("switch project: %v", err)
	}
	response.Body.Close()
}

// A row is filed into the spreadsheet of the project the session is working in,
// and into no other. This is the whole point of a store per project: the rows of
// one are not in the other's file to be leaked.
func TestProduksiIsFiledIntoTheActiveProjectOnly(t *testing.T) {
	testServer, stores, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	seedUnitIn(t, stores.forProject(testProjectName))
	seedUnitIn(t, stores.forProject(secondProjectName))

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")
	csrf := csrfFromForm(t, page)
	switchProject(t, client, testServer, csrf, secondProjectName)

	page = fetchAuthedPage(t, client, testServer.URL+"/produksi")
	response, err := client.PostForm(testServer.URL+"/produksi", validProduksiForm(csrfFromForm(t, page)))
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	response.Body.Close()

	if rows := stores.forProject(testProjectName).ProduksiList(); len(rows) != 0 {
		t.Fatalf("%s received %d rows filed while working in %s", testProjectName, len(rows), secondProjectName)
	}
	rows := stores.forProject(secondProjectName).ProduksiList()
	if len(rows) != 1 {
		t.Fatalf("%s stored %d rows, want 1", secondProjectName, len(rows))
	}
	// The project is stamped from the session, not typed: the form has no field
	// for it any more.
	if rows[0].Project != secondProjectName {
		t.Fatalf("Project = %q, want %q", rows[0].Project, secondProjectName)
	}
}

// The switcher is only worth drawing when there is somewhere to switch to.
func TestSwitcherAppearsOnlyWithSomewhereToGo(t *testing.T) {
	twoProjects, _, _ := newTwoProjectServer(t)
	page := fetchAuthedPage(t, loggedInClient(t, twoProjects), twoProjects.URL+"/dashboard")
	if !strings.Contains(page, `id="projectSwitch"`) {
		t.Fatal("two projects but no switcher")
	}
	for _, name := range []string{testProjectName, secondProjectName} {
		if !strings.Contains(page, ">"+name+"</option>") {
			t.Fatalf("switcher is missing %s", name)
		}
	}

	oneProject, _ := newTestServerWithStore(t)
	page = fetchAuthedPage(t, loggedInClient(t, oneProject), oneProject.URL+"/dashboard")
	if strings.Contains(page, `id="projectSwitch"`) {
		t.Fatal("one project still drew a switcher")
	}
	if !strings.Contains(page, `class="project-badge"`) {
		t.Fatal("the project is not named anywhere on the page")
	}
}

// Somebody belonging to one project cannot reach another by asking for it. The
// dropdown is a convenience; the check is here.
func TestSwitchingToAnUnreachableProjectIsRefused(t *testing.T) {
	testServer, stores, projects := newTwoProjectServer(t)
	client := loggedInClientAs(t, testServer, "Produksi")

	// Pin the account to the first project, the way the settings screen does.
	users := stores.master.UserList()
	if len(users) == 0 {
		t.Fatal("nobody registered")
	}
	if err := projects.Assign(context.Background(), users[0].UserID, testProjectName); err != nil {
		t.Fatalf("assign: %v", err)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if strings.Contains(page, `id="projectSwitch"`) {
		t.Fatal("an account tied to one project was offered a switcher")
	}
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	page = fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(page, ">"+testProjectName+"<") {
		t.Fatalf("the session moved to a project the account cannot reach:\n%s", firstLines(page))
	}
}

// A menu switched off has no rows behind it, so it is gone for everybody -
// Management included, which is the case worth pinning down.
func TestAMenuSwitchedOffIsGoneForManagementToo(t *testing.T) {
	testServer, _, projects := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	kendal, err := projects.Find(context.Background(), secondProjectName)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if _, err := projects.Update(context.Background(), kendal.ProjectID, service.ProjectUpdate{
		Nama:      secondProjectName,
		MenuAktif: []string{"produksi"},
		Status:    model.StatusAktif,
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	page = fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(page, `href="/produksi"`) {
		t.Fatal("the one menu this project runs is missing from the sidebar")
	}
	if strings.Contains(page, `href="/unit-dt"`) {
		t.Fatal("a menu this project does not run is still in the sidebar")
	}

	// And the page itself is refused, not merely hidden.
	response, err := client.Get(testServer.URL + "/unit-dt")
	if err != nil {
		t.Fatalf("get unit-dt: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}

	// Switching back brings it all returns: the rule is about the project, not
	// about the person.
	switchProject(t, client, testServer, csrf(t, client, testServer), testProjectName)
	page = fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(page, `href="/unit-dt"`) {
		t.Fatalf("the menu did not come back in %s", testProjectName)
	}
}

// The settings screen is never switchable off: it is where the switches are.
func TestProjectSettingsSurvivesAProjectWithOneMenu(t *testing.T) {
	testServer, _, projects := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	kendal, _ := projects.Find(context.Background(), secondProjectName)
	if _, err := projects.Update(context.Background(), kendal.ProjectID, service.ProjectUpdate{
		Nama: secondProjectName, MenuAktif: []string{"produksi"}, Status: model.StatusAktif,
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}
	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	response, err := client.Get(testServer.URL + "/project/settings")
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200: the settings screen switched itself off", response.StatusCode)
	}
}

// Only Management configures projects.
func TestProjectSettingsIsManagementOnly(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "SPV")

	response, err := client.Get(testServer.URL + "/project/settings")
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
}

// Adding a project from the settings screen is what the whole page is for.
func TestProjectSettingsAddsAProject(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(map[string]string{
		"csrf_token":     csrfFromForm(t, page),
		"aksi":           "tambah",
		"nama_baru":      secondProjectName,
		"spreadsheet_id": "kendal-spreadsheet",
	}))
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	projects := store.ProjectList()
	if len(projects) != 2 {
		t.Fatalf("stored %d projects, want 2", len(projects))
	}
	if projects[1].Nama != secondProjectName || projects[1].SpreadsheetID != "kendal-spreadsheet" {
		t.Fatalf("second project stored wrong: %+v", projects[1])
	}
	// No menus listed means every menu, which is what a project starts with.
	if len(projects[1].MenuAktif) != 0 {
		t.Fatalf("MenuAktif = %v, want empty so every menu runs", projects[1].MenuAktif)
	}
}

// Two projects sharing one spreadsheet would each write into the other's books.
func TestProjectSettingsRefusesADuplicateSpreadsheet(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(map[string]string{
		"csrf_token":     csrfFromForm(t, page),
		"aksi":           "tambah",
		"nama_baru":      secondProjectName,
		"spreadsheet_id": "test-spreadsheet",
	}))
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if projects := store.ProjectList(); len(projects) != 1 {
		t.Fatalf("stored %d projects, want the duplicate refused", len(projects))
	}
}

// csrf reads a token off any page that carries a form.
func csrf(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	return csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"))
}

// seedUnitIn registers one truck in a given project's store, so a production
// row filed there has dimensions to take.
func seedUnitIn(t *testing.T, store *repository.TestRepository) {
	t.Helper()
	unit := &model.UnitDT{
		UnitID: "UNT-2026-0001", Nopol: "B 1234 ABC",
		Panjang: 375, Lebar: 190, Tinggi: 150,
		Driver: "Slamet", Keterangan: "DT KECIL",
	}
	if err := store.CreateUnitDT(context.Background(), unit); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
}

func firstLines(page string) string {
	lines := strings.SplitN(page, "\n", 40)
	if len(lines) > 40 {
		lines = lines[:40]
	}
	return strings.Join(lines, "\n")
}

// Opening the page shows what exists and the one button that adds to it. The
// settings forms belong to one project and stay on that project's own page.
func TestProjectSettingsOpensOnTheListAlone(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	for _, fragment := range []string{
		`class="project-list"`,
		`data-open-dialog="newProjectDialog"`,
		`href="/project/settings?project=` + testProjectName + `"`,
		`href="/project/settings?project=` + secondProjectName + `"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the list view is missing %q", fragment)
		}
	}
	// None of the per-project forms may be on the landing page.
	for _, fragment := range []string{`name="work_start"`, `name="signatory_name"`, `name="menu"`} {
		if strings.Contains(page, fragment) {
			t.Fatalf("the list view is still carrying %q", fragment)
		}
	}
	// And the add dialog stays shut until asked for.
	if strings.Contains(page, `id="newProjectDialog" aria-labelledby="newProjectTitle" open`) {
		t.Fatal("the add dialog opened by itself")
	}
}

// Picking a project opens its own page, with the way back out on it.
func TestProjectSettingsDetailCarriesTheFormsAndAWayBack(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+secondProjectName)
	for _, fragment := range []string{
		`class="back-link" href="/project/settings"`,
		`name="work_start"`, `name="a2b_work_minutes"`, `name="signatory_name"`,
		`name="menu"`, `name="status"`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the detail view is missing %q", fragment)
		}
	}
	if !strings.Contains(page, secondProjectName) {
		t.Fatalf("the detail view does not name %s", secondProjectName)
	}
	// The spreadsheet is shown but never offered for editing: repointing a
	// project at another file would orphan everything already written.
	if strings.Contains(page, `name="spreadsheet_id"`) {
		t.Fatal("the detail view offered the spreadsheet id for editing")
	}
	if !strings.Contains(page, "kendal-spreadsheet") {
		t.Fatal("the detail view does not show which spreadsheet it writes to")
	}
}

// A name that matches nothing falls back to the list rather than an empty form.
func TestProjectSettingsFallsBackToTheListForAnUnknownProject(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project=TIDAKADA")
	if !strings.Contains(page, `class="project-list"`) {
		t.Fatal("an unknown project did not fall back to the list")
	}
	if !strings.Contains(page, "tidak ditemukan") {
		t.Fatal("an unknown project was not reported")
	}
}

// Adding lands back on the list with the new project on it, rather than
// dragging somebody into a form they did not ask for.
func TestAddingAProjectReturnsToTheList(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(map[string]string{
		"csrf_token":     csrfFromForm(t, page),
		"aksi":           "tambah",
		"nama_baru":      secondProjectName,
		"spreadsheet_id": "kendal-spreadsheet",
	}))
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	defer response.Body.Close()
	body := readBody(t, response)

	if !strings.Contains(body, `class="project-list"`) {
		t.Fatal("adding a project did not return to the list")
	}
	if !strings.Contains(body, `href="/project/settings?project=`+secondProjectName+`"`) {
		t.Fatalf("the new project is not on the list:\n%s", firstLines(body))
	}
	if strings.Contains(body, `name="work_start"`) {
		t.Fatal("adding a project dropped straight into its settings form")
	}
	if got := len(store.ProjectList()); got != 2 {
		t.Fatalf("stored %d projects, want 2", got)
	}
}

// A refused submission reopens the dialog, so the message and the form the
// person was filling in stay together.
func TestARefusedAddReopensTheDialog(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(map[string]string{
		"csrf_token":     csrfFromForm(t, page),
		"aksi":           "tambah",
		"nama_baru":      secondProjectName,
		"spreadsheet_id": "test-spreadsheet", // already the first project's
	}))
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	defer response.Body.Close()
	body := readBody(t, response)

	if !strings.Contains(body, `id="newProjectDialog"`) || !strings.Contains(body, ` open>`) {
		t.Fatalf("the dialog did not reopen after a refusal:\n%s", firstLines(body))
	}
	if !strings.Contains(body, "sudah dipakai project") {
		t.Fatal("the refusal did not say why")
	}
	// And it says why inside the dialog. On the page behind it the message
	// would be covered by the dialog's own backdrop.
	dialogAt := strings.Index(body, `id="newProjectDialog"`)
	messageAt := strings.Index(body, "sudah dipakai project")
	if messageAt < dialogAt {
		t.Fatal("the refusal was reported on the page, where the dialog covers it")
	}
	// What was typed comes back, so a refusal costs one correction rather than
	// filling the form in again.
	if !strings.Contains(body, `name="nama_baru" type="text" value="`+secondProjectName+`"`) {
		t.Fatal("the name that was typed was not handed back")
	}
	if !strings.Contains(body, `name="spreadsheet_id" type="text" value="test-spreadsheet"`) {
		t.Fatal("the spreadsheet id that was typed was not handed back")
	}
}

// A note about one field lives in a mark beside its label, not in a line of
// grey under the input. The text has to survive that move, or the explanation
// is simply gone.
func TestFieldNotesLiveInTheHelpMark(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings?project="+secondProjectName)
	for _, fragment := range []string{
		`class="field-help"`,
		`class="field-help-mark"`,
		`class="field-help-bubble" role="tooltip"`,
		// The note that used to sit under the status picker.
		"Project tidak aktif hilang dari switcher dan tidak bisa dibuka siapa pun.",
		// And the one under the spreadsheet, which is the reason it cannot be edited.
		"Memindahkan project ke file lain akan meninggalkan seluruh baris",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the detail view is missing %q", fragment)
		}
	}
	// The note must not also still be printed under the field, or it is said
	// twice and the mark has bought nothing.
	if strings.Contains(page, `<p class="hint">Project tidak aktif`) {
		t.Fatal("the status note is still printed under the field as well")
	}
	// The mark is reachable and announced: it is a button carrying the text.
	if !strings.Contains(page, `aria-label="Penjelasan: Project tidak aktif`) {
		t.Fatal("the help mark does not announce what it explains")
	}

	// The script that makes a click open it has to actually be served, or the
	// bubble only works on hover.
	asset, err := client.Get(testServer.URL + "/static/js/field-help.js")
	if err != nil {
		t.Fatalf("get script: %v", err)
	}
	defer asset.Body.Close()
	if asset.StatusCode != http.StatusOK {
		t.Fatalf("field-help.js is not served: %d", asset.StatusCode)
	}
}

// Adding a project waits on Google writing a dozen sheets. A button that just
// sits there through that reads as a page that ignored the click, so the
// loading state has to be wired up: the markup the script looks for, and the
// script itself.
func TestAddProjectButtonShowsItIsWorking(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	for _, fragment := range []string{
		`data-loading-form`,
		`data-submit-button data-loading-label="Menyiapkan spreadsheet…"`,
		`<span class="spinner" aria-hidden="true" hidden></span>`,
		`<span data-submit-label>Tambah project</span>`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the add button is missing %q", fragment)
		}
	}
	// The markup means nothing without the script that reads it.
	asset, err := client.Get(testServer.URL + "/static/js/auth.js")
	if err != nil {
		t.Fatalf("get script: %v", err)
	}
	defer asset.Body.Close()
	if asset.StatusCode != http.StatusOK {
		t.Fatalf("auth.js is not served: %d", asset.StatusCode)
	}
	if !strings.Contains(page, "/static/js/auth.js") {
		t.Fatal("the page does not load the script that drives the loading state")
	}
}

// A spreadsheet that cannot be prepared is reported on the screen that named
// it, and leaves no project behind to be puzzled over later.
func TestAddProjectReportsASpreadsheetItCannotPrepare(t *testing.T) {
	stores := newTestStores(repository.NewTestRepository())
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }

	deps := testDepsWithStores(t, stores, location, nowFunc, defaultTestBranding())
	deps.Provision = func(context.Context, string) error {
		return errors.New("spreadsheet itu belum dibagikan ke service account aplikasi")
	}
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/project/settings")
	response, err := client.PostForm(testServer.URL+"/project/settings", urlValues(map[string]string{
		"csrf_token":     csrfFromForm(t, page),
		"aksi":           "tambah",
		"nama_baru":      secondProjectName,
		"spreadsheet_id": "kendal-spreadsheet",
	}))
	if err != nil {
		t.Fatalf("post settings: %v", err)
	}
	defer response.Body.Close()
	body := readBody(t, response)

	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	if !strings.Contains(body, "belum dibagikan ke service account") {
		t.Fatalf("the reason did not reach the page:\n%s", firstLines(body))
	}
	if got := len(stores.master.ProjectList()); got != 1 {
		t.Fatalf("stored %d projects, want the failed one left out", got)
	}
}

// The app sends script-src 'self' with no 'unsafe-inline'. An inline handler is
// not blocked loudly under that policy - it simply never runs, so a control
// wired with one looks fine and does nothing. The switcher shipped that way
// once; this is the guard that it cannot happen again anywhere.
func TestTemplatesCarryNoInlineEventHandlers(t *testing.T) {
	entries, err := assetFiles.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no templates found")
	}
	for _, entry := range entries {
		body, err := assetFiles.ReadFile("templates/" + entry.Name())
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		for _, handler := range []string{"onclick=", "onchange=", "onsubmit=", "oninput=", "onload=", "onerror="} {
			if strings.Contains(string(body), handler) {
				t.Errorf("%s uses %s, which the Content-Security-Policy silently drops; move it to a script under /static/js", entry.Name(), handler)
			}
		}
	}
}

// The switcher is a select with no submit of its own, so the script that
// submits it on change has to be wired and served, and a button has to be there
// for the case where it is not.
func TestProjectSwitcherIsWiredToItsScript(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	for _, fragment := range []string{
		`<select id="projectSwitch" name="project" data-auto-submit>`,
		`data-auto-submit-fallback`,
		"/static/js/auto-submit.js",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the switcher is missing %q", fragment)
		}
	}
	asset, err := client.Get(testServer.URL + "/static/js/auto-submit.js")
	if err != nil {
		t.Fatalf("get script: %v", err)
	}
	defer asset.Body.Close()
	if asset.StatusCode != http.StatusOK {
		t.Fatalf("auto-submit.js is not served: %d", asset.StatusCode)
	}
}

// Switching projects has to outlive the request that did it: the choice lives
// in the session, so every page after it reads the same project until it is
// switched again.
func TestTheChosenProjectSurvivesReloads(t *testing.T) {
	testServer, stores, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if !strings.Contains(page, `>`+testProjectName+`</option>`) {
		t.Fatal("the switcher does not offer the first project")
	}
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	// Three loads of three different pages, all still in the chosen project.
	for _, path := range []string{"/dashboard", "/produksi", "/dashboard"} {
		page = fetchAuthedPage(t, client, testServer.URL+path)
		if !strings.Contains(page, `value="`+secondProjectName+`" selected`) {
			t.Fatalf("%s came back in another project after switching:\n%s", path, firstLines(page))
		}
	}

	// And the data behind those pages comes from that project's own store.
	seedUnitIn(t, stores.forProject(secondProjectName))
	page = fetchAuthedPage(t, client, testServer.URL+"/produksi")
	if !strings.Contains(page, "B 1234 ABC") {
		t.Fatal("the page is not reading the chosen project's register")
	}
	if rows := stores.forProject(testProjectName).UnitDTList(); len(rows) != 0 {
		t.Fatal("the fixture leaked into the other project's store")
	}
}
