package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/tally"
)

// sheetFromTheField is what the sample sheet actually says: a plate, a top-up
// height, and every option column left blank. The heights are 14 and 33, which
// is what the paper carries - the register's dimensions are centimetres, so a
// top-up is too. The date is not among them; it is typed on the dialog.
func sheetFromTheField() tally.Sheet {
	return tally.Sheet{
		Rows: []tally.Row{
			{Nomor: 1, Nopol: "AB 8698 GD", TT: 14},
			{Nomor: 2, Nopol: "AD 8590 FG", TT: 33},
		},
	}
}

// The sample report photographed off a real page, carried through the whole
// upload path: a 3508x2481 PNG, larger than anything a phone sends sideways.
func TestProduksiScanReadsTheSampleSheet(t *testing.T) {
	image, err := os.ReadFile("testdata/laporan-produksi.png")
	if err != nil {
		t.Fatalf("read sample sheet: %v", err)
	}

	scanner := &fakeTallyScanner{sheet: sheetFromTheField()}
	testServer, store := newTallyScanServer(t, scanner)
	for _, nopol := range []string{"AB 8698 GD", "AD 8590 FG"} {
		unit := &model.UnitDT{
			UnitID: "UNT-2026-" + nopol[3:7], Nopol: nopol,
			Panjang: 375, Lebar: 190, Tinggi: 150, Driver: "Slamet", Keterangan: "DT KECIL",
		}
		if err := store.CreateUnitDT(context.Background(), unit); err != nil {
			t.Fatalf("seed unit %s: %v", nopol, err)
		}
	}

	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))

	response := postSheetScan(t, client, testServer.URL+"/produksi/scan", csrf, map[string]string{}, image)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	var payload struct {
		OK      bool `json:"ok"`
		Siap    int  `json:"siap"`
		Ditolak int  `json:"ditolak"`
		Rows    []struct {
			Nopol  string  `json:"nopol"`
			TT     float64 `json:"tt"`
			Alasan string  `json:"alasan"`
		} `json:"rows"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !payload.OK || payload.Siap != 2 || payload.Ditolak != 0 {
		t.Fatalf("payload = %+v", payload)
	}
	// A top-up height the form itself would accept must not be thrown away here.
	if payload.Rows[0].TT != 14 || payload.Rows[1].TT != 33 {
		t.Fatalf("top-up heights were not carried: %+v", payload.Rows)
	}
	if payload.Rows[0].Alasan != "" || payload.Rows[1].Alasan != "" {
		t.Fatalf("a readable row was rejected: %+v", payload.Rows)
	}
	// The photo reached the model at full quality rather than shrunk for the
	// archive: a table this dense is unreadable once it has been squeezed into a
	// spreadsheet cell.
	if !strings.HasPrefix(scanner.dataURL, "data:image/") || len(scanner.dataURL) < 100000 {
		t.Fatalf("the model was sent %d characters", len(scanner.dataURL))
	}
}

// The same heights have to survive the commit, since the volume is computed
// from them.
func TestProduksiScanCommitKeepsTheSampleHeights(t *testing.T) {
	image, err := os.ReadFile("testdata/laporan-produksi.png")
	if err != nil {
		t.Fatalf("read sample sheet: %v", err)
	}
	testServer, store := newTallyScanServer(t, &fakeTallyScanner{sheet: sheetFromTheField()})
	for _, nopol := range []string{"AB 8698 GD", "AD 8590 FG"} {
		unit := &model.UnitDT{
			UnitID: "UNT-2026-" + nopol[3:7], Nopol: nopol,
			Panjang: 375, Lebar: 190, Tinggi: 150, Driver: "Slamet", Keterangan: "DT KECIL",
		}
		if err := store.CreateUnitDT(context.Background(), unit); err != nil {
			t.Fatalf("seed unit %s: %v", nopol, err)
		}
	}

	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi"))
	rows := `[{"no":1,"project":"","supplier":"","quary":"","kategori":"","lokasi":"","layer":"","nopol":"AB 8698 GD","tt":14},` +
		`{"no":2,"project":"","supplier":"","quary":"","kategori":"","lokasi":"","layer":"","nopol":"AD 8590 FG","tt":33}]`
	response := postSheetCommit(t, client, testServer.URL+"/produksi/scan/commit", csrf,
		map[string]string{"rows": rows, "tanggal": "2026-08-23"}, image)
	defer response.Body.Close()
	page := readBody(t, response)
	if !strings.Contains(page, "2 baris produksi tersimpan") {
		start := strings.Index(page, `class="alert`)
		snippet := page
		if start >= 0 && start+300 < len(page) {
			snippet = page[start : start+300]
		}
		t.Fatalf("the sample sheet did not store: %s", snippet)
	}

	stored, _ := store.ListProduksi(context.Background())
	if len(stored) != 2 {
		t.Fatalf("stored %d rows, want 2", len(stored))
	}
	// TF = 150 + 14/2 = 157; volume = 375 * 190 * 157 / 1e6.
	if stored[0].TT != 14 || stored[0].TF != 157 || stored[0].Volume != 11.1863 {
		t.Fatalf("first row = %+v", stored[0])
	}
	if stored[1].TT != 33 || stored[1].TF != 166.5 || stored[1].Volume != 11.8631 {
		t.Fatalf("second row = %+v", stored[1])
	}
	// The sheet left every option column blank, and blank is what is stored. A
	// date, a plate and a top-up height are enough to compute the load, so the
	// row is not thrown away over columns nobody filled in.
	if stored[0].Tanggal != "2026-08-23" {
		t.Fatalf("row dated %q, want the typed date", stored[0].Tanggal)
	}
	if stored[0].Lokasi != "" || stored[0].Layer != "" {
		t.Fatalf("a blank column was filled in: %+v", stored[0])
	}
	// The project is the exception: it is not read off the paper at all, but
	// stamped from the project this sheet is being filed into.
	if stored[0].Project != testProjectName {
		t.Fatalf("Project = %q, want %q", stored[0].Project, testProjectName)
	}
	// What the register holds is filled in regardless.
	if stored[0].Driver != "Slamet" || stored[0].JenisDT != "DT KECIL" || stored[0].Panjang != 375 {
		t.Fatalf("the register did not complete the row: %+v", stored[0])
	}
}
