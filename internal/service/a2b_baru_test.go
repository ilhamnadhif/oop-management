package service

import (
	"context"
	"math"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// The overview adds the workbook's figures up the workbook's way: the order
// table's deviation, stock as received less dispensed, and the ratio over
// only the fills that had hours, month by month.
func TestA2BBaruOverviewMatchesTheWorkbook(t *testing.T) {
	ctx := context.Background()
	store := repository.NewTestRepository()
	_ = store.AppendA2BBaruUnits(ctx, []model.A2BBaruUnit{{IDUnit: "DZOPP-006", NamaUnit: "Dozer"}, {IDUnit: "GSTOPP-01", NamaUnit: "Genset 5 KVA"}})
	_ = store.AppendA2BBaruFuelMasuk(ctx, []model.A2BBaruFuelMasuk{
		{Phase: "PO-1", Tanggal: "2026-05-20", POVolume: 8000, POHargaSatuan: 17928, SiteVolume: 8010, SiteHargaSatuan: 17928},
		{Phase: "PO-2", Tanggal: "2026-06-29", POVolume: 8000, POHargaSatuan: 21800, SiteVolume: 7724, SiteHargaSatuan: 21800},
	})
	_ = store.AppendA2BBaruFuelKeluar(ctx, []model.A2BBaruFuelKeluar{
		{Tanggal: "2026-09-10", IDUnit: "DZOPP-006", FuelLiter: 208, JamKerja: 0},
		{Tanggal: "2026-09-11", IDUnit: "DZOPP-006", FuelLiter: 200, JamKerja: 10},
		{Tanggal: "2026-10-01", IDUnit: "DZOPP-006", FuelLiter: 150, JamKerja: 15},
		{Tanggal: "2026-10-02", IDUnit: "GSTOPP-01", FuelLiter: 13},
		// A workbook line with no unit and no readings, kept as it is.
		{Tanggal: "2026-07-03", NamaUnit: "Pengisian Solar"},
	})
	view, err := NewA2BBaruService(store, time.UTC, nil).Overview(ctx, A2BBaruOverviewFilter{})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if view.Units != 2 || view.FuelDiterima != 15734 || view.FuelKeluar != 571 || view.Stock != 15163 {
		t.Fatalf("figures = units %d in %v out %v stock %v", view.Units, view.FuelDiterima, view.FuelKeluar, view.Stock)
	}
	if view.Totals.DevVolume != 266 || view.Totals.DevJumlah != -179280+6016800 {
		t.Fatalf("deviation = %v L, Rp %v", view.Totals.DevVolume, view.Totals.DevJumlah)
	}
	if len(view.Months) != 2 || view.Months[0] != "2026-09" || len(view.Ratios) != 2 {
		t.Fatalf("months %v ratios %+v", view.Months, view.Ratios)
	}
	dozer := view.Ratios[0]
	if dozer.FuelKeluar != 558 || dozer.FuelRatio != 350 || dozer.JamKerja != 25 || !dozer.HasRatio || dozer.Ratio != 14 {
		t.Fatalf("dozer = %+v", dozer)
	}
	if !dozer.Monthly[0].HasRatio || dozer.Monthly[0].Ratio != 20 || dozer.Monthly[1].Ratio != 10 {
		t.Fatalf("dozer by month = %+v", dozer.Monthly)
	}
	if genset := view.Ratios[1]; genset.HasRatio || genset.FuelKeluar != 13 {
		t.Fatalf("genset = %+v", genset)
	}
}

// Each dispense is numbered within the day it was written, its litres are the
// flow meter's distance, and the readings are checked before anything is
// stored.
func TestA2BBaruFuelKeluarIsChecked(t *testing.T) {
	ctx := context.Background()
	store := repository.NewTestRepository()
	_ = store.AppendA2BBaruUnits(ctx, []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}})
	now := func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }
	service := NewA2BBaruService(store, time.UTC, now)
	input := A2BBaruFuelKeluarInput{Tanggal: "2026-11-02", IDUnit: "exc-01", HMAwalFM: "108905", HMAkhirFM: "109036",
		JamKerja: "13,6", Operator: "Langgeng", Foto: testPhoto(t)}

	for i, want := range []string{"FUELOUT-20261102-0001", "FUELOUT-20261102-0002"} {
		fill, warning, err := service.CreateFuelKeluar(ctx, nil, input)
		if err != nil || warning != "" || fill.FuelOutID != want || fill.IDUnit != "EXC-01" || fill.FuelLiter != 131 || fill.JamKerja != 13.6 {
			t.Fatalf("fill %d = %+v, %q, %v", i, fill, warning, err)
		}
		if ratio, ok := fill.Ratio(); !ok || math.Abs(ratio-9.632) > 0.001 {
			t.Fatalf("ratio = %v", ratio)
		}
	}

	bad := func(change func(*A2BBaruFuelKeluarInput)) A2BBaruFuelKeluarInput {
		copied := input
		change(&copied)
		return copied
	}
	for name, refused := range map[string]A2BBaruFuelKeluarInput{
		"backwards meter": bad(func(in *A2BBaruFuelKeluarInput) { in.HMAkhirFM = "100" }),
		"no photo":        bad(func(in *A2BBaruFuelKeluarInput) { in.Foto = "" }),
		"unknown unit":    bad(func(in *A2BBaruFuelKeluarInput) { in.IDUnit = "EXC-99" }),
		"bad date":        bad(func(in *A2BBaruFuelKeluarInput) { in.Tanggal = "02-11-2026" }),
		"no operator":     bad(func(in *A2BBaruFuelKeluarInput) { in.Operator = " " }),
	} {
		if _, _, err := service.CreateFuelKeluar(ctx, nil, refused); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if fills, _ := store.ListA2BBaruFuelKeluar(ctx); len(fills) != 2 || !fills[0].HasFoto || fills[0].Foto != "" {
		t.Fatalf("stored fills = %+v", fills)
	}
	if next, _ := service.NextFuelOutID(ctx); next != "FUELOUT-20261102-0003" {
		t.Fatalf("next = %s", next)
	}
	if last, _ := service.LastFlowMeter(ctx); last != 109036 {
		t.Fatalf("last flow meter = %v", last)
	}
}

