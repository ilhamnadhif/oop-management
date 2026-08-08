package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func newOverviewFixture(t *testing.T) (*OverviewService, *repository.TestRepository) {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, location)
	return NewOverviewService(store, location, func() time.Time { return now }), store
}

func seedRow(t *testing.T, store *repository.TestRepository, tanggal, nopol, jenis string, volume, opp float64, lokasi string) {
	t.Helper()
	row := &model.Produksi{
		Tanggal: tanggal, Nopol: nopol, JenisDT: jenis,
		Volume: volume, VolumeOPP: opp, Lokasi: lokasi,
	}
	if err := store.CreateProduksi(context.Background(), row); err != nil {
		t.Fatalf("seed row: %v", err)
	}
}

func TestOverviewTotalsAndActiveUnits(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 10, 10, "")
	seedRow(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 12, 10, "")
	seedRow(t, store, "2026-06-02", "B 4321 XYZ", "DT BESAR", 30, 28, "")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.TotalVolume != 52 {
		t.Fatalf("TotalVolume = %v, want 52", result.TotalVolume)
	}
	if result.TotalOPP != 48 {
		t.Fatalf("TotalOPP = %v, want 48", result.TotalOPP)
	}
	if result.TotalRitase != 3 {
		t.Fatalf("TotalRitase = %d, want 3", result.TotalRitase)
	}
	// A plate hauling twice is still one unit.
	if result.ActiveUnits != 2 {
		t.Fatalf("ActiveUnits = %d, want 2", result.ActiveUnits)
	}
}

// With a range chosen the series drops to one point per day.
func TestOverviewSeriesIsOrderedAndSplitByJenis(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-03", "B 1 A", "DT BESAR", 28, 28, "")
	seedRow(t, store, "2026-06-01", "B 2 B", "DT KECIL", 10, 10, "")
	seedRow(t, store, "2026-06-01", "B 3 C", "dt besar", 27, 28, "")

	result, err := overview.Build(context.Background(), "2026-06-01", "2026-06-30")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Monthly {
		t.Fatal("a filtered range must group by day")
	}
	if len(result.Series) != 2 {
		t.Fatalf("series has %d points, want 2", len(result.Series))
	}
	if result.Series[0].Tanggal != "2026-06-01" || result.Series[1].Tanggal != "2026-06-03" {
		t.Fatalf("series is not in date order: %+v", result.Series)
	}
	first := result.Series[0]
	if first.Kecil != 1 || first.Besar != 1 {
		t.Fatalf("jenis split wrong: kecil=%d besar=%d", first.Kecil, first.Besar)
	}
	if first.Units != 2 {
		t.Fatalf("units on 06-01 = %d, want 2", first.Units)
	}
	if first.Volume != 37 {
		t.Fatalf("volume on 06-01 = %v, want 37", first.Volume)
	}
}

// The bounds are inclusive: a haul on the last day of the range belongs to it.
func TestOverviewDateRangeIsInclusive(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-05-31", "B 1 A", "DT KECIL", 5, 10, "")
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 10, 10, "")
	seedRow(t, store, "2026-06-05", "B 1 A", "DT KECIL", 20, 10, "")
	seedRow(t, store, "2026-06-06", "B 1 A", "DT KECIL", 40, 10, "")

	result, err := overview.Build(context.Background(), "2026-06-01", "2026-06-05")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.TotalRitase != 2 || result.TotalVolume != 30 {
		t.Fatalf("range filtering wrong: ritase=%d volume=%v", result.TotalRitase, result.TotalVolume)
	}
}

// A reversed range would otherwise return nothing, which reads as "no data"
// instead of "the dates are the wrong way round".
func TestOverviewSwapsReversedRange(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-02", "B 1 A", "DT KECIL", 10, 10, "")

	result, err := overview.Build(context.Background(), "2026-06-05", "2026-06-01")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.From != "2026-06-01" || result.To != "2026-06-05" {
		t.Fatalf("range not swapped: %s..%s", result.From, result.To)
	}
	if result.TotalRitase != 1 {
		t.Fatalf("TotalRitase = %d, want 1", result.TotalRitase)
	}
}

