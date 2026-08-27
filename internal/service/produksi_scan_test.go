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
		Rows: []tally.Row{
			{Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS",
				Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: 0.2},
			{Nomor: 2, Project: "PCPM", Supplier: "HPP", Quary: "HS",
				Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "b 1234 abc"},
			// A machine nobody registered.
			{Nomor: 3, Project: "PCPM", Supplier: "HPP", Quary: "HS",
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

// A commit without a date is refused before anything is read or fingerprinted,
// because the sheet has to stay fileable once the date is supplied.
func TestCommitScanRefusesASheetWithoutADate(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}

	for _, tanggal := range []string{"", "   ", "07/08/2026"} {
		commit := ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: tanggal}
		if _, err := produksi.CommitScan(context.Background(), user, commit); !errors.Is(err, ErrValidation) {
			t.Fatalf("CommitScan(%q) error = %v, want %v", tanggal, err, ErrValidation)
		}
	}
	if rows, _ := store.ListProduksi(context.Background()); len(rows) != 0 {
		t.Fatalf("a dateless commit stored %d rows", len(rows))
	}
	// Nothing was filed, so the same photograph must still be fileable.
	if scans := store.ProduksiScanList(); len(scans) != 0 {
		t.Fatalf("a refused commit was logged: %+v", scans)
	}
}

// One sheet is one day, and the day is the one that was typed.
func TestCommitScanStampsEveryRowWithTheTypedDate(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}

	commit := ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-05"}
	if _, err := produksi.CommitScan(context.Background(), user, commit); err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	rows, _ := store.ListProduksi(context.Background())
	if len(rows) != 2 {
		t.Fatalf("stored %d rows, want 2", len(rows))
	}
	for _, row := range rows {
		if row.Tanggal != "2026-08-05" {
			t.Fatalf("row dated %q, want the typed date", row.Tanggal)
		}
	}
}

// A vendor typed for the sheet speaks for every line on it. Left blank it
// changes nothing.
func TestCommitScanAppliesTheTypedVendorToEveryRow(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}

	commit := ScanCommit{
		Rows: preview.Rows, Foto: testSheetPhoto(),
		Tanggal: "2026-08-05", Supplier: "  Vendor  Baru ",
	}
	if _, err := produksi.CommitScan(context.Background(), user, commit); err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	rows, _ := store.ListProduksi(context.Background())
	for _, row := range rows {
		// Whitespace collapsed the way every other picker settles a value.
		if row.Supplier != "Vendor Baru" {
			t.Fatalf("supplier = %q, want the typed vendor", row.Supplier)
		}
	}
}

