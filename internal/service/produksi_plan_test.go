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

func seedPlan(t *testing.T, store *repository.TestRepository, lokasi string, volume float64) {
	t.Helper()
	plan := &model.ProduksiPlan{
		PlanID: "PLN-20260701-000" + lokasi[:1], Tanggal: "2026-07-01",
		Project: "PCPM", Supplier: "HPP", Lokasi: lokasi, Volume: volume,
	}
	if err := store.CreateProduksiPlan(context.Background(), plan); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
}

func newPlanFixture(t *testing.T) (*ProduksiService, *repository.TestRepository, *model.User) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	user := &model.User{UserID: "usr_1", NamaLengkap: "Budi", StatusPengguna: model.StatusAktif}
	return NewProduksiService(store, location, func() time.Time { return now }), store, user
}

func planInput() ProduksiPlanInput {
	return ProduksiPlanInput{
		Tanggal: "2026-07-01", Project: "PCPM", Supplier: "HPP",
		Lokasi: "Segmen 1c STA 62+950 - 63+050", Volume: "15000",
	}
}

func TestCreatePlanDerivesIDAndAudit(t *testing.T) {
	service, store, user := newPlanFixture(t)
	ctx := context.Background()

	first, err := service.CreatePlan(ctx, user, planInput())
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	second := planInput()
	second.Lokasi = "Segmen 1a STA 62+750 - 62+900"
	next, err := service.CreatePlan(ctx, user, second)
	if err != nil {
		t.Fatalf("create second plan: %v", err)
	}

	if first.PlanID != "PLN-20260807-0001" || next.PlanID != "PLN-20260807-0002" {
		t.Fatalf("unexpected identifiers: %q, %q", first.PlanID, next.PlanID)
	}
	if first.Volume != 15000 || first.CreatedByID != user.UserID || first.CreatedAt.IsZero() {
		t.Fatalf("plan is missing derived values: %+v", first)
	}
	if got := len(store.ProduksiPlanList()); got != 2 {
		t.Fatalf("stored plans = %d, want 2", got)
	}

	// A location planned here has to appear in the picker the next form draws,
	// or the same place gets typed a second way and neither plan matches.
	options, err := service.Options(ctx)
	if err != nil {
		t.Fatalf("options: %v", err)
	}
	found := false
	for _, lokasi := range options.Lokasi {
		if lokasi == first.Lokasi {
			found = true
		}
	}
	if !found {
		t.Fatalf("the planned location is missing from the picker: %v", options.Lokasi)
	}
}

func TestCreatePlanValidatesDateLocationAndVolume(t *testing.T) {
	service, _, user := newPlanFixture(t)
	ctx := context.Background()

	cases := map[string]func(ProduksiPlanInput) ProduksiPlanInput{
		"bad date":      func(in ProduksiPlanInput) ProduksiPlanInput { in.Tanggal = "01-07-2026"; return in },
		"no location":   func(in ProduksiPlanInput) ProduksiPlanInput { in.Lokasi = "  "; return in },
		"no volume":     func(in ProduksiPlanInput) ProduksiPlanInput { in.Volume = ""; return in },
		"zero volume":   func(in ProduksiPlanInput) ProduksiPlanInput { in.Volume = "0"; return in },
		"minus volume":  func(in ProduksiPlanInput) ProduksiPlanInput { in.Volume = "-100"; return in },
		"volume letter": func(in ProduksiPlanInput) ProduksiPlanInput { in.Volume = "banyak"; return in },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := service.CreatePlan(ctx, user, mutate(planInput())); !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want validation", err)
			}
		})
	}

	// A decimal comma is what a local keyboard produces, and it is a number.
	comma := planInput()
	comma.Volume = "1500,5"
	plan, err := service.CreatePlan(ctx, user, comma)
	if err != nil {
		t.Fatalf("decimal comma rejected: %v", err)
	}
	if plan.Volume != 1500.5 {
		t.Fatalf("volume = %v, want 1500.5", plan.Volume)
	}
}

// The plan is a standing target, so narrowing the date range narrows what was
// produced and leaves the target where it is. Filtering both would move the
// goalposts every time someone changed the range.
func TestOverviewMeasuresLocationsAgainstThePlan(t *testing.T) {
	overview, store := newOverviewFixture(t)
	const segmen = "Segmen 1c STA 62+950 - 63+050"
	seedPlan(t, store, segmen, 15000)
	seedRow(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 9000, 10, segmen)
	seedRow(t, store, "2026-07-01", "B 1234 ABC", "DT KECIL", 3300, 10, segmen)

	all, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(all.LokasiShares) != 1 || !all.HasRencana {
		t.Fatalf("unexpected shares: %+v", all.LokasiShares)
	}
	share := all.LokasiShares[0]
	if share.Rencana != 15000 || share.Volume != 12300 || share.Capaian != 82 {
		t.Fatalf("achievement is wrong: %+v", share)
	}

	// June alone: the realisation shrinks, the target does not.
	june, err := overview.Build(context.Background(), "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatalf("build june: %v", err)
	}
	if got := june.LokasiShares[0]; got.Rencana != 15000 || got.Volume != 9000 || got.Capaian != 60 {
		t.Fatalf("filtered achievement is wrong: %+v", got)
	}
}

// A location that was planned but never worked is the gap the panel exists to
// show, so it appears at zero rather than being left out.
func TestOverviewKeepsPlannedLocationsWithNoProduction(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedPlan(t, store, "Segmen 1a STA 62+750 - 62+900", 15000)
	seedRow(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 500, 10, "Segmen lain")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var idle *LokasiShare
	for i, share := range result.LokasiShares {
		if strings.HasPrefix(share.Lokasi, "Segmen 1a") {
			idle = &result.LokasiShares[i]
		}
	}
	if idle == nil {
		t.Fatalf("the planned location vanished: %+v", result.LokasiShares)
	}
	if idle.Volume != 0 || idle.Capaian != 0 || !idle.AdaRencana {
		t.Fatalf("idle plan is wrong: %+v", idle)
	}
	// Planned locations lead, ordered by how far behind they are, because that
	// is what the panel is read for.
	if result.LokasiShares[0].Lokasi != idle.Lokasi {
		t.Fatalf("the location furthest behind is not first: %+v", result.LokasiShares)
	}
	// A location with no plan keeps its share reading rather than showing 0%.
	last := result.LokasiShares[len(result.LokasiShares)-1]
	if last.AdaRencana || last.Percent != 100 {
		t.Fatalf("the unplanned location lost its share: %+v", last)
	}
}
