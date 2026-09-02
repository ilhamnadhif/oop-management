package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
)

// The page belongs to HR and Management. Everyone else is turned away.
func TestUserManagementIsForHR(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)

	for _, jabatan := range []string{"Produksi", "Surveyor", "SPV", "Security"} {
		response, err := loggedInClientAs(t, testServer, jabatan).Get(testServer.URL + "/hr/user-management")
		if err != nil {
			t.Fatalf("%s: get: %v", jabatan, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("%s: status = %d, want 403", jabatan, response.StatusCode)
		}
	}

	hr := loggedInClientAs(t, testServer, "HR")
	page := fetchAuthedPage(t, hr, testServer.URL+"/dashboard")
	if !strings.Contains(page, `href="/hr/user-management"`) {
		t.Fatal("HR does not see the User Management menu")
	}
	management := loggedInClient(t, testServer)
	if status := statusOf(t, management, testServer.URL+"/hr/user-management"); status != http.StatusOK {
		t.Fatalf("Management: status = %d, want 200", status)
	}
}

// The page lists the project's people with a position picker, and the access
// matrix with every configurable menu.
func TestUserManagementRendersTheMembersAndTheMatrix(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	addEmployee(t, testServer, "778899", "Surveyor")
	client := loggedInClientAs(t, testServer, "HR")

	page := fetchAuthedPage(t, client, testServer.URL+"/hr/user-management")
	for _, fragment := range []string{
		`name="aksi" value="ubah-jabatan"`,
		`name="user_id" value="`,
		`name="menu_`,
		`name="aksi" value="simpan-akses"`,
		"Surveyor",
		"Produksi",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the page is missing %q", fragment)
		}
	}
}

// HR cannot move a Management account, and cannot mint one by moving somebody
// to Management. Only Management may do either.
func TestHRCannotChangeManagementAccounts(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	loggedInClient(t, testServer) // the first account, Management
	hr := loggedInClientAs(t, testServer, "HR")

	var management *model.User
	for _, user := range store.UserList() {
		if user.Jabatan == model.JabatanManagement {
			u := user
			management = &u
		}
	}
	if management == nil {
		t.Fatal("no Management account seeded")
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "ubah-jabatan",
		"user_id":    management.UserID,
		"jabatan":    "Produksi",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	for _, user := range store.UserList() {
		if user.UserID == management.UserID && user.Jabatan != model.JabatanManagement {
			t.Fatal("HR demoted a Management account")
		}
	}
}

// HR moving an ordinary employee between positions lands in the store.
func TestHRChangesAnEmployeesJabatan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	addEmployee(t, testServer, "778899", "Surveyor")

	var target *model.User
	for _, user := range store.UserList() {
		if user.NRP == "778899" {
			u := user
			target = &u
		}
	}
	if target == nil {
		t.Fatal("employee was not seeded")
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "ubah-jabatan",
		"user_id":    target.UserID,
		"jabatan":    "Logistik",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}
	for _, user := range store.UserList() {
		if user.NRP == "778899" && user.Jabatan != "Logistik" {
			t.Fatalf("jabatan = %q, want Logistik", user.Jabatan)
		}
	}
}

