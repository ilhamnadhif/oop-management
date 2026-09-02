package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newJabatanService(store *repository.TestRepository) *AuthService {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	return NewAuthService(store, now.Location(), func() time.Time { return now }).WithHashCost(bcrypt.MinCost)
}

// A site invents its own job titles. The built-in eight stay, and the new one
// joins them for that project alone.
func TestCreateJabatanBelongsToOneProject(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()

	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create jabatan: %v", err)
	}

	here, err := auth.JabatanOptionsFor(ctx, "PCPM")
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if !containsString(here, "Mekanik") {
		t.Fatalf("the new position is missing from its own project: %v", here)
	}
	for _, builtIn := range JabatanOptions {
		if !containsString(here, builtIn) {
			t.Fatalf("the built-in position %q disappeared: %v", builtIn, here)
		}
	}

	elsewhere, err := auth.JabatanOptionsFor(ctx, "KENDAL")
	if err != nil {
		t.Fatalf("options elsewhere: %v", err)
	}
	if containsString(elsewhere, "Mekanik") {
		t.Fatalf("one project's position leaked into another: %v", elsewhere)
	}
}

// Two sites may both want a Mekanik. Each is its own, and neither knows about
// the other.
func TestCreateJabatanAllowsTheSameNameInAnotherProject(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()

	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create first: %v", err)
	}
	if err := auth.CreateJabatan(ctx, "KENDAL", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("the same name was refused in another project: %v", err)
	}
	if got := len(store.JabatanList()); got != 2 {
		t.Fatalf("stored %d positions, want one per project", got)
	}
}

// Within one project a name is claimed once, whatever case it was typed in.
func TestCreateJabatanRefusesADuplicateInTheSameProject(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()

	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create: %v", err)
	}
	err := auth.CreateJabatan(ctx, "PCPM", "  mekanik ", "Rina HR")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want the duplicate refused", err)
	}
	if got := len(store.JabatanList()); got != 1 {
		t.Fatalf("stored %d positions, want the duplicate refused", got)
	}
}

// Management is the position defined by reaching every project. A second one
// under that name would be an escalation, not a job title.
func TestCreateJabatanRefusesManagement(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)

	for _, name := range []string{"Management", "management", " MANAGEMENT "} {
		err := auth.CreateJabatan(context.Background(), "PCPM", name, "Rina HR")
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("%q returned %v, want it refused", name, err)
		}
	}
	if got := len(store.JabatanList()); got != 0 {
		t.Fatalf("stored %d positions, want none", got)
	}
}

// A built-in name already means something everywhere. Redefining it in one
// project would make the same word mean two things.
func TestCreateJabatanRefusesABuiltInName(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)

	err := auth.CreateJabatan(context.Background(), "PCPM", "spv", "Rina HR")
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want a built-in name refused", err)
	}
}

// An unnamed position, or one no form could show, is not a position.
func TestCreateJabatanRefusesAnUnusableName(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)

	for _, name := range []string{"", "   ", strings.Repeat("a", 41)} {
		if err := auth.CreateJabatan(context.Background(), "PCPM", name, "Rina HR"); !errors.Is(err, ErrValidation) {
			t.Fatalf("%q returned %v, want it refused", name, err)
		}
	}
}

// A position belongs to a project, so there has to be one.
func TestCreateJabatanRefusesAnEmptyProject(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)

	if err := auth.CreateJabatan(context.Background(), "  ", "Mekanik", "Rina HR"); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want a position without a project refused", err)
	}
}

// The position starts with nothing: what it may open is decided afterwards, on
// the access matrix, which is where every other position's rights live.
func TestCreateJabatanStartsWithNoMenus(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()

	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create: %v", err)
	}
	access, err := auth.JabatanAccess(ctx)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	for _, row := range access {
		if strings.EqualFold(row.Jabatan, "Mekanik") && len(row.MenuAktif) > 0 {
			t.Fatalf("a new position was born holding menus: %+v", row)
		}
	}
}

// A person may only hold a position their own project has.
func TestAddEmployeeAcceptsAProjectsOwnJabatan(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()
	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err := auth.AddEmployee(ctx, RegisterInput{
		TanggalGabung: "2026-09-01", NamaLengkap: "Budi Hartono", NRP: "770001",
		Jabatan: "mekanik", Email: "budi.mekanik@example.test",
		Status: model.StatusAktif, Project: "PCPM",
	}, false)
	if err != nil {
		t.Fatalf("add employee on the project's own position: %v", err)
	}

	// The same position in another project is not a position there.
	_, err = auth.AddEmployee(ctx, RegisterInput{
		TanggalGabung: "2026-09-01", NamaLengkap: "Cahyo Nugroho", NRP: "770002",
		Jabatan: "Mekanik", Email: "cahyo.mekanik@example.test",
		Status: model.StatusAktif, Project: "KENDAL",
	}, false)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want another project's position refused", err)
	}
}

// The access matrix is a project's own. Saving it in one site leaves the other
// following whatever it followed before.
func TestSaveJabatanAccessIsPerProject(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()

	if err := auth.SaveJabatanAccess(ctx, "PCPM", "SPV", []string{"nota"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	access, err := auth.JabatanAccess(ctx)
	if err != nil {
		t.Fatalf("access: %v", err)
	}
	if len(access) != 1 {
		t.Fatalf("stored %d rows, want 1", len(access))
	}
	if access[0].Project != "PCPM" || access[0].Jabatan != "SPV" {
		t.Fatalf("row is not keyed by project and position: %+v", access[0])
	}
}

// A position one project made can have its rights set in that project.
func TestSaveJabatanAccessAcceptsAProjectsOwnJabatan(t *testing.T) {
	store := repository.NewTestRepository()
	auth := newJabatanService(store)
	ctx := context.Background()
	if err := auth.CreateJabatan(ctx, "PCPM", "Mekanik", "Rina HR"); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := auth.SaveJabatanAccess(ctx, "PCPM", "Mekanik", []string{"a2b"}); err != nil {
		t.Fatalf("save for the project's own position: %v", err)
	}
	if err := auth.SaveJabatanAccess(ctx, "KENDAL", "Mekanik", []string{"a2b"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want another project's position refused", err)
	}
}

func containsString(list []string, want string) bool {
	for _, value := range list {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}
