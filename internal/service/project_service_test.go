package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newProjectServiceFixture(t *testing.T) (*ProjectService, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	projects := NewProjectService(store, model.ProjectSettings{
		WorkStart: "08:00", WorkEnd: "17:00", LateToleranceMinutes: 15,
		A2BWorkMinutes: 480, Company: "PT Contoh", SignatoryTitle: "Direktur",
	}, time.UTC, func() time.Time { return now })
	if _, err := projects.EnsureFirst(context.Background(), "PCPM", "sheet-pcpm"); err != nil {
		t.Fatalf("seed first project: %v", err)
	}
	return projects, store
}

func addUser(t *testing.T, store *repository.TestRepository, nrp, jabatan, project string) model.User {
	t.Helper()
	user := &model.User{
		UserID: "USR-" + nrp, NRP: nrp, NamaLengkap: "Orang " + nrp,
		Jabatan: jabatan, Email: nrp + "@example.com",
		StatusPengguna: model.StatusAktif, Project: project,
	}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return *user
}

// The first start after this release has to write the project the app was
// already keeping books for, or nobody gets in.
func TestEnsureFirstWritesTheExistingProjectOnce(t *testing.T) {
	projects, store := newProjectServiceFixture(t)

	stored := store.ProjectList()
	if len(stored) != 1 {
		t.Fatalf("stored %d projects, want 1", len(stored))
	}
	if stored[0].Nama != "PCPM" || stored[0].SpreadsheetID != "sheet-pcpm" {
		t.Fatalf("first project stored wrong: %+v", stored[0])
	}
	// No menus listed means every menu, which is what the app had before any of
	// them could be switched off.
	if len(stored[0].MenuAktif) != 0 {
		t.Fatalf("MenuAktif = %v, want empty", stored[0].MenuAktif)
	}

	// Running again must not add a second row.
	if _, err := projects.EnsureFirst(context.Background(), "PCPM", "sheet-pcpm"); err != nil {
		t.Fatalf("second EnsureFirst: %v", err)
	}
	if got := len(store.ProjectList()); got != 1 {
		t.Fatalf("stored %d projects after a second run, want 1", got)
	}
}

// An account written before projects existed carries no project. Reading that
// as "no access" would lock out everybody who already had an account.
func TestReachableTreatsAnUnassignedAccountAsTheFirstProject(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	if _, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil, nil); err != nil {
		t.Fatalf("create project: %v", err)
	}
	user := addUser(t, store, "1", "Produksi", "")

	reachable, err := projects.Reachable(context.Background(), &user)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if len(reachable) != 1 || reachable[0].Nama != "PCPM" {
		t.Fatalf("reachable = %v, want only PCPM", projectNamesOf(reachable))
	}
}

// Management reaches everything by position, whatever its row says.
func TestReachableGivesManagementEveryProject(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	if _, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil, nil); err != nil {
		t.Fatalf("create project: %v", err)
	}
	// Assigned to one project on paper, and still reaching both.
	user := addUser(t, store, "2", "Management", "PCPM")

	reachable, err := projects.Reachable(context.Background(), &user)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if len(reachable) != 2 {
		t.Fatalf("reachable = %v, want both projects", projectNamesOf(reachable))
	}
}

// A project switched off must disappear from the switcher, including for the
// people assigned to it.
func TestReachableSkipsAnInactiveProject(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	kendal, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil, nil)
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	user := addUser(t, store, "3", "Produksi", "KENDAL")

	if _, err := projects.Update(context.Background(), kendal.ProjectID, ProjectUpdate{
		Nama: "KENDAL", Status: model.StatusTidakAktif,
	}); err != nil {
		t.Fatalf("update project: %v", err)
	}

	reachable, err := projects.Reachable(context.Background(), &user)
	if err != nil {
		t.Fatalf("reachable: %v", err)
	}
	if len(reachable) != 0 {
		t.Fatalf("reachable = %v, want nothing", projectNamesOf(reachable))
	}
}

// Resolve keeps the session where it is when that is still allowed, so a switch
// survives until it is switched again.
func TestResolveKeepsTheSessionProjectWhenItIsStillReachable(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	if _, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil, nil); err != nil {
		t.Fatalf("create project: %v", err)
	}
	manager := addUser(t, store, "4", "Management", model.ProjectSemua)

	settled, err := projects.Resolve(context.Background(), &manager, "KENDAL")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settled.Nama != "KENDAL" {
		t.Fatalf("settled on %q, want KENDAL", settled.Nama)
	}

	// A project they cannot reach falls back to the first they can, rather than
	// failing: the session may simply be older than the change.
	pinned := addUser(t, store, "5", "Produksi", "PCPM")
	settled, err = projects.Resolve(context.Background(), &pinned, "KENDAL")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if settled.Nama != "PCPM" {
		t.Fatalf("settled on %q, want PCPM", settled.Nama)
	}
}

// Two projects sharing one spreadsheet would each write into the other's books,
// which is the one thing the arrangement exists to prevent.
func TestCreateRefusesADuplicateNameOrSpreadsheet(t *testing.T) {
	projects, _ := newProjectServiceFixture(t)

	if _, err := projects.Create(context.Background(), "pcpm", "sheet-lain", nil, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate name: err = %v, want ErrValidation", err)
	}
	if _, err := projects.Create(context.Background(), "KENDAL", "sheet-pcpm", nil, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("duplicate spreadsheet: err = %v, want ErrValidation", err)
	}
}

