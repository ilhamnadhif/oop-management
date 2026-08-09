package handler

import (
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
)

func TestNotaOverviewHighlightsTheMoney(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Kertas", 2, 55000))
	seedNota(t, store, "NTA-20260808-0001", "2026-08-08", notaLine("Tinta", 1, 120000))
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/overview")
	for _, fragment := range []string{
		"TOTAL PENGELUARAN", "BELUM DIBAYAR", "SUDAH DIBAYAR", "RATA-RATA PER NOTA",
		// Money reads grouped wherever it appears.
		"230.000",
		"115.000",
		"2 nota pada periode ini",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the overview is missing %q", fragment)
		}
	}
}

// Everything unpaid is a reimbursement waiting on finance, so the page says so
// and points at the page that settles it.
func TestNotaOverviewPointsAtTheOutstandingWork(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Kertas", 2, 55000))
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/overview")
	if !strings.Contains(page, "menunggu rekonsiliasi") ||
		!strings.Contains(page, `href="/nota/rekonsiliasi"`) {
		t.Fatal("the overview does not point at the reconciliation page")
	}

	// With nothing outstanding the band disappears rather than reading "Rp 0".
	settled := &model.Nota{
		NotaID: "NTA-20260809-0001", Tanggal: "2026-08-09", PIC: "Sari",
		MetodePembayaran: model.NotaMetodeCA, StatusPembayaran: model.NotaStatusSudahDibayar,
		Kategori: "Operasional", SubKategori: "QHSE", Total: 50000,
	}
	if err := store.CreateNota(t.Context(), settled); err != nil {
		t.Fatalf("seed settled nota: %v", err)
	}
	filtered := fetchAuthedPage(t, client, testServer.URL+"/nota/overview?from=2026-08-09&to=2026-08-09")
	if strings.Contains(filtered, "menunggu rekonsiliasi") {
		t.Fatal("a period with nothing outstanding still asks for action")
	}
}

func TestNotaOverviewGroupsByMonthUntilTheRangeNarrows(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNota(t, store, "NTA-20260707-0001", "2026-07-07", notaLine("Kertas", 1, 50000))
	seedNota(t, store, "NTA-20260807-0001", "2026-08-07", notaLine("Tinta", 1, 120000))
	client := loggedInClient(t, testServer)

	monthly := fetchAuthedPage(t, client, testServer.URL+"/nota/overview")
	if !strings.Contains(monthly, "dikelompokkan per bulan") || !strings.Contains(monthly, "Jul 2026") {
		t.Fatal("an unfiltered overview is not grouped by month")
	}

	daily := fetchAuthedPage(t, client, testServer.URL+"/nota/overview?from=2026-08-01&to=2026-08-31")
	if !strings.Contains(daily, "dikelompokkan per hari") {
		t.Fatal("a filtered overview is not grouped by day")
	}
	// The July nota is outside the range, so its spending must not be counted.
	if strings.Contains(daily, "170.000") {
		t.Fatal("the filtered overview counted a nota outside the range")
	}
}

func TestNotaOverviewRejectsBadDatesAndRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/nota/overview?from=07-08-2026")
	if !strings.Contains(page, "tanggal awal tidak valid") {
		t.Fatal("a malformed date is not reported")
	}

	anonymous := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := anonymous.Get(testServer.URL + "/nota/overview")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}
