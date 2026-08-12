package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedTruck(t *testing.T, store *repository.TestRepository, unitID, nopol, driver, keterangan string) {
	t.Helper()
	unit := &model.UnitDT{
		UnitID: unitID, Nopol: nopol, Panjang: 3.75, Lebar: 1.9, Tinggi: 1.5,
		Driver: driver, Keterangan: keterangan,
	}
	if err := store.CreateUnitDT(context.Background(), unit); err != nil {
		t.Fatalf("seed unit dt: %v", err)
	}
}

func seedMachine(t *testing.T, store *repository.TestRepository, number int, idUnit, merek, lokasi string, fuel, rate float64) {
	t.Helper()
	unit := &model.UnitA2B{
		NoUrut: number, TanggalIn: "2026-08-01", IDUnit: idUnit,
		NamaUnit: "Excavator", MerekType: merek, FuelStorage: fuel, FRUnit: rate, Lokasi: lokasi,
	}
	if err := store.CreateUnitA2B(context.Background(), unit); err != nil {
		t.Fatalf("seed unit a2b: %v", err)
	}
}

func TestUnitOverviewCountsBothRegisters(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedTruck(t, store, "UNT-2026-0001", "B 1 A", "Slamet", "DT KECIL")
	seedTruck(t, store, "UNT-2026-0002", "B 2 B", "Dodi", "DT BESAR")
	seedTruck(t, store, "UNT-2026-0003", "B 3 C", "Slamet", "DT KECIL")
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 300, 19.3)
	seedMachine(t, store, 2, "BLD-01", "Komatsu", "PIT B", 400, 26.3)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/unit/overview")
	for _, fragment := range []string{
		"TOTAL UNIT DT", "DRIVER TERDAFTAR",
		// Three trucks; one driver appears twice and is one person, not two.
		">3</p>", ">2</p>",
		"DT KECIL", "DT BESAR",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the overview is missing %q", fragment)
		}
	}
	// The machines have a menu of their own; this page is about the trucks.
	for _, moved := range []string{"TOTAL UNIT A2B", "TOP 5 MEREK A2B", "KAPASITAS TANGKI"} {
		if strings.Contains(page, moved) {
			t.Fatalf("the unit overview still carries %q", moved)
		}
	}
	if !strings.Contains(page, `href="/a2b/overview"`) {
		t.Fatal("the unit overview does not point at the machines")
	}
}

// The machine dashboard reads what the fleet did over a range, not what the
// register holds.
func TestA2BOverviewReadsTheFleet(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedTruck(t, store, "UNT-2026-0001", "B 1 A", "Slamet", "DT KECIL")
	seedMachine(t, store, 1, "EXC-01", "Komatsu", "PIT A", 300, 19.3)
	seedMachine(t, store, 2, "BLD-01", "Komatsu", "PIT B", 400, 26.3)
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/overview")
	for _, fragment := range []string{
		"TOTAL UNIT", "UNIT ACTIVE", "UNIT STANDBY", "UNIT BREAKDOWN", "STOCK FUEL",
		"PERFORMANCE PER UNIT", "TOP 5 DELAY",
		// Two machines registered, neither of them worked yet.
		`<p class="kpi-value">2</p>`, `<p class="kpi-value">0</p>`,
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the a2b overview is missing %q", fragment)
		}
	}
	// The trucks belong to the other page, and the register breakdown to Unit A2B.
	for _, gone := range []string{"TOTAL UNIT DT", "TOP 5 MEREK A2B"} {
		if strings.Contains(page, gone) {
			t.Fatalf("the a2b overview still shows %q", gone)
		}
	}
}

// A truck with nobody assigned cannot be dispatched, so the page names them and
// points at the register that fixes it.
func TestUnitOverviewFlagsTrucksWithoutADriver(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedTruck(t, store, "UNT-2026-0001", "B 1 A", "Slamet", "DT KECIL")
	seedTruck(t, store, "UNT-2026-0002", "B 2 B", "BELUM DIISI", "DT KECIL")
	seedTruck(t, store, "UNT-2026-0003", "B 3 C", "", "DT KECIL")
	client := loggedInClient(t, testServer)

	page := fetchAuthedPage(t, client, testServer.URL+"/unit/overview")
	if !strings.Contains(page, "2 unit DT belum punya driver") {
		t.Fatalf("the placeholder drivers were counted as names: %s", page)
	}
	if !strings.Contains(page, `href="/unit-dt"`) {
		t.Fatal("the page does not point at the register that fixes it")
	}
	// The placeholders are not drivers, so only one name is counted.
	if !strings.Contains(page, `<p class="kpi-value">1</p>`) {
		t.Fatal("a placeholder was counted as a driver")
	}

	// With every truck assigned the band disappears rather than reading zero.
	settled, _ := newTestServerWithStore(t)
	quiet := fetchAuthedPage(t, loggedInClient(t, settled), settled.URL+"/unit/overview")
	if strings.Contains(quiet, "belum punya driver") {
		t.Fatal("an empty register still asks for drivers to be filled in")
	}
}

func TestUnitOverviewRequiresASession(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Get(testServer.URL + "/unit/overview")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	response.Body.Close()
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("anonymous request went to %q, want /login", location)
	}
}

// The dashboard reads the range it was asked for: what worked, what stood
// still, what broke, and what is left in the tank.
func TestA2BOverviewReportsTheRangeItWasAsked(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	seedNamedMachine(t, store, 2, "bul02", "Bulldozer D6 CAT (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	// One machine worked seven hours and rested the eighth; the other never ran.
	postHourMeterWithStandby(t, client, testServer, hourMeterFieldsWorking("1207"),
		[]string{"HUJAN"}, []string{"60"}).Body.Close()
	postFuelMasuk(t, client, testServer, fuelFormCSRF(t, client, testServer), fuelFields(), testJPEG(t)).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/overview?from=2026-08-01&to=2026-08-10")
	for _, fragment := range []string{
		// Two registered, one of them active, so one is standby and none broke.
		"TOTAL UNIT", "UNIT ACTIVE", "UNIT STANDBY",
		"exc01", "Excavator PC200 Kobelco (Rent)",
		// Seven hours worked and the delivery still in the tank.
		">7<", ">8010<",
		// The hour it rested, ranked as the fleet's only delay.
		"HUJAN", "1 jam standby",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the a2b overview is missing %q: %s", fragment, page)
		}
	}
	// A machine that never ran is not in the performance table.
	if strings.Contains(page, "bul02") {
		t.Fatalf("a machine with no readings was listed: %s", page)
	}
}