func TestOverviewRejectsMalformedDates(t *testing.T) {
	overview, _ := newOverviewFixture(t)
	if _, err := overview.Build(context.Background(), "01/06/2026", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if _, err := overview.Build(context.Background(), "", "kemarin"); !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
}

func TestOverviewTopUnitsRankByVolume(t *testing.T) {
	overview, store := newOverviewFixture(t)
	for i := 0; i < 6; i++ {
		plate := string(rune('A'+i)) + " 1000 XY"
		for j := 0; j <= i; j++ {
			seedRow(t, store, "2026-06-01", plate, "DT KECIL", 10, 10, "")
		}
	}

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(result.TopUnits) != 5 {
		t.Fatalf("TopUnits has %d entries, want 5", len(result.TopUnits))
	}
	if result.TopUnits[0].Ritase != 6 || result.TopUnits[0].Volume != 60 {
		t.Fatalf("top entry wrong: %+v", result.TopUnits[0])
	}
	for i := 1; i < len(result.TopUnits); i++ {
		if result.TopUnits[i-1].Volume < result.TopUnits[i].Volume {
			t.Fatalf("top units are not ordered by volume: %+v", result.TopUnits)
		}
	}
	// m3 per ritase, not per hour: the sheet holds no working hours at all.
	if result.TopUnits[0].VolumePerRitase != 10 {
		t.Fatalf("VolumePerRitase = %v, want 10", result.TopUnits[0].VolumePerRitase)
	}
}

func TestOverviewLokasiShares(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 30, 10, "Blok A")
	seedRow(t, store, "2026-06-01", "B 2 B", "DT KECIL", 10, 10, "Blok B")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !result.HasLokasi {
		t.Fatal("HasLokasi is false despite named locations")
	}
	if len(result.LokasiShares) != 2 || result.LokasiShares[0].Lokasi != "Blok A" {
		t.Fatalf("shares wrong: %+v", result.LokasiShares)
	}
	if result.LokasiShares[0].Percent != 75 {
		t.Fatalf("first share = %v%%, want 75", result.LokasiShares[0].Percent)
	}
}

// Rows imported without a location must not claim one.
func TestOverviewFlagsMissingLokasi(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 10, 10, "")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.HasLokasi {
		t.Fatal("HasLokasi is true although every row has an empty location")
	}
}

// Reading thousands of rows on every dashboard hit would make the page crawl.
func TestOverviewCachesTheSheetRead(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 10, 10, "")

	if _, err := overview.Build(context.Background(), "", ""); err != nil {
		t.Fatalf("first build: %v", err)
	}
	seedRow(t, store, "2026-06-02", "B 2 B", "DT KECIL", 99, 10, "")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("second build: %v", err)
	}
	if result.TotalRitase != 1 {
		t.Fatalf("TotalRitase = %d, want the cached 1", result.TotalRitase)
	}
}

// Without a range the charts group by month: plotting every day since records
// began is unreadable.
func TestOverviewGroupsByMonthWhenUnfiltered(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 10, 10, "")
	seedRow(t, store, "2026-06-20", "B 2 B", "DT KECIL", 15, 10, "")
	seedRow(t, store, "2026-08-02", "B 1 A", "DT BESAR", 28, 28, "")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !result.Monthly {
		t.Fatal("an unfiltered overview must group by month")
	}
	// June, July and August: July recorded nothing but must still appear, or the
	// chart implies production ran without a break.
	if len(result.Series) != 3 {
		t.Fatalf("series has %d points, want 3 months", len(result.Series))
	}
	if result.Series[0].Tanggal != "2026-06" || result.Series[0].Label != "Jun 2026" {
		t.Fatalf("first bucket = %+v", result.Series[0])
	}
	if result.Series[0].Volume != 25 || result.Series[0].Units != 2 {
		t.Fatalf("June aggregation wrong: %+v", result.Series[0])
	}
	if result.Series[1].Tanggal != "2026-07" || result.Series[1].Volume != 0 {
		t.Fatalf("the empty month is missing or not empty: %+v", result.Series[1])
	}
	if result.Series[2].Tanggal != "2026-08" || result.Series[2].Besar != 1 {
		t.Fatalf("August aggregation wrong: %+v", result.Series[2])
	}
}

// The comparison chart plots both figures over time, so each bucket carries its
// own nominal total alongside the realised one.
func TestOverviewSeriesCarriesVolumeOPP(t *testing.T) {
	overview, store := newOverviewFixture(t)
	seedRow(t, store, "2026-06-01", "B 1 A", "DT KECIL", 12, 10, "")
	seedRow(t, store, "2026-06-02", "B 2 B", "DT BESAR", 30, 28, "")

	result, err := overview.Build(context.Background(), "", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if result.Series[0].Volume != 42 || result.Series[0].VolumeOPP != 38 {
		t.Fatalf("bucket totals wrong: %+v", result.Series[0])
	}
}
