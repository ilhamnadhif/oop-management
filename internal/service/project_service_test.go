package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/secret"
)

func newProjectServiceFixture(t *testing.T) (*ProjectService, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC)
	projects := NewProjectService(store, model.ProjectSettings{
		WorkStart: "08:00", WorkEnd: "17:00", LateToleranceMinutes: 15,
		A2BWorkMinutes: 480, Company: "PT Contoh", SignatoryPlace: "Balikpapan",
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

// widyaUpdate is a project's Widya account saved through the settings screen,
// with every other field left as a save would send it.
func widyaUpdate(address, email, password string) ProjectUpdate {
	return ProjectUpdate{Nama: "PCPM", Status: model.StatusAktif,
		WidyaURL: address, WidyaEmail: email, WidyaPassword: password}
}

func newWidyaProjects(t *testing.T) (*ProjectService, model.Project, *[]model.WidyaAccount, *error) {
	t.Helper()
	box, err := secret.NewBox("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("new box: %v", err)
	}
	projects := NewProjectService(repository.NewTestRepository(), model.ProjectSettings{}, time.UTC, nil)
	var tried []model.WidyaAccount
	var refuse error
	projects.WithWidya(box, func(_ context.Context, account model.WidyaAccount) error {
		tried = append(tried, account)
		return refuse
	})
	first, err := projects.EnsureFirst(context.Background(), "PCPM", "sheet-1")
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	return projects, first, &tried, &refuse
}

func TestWidyaAccountIsSealedAndReadsBack(t *testing.T) {
	projects, first, tried, _ := newWidyaProjects(t)
	ctx := context.Background()

	saved, err := projects.Update(ctx, first.ProjectID, widyaUpdate(" https://kendal.example/ ", "a@example.com", "rahasia"))
	if err != nil {
		t.Fatalf("save account: %v", err)
	}
	if saved.Settings.WidyaURL != "https://kendal.example" || saved.Settings.WidyaEmail != "a@example.com" {
		t.Fatalf("settings = %+v", saved.Settings)
	}
	if saved.Settings.WidyaPasswordSealed == "" || strings.Contains(saved.Settings.WidyaPasswordSealed, "rahasia") {
		t.Fatalf("the password reached the sheet readable: %q", saved.Settings.WidyaPasswordSealed)
	}
	if len(*tried) != 1 || (*tried)[0].Password != "rahasia" {
		t.Fatalf("the account was not tried before saving: %+v", *tried)
	}
	account, err := projects.WidyaAccount(saved)
	if err != nil || account.Password != "rahasia" || !account.Configured() {
		t.Fatalf("account = %+v, %v", account, err)
	}

	// Saving the screen again with a blank password keeps it and reaches
	// nothing: the rest of the settings must not depend on the scanner.
	again, err := projects.Update(ctx, first.ProjectID, widyaUpdate("https://kendal.example", "a@example.com", ""))
	if err != nil {
		t.Fatalf("save again: %v", err)
	}
	if again.Settings.WidyaPasswordSealed != saved.Settings.WidyaPasswordSealed || len(*tried) != 1 {
		t.Fatalf("an untouched account changed: %d tries", len(*tried))
	}

	// A new URL with the stored password is tried with that password.
	moved, err := projects.Update(ctx, first.ProjectID, widyaUpdate("https://baru.example", "a@example.com", ""))
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if len(*tried) != 2 || (*tried)[1].Password != "rahasia" || (*tried)[1].URL != "https://baru.example" ||
		moved.Settings.WidyaPasswordSealed != saved.Settings.WidyaPasswordSealed {
		t.Fatalf("tries = %+v", *tried)
	}

	// All three blank lets the account go.
	cleared, err := projects.Update(ctx, first.ProjectID, widyaUpdate("", "", ""))
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared.Settings.WidyaURL != "" || cleared.Settings.WidyaPasswordSealed != "" {
		t.Fatalf("settings = %+v", cleared.Settings)
	}
	if account, err := projects.WidyaAccount(cleared); err != nil || account.Configured() {
		t.Fatalf("cleared account = %+v, %v", account, err)
	}
}

func TestWidyaAccountIsRefusedWhenItCannotWork(t *testing.T) {
	projects, first, _, refuse := newWidyaProjects(t)
	ctx := context.Background()

	for name, update := range map[string]ProjectUpdate{
		"no scheme":   widyaUpdate("kendal.example", "a@example.com", "rahasia"),
		"plain http":  widyaUpdate("http://kendal.example", "a@example.com", "rahasia"),
		"no email":    widyaUpdate("https://kendal.example", "", "rahasia"),
		"no password": widyaUpdate("https://kendal.example", "a@example.com", ""),
	} {
		if _, err := projects.Update(ctx, first.ProjectID, update); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v", name, err)
		}
	}

	// A refused sign-in saves nothing, and does not say why in the scanner's
	// own words.
	*refuse = errors.New("status 500 from scanner")
	_, err := projects.Update(ctx, first.ProjectID, widyaUpdate("https://kendal.example", "a@example.com", "salah"))
	if !errors.Is(err, ErrValidation) || strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
	stored, err := projects.Find(ctx, "PCPM")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if stored.Settings.WidyaURL != "" || stored.Settings.WidyaPasswordSealed != "" {
		t.Fatalf("a refused account was saved: %+v", stored.Settings)
	}

	// Without the key nothing can be sealed, and the screen says what is wrong.
	*refuse = nil
	projects.WithWidya(nil, nil)
	_, err = projects.Update(ctx, first.ProjectID, widyaUpdate("https://kendal.example", "a@example.com", "rahasia"))
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "WIDYA_SECRET_KEY") {
		t.Fatalf("err = %v", err)
	}
}

// A password sealed for one project does not open on another's row.
func TestWidyaPasswordBelongsToItsProject(t *testing.T) {
	projects, first, _, _ := newWidyaProjects(t)
	saved, err := projects.Update(context.Background(), first.ProjectID, widyaUpdate("https://kendal.example", "a@example.com", "rahasia"))
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	copied := saved
	copied.ProjectID = "PRJ-9999"
	if _, err := projects.WidyaAccount(copied); err == nil {
		t.Fatal("a password opened on another project's row")
	}
}
