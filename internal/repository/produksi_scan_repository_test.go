package repository

import (
	"context"
	"reflect"
	"testing"
	"time"

	"opp-management/internal/model"
)

// The photo is tens of thousands of base64 characters. Every lookup that only
// needs the fingerprint would pay for it if it sat anywhere but last.
func TestProduksiScanHeadersKeepThePhotoLast(t *testing.T) {
	want := []string{
		"scan_id", "sidik_sha256", "baris_masuk", "baris_ditolak",
		"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "foto_lembar",
	}
	if !reflect.DeepEqual(produksiScanHeaders, want) {
		t.Fatalf("produksiScanHeaders = %#v, want %#v", produksiScanHeaders, want)
	}
}

func TestProduksiScanRowRoundTripAndNarrowProjection(t *testing.T) {
	loc := testLocation(t)
	created := time.Date(2026, 8, 23, 9, 30, 0, 0, loc)
	scan := &model.ProduksiScan{
		ScanID:       "SCN-20260823-0001",
		Sidik:        "a3f9c1",
		BarisMasuk:   100,
		BarisDitolak: 5,
		DibuatOleh:   "Tommy",
		DibuatOlehID: "user-1",
		CreatedAt:    created,
		Foto:         "data:image/jpeg;base64,abc",
	}

	row := produksiScanToRow(scan)
	if len(row) != len(produksiScanHeaders) {
		t.Fatalf("produksiScanToRow width = %d, want %d", len(row), len(produksiScanHeaders))
	}
	parsed := rowToProduksiScan(row, loc)
	if parsed.ScanID != scan.ScanID || parsed.Sidik != scan.Sidik || parsed.Foto != scan.Foto {
		t.Fatalf("unexpected parsed scan: %+v", parsed)
	}
	if parsed.BarisMasuk != 100 || parsed.BarisDitolak != 5 {
		t.Fatalf("counts not preserved: %+v", parsed)
	}
	if !parsed.CreatedAt.Equal(created) {
		t.Fatalf("created_at = %v, want %v", parsed.CreatedAt, created)
	}

	// The duplicate lookup reads up to G and never asks for the photo. A row
	// cut short there must still carry everything the check needs.
	projected := rowToProduksiScan(row[:7], loc)
	if projected.Sidik != scan.Sidik || projected.DibuatOleh != scan.DibuatOleh || projected.Foto != "" {
		t.Fatalf("projected scan = %+v", projected)
	}
}

// The same sheet photographed twice is two different files; the same file sent
// twice is what this refuses.
func TestTestRepositoryFindsAProduksiScanByFingerprint(t *testing.T) {
	store := NewTestRepository()
	scan := &model.ProduksiScan{
		ScanID: "SCN-20260823-0001", Sidik: "a3f9c1", BarisMasuk: 3,
		DibuatOleh: "Tommy", CreatedAt: time.Now(),
	}
	if err := store.CreateProduksiScan(context.Background(), scan); err != nil {
		t.Fatalf("create produksi scan: %v", err)
	}

	found, err := store.FindProduksiScan(context.Background(), "a3f9c1")
	if err != nil {
		t.Fatalf("find produksi scan: %v", err)
	}
	if found == nil || found.ScanID != "SCN-20260823-0001" {
		t.Fatalf("found = %+v", found)
	}

	missing, err := store.FindProduksiScan(context.Background(), "beefbeef")
	if err != nil {
		t.Fatalf("find missing produksi scan: %v", err)
	}
	if missing != nil {
		t.Fatalf("an unseen fingerprint matched: %+v", missing)
	}
}
