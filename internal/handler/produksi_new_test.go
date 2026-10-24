package handler

import (
	"context"
	"encoding/json"
	"errors"
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
		Materials: []widya.MaterialVolume{
			{Name: "pasir", Volume: 342008.78},
			{Name: "sirtu", Volume: 38577.58},
		},
		FetchedAt: time.Date(2026, 10, 24, 2, 0, 0, 0, time.UTC),
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
		"VOLUME PER MATERIAL", "pasir", "342.009", "380.586",
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
	if len(view.Materials) != 2 || view.Materials[0].Percent != "89.9" {
		t.Fatalf("materials = %+v", view.Materials)
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
