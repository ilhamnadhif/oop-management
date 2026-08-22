package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"opp-management/internal/model"
)

func postPlan(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string) *http.Response {
	t.Helper()
	values := url.Values{"csrf_token": {csrf}}
	for name, value := range fields {
		values.Set(name, value)
	}
	response, err := client.PostForm(testServer.URL+"/produksi/plan", values)
	if err != nil {
		t.Fatalf("post plan: %v", err)
	}
	return response
}

// escapedSegmen is how the template writes the location: Go escapes "+" to
// "&#43;" in HTML text, and the browser renders it back as a plus.
const escapedSegmen = "Segmen 1c STA 62&#43;950 - 63&#43;050"

func planFields() map[string]string {
	return map[string]string{
		"tanggal": "2026-07-01", "project": "PCPM", "supplier": "HPP",
		"lokasi": "Segmen 1c STA 62+950 - 63+050", "volume": "15000",
	}
}

// The submenu is how the page is found at all, so it is checked rather than
// assumed.
func TestProduksiPlanAppearsUnderProduksi(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi")

	for _, fragment := range []string{`href="/produksi/plan"`, "Input Plan"} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the produksi menu is missing %q", fragment)
		}
	}
}

func TestProduksiPlanSavesAndListsTheTarget(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi/plan"))

	response := postPlan(t, client, testServer, csrf, planFields())
	body := readBody(t, response)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", response.StatusCode, body)
	}
	if !strings.Contains(body, "tersimpan untuk "+escapedSegmen) {
		t.Fatalf("the save was not confirmed: %s", body)
	}
	// The stored plan is listed back on the same page, so nobody has to open
	// the sheet to see what has been set.
	for _, fragment := range []string{escapedSegmen, "15.000", "1 rencana"} {
		if !strings.Contains(body, fragment) {
			t.Fatalf("the plan list is missing %q: %s", fragment, body)
		}
	}

	plans := store.ProduksiPlanList()
	if len(plans) != 1 {
		t.Fatalf("stored plans = %d, want 1", len(plans))
	}
	if plans[0].Volume != 15000 || plans[0].Lokasi != "Segmen 1c STA 62+950 - 63+050" {
		t.Fatalf("stored plan is wrong: %+v", plans[0])
	}
	if !strings.HasPrefix(plans[0].PlanID, "PLN-") {
		t.Fatalf("plan id = %q", plans[0].PlanID)
	}
}

func TestProduksiPlanRejectsBadVolumeAndKeepsTyping(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	client := loggedInClient(t, testServer)
	csrf := csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/produksi/plan"))

	fields := planFields()
	fields["volume"] = "0"
	response := postPlan(t, client, testServer, csrf, fields)
	body := readBody(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", response.StatusCode, body)
	}
	if !strings.Contains(body, "volume rencana harus lebih dari nol") {
		t.Fatalf("the reason is not stated: %s", body)
	}
	// The location that was typed comes back, so the row is not retyped whole.
	if !strings.Contains(body, `value="`+escapedSegmen+`"`) {
		t.Fatalf("the typed values were discarded: %s", body)
	}
	if got := len(store.ProduksiPlanList()); got != 0 {
		t.Fatalf("a rejected plan was written: %d rows", got)
	}
}

// The point of the plan is the reading it gives the overview.
func TestProduksiOverviewShowsAchievementAgainstThePlan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	const segmen = "Segmen 1c STA 62+950 - 63+050"
	if err := store.CreateProduksiPlan(t.Context(), &model.ProduksiPlan{
		PlanID: "PLN-20260701-0001", Tanggal: "2026-07-01",
		Project: "PCPM", Supplier: "HPP", Lokasi: segmen, Volume: 15000,
	}); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if err := store.CreateProduksi(t.Context(), &model.Produksi{
		ProduksiID: "PRD-1", Tanggal: "2026-07-02", Nopol: "B 1234 ABC", JenisDT: "DT KECIL",
		Volume: 12300, VolumeOPP: 10, Lokasi: segmen,
	}); err != nil {
		t.Fatalf("seed produksi: %v", err)
	}

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	for _, fragment := range []string{
		"Produksi terhadap plan",
		escapedSegmen,
		"82.00%",
		"12.300 / 15.000 m³",
		// Two bars a row: the plan at full width, the realisation against it.
		`<rect class="lokasi-chart-actual"`,
		`<rect class="lokasi-chart-plan"`,
		"Plan (100%)",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the overview is missing %q", fragment)
		}
	}
	// 82% of the 458pt track, against a plan drawn the whole way across.
	if !strings.Contains(page, `width="375.6"`) || !strings.Contains(page, `width="458.0"`) {
		t.Fatalf("the bars are not drawn to scale: %s", page)
	}
}

