package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedProduksi(t *testing.T, store *repository.TestRepository, tanggal, nopol, jenis string, volume, opp float64) {
	t.Helper()
	row := &model.Produksi{Tanggal: tanggal, Nopol: nopol, JenisDT: jenis, Volume: volume, VolumeOPP: opp}
	if err := store.CreateProduksi(context.Background(), row); err != nil {
		t.Fatalf("seed produksi: %v", err)
	}
}

func TestOverviewPageShowsTotalsAndCharts(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 10.5, 10)
	seedProduksi(t, store, "2026-06-02", "B 4321 XYZ", "DT BESAR", 30, 28)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	for _, fragment := range []string{
		"TOTAL PRODUKSI", "TOTAL RITASE", "UNIT DT AKTIF", "STOCK FUEL",
		"40.50", // total volume
		">2<",   // ritase and active units
		// Unfiltered, the charts group by month.
		"PRODUKSI PER BULAN",
		"RITASE DT KECIL VS DT BESAR",
		"UNIT AKTIF PER BULAN",
		"VOLUME VS VOLUME OPP",
		"Volume Real",
		"Volume OPP",
		"TOP 5 DT PALING PRODUKTIF",
		`class="chart"`,
		`class="chart-bar`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("overview page missing %q", fragment)
		}
	}

	// The per-ritase column must not claim to be per hour: the sheet has no
	// working hours to divide by.
	if strings.Contains(page, "JAM") || strings.Contains(page, "jam</th>") {
		t.Fatal("overview still labels the ratio as per hour")
	}
	if !strings.Contains(page, "m³/rit") {
		t.Fatal("overview does not label the ratio as per ritase")
	}
}

// Charts are server-rendered SVG on purpose: the CSP forbids external scripts,
// so a charting library would silently fail to load.
func TestOverviewChartsAreInlineSVG(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-06-01", "B 1234 ABC", "DT KECIL", 10, 10)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	if !strings.Contains(page, "<svg class=\"chart\"") {
		t.Fatal("charts are not inline SVG")
	}
	for _, external := range []string{"cdn.jsdelivr.net", "chart.js", "unpkg.com", "d3js.org"} {
		if strings.Contains(page, external) {
			t.Fatalf("overview references the external resource %q, which the CSP blocks", external)
		}
	}
}

func TestOverviewFiltersByDateRange(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-06-01", "B 1 A", "DT KECIL", 11, 10)
	seedProduksi(t, store, "2026-07-01", "B 2 B", "DT KECIL", 22, 10)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview?from=2026-06-01&to=2026-06-30")

	if !strings.Contains(page, "11.00") {
		t.Fatal("filtered total is missing")
	}
	if strings.Contains(page, "33.00") {
		t.Fatal("rows outside the range were counted")
	}
	if !strings.Contains(page, `value="2026-06-01"`) || !strings.Contains(page, `value="2026-06-30"`) {
		t.Fatal("the filter inputs do not echo the applied range")
	}
}

func TestOverviewRejectsMalformedDateWithoutBreakingThePage(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview?from=01-06-2026")
	if !strings.Contains(page, "tidak valid") {
		t.Fatal("a malformed date produced no explanation")
	}
}

func TestOverviewRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(testServer.URL + "/produksi/overview")
	if err != nil {
		t.Fatalf("get overview: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}

// The overview is its own menu entry, and Absensi must stay first.
func TestOverviewHasItsOwnMenuEntry(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	nav := navSection(t, fetchAuthedPage(t, client, testServer.URL+"/produksi/overview"))

	if !strings.Contains(nav, `href="/produksi/overview"`) {
		t.Fatal("sidebar has no overview link")
	}
	if !strings.Contains(nav, ">Overview<") {
		t.Fatal("sidebar does not label the overview")
	}
	if strings.Index(nav, ">Absensi<") > strings.Index(nav, ">Overview<") {
		t.Fatal("Absensi is no longer first")
	}
}

// The comparison chart is two bars per period, not two bars overall, so it has
// to carry the same number of x labels as the other charts.
func TestOverviewCompareChartIsPlottedOverTime(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-06-01", "B 1 A", "DT KECIL", 12, 10)
	seedProduksi(t, store, "2026-08-01", "B 2 B", "DT BESAR", 30, 28)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	for _, fragment := range []string{
		"series-real", "series-opp",
		"Jun 2026 · Volume Real",
		"Jun 2026 · Volume OPP",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("compare chart missing %q", fragment)
		}
	}
}

// A chosen range switches every chart to daily buckets.
func TestOverviewSwitchesToDailyWhenFiltered(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-06-01", "B 1 A", "DT KECIL", 12, 10)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview?from=2026-06-01&to=2026-06-30")

	if !strings.Contains(page, "PRODUKSI PER TANGGAL") {
		t.Fatal("a filtered range must chart per day")
	}
	if strings.Contains(page, "PRODUKSI PER BULAN") {
		t.Fatal("a filtered range still charts per month")
	}
}

// The daily volume panel is a smoothed line with value badges, not bars.
func TestOverviewVolumePanelIsALineChart(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedProduksi(t, store, "2026-08-01", "B 1 A", "DT KECIL", 12, 10)
	seedProduksi(t, store, "2026-08-02", "B 2 B", "DT KECIL", 30, 10)

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview?from=2026-08-01&to=2026-08-31")

	for _, fragment := range []string{`class="chart-curve"`, `class="chart-dot"`, `class="chart-badge"`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("volume panel missing %q", fragment)
		}
	}
	// Curved, not a polyline of straight segments.
	if !strings.Contains(page, `d="M `) || !strings.Contains(page, " C ") {
		t.Fatal("the volume line is not drawn as a curve")
	}
}
