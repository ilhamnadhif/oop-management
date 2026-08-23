package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/tally"
)

// testSheetPhoto is one small JPEG, the way an upload arrives. The same bytes
// every time is what makes the duplicate test mean something.
func testSheetPhoto() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 200, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

func scannedSheet() tally.Sheet {
	return tally.Sheet{
		TanggalKepala: "2026-08-07",
		Rows: []tally.Row{
			// Its own date.
			{Nomor: 1, Tanggal: "2026-08-06", Project: "PCPM", Supplier: "HPP", Quary: "HS",
				Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: 0.2},
			// No date of its own, so the head of the sheet supplies one.
			{Nomor: 2, Project: "PCPM", Supplier: "HPP", Quary: "HS",
				Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "b 1234 abc"},
			// A machine nobody registered.
			{Nomor: 3, Tanggal: "2026-08-06", Project: "PCPM", Supplier: "HPP", Quary: "HS",
				Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 9021 XY"},
		},
		Warnings: []string{"baris 3 samar"},
	}
}

// The sheet is read once and judged here: which rows can be stored, which
// cannot, and why. Nothing is written yet.
func TestPrepareScanSortsStorableRowsFromTheRest(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)

	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if preview.Siap != 2 || preview.Ditolak != 1 {
		t.Fatalf("siap = %d ditolak = %d, want 2 and 1", preview.Siap, preview.Ditolak)
	}
	if preview.Rows[1].Tanggal != "2026-08-07" {
		t.Fatalf("an empty date cell did not fall back to the sheet: %+v", preview.Rows[1])
	}
	// The register spells the plate; the paper only points at it.
	if preview.Rows[1].Nopol != "B 1234 ABC" {
		t.Fatalf("nopol not settled from the register: %+v", preview.Rows[1])
	}
	if preview.Rows[0].Alasan != "" || preview.Rows[1].Alasan != "" {
		t.Fatalf("a storable row carries a reason: %+v", preview.Rows[:2])
	}
	if !strings.Contains(preview.Rows[2].Alasan, "Nopol") {
		t.Fatalf("the rejected row does not say why: %+v", preview.Rows[2])
	}
	if len(preview.Warnings) != 1 {
		t.Fatalf("warnings = %#v", preview.Warnings)
	}
}

// A row with no date on it and no date at the head of the sheet belongs to no
// day, and a guess would put a load on the wrong one.
func TestPrepareScanRejectsARowWithNoDateAnywhere(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	sheet := tally.Sheet{Rows: []tally.Row{{
		Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS",
		Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC",
	}}}

	preview, err := produksi.PrepareScan(context.Background(), sheet)
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if preview.Siap != 0 || !strings.Contains(preview.Rows[0].Alasan, "Tanggal") {
		t.Fatalf("preview = %+v", preview)
	}
}

// Committing stores every storable row in one go and hands back what it left
// behind, so the page can name the units that still need registering.
func TestCommitScanStoresTheStorableRowsAndReportsTheRest(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}

	result, err := produksi.CommitScan(context.Background(), user, preview.Rows, testSheetPhoto())
	if err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	if result.Tersimpan != 2 {
		t.Fatalf("tersimpan = %d, want 2", result.Tersimpan)
	}
	if len(result.Dilewati) != 1 || result.Dilewati[0].Nopol != "B 9021 XY" {
		t.Fatalf("dilewati = %+v", result.Dilewati)
	}

	rows, err := store.ListProduksi(context.Background())
	if err != nil {
		t.Fatalf("list produksi: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("stored %d rows, want 2", len(rows))
	}
	// Dimensions and volume come from the register, never from the paper.
	if rows[0].Panjang != 375 || rows[0].Volume <= 0 || rows[0].Driver != "Slamet" {
		t.Fatalf("row not completed from the register: %+v", rows[0])
	}
	if rows[0].ProduksiID == rows[1].ProduksiID {
		t.Fatalf("two rows share an id: %s", rows[0].ProduksiID)
	}
	if rows[0].CreatedBy != user.NamaLengkap {
		t.Fatalf("created_by = %q", rows[0].CreatedBy)
	}
}

// The same photograph filed twice is the same work counted twice. A unit can
// legitimately run the same route twice in a day, so it is the file that is
// refused, never the rows.
func TestCommitScanRefusesAPhotoAlreadyFiled(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if _, err := produksi.CommitScan(context.Background(), user, preview.Rows, testSheetPhoto()); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	_, err = produksi.CommitScan(context.Background(), user, preview.Rows, testSheetPhoto())
	if !errors.Is(err, ErrScanDuplicate) {
		t.Fatalf("second commit error = %v, want %v", err, ErrScanDuplicate)
	}
	rows, err := store.ListProduksi(context.Background())
	if err != nil {
		t.Fatalf("list produksi: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the refused commit still stored rows: %d", len(rows))
	}
}

// The log is what the duplicate check reads, so it has to carry the fingerprint,
// the counts, and who filed it.
func TestCommitScanLogsTheSheet(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if _, err := produksi.CommitScan(context.Background(), user, preview.Rows, testSheetPhoto()); err != nil {
		t.Fatalf("commit scan: %v", err)
	}

	scans := store.ProduksiScanList()
	if len(scans) != 1 {
		t.Fatalf("logged %d scans, want 1", len(scans))
	}
	scan := scans[0]
	if scan.ScanID == "" || len(scan.Sidik) != 64 {
		t.Fatalf("scan not fingerprinted: %+v", scan)
	}
	if scan.BarisMasuk != 2 || scan.BarisDitolak != 1 {
		t.Fatalf("counts = %d/%d, want 2/1", scan.BarisMasuk, scan.BarisDitolak)
	}
	if scan.DibuatOlehID != user.UserID || scan.Foto == "" {
		t.Fatalf("scan log incomplete: %+v", scan)
	}
}

// A commit is only ever as trusted as the register: rows come back from a
// browser, so the plate is looked up again rather than taken on its word.
func TestCommitScanRevalidatesRowsItIsHandedBack(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	tampered := []ScanRow{{
		Nomor: 1, Tanggal: "2026-08-07", Project: "PCPM", Supplier: "HPP", Quary: "HS",
		Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 0000 ZZ",
	}}

	result, err := produksi.CommitScan(context.Background(), user, tampered, testSheetPhoto())
	if err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	if result.Tersimpan != 0 || len(result.Dilewati) != 1 {
		t.Fatalf("an unregistered plate was stored: %+v", result)
	}
	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 0 {
		t.Fatalf("stored %d rows, want none", len(rows))
	}
	_ = model.Produksi{}
}
