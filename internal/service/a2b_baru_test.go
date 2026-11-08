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
	view, err := NewA2BBaruService(store, time.UTC, nil).Overview(ctx)
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

// Each dispense is numbered within the day it was written, and the readings
// are checked before anything is stored.
func TestA2BBaruFuelKeluarIsChecked(t *testing.T) {
	ctx := context.Background()
	store := repository.NewTestRepository()
	_ = store.AppendA2BBaruUnits(ctx, []model.A2BBaruUnit{{IDUnit: "EXC-01", NamaUnit: "Excavator"}})
	now := func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) }
	service := NewA2BBaruService(store, time.UTC, now)
	input := A2BBaruFuelKeluarInput{Tanggal: "2026-11-02", IDUnit: "exc-01", HMAwalFM: "108905", HMAkhirFM: "109031", FuelLiter: "131", JamKerja: "13,6", Operator: "Langgeng"}

	for i, want := range []string{"FUELOUT-20261102-0001", "FUELOUT-20261102-0002"} {
		fill, err := service.CreateFuelKeluar(ctx, nil, input)
		if err != nil || fill.FuelOutID != want || fill.IDUnit != "EXC-01" || fill.JamKerja != 13.6 {
			t.Fatalf("fill %d = %+v, %v", i, fill, err)
		}
		if ratio, ok := fill.Ratio(); !ok || math.Abs(ratio-9.632) > 0.001 {
			t.Fatalf("ratio = %v", ratio)
		}
	}

	backwards := input
	backwards.HMAkhirFM = "100"
	for name, bad := range map[string]A2BBaruFuelKeluarInput{
		"backwards meter": backwards,
		"no fuel":         {Tanggal: "2026-11-02", IDUnit: "EXC-01", Operator: "x"},
		"unknown unit":    {Tanggal: "2026-11-02", IDUnit: "EXC-99", FuelLiter: "5", Operator: "x"},
		"bad date":        {Tanggal: "02-11-2026", IDUnit: "EXC-01", FuelLiter: "5", Operator: "x"},
	} {
		if _, err := service.CreateFuelKeluar(ctx, nil, bad); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if fills, _ := store.ListA2BBaruFuelKeluar(ctx); len(fills) != 2 {
		t.Fatalf("stored %d fills, want 2", len(fills))
	}
	if next, _ := service.NextFuelOutID(ctx); next != "FUELOUT-20261102-0003" {
		t.Fatalf("next = %s", next)
	}
	if last, _ := service.LastFlowMeter(ctx); last != 109031 {
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