func TestCommitScanKeepsThePaperVendorWhenNoneIsTyped(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	preview, err := produksi.PrepareScan(context.Background(), scannedSheet())
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}

	commit := ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-05"}
	if _, err := produksi.CommitScan(context.Background(), user, commit); err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	rows, _ := store.ListProduksi(context.Background())
	for _, row := range rows {
		if row.Supplier != "HPP" {
			t.Fatalf("supplier = %q, want what the paper carried", row.Supplier)
		}
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

	result, err := produksi.CommitScan(context.Background(), user, ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07"})
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
	if _, err := produksi.CommitScan(context.Background(), user, ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07"}); err != nil {
		t.Fatalf("first commit: %v", err)
	}

	_, err = produksi.CommitScan(context.Background(), user, ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07"})
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
	if _, err := produksi.CommitScan(context.Background(), user, ScanCommit{Rows: preview.Rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07"}); err != nil {
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
		Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS",
		Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 0000 ZZ",
	}}

	result, err := produksi.CommitScan(context.Background(), user, ScanCommit{Rows: tampered, Foto: testSheetPhoto(), Tanggal: "2026-08-07"})
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

// The dialog lets the top-up height be corrected by hand, so the figure now
// arrives from a browser rather than from the reader. A negative height would
// shrink the load below the bed it was carried in.
func TestCommitScanRefusesATopUpHeightBelowZero(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	rows := []ScanRow{
		{Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
			Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: -5},
		{Nomor: 2, Project: "PCPM", Supplier: "HPP", Quary: "HS", Kategori: "Replace",
			Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: 0.2},
	}

	result, err := produksi.CommitScan(context.Background(), user, ScanCommit{
		Rows: rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07",
	})
	if err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	if result.Tersimpan != 1 || len(result.Dilewati) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Dilewati[0].Alasan, "TT") {
		t.Fatalf("the rejected row does not say why: %+v", result.Dilewati[0])
	}
	stored, _ := store.ListProduksi(context.Background())
	if len(stored) != 1 || stored[0].TT != 0.2 {
		t.Fatalf("stored = %+v", stored)
	}
}

// A corrected plate is looked up like any other: the dialog may say anything,
// and the register decides.
func TestPrepareScanFlagsATopUpHeightBelowZero(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	sheet := tally.Sheet{Rows: []tally.Row{{
		Nomor: 1, Project: "PCPM", Supplier: "HPP", Quary: "HS",
		Kategori: "Replace", Lokasi: "Blok A", Layer: "L1", Nopol: "B 1234 ABC", TT: -1,
	}}}

	preview, err := produksi.PrepareScan(context.Background(), sheet)
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if preview.Siap != 0 || !strings.Contains(preview.Rows[0].Alasan, "TT") {
		t.Fatalf("preview = %+v", preview)
	}
}

// The page is read top to bottom, and the No column is what says which line is
// which. A reader that hands the lines back out of order would file them out of
// order too, and the produksi ids would stop following the paper.
func TestPrepareScanPutsTheRowsBackInSheetOrder(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	sheet := tally.Sheet{Rows: []tally.Row{
		{Nomor: 3, Nopol: "B 1234 ABC", TT: 3},
		{Nomor: 1, Nopol: "B 1234 ABC", TT: 1},
		{Nomor: 2, Nopol: "B 1234 ABC", TT: 2},
	}}

	preview, err := produksi.PrepareScan(context.Background(), sheet)
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	for index, row := range preview.Rows {
		if row.Nomor != index+1 || row.TT != float64(index+1) {
			t.Fatalf("row %d = %+v", index, row)
		}
	}
}

// Numbering that is missing or repeated says nothing about the order, so the
// order the page was read in stands rather than being shuffled by a key that
// does not mean anything.
func TestPrepareScanKeepsReadingOrderWithoutUsableNumbers(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	tests := map[string][]tally.Row{
		"unnumbered": {
			{Nopol: "B 1234 ABC", TT: 1},
			{Nopol: "B 1234 ABC", TT: 2},
			{Nopol: "B 1234 ABC", TT: 3},
		},
		"repeated": {
			{Nomor: 2, Nopol: "B 1234 ABC", TT: 1},
			{Nomor: 2, Nopol: "B 1234 ABC", TT: 2},
			{Nomor: 1, Nopol: "B 1234 ABC", TT: 3},
		},
	}
	for name, rows := range tests {
		t.Run(name, func(t *testing.T) {
			preview, err := produksi.PrepareScan(context.Background(), tally.Sheet{Rows: rows})
			if err != nil {
				t.Fatalf("prepare scan: %v", err)
			}
			for index, row := range preview.Rows {
				if row.TT != float64(index+1) {
					t.Fatalf("row %d = %+v", index, row)
				}
			}
		})
	}
}

// Committing files them in the same order, so the produksi ids run down the
// page the way the lines do.
func TestCommitScanFilesTheRowsInSheetOrder(t *testing.T) {
	produksi, store, user := newProduksiFixture(t)
	rows := []ScanRow{
		{Nomor: 3, Nopol: "B 1234 ABC", TT: 3},
		{Nomor: 1, Nopol: "B 1234 ABC", TT: 1},
		{Nomor: 2, Nopol: "B 1234 ABC", TT: 2},
	}

	if _, err := produksi.CommitScan(context.Background(), user, ScanCommit{
		Rows: rows, Foto: testSheetPhoto(), Tanggal: "2026-08-07",
	}); err != nil {
		t.Fatalf("commit scan: %v", err)
	}
	stored, _ := store.ListProduksi(context.Background())
	if len(stored) != 3 {
		t.Fatalf("stored %d rows, want 3", len(stored))
	}
	for index, row := range stored {
		if row.TT != float64(index+1) {
			t.Fatalf("stored row %d = %+v", index, row)
		}
	}
}

// A plate typed the wrong shape is not a plate nobody registered. Saying it is
// sends the operator to the Unit DT register to look for something that was
// never going to be there.
func TestPrepareScanTellsAMalformedPlateFromAnUnknownOne(t *testing.T) {
	produksi, _, _ := newProduksiFixture(t)
	sheet := tally.Sheet{Rows: []tally.Row{
		{Nomor: 1, Nopol: "AB6990OE"},   // no spaces
		{Nomor: 2, Nopol: "AB 6990 OE"}, // well formed, simply not in the register
		{Nomor: 3, Nopol: ""},           // nothing at all
		{Nomor: 4, Nopol: "B 1234 ABC"}, // registered
	}}

	preview, err := produksi.PrepareScan(context.Background(), sheet)
	if err != nil {
		t.Fatalf("prepare scan: %v", err)
	}
	if !strings.Contains(preview.Rows[0].Alasan, "Format") {
		t.Fatalf("a misspelled plate reads as unregistered: %+v", preview.Rows[0])
	}
	if !strings.Contains(preview.Rows[1].Alasan, "belum terdaftar") {
		t.Fatalf("an unknown plate = %+v", preview.Rows[1])
	}
	if !strings.Contains(preview.Rows[2].Alasan, "wajib diisi") {
		t.Fatalf("an empty plate = %+v", preview.Rows[2])
	}
	if preview.Rows[3].Alasan != "" {
		t.Fatalf("a registered plate was rejected: %+v", preview.Rows[3])
	}
	// What was typed stays visible, so the operator can see what to correct.
	if preview.Rows[0].Nopol != "AB6990OE" {
		t.Fatalf("the typed plate was rewritten: %+v", preview.Rows[0])
	}
}
