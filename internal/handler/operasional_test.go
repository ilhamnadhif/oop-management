package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/service"
)

// operasionalPaths is every page in the section, with the label the sidebar
// gives it.
var operasionalPaths = map[string]string{
	"/operasional/gaji":      "Gaji",
	"/operasional/makan":     "Makan",
	"/operasional/sewa-a2b":  "Bayar Sewa A2B",
	"/operasional/sewa-dt":   "Bayar Sewa DT",
	"/operasional/lain-lain": "Pengeluaran Lain-lain",
}

// The pages are placeholders, but they are real pages: they open, they carry
// their own title, and they say plainly that there is nothing in them yet.
func TestOperasionalPagesOpenAsPlaceholders(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	for path, label := range operasionalPaths {
		page := fetchAuthedPage(t, client, testServer.URL+path)
		if !strings.Contains(page, label) {
			t.Fatalf("%s does not carry its own title %q", path, label)
		}
		if !strings.Contains(page, "belum berisi apa-apa") {
			t.Fatalf("%s does not say it is empty", path)
		}
	}
}

// HR keeps the payroll and SPV signs off the machines the rental is paid for,
// so both see the menu. Management sees every menu.
func TestOperasionalIsVisibleToTheJabatanThatKeepIt(t *testing.T) {
	testServer := newTestServer(t)
	for _, jabatan := range []string{JabatanManagement, "HR", "SPV"} {
		client := loggedInClientAs(t, testServer, jabatan)
		nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"))
		if !strings.Contains(nav, "Operasional") {
			t.Fatalf("%s cannot see the Operasional menu", jabatan)
		}
	}
}

// Everybody else neither sees the menu nor reaches the pages behind it. What is
// paid to whom is not open to the site at large.
func TestOperasionalIsHiddenFromEveryOtherJabatan(t *testing.T) {
	testServer := newTestServer(t)
	for _, jabatan := range []string{"Surveyor", "Produksi", "Logistik"} {
		client := loggedInClientAs(t, testServer, jabatan)
		nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"))
		if strings.Contains(nav, "Operasional") {
			t.Fatalf("%s can see the Operasional menu", jabatan)
		}
		for path := range operasionalPaths {
			response, err := client.Get(testServer.URL + path)
			if err != nil {
				t.Fatalf("get %s as %s: %v", path, jabatan, err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("%s opened %s with status %d, want 403", jabatan, path, response.StatusCode)
			}
		}
	}
}

// A page nobody is signed in to is a login, not a placeholder.
func TestOperasionalPagesRequireASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for path := range operasionalPaths {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("%s: anonymous request went to %q, want /login", path, location)
		}
	}
}

// The settings screen offers Operasional like any other module, with its pages
// named, so somebody deciding whether to run it can see what it takes away.
func TestProjectSettingsOffersOperasional(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client,
		testServer.URL+"/project/settings?project="+store.ProjectList()[0].Nama)
	if !strings.Contains(page, `value="operasional"`) {
		t.Fatal("the settings screen does not offer the Operasional module")
	}
	for _, label := range operasionalPaths {
		if !strings.Contains(page, label) {
			t.Fatalf("the Operasional entry does not name %q", label)
		}
	}
}

// A project that does not run Operasional loses the menu and the pages behind
// it, Management included: the module is off, not merely hidden.
func TestProjectWithoutOperasionalLosesItsPages(t *testing.T) {
	testServer, _, projects := newTwoProjectServer(t)
	client := loggedInClient(t, testServer)

	first, err := projects.Find(context.Background(), testProjectName)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	if _, err := projects.Update(context.Background(), first.ProjectID, service.ProjectUpdate{
		Nama: testProjectName, MenuAktif: []string{"produksi"}, Status: model.StatusAktif,
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}

	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/dashboard"))
	if strings.Contains(nav, "Operasional") {
		t.Fatal("a project that does not run Operasional still shows the menu")
	}
	for path := range operasionalPaths {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Fatalf("%s opened in a project that does not run Operasional", path)
		}
	}
}
