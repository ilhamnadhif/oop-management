package handler

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// testProjectName is the project every fixture works in. One store, one project:
// what the tests are about is the pages, and a second project would only make
// each of them arrange more before it could ask anything.
const testProjectName = "PCPM"

// testStores is one store per project, the way the real deployment has one
// spreadsheet per project. The first project shares the master store, because
// that is exactly the arrangement the app carries an existing deployment across
// with: the file holding the accounts is also the first project's books.
//
// Having real separate stores here is what makes isolation testable at all. A
// single shared store would let a leak pass every test in this package.
type testStores struct {
	mu     sync.Mutex
	master *repository.TestRepository
	others map[string]*repository.TestRepository
}

func newTestStores(master *repository.TestRepository) *testStores {
	return &testStores{master: master, others: make(map[string]*repository.TestRepository)}
}

// forProject returns the store holding one project's books, creating it the
// first time it is asked for.
func (s *testStores) forProject(nama string) *repository.TestRepository {
	if strings.EqualFold(strings.TrimSpace(nama), testProjectName) {
		return s.master
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if store, ok := s.others[nama]; ok {
		return store
	}
	store := repository.NewTestRepository()
	s.others[nama] = store
	return store
}

// testDeps builds a server's dependencies with one project already seeded. The
// project is seeded here rather than by the tests because the web command seeds
// it at startup: a server with no projects is a state that only exists between
// the two, and no page is meant to work in it.
func testDeps(t *testing.T, store *repository.TestRepository, location *time.Location, nowFunc service.NowFunc, branding Branding) Deps {
	t.Helper()
	return testDepsWithStores(t, newTestStores(store), location, nowFunc, branding)
}

func testDepsWithStores(t *testing.T, stores *testStores, location *time.Location, nowFunc service.NowFunc, branding Branding) Deps {
	t.Helper()
	master := stores.master
	projects := service.NewProjectService(master, model.ProjectSettings{
		Company:        branding.Company,
		SignatoryName:  branding.Signatory.Name,
		SignatoryTitle: branding.Signatory.Title,
		SignatoryPlace: branding.Signatory.Place,
	}, location, nowFunc)
	if _, err := projects.EnsureFirst(context.Background(), testProjectName, "test-spreadsheet"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return Deps{
		Auth:     service.NewAuthService(master, location, nowFunc).WithHashCost(bcrypt.MinCost),
		Projects: projects,
		Services: func(_ context.Context, project model.Project) (*ProjectServices, error) {
			return testProjectServices(stores.forProject(project.Nama), location, nowFunc, project), nil
		},
		Sessions:       session.NewManager(24*time.Hour, false),
		Location:       location,
		Now:            nowFunc,
		MaxUploadBytes: 2 * 1024 * 1024,
		MaxPhotoChars:  photo.MaxOutputChars,
		Branding:       branding,
	}
}

// testProjectServices wires every service over one project's store. The
// accounts are left where they are rather than filtered through the project's
// membership: the fixtures put everybody in the one project anyway, and
// pointing the services at a directory would only add a step to arrange.
func testProjectServices(store repository.Store, location *time.Location, nowFunc service.NowFunc, project model.Project) *ProjectServices {
	return &ProjectServices{
		Attendance:   service.NewAttendanceService(store, location, nowFunc),
		UnitDT:       service.NewUnitDTService(store, location, nowFunc),
		Produksi:     service.NewProduksiService(store, location, nowFunc),
		Overview:     service.NewOverviewService(store, location, nowFunc),
		UnitA2B:      service.NewUnitA2BService(store, location, nowFunc),
		Nota:         service.NewNotaService(store, location, nowFunc),
		Leave:        service.NewLeaveService(store, location, nowFunc).WithSchedule(service.DefaultSchedule()),
		UnitOverview: service.NewUnitOverviewService(store, location, nowFunc),
		FuelMasuk:    service.NewFuelMasukService(store, location, nowFunc),
		FuelKeluar:   service.NewFuelKeluarService(store, location, nowFunc),
		HourMeter:    service.NewHourMeterService(store, location, nowFunc),
		Company:      project.Settings.Company,
		Signatory: export.Signatory{
			Name:  project.Settings.SignatoryName,
			Title: project.Settings.SignatoryTitle,
			Place: project.Settings.SignatoryPlace,
		},
	}
}

// The registration page closes as soon as an account exists, so fixtures that
// need a second person cannot go through it. They reach the auth service
// directly instead, which is also what the app itself does from the HR screen.
//
// The service is looked up by the test server's own URL: fixtures run in
// parallel, and a package-level "the last one built" would hand a test somebody
// else's store.
var (
	fixtureMu   sync.Mutex
	fixtureAuth = map[string]*service.AuthService{}
)

// newFixtureServer starts a test server and records the auth service behind it.
func newFixtureServer(t *testing.T, deps Deps) *httptest.Server {
	t.Helper()
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return startFixtureServer(t, server, deps)
}

// startFixtureServer is newFixtureServer for the fixtures that need to reach
// the *Server first, to hang a scanner off it.
func startFixtureServer(t *testing.T, server *Server, deps Deps) *httptest.Server {
	t.Helper()
	testServer := httptest.NewServer(server.Handler())
	fixtureMu.Lock()
	fixtureAuth[testServer.URL] = deps.Auth
	fixtureMu.Unlock()
	t.Cleanup(func() {
		testServer.Close()
		fixtureMu.Lock()
		delete(fixtureAuth, testServer.URL)
		fixtureMu.Unlock()
	})
	return testServer
}

func fixtureAuthFor(t *testing.T, testServer *httptest.Server) *service.AuthService {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	auth, ok := fixtureAuth[testServer.URL]
	if !ok {
		t.Fatal("this test server was not started through newFixtureServer")
	}
	return auth
}

// defaultTestBranding is what most fixtures print on an export.
func defaultTestBranding() Branding {
	return Branding{Company: "PT Orecon Putra Perkasa", Signatory: export.Signatory{Title: "Direktur"}}
}
