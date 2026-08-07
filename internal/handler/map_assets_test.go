package handler

import (
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	testServer, _ := newTestServerWithStore(t)
	return testServer
}

func newTestServerWithStore(t *testing.T) (*httptest.Server, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	nowFunc := func() time.Time { return now }
	server, err := NewServer(
		service.NewAuthService(store, location, nowFunc),
		service.NewAttendanceService(store, location, nowFunc),
		service.NewUnitDTService(store, location, nowFunc),
		session.NewManager(24*time.Hour, false),
		location, nowFunc, 2*1024*1024, photo.MaxOutputChars,
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	t.Cleanup(testServer.Close)
	return testServer, store
}

// The //go:embed patterns do not recurse into subdirectories, so a missing
// vendor path silently ships a dashboard with no map.
func TestLeafletVendorAssetsAreServed(t *testing.T) {
	testServer := newTestServer(t)

	for _, path := range []string{
		"/static/vendor/leaflet/leaflet.js",
		"/static/vendor/leaflet/leaflet.css",
		"/static/vendor/leaflet/images/marker-icon.png",
		"/static/vendor/leaflet/images/marker-icon-2x.png",
		"/static/vendor/leaflet/images/marker-shadow.png",
	} {
		response, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body := readBody(t, response)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("get %s: status %d", path, response.StatusCode)
		}
		if len(body) == 0 {
			t.Fatalf("get %s: empty body", path)
		}
	}
}

// The map placeholder, camera placeholder and location chip are all toggled
// through the `hidden` attribute while carrying an author `display` rule, which
// overrides the UA sheet. Without this rule they stay on screen forever and the
// placeholder covers the map tiles.
func TestStylesheetForcesHiddenAttribute(t *testing.T) {
	testServer := newTestServer(t)

	response, err := http.Get(testServer.URL + "/static/css/style.css")
	if err != nil {
		t.Fatalf("get stylesheet: %v", err)
	}
	stylesheet := readBody(t, response)
	if !strings.Contains(stylesheet, "[hidden] { display: none !important; }") {
		t.Fatal("stylesheet lost the [hidden] override")
	}
}

func TestContentSecurityPolicyAllowsTilesOnly(t *testing.T) {
	testServer := newTestServer(t)

	response, err := http.Get(testServer.URL + "/login")
	if err != nil {
		t.Fatalf("get login: %v", err)
	}
	response.Body.Close()

	policy := response.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "img-src 'self' data: https://*.tile.openstreetmap.org") {
		t.Fatalf("tile hosts are not allowed by img-src: %q", policy)
	}
	// The tile hosts must stay confined to images. If they ever leak into
	// script-src or connect-src, a third party could run code in the app.
	for _, directive := range []string{
		"default-src 'self';",
		"script-src 'self';",
		"style-src 'self';",
		"connect-src 'self';",
		"frame-ancestors 'none';",
	} {
		if !strings.Contains(policy, directive) {
			t.Fatalf("policy no longer pins %q: %q", directive, policy)
		}
	}
}

func TestDashboardRendersLocationMap(t *testing.T) {
	testServer := newTestServer(t)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &http.Client{Jar: jar}

	registerResponse, err := client.PostForm(testServer.URL+"/register", urlValues(map[string]string{
		"tanggal_gabung":  "2026-08-07",
		"nama_lengkap":    "Budi Santoso",
		"nrp":             "123456",
		"jabatan":         "Produksi",
		"email":           "budi@example.com",
		"password":        "rahasia123",
		"status_pengguna": model.StatusAktif,
	}))
	if err != nil {
		t.Fatalf("register request: %v", err)
	}
	registerResponse.Body.Close()

	loginResponse, err := client.PostForm(testServer.URL+"/login", urlValues(map[string]string{
		"identifier": "123456",
		"password":   "rahasia123",
	}))
	if err != nil {
		t.Fatalf("login request: %v", err)
	}
	dashboard := readBody(t, loginResponse)
	if loginResponse.StatusCode != http.StatusOK {
		t.Fatalf("login status: %d", loginResponse.StatusCode)
	}

	for _, fragment := range []string{
		`id="attendanceMap"`,
		`id="locationChip"`,
		`id="refreshLocation"`,
		`/static/vendor/leaflet/leaflet.js`,
		`/static/vendor/leaflet/leaflet.css`,
	} {
		if !strings.Contains(dashboard, fragment) {
			t.Fatalf("dashboard missing %q", fragment)
		}
	}

	// The camera now lives in a modal that only opens once an action is picked.
	if !strings.Contains(dashboard, `id="cameraModal"`) {
		t.Fatal("dashboard is missing the camera modal")
	}
}