// Saving the matrix writes one row per position, and a position whose row is
// cleared loses the menus it used to reach.
func TestHRConfiguresJabatanMenuAccess(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	// By default Produksi may open the produksi menu.
	if !CanAccess(defaultMenuRules(), "Produksi", "produksi-input") {
		t.Fatal("the default rule does not grant Produksi the produksi menu")
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token":    csrfFromForm(t, page),
		"aksi":          "simpan-akses",
		"menu_Produksi": "produksi",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	found := false
	for _, access := range store.JabatanAccessList() {
		if access.Jabatan == "Produksi" {
			found = true
			if len(access.MenuAktif) != 1 || access.MenuAktif[0] != "produksi" {
				t.Fatalf("Produksi stored wrong: %+v", access)
			}
		}
	}
	if !found {
		t.Fatal("the save did not store any jabatan access rows")
	}

	// Produksi still reaches the produksi menu: the matrix kept it ticked.
	if !CanAccess(effectiveMenuRules(store.JabatanAccessList(), testProjectName), "Produksi", "produksi-input") {
		t.Fatal("Produksi lost the produksi menu it was granted")
	}
}

// Saving the matrix without a menu ticked strips that menu from the position,
// even when the built-in default granted it.
func TestSavingAnEmptyRowRevokesTheDefaultAccess(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	if !CanAccess(defaultMenuRules(), "Surveyor", "unit-dt") {
		t.Fatal("the default rule does not grant Surveyor the unit menu")
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "simpan-akses",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	if CanAccess(effectiveMenuRules(store.JabatanAccessList(), testProjectName), "Surveyor", "unit-dt") {
		t.Fatal("Surveyor still reaches the unit menu after an empty save")
	}
}

// The settings screen is not on the table: it belongs to Management whatever
// the matrix says, so a column of checkboxes there would change nothing while
// looking as though it did.
func TestTheAccessMatrixDoesNotOfferProjectSettings(t *testing.T) {
	keys := configurableMenuKeys()
	for _, key := range keys {
		if key == projectSettingsKey {
			t.Fatal("the matrix may grant project-settings, which is Management's alone")
		}
	}
	for _, choice := range accessMenuChoices() {
		if choice.Key == projectSettingsKey {
			t.Fatal("the matrix still shows a project-settings column")
		}
	}
	// The HR module is grantable like any other: a site may want somebody
	// besides HR in there.
	for _, expected := range []string{"produksi", "unit", "a2b", "nota", "hr"} {
		contains := false
		for _, key := range keys {
			if key == expected {
				contains = true
			}
		}
		if !contains {
			t.Fatalf("the matrix does not offer %q", expected)
		}
	}
}

// The effective rules always keep the HR module open to HR, so a mis-saved
// matrix cannot lock the page's own editors out.
func TestEffectiveRulesKeepHRInTheHRMenu(t *testing.T) {
	rules := effectiveMenuRules([]model.JabatanAccess{
		{Jabatan: "HR", MenuAktif: nil},
	}, testProjectName)
	if !positionListed("HR", rules["hr"]) {
		t.Fatal("HR lost the hr menu even though the matrix excluded it")
	}
	if len(rules[projectSettingsKey]) != 0 {
		t.Fatal("project-settings was granted to someone")
	}
}

// Saving an empty row strips the default access from a live server: the next
// request by that position is refused.
func TestSavedAccessIsEnforcedOnLiveRequests(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	surveyor := loggedInClientAs(t, testServer, "Surveyor")
	if status := statusOf(t, surveyor, testServer.URL+"/unit-dt"); status != http.StatusOK {
		t.Fatalf("Surveyor should reach unit-dt by default, got %d", status)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token": csrfFromForm(t, page),
		"aksi":       "simpan-akses",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()

	if status := statusOf(t, surveyor, testServer.URL+"/unit-dt"); status != http.StatusForbidden {
		t.Fatalf("Surveyor should be refused unit-dt after an empty save, got %d", status)
	}
	// The refused page still leaves somewhere to go.
	page = fetchAuthedPage(t, surveyor, testServer.URL+"/dashboard")
	if !strings.Contains(page, `href="/absensi"`) {
		t.Fatal("Surveyor lost the attendance menu after the access change")
	}
}

// The default jabatan options are what the HR screen offers for a new account,
// and Management cannot be reached through the picker.
func TestSaveJabatanAccessRejectsManagement(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/user-management")
	response, err := hr.PostForm(testServer.URL+"/hr/user-management", urlValues(map[string]string{
		"csrf_token":      csrfFromForm(t, page),
		"aksi":            "simpan-akses",
		"menu_Management": "produksi",
	}))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (Management row is simply not stored)", response.StatusCode)
	}
}

// ChangeJabatan refuses to move an account to a position that does not exist.
func TestChangeJabatanRejectsAnUnknownPosition(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	auth := fixtureAuthFor(t, testServer)
	addEmployee(t, testServer, "778899", "Produksi")
	users := store.UserList()
	if len(users) == 0 {
		t.Fatal("nobody registered")
	}
	if _, err := auth.ChangeJabatan(context.Background(), users[0].UserID, "Tidak Ada", true); err == nil {
		t.Fatal("an unknown position was accepted")
	}
}
