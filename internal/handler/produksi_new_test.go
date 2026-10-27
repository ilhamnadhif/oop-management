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
	// ranges records which ranges were asked for, days which detail days.
	ranges [][2]string
	days   []string
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

func (f *stubWidyaFeed) Detail(_ context.Context, day string, location *time.Location) (widya.UnitDetailReport, bool, error) {
	f.days = append(f.days, day)
	if f.err != nil && !f.has {
		return widya.UnitDetailReport{}, false, f.err
	}
	return widya.DetailUnits(f.tickets, day, location), true, f.err
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
		server.WithWidya(feed, testProjectName)
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
	if len(feed.ranges) != 0 || strings.Contains(page, "ritase-table") || strings.Count(page, "data-ritase-loading") != 4 {
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
		// Each refresh reads the summary's range and then the rekap's; the
		// summary's is the one before last.
		if got := feed.ranges[len(feed.ranges)-2]; got != [2]string{c.from, c.to} {
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

// The rekap lays the same range out by vendor and truck class, with the share
// of the volume each carried and the total at the foot.
func TestProduksiNewBreaksTheRangeDownByVendor(t *testing.T) {
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
	compact := strings.Join(strings.Fields(view.RekapHTML), " ")
	for _, fragment := range []string{
		"Vendor / Armada", "Porsi Vol", "1 – 24 Oktober 2026",
		// HPP: one DTT, two loads, 55.5 of the 67.5 m³.
		`<th scope="row">HPP</th> <td class="ritase-value">1</td> <td class="ritase-value">2</td> <td class="ritase-value">55.50</td> <td class="ritase-value">82.2%</td> <td class="ritase-value">27.75</td> <td class="ritase-value">2.00</td>`,
		// A class with nothing reads as a dash, greyed.
		`<tr class="ritase-child rekap-muted"> <th scope="row">DTK</th> <td class="ritase-value">0</td> <td class="ritase-value">0</td> <td class="ritase-value">0.00</td> <td class="ritase-value">0.0%</td> <td class="ritase-value">-</td>`,
		`<tr class="rekap-total"> <th scope="row">TOTAL</th> <td class="ritase-value">2</td> <td class="ritase-value">3</td> <td class="ritase-value">67.50</td> <td class="ritase-value">100.0%</td>`,
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("the rekap is missing %q\n%s", fragment, compact)
		}
	}
}

// The unit detail reads one day of its own: each truck's loads in order, a
// load in the midday break marked, a truck with one load flagged, and the day
// defaulting to today and never running past it.
func TestProduksiNewDetailsEachTruckForOneDay(t *testing.T) {
	feed := widyaFixture()
	feed.tickets = append(feed.tickets,
		widya.Ticket{Vehicle: "B 1 A", Supplier: "HPP", Volume: 27, ScanFullAt: time.Date(2026, 10, 24, 5, 15, 0, 0, time.UTC)})
	testServer := newProduksiNewServer(t, feed)
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
	if view.Day != "2026-10-24" || feed.days[len(feed.days)-2] != "2026-10-24" {
		t.Fatalf("detail day = %q, want today", view.Day)
	}
	compact := strings.Join(strings.Fields(view.DetailHTML), " ")
	for _, fragment := range []string{
		"24 Oktober 2026", "Rit 1", "Rit 3", ">Mulai 08:06 · selesai 12:15<", ">Jam operasi 08:06 – 12:15<",
		// B 1 A: 08:06, 09:10 and 12:15, the last in the break; 249 minutes
		// over two gaps rounds to 125.
		`<td class="ritase-value detail-time">08:06</td><td class="ritase-value detail-time">09:10</td><td class="ritase-value detail-time tone-peak">12:15</td> <td class="ritase-value">125</td>`,
		// B 2 A made one load: flagged, two empty rit cells, no interval.
		`<td class="ritase-value tone-bad-text">1</td>`,
		`<td class="ritase-value detail-time">09:40</td><td class="ritase-value detail-time"></td><td class="ritase-value detail-time"></td> <td class="ritase-value">-</td>`,
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("the detail is missing %q\n%s", fragment, compact)
		}
	}

	// Each refresh reads the detail's day and then the per-load list's; the
	// detail's is the one before last.
	fetchRitaseHTML(t, client, testServer.URL+"/produksi-new/overview/data?day=2026-10-30")
	if last := feed.days[len(feed.days)-2]; last != "2026-10-24" {
		t.Fatalf("a day to come read %q, want today", last)
	}
	fetchRitaseHTML(t, client, testServer.URL+"/produksi-new/overview/data?day=2026-10-18")
	if last := feed.days[len(feed.days)-2]; last != "2026-10-18" {
		t.Fatalf("the picked day read %q", last)
	}
}

// Every load of the detail day is listed under its truck and vendor, keyed by
// name so the page can fold them and keep them folded across a refresh.
func TestProduksiNewListsEveryLoad(t *testing.T) {
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
	compact := strings.Join(strings.Fields(view.RitHTML), " ")
	for _, fragment := range []string{
		"Vendor / Unit / Waktu", "Rit ke",
		`<tr class="rit-vendor" data-rit-row="vendor" data-key="v:HPP">`,
		`<button type="button" class="rit-toggle" data-rit-toggle="v:HPP" aria-expanded="true"><span class="rit-caret" aria-hidden="true"></span>HPP</button> </th> <td class="ritase-value">2</td> <td class="ritase-value">55.50</td>`,
		`<tr class="rit-unit" data-rit-row="unit" data-key="v:HPP|B 1 A" data-parent="v:HPP">`,
		`<td class="ritase-value">DTT</td>`,
		// The first load of B 1 A, to the second.
		`<tr class="rit-load" data-rit-row="load" data-parent="v:HPP|B 1 A"> <th scope="row"> 08:06:00 </th> <td class="ritase-value">1</td> <td class="ritase-value">27.50</td>`,
	} {
		if !strings.Contains(compact, fragment) {
			t.Fatalf("the per-load list is missing %q\n%s", fragment, compact)
		}
	}
}

// The per-load list has a day of its own, apart from the unit detail's.
func TestProduksiNewListsLoadsForItsOwnDay(t *testing.T) {
	feed := widyaFixture()
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/produksi-new/overview/data?day=2026-10-24&rit_day=2026-10-18")
	if err != nil {
		t.Fatalf("get data: %v", err)
	}
	defer response.Body.Close()
	var view ProduksiNewView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.Day != "2026-10-24" || view.RitDay != "2026-10-18" {
		t.Fatalf("days = %q and %q", view.Day, view.RitDay)
	}
	if !strings.Contains(view.DetailHTML, "24 Oktober 2026") || !strings.Contains(view.RitHTML, "18 Oktober 2026") {
		t.Fatal("the two tables do not read their own days")
	}
	// Nothing was hauled on the 18th in the fixture.
	if !strings.Contains(view.RitHTML, "Tidak ada rit pada tanggal ini.") || strings.Contains(view.RitHTML, "B 1 A") {
		t.Fatal("the per-load list read the detail's day")
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview?rit_day=2026-10-18")
	if !strings.Contains(page, `name="rit_day" type="date" value="2026-10-18"`) {
		t.Fatal("the page does not carry the per-load day")
	}
}

// The rekap reads a range of its own, apart from the summary's.
func TestProduksiNewRekapHasItsOwnRange(t *testing.T) {
	feed := widyaFixture()
	testServer := newProduksiNewServer(t, feed)
	client := loggedInClient(t, testServer)

	response, err := client.Get(testServer.URL + "/produksi-new/overview/data?from=2026-10-01&to=2026-10-24&rekap_from=2026-10-18&rekap_to=2026-10-18")
	if err != nil {
		t.Fatalf("get data: %v", err)
	}
	defer response.Body.Close()
	var view ProduksiNewView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if view.From != "2026-10-01" || view.RekapFrom != "2026-10-18" || view.RekapTo != "2026-10-18" {
		t.Fatalf("ranges = %s-%s and %s-%s", view.From, view.To, view.RekapFrom, view.RekapTo)
	}
	if !strings.Contains(view.RitaseHTML, "1 – 24 Oktober 2026") || !strings.Contains(view.RekapHTML, "18 Oktober 2026") {
		t.Fatal("the summary and rekap do not read their own ranges")
	}
	// Nothing hauled on the 18th: the rekap is its total alone.
	if strings.Contains(view.RekapHTML, "HPP") {
		t.Fatal("the rekap read the summary's range")
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if !strings.Contains(page, `name="rekap_from" type="date" value="2026-10-01"`) ||
		!strings.Contains(page, `name="rekap_to" type="date" value="2026-10-24"`) {
		t.Fatal("the rekap range does not default to the month so far")
	}
}

// The scanner's tickets are kept in one project's spreadsheet, so Produksi
// New shows them there and, anywhere else, says which project has them.
func TestProduksiNewBelongsToOneProject(t *testing.T) {
	stores := newTestStores(repository.NewTestRepository())
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 10, 24, 9, 0, 0, 0, location)
	deps := testDepsWithStores(t, stores, location, func() time.Time { return now }, defaultTestBranding())
	if _, err := deps.Projects.Create(context.Background(), secondProjectName, "kendal-spreadsheet", nil, nil); err != nil {
		t.Fatalf("create second project: %v", err)
	}
	server, err := NewServer(deps)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	server.WithWidya(widyaFixture(), testProjectName)
	testServer := startFixtureServer(t, server, deps)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if !strings.Contains(page, ">6.803<") {
		t.Fatalf("%s does not show the scanner", testProjectName)
	}

	switchProject(t, client, testServer, csrfFromForm(t, page), secondProjectName)
	page = fetchAuthedPage(t, client, testServer.URL+"/produksi-new/overview")
	if strings.Contains(page, ">6.803<") || !strings.Contains(page, "terhubung ke project <strong>"+testProjectName+"</strong>") {
		t.Fatalf("%s shows another project's scanner", secondProjectName)
	}
}
