package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// addJabatan drives the add-position form the way the page does.
func addJabatan(t *testing.T, client *http.Client, testServer *httptest.Server, nama string) *http.Response {
	t.Helper()
	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")
	response, err := client.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token":   csrfFromForm(t, page),
		"aksi":         "tambah-jabatan",
		"nama_jabatan": nama,
	}))
	if err != nil {
		t.Fatalf("post add jabatan: %v", err)
	}
	return response
}

// The section is on the page HR already uses to decide who may do what.
func TestUserManagementOffersAddJabatan(t *testing.T) {
	testServer := newTestServer(t)
	page := fetchAuthedPage(t, loggedInClientAs(t, testServer, "HR"), testServer.URL+"/hr/user-management")

	for _, want := range []string{"TAMBAH JABATAN", `name="nama_jabatan"`, `value="tambah-jabatan"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the page is missing %q", want)
		}
	}
	// The page says which project the position will belong to, rather than
	// leaving it to be assumed.
	if !strings.Contains(page, "hanya ada di "+testProjectName) {
		t.Fatal("the page does not say the position belongs to this project alone")
	}
}

// HR may add a position: whatever it makes is beneath its own authority, since
// a made-up position can never be Management and never leaves the project.
func TestHRCanAddAJabatan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	response := addJabatan(t, hr, testServer, "Mekanik")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	list := store.JabatanList()
	if len(list) != 1 || list[0].Nama != "Mekanik" {
		t.Fatalf("stored %+v, want one Mekanik", list)
	}
	if list[0].Project != testProjectName {
		t.Fatalf("project = %q, want the project the page was opened in", list[0].Project)
	}
	if list[0].DibuatOleh == "" {
		t.Fatal("the row does not say who added it")
	}
}

// Once added, the position is offered everywhere this project picks one, and
// it has a row of its own on the access matrix.
func TestANewJabatanAppearsInThisProjectsForms(t *testing.T) {
	testServer := newTestServer(t)
	hr := loggedInClientAs(t, testServer, "HR")
	addJabatan(t, hr, testServer, "Mekanik").Body.Close()

	for _, path := range []string{
		"/hr/user-management",
		"/hr/karyawan",
		"/hr/export",
		"/hr/performance",
	} {
		page := fetchAuthedPage(t, hr, testServer.URL+path)
		if !strings.Contains(page, "Mekanik") {
			t.Fatalf("%s does not offer the new position", path)
		}
	}
}

// The name is refused for the reasons the service refuses it, and the page
// says which rather than reporting a generic failure.
func TestAddJabatanReportsWhyANameIsRefused(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	for _, testCase := range []struct{ nama, want string }{
		{"Management", "Management tidak bisa dibuat"},
		{"spv", "sudah ada di seluruh project"},
		{strings.Repeat("a", 41), "maksimal 40 karakter"},
	} {
		response := addJabatan(t, hr, testServer, testCase.nama)
		body := readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%q: status = %d, want 422", testCase.nama, response.StatusCode)
		}
		if !strings.Contains(body, testCase.want) {
			t.Fatalf("%q: the page does not say %q", testCase.nama, testCase.want)
		}
		// What was typed comes back, so a refusal does not mean retyping it.
		if !strings.Contains(body, `value="`+testCase.nama+`"`) {
			t.Fatalf("%q: the form did not keep what was typed", testCase.nama)
		}
	}
	if got := len(store.JabatanList()); got != 0 {
		t.Fatalf("stored %d positions, want none", got)
	}
}

// The page is HR's, and so is the section on it.
func TestAddJabatanIsGuardedLikeTheRestOfTheScreen(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	surveyor := loggedInClientAs(t, testServer, "Surveyor")

	response, err := surveyor.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"aksi":         "tambah-jabatan",
		"nama_jabatan": "Mekanik",
	}))
	if err != nil {
		t.Fatalf("post as surveyor: %v", err)
	}
	response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("a position outside HR added a jabatan")
	}
	if got := len(store.JabatanList()); got != 0 {
		t.Fatalf("stored %d positions, want none", got)
	}
}

// The whole point: a position one site made stays there. Nothing but
// Management crosses projects, and a made-up position cannot be Management.
func TestANewJabatanDoesNotCrossProjects(t *testing.T) {
	testServer, _, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	addJabatan(t, client, testServer, "Mekanik").Body.Close()
	if page := fetchAuthedPage(t, client, testServer.URL+"/hr/karyawan"); !strings.Contains(page, "Mekanik") {
		t.Fatal("the position is missing from the project that made it")
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	elsewhere := fetchAuthedPage(t, client, testServer.URL+"/hr/karyawan")
	if strings.Contains(elsewhere, "Mekanik") {
		t.Fatal("one project's position is offered in another")
	}

	// The same name is free in the other project, and the two stand alone.
	response := addJabatan(t, client, testServer, "Mekanik")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d: the name was not free in the other project", response.StatusCode)
	}
}

// The access matrix belongs to the project it was saved in: the same position
// may open different menus at another site.
func TestAccessMatrixIsSavedPerProject(t *testing.T) {
	testServer, stores, _ := newTwoProjectServer(t)
	store := stores.forProject(testProjectName)
	client := loggedInClient(t, testServer)

	// Strip the unit menu from Surveyor here by saving the matrix with nothing
	// ticked for it.
	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")
	response, err := client.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "simpan-akses",
	}))
	if err != nil {
		t.Fatalf("save matrix: %v", err)
	}
	response.Body.Close()

	rows := store.JabatanAccessList()
	if len(rows) == 0 {
		t.Fatal("the save stored nothing")
	}
	for _, row := range rows {
		if row.Project != testProjectName {
			t.Fatalf("row %+v was not written against the project it was saved in", row)
		}
	}
	// The other project has no row of its own, so it keeps the built-in rule.
	if !CanAccess(effectiveMenuRules(rows, secondProjectName), "Surveyor", "unit-dt") {
		t.Fatal("a project that never saved its matrix lost the built-in access")
	}
	if CanAccess(effectiveMenuRules(rows, testProjectName), "Surveyor", "unit-dt") {
		t.Fatal("the project that saved an empty matrix kept the access")
	}
}

// The HR column is open like any other: a site may want somebody besides HR in
// the HR module. What it may not do is take it away from HR, since this is the
// screen that edits these rights.
func TestAccessMatrixOpensTheHRColumnButLocksHRsOwnCell(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")

	// The markup wraps, so the checks read a whitespace-collapsed copy.
	flat := strings.Join(strings.Fields(page), " ")
	if !strings.Contains(flat, `name="menu_SPV" value="hr" >`) {
		t.Fatal("the HR column cannot be granted to another position")
	}
	if strings.Contains(flat, `value="project-settings"`) {
		t.Fatal("the matrix still shows a project-settings column")
	}
	// HR's own cell is ticked and cannot be cleared.
	if !strings.Contains(flat, `name="menu_HR" value="hr" checked disabled>`) {
		t.Fatal("HR's own cell is not locked on")
	}
}

// Saving an empty matrix strips every position of everything, except that HR
// keeps the HR module - otherwise the save would lock the page's own editors
// out of the page that made it.
func TestAnEmptySaveCannotLockHROutOfHR(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")
	response, err := client.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "simpan-akses",
	}))
	if err != nil {
		t.Fatalf("save matrix: %v", err)
	}
	response.Body.Close()

	rules := effectiveMenuRules(store.JabatanAccessList(), testProjectName)
	if !CanAccess(rules, "HR", "hr-user-management") {
		t.Fatal("an empty save locked HR out of the HR module")
	}
	if CanAccess(rules, "SPV", "hr-user-management") {
		t.Fatal("an empty save left the HR module open to another position")
	}
}

// Granting the HR module to another position actually works: the column is not
// merely drawn.
func TestTheHRModuleCanBeGrantedToAnotherPosition(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")
	response, err := client.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "simpan-akses",
		"menu_SPV":   "hr",
	}))
	if err != nil {
		t.Fatalf("save matrix: %v", err)
	}
	response.Body.Close()

	if !CanAccess(effectiveMenuRules(store.JabatanAccessList(), testProjectName), "SPV", "hr-overview") {
		t.Fatal("SPV was granted the HR module and still cannot open it")
	}
}
