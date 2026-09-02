package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedNamedMachine(t *testing.T, store *repository.TestRepository, number int, idUnit, namaUnit string) {
	t.Helper()
	unit := &model.UnitA2B{
		NoUrut: number, TanggalIn: "2026-08-01", IDUnit: idUnit, NamaUnit: namaUnit,
		MerekType: "Kobelco", FuelStorage: 300, FRUnit: 19.3, Lokasi: "PIT A", HMAwal: 100,
	}
	if err := store.CreateUnitA2B(context.Background(), unit); err != nil {
		t.Fatalf("seed unit a2b: %v", err)
	}
}

func fuelKeluarFields() map[string]string {
	return map[string]string{
		"tanggal":  "2026-08-03",
		"id_unit":  "exc01",
		"hm_awal":  "20",
		"hm_akhir": "30",
		"operator": "kadal",
	}
}

func postFuelKeluar(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, image []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if csrf != "" {
		if err := writer.WriteField("csrf_token", csrf); err != nil {
			t.Fatalf("write csrf: %v", err)
		}
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if image != nil {
		part, err := writer.CreateFormFile("foto_flow_meter", "flowmeter.jpg")
		if err != nil {
			t.Fatalf("create photo: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatalf("write photo: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close fuel keluar form: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/a2b/fuel-keluar", &body)
	if err != nil {
		t.Fatalf("create fuel keluar request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post fuel keluar: %v", err)
	}
	return response
}

func fuelKeluarCSRF(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	return csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-keluar"))
}

// The litres are the distance between the two flowmeter readings, and the unit
// name comes from the register rather than the form.
func TestFuelKeluarDerivesLitresAndUnitName(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fuelKeluarFields(), testJPEG(t))
	requireFuelResponse(t, response, http.StatusOK, "FUELOUT-20260807-0001", "10 liter")

	rows := store.FuelKeluarList()
	if len(rows) != 1 {
		t.Fatalf("stored %d dispenses, want 1", len(rows))
	}
	stored := rows[0]
	if stored.FuelOutID != "FUELOUT-20260807-0001" {
		t.Fatalf("transaction number = %q", stored.FuelOutID)
	}
	if stored.Tanggal != "2026-08-03" {
		t.Fatalf("date = %q", stored.Tanggal)
	}
	if stored.IDUnit != "exc01" || stored.NamaUnit != "Excavator PC200 Kobelco (Rent)" {
		t.Fatalf("the unit was not taken from the register: %+v", stored)
	}
	if stored.Liter != 10 {
		t.Fatalf("litres = %v, want 10", stored.Liter)
	}
	if stored.Operator != "kadal" {
		t.Fatalf("operator = %q", stored.Operator)
	}
	if stored.FotoAkhirFlowMeter == "" {
		t.Fatal("the flow meter photo was not stored")
	}
	if stored.HMAlatBerat != nil {
		t.Fatalf("an hour meter nobody entered was stored as %v", *stored.HMAlatBerat)
	}
}

// A machine outside the A2B register cannot be fuelled: the litres would belong
// to nothing.
func TestFuelKeluarRefusesAnUnregisteredUnit(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := fuelKeluarFields()
	fields["id_unit"] = "exc99"

	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fields, testJPEG(t))
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "tidak terdaftar di Unit A2B")
	if rows := store.FuelKeluarList(); len(rows) != 0 {
		t.Fatalf("fuel was booked against an unknown machine: %+v", rows)
	}
}

// A totaliser only goes forwards, so a closing reading at or below the opening
// one is a typo rather than a dispense.
func TestFuelKeluarRefusesAFlowMeterThatGoesBackwards(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := fuelKeluarFields()
	fields["hm_akhir"] = "20"

	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fields, testJPEG(t))
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "HM akhir flow meter harus lebih besar")
	if rows := store.FuelKeluarList(); len(rows) != 0 {
		t.Fatalf("a backwards reading was stored: %+v", rows)
	}
}

// A machine's own hour meter only goes forwards. This is the typo that made a
// bulldozer's fuel ratio read zero: 1368 typed as 136.8.
func TestFuelKeluarRefusesAMachineMeterThatGoesBackwards(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	first := fuelKeluarFields()
	first["hm_alat_berat"] = "1294.9"
	postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), first, testJPEG(t)).Body.Close()

	second := fuelKeluarFields()
	second["tanggal"] = "2026-08-04"
	second["hm_awal"] = "30"
	second["hm_akhir"] = "45"
	second["hm_alat_berat"] = "136.8"
	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), second, testJPEG(t))
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "lebih kecil dari pengisian terakhir")
	if rows := store.FuelKeluarList(); len(rows) != 1 {
		t.Fatalf("a backwards machine meter was stored: %+v", rows)
	}
}

