package handler

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/service"
)

// validKaryawanForm is one complete employee, minus the password nobody types.
func validKaryawanForm(csrf string) map[string]string {
	return map[string]string{
		"csrf_token":      csrf,
		"tanggal_gabung":  "2026-08-07",
		"nama_lengkap":    "Siti Rahayu",
		"nrp":             "778899",
		"jabatan":         "Produksi",
		"email":           "siti@example.com",
		"status_pengguna": model.StatusAktif,
	}
}

// signInWith drives the real sign-in form, because what the session ends up
// holding depends on the password that was typed.
func signInWith(t *testing.T, testServer *httptest.Server, nrp, password string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.PostForm(testServer.URL+"/login", urlValues(map[string]string{
		"identifier": nrp,
		"password":   password,
	}))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	response.Body.Close()
	return client
}

// The account HR creates lands in the project HR is working in, on the shared
// starting password.
func TestHRAddsAnEmployeeToTheActiveProject(t *testing.T) {
	testServer, stores, _ := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)

	page = fetchAuthedPage(t, client, testServer.URL+"/hr/karyawan")
	response, err := client.PostForm(testServer.URL+"/hr/karyawan", urlValues(validKaryawanForm(csrfFromForm(t, page))))
	if err != nil {
		t.Fatalf("post karyawan: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.StatusCode)
	}

	// Accounts live in the master store whatever project they belong to.
	var created *model.User
	for _, user := range stores.master.UserList() {
		if user.NRP == "778899" {
			stored := user
			created = &stored
		}
	}
	if created == nil {
		t.Fatal("the employee was not stored")
	}
	if created.Project != secondProjectName {
		t.Fatalf("Project = %q, want %q", created.Project, secondProjectName)
	}
	if created.NamaLengkap != "Siti Rahayu" || created.Jabatan != "Produksi" {
		t.Fatalf("stored wrong: %+v", created)
	}
	// The form has no password field; the account starts on the shared one.
	if !signedInOK(t, testServer, "778899", service.DefaultPassword) {
		t.Fatal("the new account does not accept the default password")
	}
}

// HR minting a Management account would hand itself every project. The form
// does not offer it and the server does not accept it.
func TestHRCannotCreateAManagementAccount(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/karyawan")
	if strings.Contains(page, `<option value="Management"`) {
		t.Fatal("the form offered Management to HR")
	}

	form := validKaryawanForm(csrfFromForm(t, page))
	form["jabatan"] = "Management"
	response, err := hr.PostForm(testServer.URL+"/hr/karyawan", urlValues(form))
	if err != nil {
		t.Fatalf("post karyawan: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", response.StatusCode)
	}
	for _, user := range store.UserList() {
		if user.NRP == "778899" {
			t.Fatal("HR created a Management account anyway")
		}
	}
}

// Management may create one, because it already reaches everything.
func TestManagementCanCreateAManagementAccount(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/hr/karyawan")
	if !strings.Contains(page, `<option value="Management"`) {
		t.Fatal("the form did not offer Management to Management")
	}
	form := validKaryawanForm(csrfFromForm(t, page))
	form["jabatan"] = "Management"
	response, err := client.PostForm(testServer.URL+"/hr/karyawan", urlValues(form))
	if err != nil {
		t.Fatalf("post karyawan: %v", err)
	}
	defer response.Body.Close()

	found := false
	for _, user := range store.UserList() {
		if user.NRP == "778899" && user.Jabatan == model.JabatanManagement {
			found = true
		}
	}
	if !found {
		t.Fatal("Management could not create a Management account")
	}
}

// The page belongs to HR. Nobody else opens it.
func TestInputKaryawanIsForHR(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)

	produksi := loggedInClientAs(t, testServer, "Produksi")
	response, err := produksi.Get(testServer.URL + "/hr/karyawan")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 for Produksi", response.StatusCode)
	}

	hr := loggedInClientAs(t, testServer, "HR")
	page := fetchAuthedPage(t, hr, testServer.URL+"/dashboard")
	if !strings.Contains(page, `href="/hr/karyawan"`) {
		t.Fatal("HR does not see the menu")
	}
}

// signedInOK reports whether these credentials open a session.
func signedInOK(t *testing.T, testServer *httptest.Server, nrp, password string) bool {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.PostForm(testServer.URL+"/login", urlValues(map[string]string{
		"identifier": nrp,
		"password":   password,
	}))
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer response.Body.Close()
	return response.StatusCode == http.StatusSeeOther || response.StatusCode == http.StatusFound
}

// addEmployee creates one account straight through the service, for the tests
// that are about what happens after it exists rather than about the form.
func addEmployee(t *testing.T, testServer *httptest.Server, nrp, jabatan string) {
	t.Helper()
	if _, err := fixtureAuthFor(t, testServer).AddEmployee(context.Background(), service.RegisterInput{
		TanggalGabung: "2026-08-07",
		NamaLengkap:   "Karyawan Baru",
		NRP:           nrp,
		Jabatan:       jabatan,
		Email:         nrp + "@example.com",
		Status:        model.StatusAktif,
		Project:       testProjectName,
	}, false); err != nil {
		t.Fatalf("add employee: %v", err)
	}
}

