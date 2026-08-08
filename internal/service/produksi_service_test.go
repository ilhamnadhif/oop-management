package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newProduksiFixture(t *testing.T) (*ProduksiService, *repository.TestRepository, *model.User) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	produksi := NewProduksiService(store, location, func() time.Time { return now })
	user := &model.User{UserID: "usr_1", NamaLengkap: "Budi", StatusPengguna: model.StatusAktif}

	unit := &model.UnitDT{
		UnitID: "UNT-2026-0001", Nopol: "B 1234 ABC",
		Panjang: 375, Lebar: 190, Tinggi: 150,
		Driver: "Slamet", Keterangan: "DT KECIL",
	}
	if err := store.CreateUnitDT(context.Background(), unit); err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	return produksi, store, user
}

func validProduksiInput() ProduksiInput {
	return ProduksiInput{
		Tanggal:  "2026-08-07",
		Project:  "PCPM",
		Supplier: "HPP",
		Quary:    "HS",
		Kategori: "Replace",
		Lokasi:   "Blok A",
		Layer:    "L1",
		Nopol:    "B 1234 ABC",
		TT:       "0",
	}
}

// The reference case from the field: P=375, L=190, T=150, no TT, DT KECIL.
// TF passes through unconverted; only the volume is divided by 10^6.
func TestCalculateMatchesFieldReferenceCase(t *testing.T) {
	tf, volume, opp, deviasi := Calculate(375, 190, 150, 0, "DT KECIL")

	if tf != 150 {
		t.Fatalf("TF = %v, want 150", tf)
	}
	if volume != 10.6875 {
		t.Fatalf("Volume = %v, want 10.6875", volume)
	}
	if opp != 10 {
		t.Fatalf("OPP = %v, want 10", opp)
	}
	if deviasi != 0.6875 {
		t.Fatalf("Deviasi = %v, want 0.6875", deviasi)
	}
}

// TT lifts TF by half its value, which raises the volume with it.
func TestCalculateAddsHalfOfTT(t *testing.T) {
	tf, volume, _, _ := Calculate(375, 190, 150, 20, "DT KECIL")

	if tf != 160 {
		t.Fatalf("TF = %v, want 160", tf)
	}
	if volume != 11.4 {
		t.Fatalf("Volume = %v, want 11.4", volume)
	}
}

// Volume and deviation are stored to four decimals. Round figures would pass
// even at two, so this uses dimensions that produce a long tail.
func TestCreateProduksiKeepsFourDecimals(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)

	odd := &model.UnitDT{
		UnitID: "UNT-2026-0002", Nopol: "B 4321 XYZ",
		Panjang: 377, Lebar: 191, Tinggi: 151,
		Driver: "Sari", Keterangan: "DT KECIL",
	}
	if err := store.CreateUnitDT(context.Background(), odd); err != nil {
		t.Fatalf("seed unit: %v", err)
	}

	input := validProduksiInput()
	input.Nopol = odd.Nopol
	input.TT = "3"

	row, err := produksi.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// 377 x 191 x 152.5 / 10^6 = 10.9810675
	if row.TF != 152.5 {
		t.Fatalf("TF = %v, want 152.5", row.TF)
	}
	if row.Volume != 10.9811 {
		t.Fatalf("Volume = %v, want 10.9811", row.Volume)
	}
	if row.Deviasi != 0.9811 {
		t.Fatalf("Deviasi = %v, want 0.9811", row.Deviasi)
	}
}

func TestCalculateVolumeOPPPerJenisDT(t *testing.T) {
	if _, _, opp, _ := Calculate(100, 100, 100, 0, "DT KECIL"); opp != 10 {
		t.Fatalf("DT KECIL opp = %v, want 10", opp)
	}
	if _, _, opp, _ := Calculate(100, 100, 100, 0, "dt besar"); opp != 28 {
		t.Fatalf("lowercase DT BESAR opp = %v, want 28", opp)
	}
	// An unknown class must credit nothing rather than silently borrow a value.
	if _, _, opp, _ := Calculate(100, 100, 100, 0, "DT SEDANG"); opp != 0 {
		t.Fatalf("unknown class opp = %v, want 0", opp)
	}
}

// A small load must show as a negative deviation, not an absolute difference.
func TestCalculateReportsNegativeDeviation(t *testing.T) {
	_, volume, opp, deviasi := Calculate(200, 200, 100, 0, "DT BESAR")
	if volume != 4 || opp != 28 || deviasi != -24 {
		t.Fatalf("volume=%v opp=%v deviasi=%v, want 4 / 28 / -24", volume, opp, deviasi)
	}
}

func TestCreateProduksiStoresUnitDataFromTheRegister(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)

	row, err := produksi.Create(context.Background(), user, validProduksiInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if row.ProduksiID != "PRD-2026-0001" {
		t.Fatalf("ProduksiID = %q, want PRD-2026-0001", row.ProduksiID)
	}
	if row.UnitID != "UNT-2026-0001" || row.Driver != "Slamet" || row.JenisDT != "DT KECIL" {
		t.Fatalf("unit data not copied: %+v", row)
	}
	if row.Panjang != 375 || row.Lebar != 190 || row.Tinggi != 150 {
		t.Fatalf("dimensions not copied: %+v", row)
	}
	if row.TF != 150 || row.Volume != 10.6875 || row.VolumeOPP != 10 || row.Deviasi != 0.6875 {
		t.Fatalf("calculation wrong: TF=%v volume=%v opp=%v deviasi=%v", row.TF, row.Volume, row.VolumeOPP, row.Deviasi)
	}
	if len(store.ProduksiList()) != 1 {
		t.Fatalf("stored %d rows, want 1", len(store.ProduksiList()))
	}
}

