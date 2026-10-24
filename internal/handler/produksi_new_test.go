package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"opp-management/internal/repository"
	"opp-management/internal/widya"
)

type stubWidyaFeed struct {
	snapshot widya.Snapshot
	has      bool
	err      error
	tickets  []widya.Ticket
	// ranges records which ranges were asked for.
	ranges [][2]string
}

func (f *stubWidyaFeed) Ritase(_ context.Context, from, to string, location *time.Location) (widya.RitaseSummary, bool, error) {
	f.ranges = append(f.ranges, [2]string{from, to})
	if f.err != nil && !f.has {
		return widya.RitaseSummary{}, false, f.err
	}
	return widya.Summarize(f.tickets, from, to, location), true, f.err
}

func (f *stubWidyaFeed) Latest(context.Context) (widya.Snapshot, bool, error) {
	return f.snapshot, f.has, f.err
}

func (f *stubWidyaFeed) MaxAge() time.Duration { return 30 * time.Second }

func newProduksiNewServer(t *testing.T, feed WidyaFeed) *httptest.Server {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, location)
	deps := testDeps(t, store, location, func() time.Time { return now }, defaultTestBranding())
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	if feed != nil {
		server.WithWidya(feed)
	}
	return startFixtureServer(t, server, deps)
}

func widyaFixture() *stubWidyaFeed {
	return &stubWidyaFeed{has: true, snapshot: widya.Snapshot{
		Stats: widya.Stats{Tickets: 6803, Vehicles: 324, Materials: 5, Suppliers: 8,
			Operators: 10, Units: 3, Users: 4},
		FetchedAt: time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC),
	}, tickets: []widya.Ticket{
		{Vehicle: "B 1 A", Supplier: "HPP", Volume: 27.5, ScanFullAt: time.Date(2026, 10, 24, 1, 6, 0, 0, time.UTC)},
		{Vehicle: "B 1 A", Supplier: "HPP", Volume: 28, ScanFullAt: time.Date(2026, 10, 24, 2, 10, 0, 0, time.UTC)},
		{Vehicle: "B 2 A", Supplier: "MNP", Volume: 12, ScanFullAt: time.Date(2026, 10, 24, 2, 40, 0, 0, time.UTC)},
	}}
}

// The overview shows the scanner's figures as they stand, grouped the way the
// rest of the app writes numbers, and the time they were read in site time.
func TestProduksiNewShowsTheScannerFigures(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	for _, fragment := range []string{
		"TOTAL TIKET", `data-widya-stat="tickets">6.803<`, `data-widya-stat="vehicles">324<`,
		`data-widya-stat="materials">5<`, `data-widya-stat="units">3<`,
		"SUMMARY RITASE", "data-ritase-loading", "Memuat tiket dari Widya",
		`name="from" type="date" value="2026-10-01"`, `name="to" type="date" value="2026-10-24"`,
		"24 Oct 2026 09:00:00", `data-refresh-ms="30000"`, "/static/js/produksi-new.js",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the produksi new overview is missing %q", fragment)
		}
	}
	// Four cards, one row: the scanner's own bookkeeping stays off the page.
	if strings.Count(page, "data-widya-stat=") != 4 || strings.Contains(page, "TOTAL SUPPLIER") {
		t.Fatal("the overview shows more than tiket, kendaraan, material and unit")
	}
	if !strings.Contains(page, `href="/produksi-new/overview"`) {
		t.Fatal("the sidebar does not link Produksi New")
	}
}

// The page refreshes itself from the same view, as JSON.
func TestProduksiNewDataServesTheSameView(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/produksi-new/overview/data")
	if err != nil {
		t.Fatalf("get data: %v", err)
	}
	defer response.Body.Close()
	var view ProduksiNewView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !view.Available || view.Stats[0].Key != "tickets" || view.Stats[0].Value != "6.803" {
		t.Fatalf("view = %+v", view)
	}
	if !strings.Contains(view.RitaseHTML, "ritase-table") || !strings.Contains(view.RitaseHTML, "HPP") {
		t.Fatalf("the refresh carries no ritase summary: %q", view.RitaseHTML)
	}
}