// An account still on the shared password can reach nothing but the page that
// replaces it. Hiding the app behind a dialog would leave it open to anything
// that is not a browser.
func TestDefaultPasswordSignInIsHeldAtOnboarding(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	loggedInClient(t, testServer) // the first account, so registration closes
	addEmployee(t, testServer, "778899", "Produksi")

	client := signInWith(t, testServer, "778899", service.DefaultPassword)

	// Every page it asks for lands on the same one.
	for _, path := range []string{"/dashboard", "/absensi", "/produksi", "/hr/karyawan"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body := readBody(t, response)
		response.Body.Close()
		if !strings.Contains(body, "Buat password baru") {
			t.Fatalf("%s was served to an account still on the default password", path)
		}
	}

	// And a POST is turned away too, not just the pages that draw a screen.
	response, err := client.PostForm(testServer.URL+"/produksi", urlValues(map[string]string{"tanggal": "2026-08-07"}))
	if err != nil {
		t.Fatalf("post produksi: %v", err)
	}
	defer response.Body.Close()
	if !strings.Contains(readBody(t, response), "Buat password baru") {
		t.Fatal("a form post went through for an account still on the default password")
	}
}

// Once the password is theirs, the block lifts for good and the shared one stops
// working.
func TestSettingAPasswordLiftsTheBlockForGood(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	loggedInClient(t, testServer)
	addEmployee(t, testServer, "778899", "Produksi")

	client := signInWith(t, testServer, "778899", service.DefaultPassword)
	page := fetchAuthedPage(t, client, testServer.URL+"/onboarding")
	response, err := client.PostForm(testServer.URL+"/onboarding", urlValues(map[string]string{
		"csrf_token":          csrfFromForm(t, page),
		"password":            "rahasiasaya",
		"konfirmasi_password": "rahasiasaya",
	}))
	if err != nil {
		t.Fatalf("post onboarding: %v", err)
	}
	response.Body.Close()

	// The session carries on rather than making them sign in again.
	dashboard := fetchAuthedPage(t, client, testServer.URL+"/dashboard")
	if strings.Contains(dashboard, "Buat password baru") {
		t.Fatal("the block did not lift after the password was set")
	}

	// Signing in again with the new password goes straight through.
	next := signInWith(t, testServer, "778899", "rahasiasaya")
	if page := fetchAuthedPage(t, next, testServer.URL+"/dashboard"); strings.Contains(page, "Buat password baru") {
		t.Fatal("onboarding came back for an account that already set its password")
	}
	// And the shared one no longer opens it.
	if signedInOK(t, testServer, "778899", service.DefaultPassword) {
		t.Fatal("the default password still works after it was replaced")
	}
}

// Setting the shared password again would only bring the block straight back.
func TestOnboardingRefusesTheDefaultAndAMismatch(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	loggedInClient(t, testServer)
	addEmployee(t, testServer, "778899", "Produksi")
	client := signInWith(t, testServer, "778899", service.DefaultPassword)

	for name, form := range map[string]map[string]string{
		"the default again": {"password": service.DefaultPassword, "konfirmasi_password": service.DefaultPassword},
		"a mismatch":        {"password": "rahasiasaya", "konfirmasi_password": "rahasialain"},
		"too short":         {"password": "pendek", "konfirmasi_password": "pendek"},
	} {
		page := fetchAuthedPage(t, client, testServer.URL+"/onboarding")
		form["csrf_token"] = csrfFromForm(t, page)
		response, err := client.PostForm(testServer.URL+"/onboarding", urlValues(form))
		if err != nil {
			t.Fatalf("%s: post onboarding: %v", name, err)
		}
		body := readBody(t, response)
		response.Body.Close()
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status = %d, want 422", name, response.StatusCode)
		}
		if !strings.Contains(body, "Buat password baru") {
			t.Fatalf("%s: the page did not come back", name)
		}
	}
	// Still held, having set nothing.
	if page := fetchAuthedPage(t, client, testServer.URL+"/dashboard"); !strings.Contains(page, "Buat password baru") {
		t.Fatal("the block lifted without a password being set")
	}
}

// The registration page let anyone who found the URL mint an account and pick
// their own position, Management included. It is open only while there is
// nobody to ask for one instead.
func TestRegistrationClosesOnceAnAccountExists(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)

	// Open on an empty deployment: somebody has to make the first account.
	if page := fetchPage(t, testServer.URL+"/register"); !strings.Contains(page, "Buat akun") {
		t.Fatal("the bootstrap registration page is not open on an empty deployment")
	}

	loggedInClient(t, testServer)

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get(testServer.URL + "/register")
	if err != nil {
		t.Fatalf("get register: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want a redirect away", response.StatusCode)
	}

	// And the POST is closed too, not just the page that draws the form.
	posted, err := client.PostForm(testServer.URL+"/register", urlValues(map[string]string{
		"tanggal_gabung": "2026-08-07", "nama_lengkap": "Penyusup", "nrp": "999999",
		"jabatan": "Management", "email": "penyusup@example.com",
		"password": "rahasia123", "status_pengguna": model.StatusAktif,
	}))
	if err != nil {
		t.Fatalf("post register: %v", err)
	}
	defer posted.Body.Close()
	if _, err := fixtureAuthFor(t, testServer).Authenticate(context.Background(), "999999", "rahasia123", service.ActivityMeta{}); err == nil {
		t.Fatal("an account was created through the closed registration page")
	}
}