func TestA2BBaruNextPhase(t *testing.T) {
	ctx := context.Background()
	store := repository.NewTestRepository()
	_ = store.AppendA2BBaruFuelMasuk(ctx, []model.A2BBaruFuelMasuk{{Phase: "PO-1"}, {Phase: "PO-7"}, {Phase: "Tambahan"}})
	if next, _ := NewA2BBaruService(store, time.UTC, nil).NextPhase(ctx); next != "PO-8" {
		t.Fatalf("next phase = %s", next)
	}
}

// Each table is narrowed by its own range, while the figures at the top stay
// the whole book.
func TestA2BBaruOverviewFiltersEachTable(t *testing.T) {
	ctx := context.Background()
	store := repository.NewTestRepository()
	_ = store.AppendA2BBaruUnits(ctx, []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}, {IDUnit: "BLD-01", NamaUnit: "Bulldozer"}})
	_ = store.AppendA2BBaruFuelMasuk(ctx, []model.A2BBaruFuelMasuk{
		{Phase: "PO-1", Tanggal: "2026-08-01", POVolume: 8000, SiteVolume: 8000},
		{Phase: "PO-2", Tanggal: "2026-10-01", POVolume: 8000, SiteVolume: 7800},
	})
	_ = store.AppendA2BBaruFuelKeluar(ctx, []model.A2BBaruFuelKeluar{
		{Tanggal: "2026-09-01", IDUnit: "EXC-01", FuelLiter: 100, JamKerja: 10},
		{Tanggal: "2026-10-01", IDUnit: "EXC-01", FuelLiter: 120, JamKerja: 10},
		{Tanggal: "2026-10-02", IDUnit: "BLD-01", FuelLiter: 200, JamKerja: 10},
	})
	view, err := NewA2BBaruService(store, time.UTC, nil).Overview(ctx, A2BBaruOverviewFilter{
		Orders: A2BBaruRange{From: "2026-09-01"},
		Ratios: A2BBaruRange{From: "2026-10-01", To: "2026-10-31", Units: []string{"exc-01"}},
		Fills:  A2BBaruRange{Units: []string{"BLD-01", "WT-01"}},
	})
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if view.Stock != 15800-420 || view.FuelKeluar != 420 || view.FuelDiterima != 15800 {
		t.Fatalf("the top figures were filtered: %+v", view)
	}
	if len(view.Orders) != 1 || view.Orders[0].Phase != "PO-2" || view.Totals.DevVolume != 200 {
		t.Fatalf("orders = %+v totals %+v", view.Orders, view.Totals)
	}
	if len(view.Ratios) != 1 || view.Ratios[0].IDUnit != "EXC-01" || view.Ratios[0].Ratio != 12 || len(view.Months) != 1 {
		t.Fatalf("ratios = %+v months %v", view.Ratios, view.Months)
	}
	if len(view.Fills) != 1 || view.Fills[0].IDUnit != "BLD-01" {
		t.Fatalf("fills = %+v", view.Fills)
	}
}