// A jump past the hours that have actually passed is saved with a caution: the
// operator at the pump can see the machine, and this code cannot.
func TestFuelKeluarWarnsAboutAnImpossibleMachineMeterJump(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	first := fuelKeluarFields()
	first["hm_alat_berat"] = "1294.9"
	postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), first, testJPEG(t)).Body.Close()

	second := fuelKeluarFields()
	second["tanggal"] = "2026-08-04"
	second["hm_awal"] = "30"
	second["hm_akhir"] = "45"
	second["hm_alat_berat"] = "12949"
	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), second, testJPEG(t))
	requireFuelResponse(t, response, http.StatusOK, "kelebihan digit")
	if rows := store.FuelKeluarList(); len(rows) != 2 {
		t.Fatalf("the row was refused rather than flagged: %+v", rows)
	}
}

// The photo is the only evidence the readings match the pump.
func TestFuelKeluarRefusesAMissingPhoto(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fuelKeluarFields(), nil)
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "foto akhir flow meter wajib dilampirkan")
	if rows := store.FuelKeluarList(); len(rows) != 0 {
		t.Fatalf("a dispense without evidence was stored: %+v", rows)
	}
}

// The pump carries on from where it stopped, so the next form opens on the last
// closing reading instead of an empty box.
func TestFuelKeluarFormOpensOnTheLastClosingReading(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fuelKeluarFields(), testJPEG(t)).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-keluar")
	opening := regexp.MustCompile(`<input id="hm_awal"[^>]*value="30"`)
	if !opening.MatchString(page) {
		t.Fatalf("the opening reading was not carried over from the last dispense: %s", page)
	}
}

// With no machines registered the form is not drawn at all, and the page says
// where to go instead of failing on submit.
func TestFuelKeluarPointsAtTheRegisterWhenNoMachineExists(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/fuel-keluar")
	if !strings.Contains(page, `href="/unit-a2b"`) || !strings.Contains(page, "Belum ada unit A2B terdaftar") {
		t.Fatalf("the empty page does not point at the register: %s", page)
	}
	if strings.Contains(page, `name="foto_flow_meter"`) {
		t.Fatal("the form was drawn with nothing to book fuel against")
	}
}

// The flowmeter photo comes back as an image, and an unknown transaction is a
// miss rather than a guess.
func TestFuelKeluarPhotoIsServedByTransaction(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fuelKeluarFields(), testJPEG(t)).Body.Close()

	response, err := client.Get(testServer.URL + "/a2b/fuel-keluar/foto?fuel_id=FUELOUT-20260807-0001")
	if err != nil {
		t.Fatalf("get photo: %v", err)
	}
	payload := readBodyBytes(t, response)
	if response.StatusCode != http.StatusOK || len(payload) == 0 {
		t.Fatalf("photo status = %d, %d bytes", response.StatusCode, len(payload))
	}
	if got := response.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q", got)
	}
	if status := statusOf(t, client, testServer.URL+"/a2b/fuel-keluar/foto?fuel_id=FUELOUT-20260807-9999"); status != http.StatusNotFound {
		t.Fatalf("unknown transaction returned %d, want 404", status)
	}
}

// The flow meter readings take a decimal comma too.
func TestFuelKeluarAcceptsADecimalComma(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := fuelKeluarFields()
	fields["hm_awal"] = "20,5"
	fields["hm_akhir"] = "30,25"
	fields["hm_alat_berat"] = "1024,75"

	response := postFuelKeluar(t, client, testServer, fuelKeluarCSRF(t, client, testServer), fields, testJPEG(t))
	requireFuelResponse(t, response, http.StatusOK, "9.75 liter")

	stored := store.FuelKeluarList()[0]
	if stored.HMAwalFlowMeter != 20.5 || stored.HMAkhirFlowMeter != 30.25 || stored.Liter != 9.75 {
		t.Fatalf("readings were not parsed: %+v", stored)
	}
	if stored.HMAlatBerat == nil || *stored.HMAlatBerat != 1024.75 {
		t.Fatalf("machine hour meter = %+v", stored.HMAlatBerat)
	}
}