func TestCreateProduksiRejectsUnknownNopol(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)

	input := validProduksiInput()
	input.Nopol = "B 9999 ZZZ"
	if _, err := produksi.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if len(store.ProduksiList()) != 0 {
		t.Fatal("row stored for an unregistered unit")
	}
}

// The pickers are creatable, so every one of them still has to be filled in,
// but an unfamiliar value is no longer a reason to refuse the row.
func TestCreateProduksiRequiresEveryPicker(t *testing.T) {
	produksi, _, user := newProduksiFixture(t)

	for name, mutate := range map[string]func(*ProduksiInput){
		"project":  func(in *ProduksiInput) { in.Project = "" },
		"supplier": func(in *ProduksiInput) { in.Supplier = "  " },
		"quary":    func(in *ProduksiInput) { in.Quary = "" },
		"kategori": func(in *ProduksiInput) { in.Kategori = " " },
		"layer":    func(in *ProduksiInput) { in.Layer = "" },
		"lokasi":   func(in *ProduksiInput) { in.Lokasi = "  " },
		"tanggal":  func(in *ProduksiInput) { in.Tanggal = "07/08/2026" },
		"tt":       func(in *ProduksiInput) { in.TT = "-1" },
	} {
		input := validProduksiInput()
		mutate(&input)
		if _, err := produksi.Create(context.Background(), user, input); !errors.Is(err, ErrValidation) {
			t.Fatalf("%s: err = %v, want ErrValidation", name, err)
		}
	}
}

// A value nobody has used before is accepted and kept as typed.
func TestCreateProduksiAcceptsNewOptionValues(t *testing.T) {
	produksi, _, user := newProduksiFixture(t)

	input := validProduksiInput()
	input.Project = "PCPM 2"
	input.Kategori = "Bongkar"
	input.Layer = "L9"
	input.Lokasi = "63+575"

	row, err := produksi.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.Project != "PCPM 2" || row.Kategori != "Bongkar" || row.Layer != "L9" || row.Lokasi != "63+575" {
		t.Fatalf("new values were not kept: %+v", row)
	}
}

// Once a spelling exists, a differently-cased entry adopts it. Otherwise
// "replace" and "Replace" become two categories and per-category totals split.
func TestCreateProduksiAdoptsExistingSpelling(t *testing.T) {
	produksi, _, user := newProduksiFixture(t)

	first := validProduksiInput()
	first.Kategori = "Bongkar"
	if _, err := produksi.Create(context.Background(), user, first); err != nil {
		t.Fatalf("first create: %v", err)
	}

	second := validProduksiInput()
	second.Kategori = "  bongkar  "
	second.Lokasi = "Blok A"
	row, err := produksi.Create(context.Background(), user, second)
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if row.Kategori != "Bongkar" {
		t.Fatalf("Kategori = %q, want the existing spelling Bongkar", row.Kategori)
	}
}

// The pickers offer what the sheet already holds, merged with the seeds.
func TestProduksiOptionsComeFromTheSheet(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)

	input := validProduksiInput()
	input.Kategori = "Bongkar"
	input.Lokasi = "63+575"
	if _, err := produksi.Create(context.Background(), user, input); err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = store

	options, err := produksi.Options(context.Background())
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	if !contains(options.Kategori, "Bongkar") {
		t.Fatalf("Kategori options %v do not include the value just used", options.Kategori)
	}
	// Seeds survive alongside whatever the sheet holds.
	for _, want := range []string{"Replace", "Timbunan", "Akses"} {
		if !contains(options.Kategori, want) {
			t.Fatalf("Kategori options %v lost the seed %q", options.Kategori, want)
		}
	}
	if !contains(options.Lokasi, "63+575") {
		t.Fatalf("Lokasi options %v do not include the value just used", options.Lokasi)
	}
	if !contains(options.Project, "PCPM") {
		t.Fatalf("Project options %v lost the seed", options.Project)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestCreateProduksiNormalisesOptionCasing(t *testing.T) {
	produksi, _, user := newProduksiFixture(t)

	input := validProduksiInput()
	input.Kategori = "replace"
	input.Layer = "l3"
	input.Nopol = "b 1234 abc"

	row, err := produksi.Create(context.Background(), user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if row.Kategori != "Replace" || row.Layer != "L3" || row.Nopol != "B 1234 ABC" {
		t.Fatalf("values not normalised: %+v", row)
	}
}

func TestProduksiIDsNumberSequentially(t *testing.T) {
	produksi, _, user := newProduksiFixture(t)

	for index, want := range []string{"PRD-2026-0001", "PRD-2026-0002", "PRD-2026-0003"} {
		row, err := produksi.Create(context.Background(), user, validProduksiInput())
		if err != nil {
			t.Fatalf("create %d: %v", index+1, err)
		}
		if row.ProduksiID != want {
			t.Fatalf("row %d id = %q, want %q", index+1, row.ProduksiID, want)
		}
	}
}

func TestProduksiTodayIsPreselected(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	if got := produksi.Today(); got != "2026-08-07" {
		t.Fatalf("Today() = %q, want 2026-08-07", got)
	}
}