// A plan that has been passed fills its track and keeps counting in the figure,
// because a bar has nowhere to go past the end and the overshoot is the point.
func TestProduksiOverviewClampsTheBarButNotTheFigure(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	const segmen = "Segmen 1a"
	if err := store.CreateProduksiPlan(t.Context(), &model.ProduksiPlan{
		PlanID: "PLN-20260701-0001", Tanggal: "2026-07-01",
		Project: "PCPM", Supplier: "HPP", Lokasi: segmen, Volume: 15000,
	}); err != nil {
		t.Fatalf("seed plan: %v", err)
	}
	if err := store.CreateProduksi(t.Context(), &model.Produksi{
		ProduksiID: "PRD-1", Tanggal: "2026-07-02", Nopol: "B 1234 ABC", JenisDT: "DT KECIL",
		Volume: 16000, VolumeOPP: 10, Lokasi: segmen,
	}); err != nil {
		t.Fatalf("seed produksi: %v", err)
	}

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	if !strings.Contains(page, "106.67%") {
		t.Fatal("the figure was clamped along with the bar")
	}
	// A met plan is drawn in its own colour so a full bar is not read as one
	// that merely happens to be long.
	if !strings.Contains(page, `class="lokasi-chart-actual lokasi-chart-full"`) {
		t.Fatalf("a met plan is not marked: %s", page)
	}
	// Four full-width bars: the location's plan and its overshooting
	// realisation, then the same pair again in the total row below it.
	if strings.Count(page, `width="458.0"`) != 4 {
		t.Fatalf("the overshooting bar does not stop at its track: %s", page)
	}
}

// Without any plan the panel keeps its original reading rather than showing
// every location at zero per cent.
func TestProduksiOverviewFallsBackToShareWithoutAPlan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	if err := store.CreateProduksi(t.Context(), &model.Produksi{
		ProduksiID: "PRD-1", Tanggal: "2026-07-02", Nopol: "B 1234 ABC", JenisDT: "DT KECIL",
		Volume: 500, VolumeOPP: 10, Lokasi: "Segmen tanpa rencana",
	}); err != nil {
		t.Fatalf("seed produksi: %v", err)
	}
	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	if !strings.Contains(page, "Porsi volume") || strings.Contains(page, "Produksi terhadap plan") {
		t.Fatal("the panel changed its reading without a plan to measure against")
	}
	if !strings.Contains(page, "belum ada plan") {
		t.Fatal("an unplanned location does not say so")
	}
}

func TestProduksiPlanNeedsASessionAndPermission(t *testing.T) {
	testServer := newTestServer(t)

	anonymous := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := anonymous.Get(testServer.URL + "/produksi/plan")
	if err != nil {
		t.Fatalf("get without a session: %v", err)
	}
	readBody(t, response)
	if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
		t.Fatalf("status without a session = %d, want a redirect", response.StatusCode)
	}

	// Access follows the Produksi menu, which HR is not part of.
	hr := loggedInClientAs(t, testServer, "HR")
	forbidden, err := hr.Get(testServer.URL + "/produksi/plan")
	if err != nil {
		t.Fatalf("get as HR: %v", err)
	}
	readBody(t, forbidden)
	if forbidden.StatusCode != http.StatusForbidden {
		t.Fatalf("HR opened the plan page: %d", forbidden.StatusCode)
	}

	authed := loggedInClient(t, testServer)
	forged := postPlan(t, authed, testServer, "not-the-token", planFields())
	readBody(t, forged)
	if forged.StatusCode != http.StatusForbidden {
		t.Fatalf("a forged token was accepted: %d", forged.StatusCode)
	}
}

// Per-location bars answer "which segment is behind"; the total row answers
// "is the job as a whole behind", which is a different question and the one
// asked first. Everything produced counts against the plan, including volume
// booked to a location nobody planned.
func TestProduksiOverviewTotalsAllProduksiAgainstAllPlan(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	plans := []struct {
		id, lokasi string
		volume     float64
	}{
		{"PLN-20260701-0001", "Segmen 1a", 15000},
		{"PLN-20260701-0002", "Segmen 1b", 10000},
	}
	for _, plan := range plans {
		if err := store.CreateProduksiPlan(t.Context(), &model.ProduksiPlan{
			PlanID: plan.id, Tanggal: "2026-07-01",
			Project: "PCPM", Supplier: "HPP", Lokasi: plan.lokasi, Volume: plan.volume,
		}); err != nil {
			t.Fatalf("seed plan: %v", err)
		}
	}
	rows := []struct {
		id, lokasi string
		volume     float64
	}{
		{"PRD-1", "Segmen 1a", 12300},
		{"PRD-2", "Segmen 1b", 8000},
		{"PRD-3", "Segmen tanpa rencana", 5000},
	}
	for _, row := range rows {
		if err := store.CreateProduksi(t.Context(), &model.Produksi{
			ProduksiID: row.id, Tanggal: "2026-07-02", Nopol: "B 1234 ABC",
			JenisDT: "DT KECIL", Volume: row.volume, VolumeOPP: 10, Lokasi: row.lokasi,
		}); err != nil {
			t.Fatalf("seed produksi: %v", err)
		}
	}

	client := loggedInClient(t, testServer)
	page := fetchAuthedPage(t, client, testServer.URL+"/produksi/overview")

	for _, fragment := range []string{
		"SEMUA LOKASI",
		// 25.300 produced against 25.000 planned: the unplanned 5.000 counts.
		"101.20%",
		"25.300 / 25.000 m³",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the total row is missing %q: %s", fragment, page)
		}
	}
	// Past the plan the total bar fills its track and takes the met-plan
	// colour, the same way a location's bar does.
	if !strings.Contains(page, `class="lokasi-chart-actual lokasi-chart-full"`) {
		t.Fatalf("a passed total is not marked: %s", page)
	}
}
