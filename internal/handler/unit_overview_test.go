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
		"TOTAL UNIT DT", "TOTAL UNIT A2B", "DRIVER TERDAFTAR", "KAPASITAS TANGKI A2B",
		// Three trucks, two machines, 700 litres of tank between them.
		">3</p>", ">2</p>", "700 <small>L</small>",
		// One driver appears twice and is one person, not two.
		"DT KECIL", "DT BESAR", "Komatsu",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the overview is missing %q", fragment)
		}
	}
	if !strings.Contains(page, "45.6 L/jam") {
		t.Fatalf("the overview does not total the fuel rate: %s", page)
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
