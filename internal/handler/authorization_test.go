package handler

import (
	"net/http"
	"strings"
	"testing"
)

// menuPaths is every page behind the menu, with the position rule it falls
// under.
var menuPaths = map[string][]string{
	"beranda":  {"/dashboard"},
	"absensi":  {"/absensi"},
	"leave":    {"/leave/request"},
	"hr":       {"/hr/overview", "/hr/approval-leave"},
	"produksi": {"/produksi", "/produksi/overview", "/produksi/export"},
	"unit":     {"/unit/overview", "/unit-dt", "/unit/export"},
	"a2b":      {"/a2b/overview", "/unit-a2b", "/a2b/hm", "/a2b/fuel", "/a2b/export"},
	"nota":     {"/nota", "/nota/overview", "/nota/rekonsiliasi", "/nota/export"},
}

func statusOf(t *testing.T, client *http.Client, url string) int {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	response.Body.Close()
	return response.StatusCode
}

// Attendance is the one thing every employee has to do, so Dashboard and
// Absensi are open to every position.
func TestEveryPositionReachesDashboardAndAbsensi(t *testing.T) {
	for _, jabatan := range []string{"Flagman", "Security", "SHE", "Surveyor", "Logistik", "HR", "SPV", "Management", "Produksi"} {
		testServer := newTestServer(t)
		client := loggedInClientAs(t, testServer, jabatan)
		paths := append([]string{}, menuPaths["beranda"]...)
		paths = append(paths, menuPaths["absensi"]...)
		paths = append(paths, menuPaths["leave"]...)
		for _, path := range paths {
			if status := statusOf(t, client, testServer.URL+path); status != http.StatusOK {
				t.Fatalf("%s: %s returned %d", jabatan, path, status)
			}
		}
	}
}

// Each menu is open to the positions that work in it, and closed to the rest.
// A hidden menu is a courtesy; the refusal is what keeps the page out of reach.
func TestMenusAreOpenToTheirOwnPositions(t *testing.T) {
	cases := map[string]map[string]bool{
		// jabatan -> menu -> may open
		// A2B is the same fleet from another angle, so it follows the unit rule.
		"Surveyor":   {"produksi": true, "unit": true, "a2b": true, "nota": false, "hr": false},
		"Produksi":   {"produksi": true, "unit": true, "a2b": true, "nota": false, "hr": false},
		"SPV":        {"produksi": true, "unit": true, "a2b": true, "nota": false, "hr": false},
		"Logistik":   {"produksi": false, "unit": true, "a2b": true, "nota": false, "hr": false},
		"HR":         {"produksi": false, "unit": false, "a2b": false, "nota": true, "hr": true},
		"Management": {"produksi": true, "unit": true, "a2b": true, "nota": true, "hr": true},
		"Security":   {"produksi": false, "unit": false, "a2b": false, "nota": false, "hr": false},
		"Flagman":    {"produksi": false, "unit": false, "a2b": false, "nota": false, "hr": false},
		"SHE":        {"produksi": false, "unit": false, "a2b": false, "nota": false, "hr": false},
	}
	for jabatan, menus := range cases {
		testServer := newTestServer(t)
		client := loggedInClientAs(t, testServer, jabatan)
		for menu, allowed := range menus {
			for _, path := range menuPaths[menu] {
				status := statusOf(t, client, testServer.URL+path)
				if allowed && status != http.StatusOK {
					t.Fatalf("%s should reach %s, got %d", jabatan, path, status)
				}
				if !allowed && status != http.StatusForbidden {
					t.Fatalf("%s should be refused %s, got %d", jabatan, path, status)
				}
			}
		}
	}
}

// The menu shows what a position may open and nothing else, so nobody is
// invited into a page that will turn them away.
func TestMenuShowsOnlyWhatThePositionMayOpen(t *testing.T) {
	testServer := newTestServer(t)

	hr := navSection(t, fetchAuthedPage(t, loggedInClientAs(t, testServer, "HR"), testServer.URL+"/dashboard"))
	if !strings.Contains(hr, `href="/nota"`) {
		t.Fatal("HR cannot see the nota menu")
	}
	if !strings.Contains(hr, `href="/hr/overview"`) || !strings.Contains(hr, `href="/hr/approval-leave"`) {
		t.Fatal("HR cannot see the HR module")
	}
	if !strings.Contains(hr, `href="/leave/request"`) {
		t.Fatal("HR cannot see Request Leave")
	}
	for _, hidden := range []string{`href="/produksi"`, `href="/unit-dt"`} {
		if strings.Contains(hr, hidden) {
			t.Fatalf("HR is shown %q", hidden)
		}
	}

	logistik := navSection(t, fetchAuthedPage(t, loggedInClientAs(t, testServer, "Logistik"), testServer.URL+"/dashboard"))
	if !strings.Contains(logistik, `href="/unit-dt"`) {
		t.Fatal("Logistik cannot see the unit menu")
	}
	if strings.Contains(logistik, `href="/produksi/overview"`) {
		t.Fatal("Logistik is shown the produksi menu")
	}

	// A group with nothing left in it disappears rather than opening onto an
	// empty list.
	security := navSection(t, fetchAuthedPage(t, loggedInClientAs(t, testServer, "Security"), testServer.URL+"/dashboard"))
	for _, heading := range []string{">HR<", ">Produksi<", ">Nota<", ">Unit<", ">A2B<"} {
		if strings.Contains(security, heading) {
			t.Fatalf("Security is shown the %q heading with no pages under it", heading)
		}
	}
	if !strings.Contains(security, `href="/absensi"`) {
		t.Fatal("Security lost the attendance menu")
	}
	if !strings.Contains(security, `href="/leave/request"`) {
		t.Fatal("Security lost Request Leave")
	}
}

// Writing is guarded by the same rule as reading: a form post is a URL too, and
// a hidden form is not a closed door.
func TestWritesAreRefusedForPositionsWithoutAccess(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postNota(t, client, testServer, "", notaFields(), notaItems(), []string{"foto_kwitansi"})
	response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("posting a nota as Logistik returned %d, want 403", response.StatusCode)
	}
}

// The refusal explains itself and leaves somewhere to go, rather than dropping
// someone on a bare error.
func TestRefusalNamesThePositionAndOffersAWayBack(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClientAs(t, testServer, "Security")

	response, err := client.Get(testServer.URL + "/nota")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	page := readBody(t, response)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d, want 403", response.StatusCode)
	}
	for _, fragment := range []string{"AKSES DITOLAK", "jabatan Security", `href="/dashboard"`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the refusal page is missing %q", fragment)
		}
	}
}

// A page nobody listed in the menu is refused rather than left open: a route
// added without a rule should be unreachable, not public.
func TestUnknownPagesAreRefusedByDefault(t *testing.T) {
	if CanAccess("Produksi", "menu-yang-belum-ada") {
		t.Fatal("a page with no rule is open to everyone")
	}
	if !CanAccess("Management", "menu-yang-belum-ada") {
		t.Fatal("Management lost its blanket access")
	}
}