// A blank setting means "follow the deployment", so a project nobody has
// configured behaves exactly as the app did before it could be configured.
func TestSettingsFallBackToTheDeploymentDefaults(t *testing.T) {
	projects, _ := newProjectServiceFixture(t)

	first, err := projects.Find(context.Background(), "PCPM")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if first.Settings.WorkStart != "08:00" || first.Settings.LateToleranceMinutes != 15 {
		t.Fatalf("defaults not applied: %+v", first.Settings)
	}
	if first.Settings.A2BWorkMinutes != 480 || first.Settings.Company != "PT Contoh" {
		t.Fatalf("defaults not applied: %+v", first.Settings)
	}

	// A setting the project does state wins over the default.
	if _, err := projects.Update(context.Background(), first.ProjectID, ProjectUpdate{
		Nama: "PCPM", Status: model.StatusAktif, WorkStart: "07:00", A2BWorkMinutes: 600,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, err := projects.Find(context.Background(), "PCPM")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if updated.Settings.WorkStart != "07:00" || updated.Settings.A2BWorkMinutes != 600 {
		t.Fatalf("stated settings lost: %+v", updated.Settings)
	}
	// The ones left blank still follow the deployment rather than freezing.
	if updated.Settings.WorkEnd != "17:00" || updated.Settings.LateToleranceMinutes != 15 {
		t.Fatalf("blank settings stopped following the default: %+v", updated.Settings)
	}
}

func TestUpdateRejectsAMalformedClock(t *testing.T) {
	projects, _ := newProjectServiceFixture(t)
	first, _ := projects.Find(context.Background(), "PCPM")

	_, err := projects.Update(context.Background(), first.ProjectID, ProjectUpdate{
		Nama: "PCPM", Status: model.StatusAktif, WorkStart: "8 pagi",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

// Members is what the two services reading accounts are pointed at, so it has
// to answer with the people actually on site plus the ones who reach every site.
func TestMembersCountsTheUnassignedAsTheFirstProject(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	if _, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil, nil); err != nil {
		t.Fatalf("create project: %v", err)
	}
	addUser(t, store, "10", "Produksi", "")       // predates projects
	addUser(t, store, "11", "Produksi", "KENDAL") // the new site
	addUser(t, store, "12", "Management", "PCPM") // reaches everything

	pcpm, err := projects.Members(context.Background(), "PCPM")
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if got := nrpsOf(pcpm); len(got) != 2 || got[0] != "10" || got[1] != "12" {
		t.Fatalf("PCPM members = %v, want the unassigned account and Management", got)
	}

	kendal, err := projects.Members(context.Background(), "KENDAL")
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	if got := nrpsOf(kendal); len(got) != 2 || got[0] != "11" || got[1] != "12" {
		t.Fatalf("KENDAL members = %v, want its own account and Management", got)
	}
}

func TestAssignRefusesAnUnknownProject(t *testing.T) {
	projects, store := newProjectServiceFixture(t)
	user := addUser(t, store, "20", "Produksi", "")

	if err := projects.Assign(context.Background(), user.UserID, "TIDAK ADA"); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if err := projects.Assign(context.Background(), user.UserID, model.ProjectSemua); err != nil {
		t.Fatalf("assign to every project: %v", err)
	}
	if got := store.UserList()[0].Project; got != model.ProjectSemua {
		t.Fatalf("Project = %q, want %q", got, model.ProjectSemua)
	}
}

func projectNamesOf(projects []model.Project) []string {
	names := make([]string, 0, len(projects))
	for _, project := range projects {
		names = append(names, project.Nama)
	}
	return names
}

func nrpsOf(users []model.User) []string {
	nrps := make([]string, 0, len(users))
	for _, user := range users {
		nrps = append(nrps, user.NRP)
	}
	return nrps
}

// The spreadsheet is prepared as part of naming the project, so a file that
// cannot be written to is refused there and then rather than becoming a project
// that will not open.
func TestCreatePreparesTheSpreadsheetBeforeWritingTheRow(t *testing.T) {
	projects, store := newProjectServiceFixture(t)

	prepared := ""
	created, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil,
		func(_ context.Context, spreadsheetID string) error {
			// Called before the row exists, or an unusable project would be left
			// behind whenever this failed.
			if len(store.ProjectList()) != 1 {
				t.Fatalf("the project row was written before its spreadsheet was prepared")
			}
			prepared = spreadsheetID
			return nil
		})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if prepared != "sheet-kendal" {
		t.Fatalf("prepared %q, want the spreadsheet the project named", prepared)
	}
	if created.Nama != "KENDAL" || len(store.ProjectList()) != 2 {
		t.Fatalf("the project was not stored: %+v", store.ProjectList())
	}
}

// A spreadsheet that cannot be prepared leaves no project behind, and the
// reason reaches the page rather than the log.
func TestCreateRefusesAndStoresNothingWhenTheSpreadsheetFails(t *testing.T) {
	projects, store := newProjectServiceFixture(t)

	_, err := projects.Create(context.Background(), "KENDAL", "sheet-kendal", nil,
		func(context.Context, string) error {
			return errors.New("spreadsheet itu belum dibagikan ke service account aplikasi")
		})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation so the page shows it", err)
	}
	if !strings.Contains(err.Error(), "belum dibagikan ke service account") {
		t.Fatalf("err = %v, want the reason carried through", err)
	}
	if got := len(store.ProjectList()); got != 1 {
		t.Fatalf("stored %d projects, want the failed one left out", got)
	}
}

// A name or a file that is going to be refused is refused before anything is
// written into a spreadsheet.
func TestCreateChecksForDuplicatesBeforeTouchingTheSpreadsheet(t *testing.T) {
	projects, _ := newProjectServiceFixture(t)

	touched := false
	_, err := projects.Create(context.Background(), "pcpm", "sheet-lain", nil,
		func(context.Context, string) error {
			touched = true
			return nil
		})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if touched {
		t.Fatal("a duplicate name still had sheets written into its spreadsheet")
	}
}