// A scanner that stops answering leaves the last figures on the page, with a
// warning saying they are the last reading.
func TestProduksiNewKeepsTheLastFiguresWhenTheScannerIsDown(t *testing.T) {
	feed := widyaFixture()
	feed.err = errors.New("down")
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if !strings.Contains(page, "pembacaan terakhir") || !strings.Contains(page, ">6.803<") {
		t.Fatal("a scanner outage blanked the figures or hid the warning")
	}
}

// Without an account the page says what to set rather than failing.
func TestProduksiNewSaysWhenItIsNotSetUp(t *testing.T) {
	testServer := newProduksiNewServer(t, nil)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if !strings.Contains(page, "WIDYA_URL") || strings.Contains(page, "data-widya ") {
		t.Fatal("an unconfigured page does not say what to set")
	}
}

// Produksi New is open to the positions Produksi is, and closed to the rest.
func TestProduksiNewFollowsTheProduksiPositions(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())

	surveyor := loggedInClientAs(t, testServer, "Surveyor")
	fetchAuthedPage(t, surveyor, testServer.URL+"/produksi-new/overview")

	logistik := loggedInClientAs(t, testServer, "Logistik")
	response, err := logistik.Get(testServer.URL + "/produksi-new/overview/data")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	body := readBody(t, response)
	if strings.Contains(body, "6.803") {
		t.Fatal("logistik read the produksi new figures")
	}
}

// The page itself goes out before the summary, which can be a month of
// tickets: the scanner is not asked for tickets until the script asks.
func TestProduksiNewOpensBeforeTheSummary(t *testing.T) {
	feed := widyaFixture()
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if len(feed.ranges) != 0 || strings.Contains(page, "ritase-table") {
		t.Fatal("the page waited for the tickets")
	}
	if !strings.Contains(page, ">6.803<") {
		t.Fatal("the page came without the figures")
	}
}

// fetchRitaseHTML reads the page's refresh and returns the summary it carries.
func fetchRitaseHTML(t *testing.T, client *http.Client, address string) string {
	t.Helper()
	response, err := client.Get(address)
	if err != nil {
		t.Fatalf("get %s: %v", address, err)
	}
	defer response.Body.Close()
	var view ProduksiNewView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode %s: %v", address, err)
	}
	return view.RitaseHTML
}

// The summary reads the range picked. Nothing picked is the month so far; an
// end past today stops at today, a range given backwards is turned round, and
// one longer than the cap keeps its end.
func TestProduksiNewReadsTheRangePicked(t *testing.T) {
	feed := widyaFixture()
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	page := fetchRitaseHTML(t, client, testServer.URL+"/produksi-new/overview/data?from=2026-10-18&to=2026-10-18")
	if !strings.Contains(page, "18 Oktober 2026") || !strings.Contains(page, "Tidak ada vendor aktif") {
		t.Fatal("the summary does not show the picked day, empty")
	}
	for _, c := range []struct{ query, from, to string }{
		{"", "2026-10-01", "2026-10-24"},
		{"?from=bukan&to=2026-10-30", "2026-10-01", "2026-10-24"},
		{"?from=2026-10-20&to=2026-10-10", "2026-10-10", "2026-10-20"},
		{"?from=2026-01-01&to=2026-10-24", "2026-08-24", "2026-10-24"},
	} {
		fetchRitaseHTML(t, client, testServer.URL+"/produksi-new/overview/data"+c.query)
		if got := feed.ranges[len(feed.ranges)-1]; got != [2]string{c.from, c.to} {
			t.Fatalf("%q read %v, want %s - %s", c.query, got, c.from, c.to)
		}
	}
}

// Vendors that hauled are green, a truck class with nothing is red, and the
// busiest hour is marked.
func TestProduksiNewColoursTheSummary(t *testing.T) {
	testServer := newProduksiNewServer(t, widyaFixture())
	client := loggedInClient(t, testServer)

	page := fetchRitaseHTML(t, client, testServer.URL+"/produksi-new/overview/data")
	for _, fragment := range []string{
		"1 – 24 Oktober 2026", "67.50", "08:06 – 09:40",
		`<th scope="row">HPP</th>
          <td class="ritase-value tone-good">Aktif</td>`,
		`<th scope="row">DTE</th>
          <td class="ritase-value tone-bad">0</td>`,
		`<td class="ritase-value tone-peak">09:00 – 10:00</td>`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the summary is missing %q", fragment)
		}
	}
}
