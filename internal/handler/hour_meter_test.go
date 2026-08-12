package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hourMeterFields is a machine that worked the whole eight-hour shift, so
// nothing is left to account for as standby or breakdown.
func hourMeterFields() map[string]string {
	return map[string]string{
		"tanggal":    "2026-08-07",
		"shift":      "Shift 1",
		"id_unit":    "exc01",
		"operator":   "kadal",
		"hm_awal":    "1200",
		"hm_akhir":   "1208",
		"fuel_liter": "245",
	}
}

// hourMeterFieldsWorking is the same reading over fewer hours, leaving minutes
// the two sections have to account for.
func hourMeterFieldsWorking(hours string) map[string]string {
	fields := hourMeterFields()
	fields["hm_akhir"] = hours
	return fields
}

func postHourMeter(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string) *http.Response {
	t.Helper()
	values := urlValues(fields)
	values.Set("csrf_token", csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")))
	response, err := client.PostForm(testServer.URL+"/a2b/hm", values)
	if err != nil {
		t.Fatalf("post hour meter: %v", err)
	}
	return response
}

// The total is the distance between the two readings, and the unit name comes
// from the register rather than the form.
func TestHourMeterDerivesTotalAndUnitName(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeter(t, client, testServer, hourMeterFields())
	requireFuelResponse(t, response, http.StatusOK, "HM-20260807-0001", "8 jam kerja")

	rows := store.HourMeterList()
	if len(rows) != 1 {
		t.Fatalf("stored %d readings, want 1", len(rows))
	}
	stored := rows[0]
	if stored.HMID != "HM-20260807-0001" {
		t.Fatalf("transaction number = %q", stored.HMID)
	}
	if stored.Tanggal != "2026-08-07" || stored.Shift != "Shift 1" || stored.Operator != "kadal" {
		t.Fatalf("general information was not stored: %+v", stored)
	}
	if stored.IDUnit != "exc01" || stored.NamaUnit != "Excavator PC200 Kobelco (Rent)" {
		t.Fatalf("the unit was not taken from the register: %+v", stored)
	}
	if stored.TotalHM != 8 {
		t.Fatalf("total = %v hours, want 8", stored.TotalHM)
	}
	if stored.FuelLiter != 245 {
		t.Fatalf("fuel = %v, want 245", stored.FuelLiter)
	}
}

// A machine outside the A2B register cannot have hours booked against it.
func TestHourMeterRefusesAnUnregisteredUnit(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := hourMeterFields()
	fields["id_unit"] = "exc99"

	response := postHourMeter(t, client, testServer, fields)
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "tidak terdaftar di Unit A2B")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("hours were booked against an unknown machine: %+v", rows)
	}
}

// An hour meter only goes forwards.
func TestHourMeterRefusesAReadingThatGoesBackwards(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := hourMeterFields()
	fields["hm_akhir"] = "1100"

	response := postHourMeter(t, client, testServer, fields)
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "HM akhir tidak boleh lebih kecil")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("a backwards reading was stored: %+v", rows)
	}
}

// A machine that stood idle reads the same at both ends. That is a real shift,
// but the whole of it then has to be accounted for.
func TestHourMeterAcceptsAnIdleShift(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := hourMeterFields()
	fields["hm_akhir"] = fields["hm_awal"]
	fields["fuel_liter"] = "0"

	response := postHourMeterWithStandby(t, client, testServer, fields,
		[]string{"TUNGGU ALAT"}, []string{"480"})
	requireFuelResponse(t, response, http.StatusOK, "0 jam kerja", "480 menit standby")
	rows := store.HourMeterList()
	if len(rows) != 1 || rows[0].TotalHM != 0 {
		t.Fatalf("an idle shift was not stored: %+v", rows)
	}
}

// A shift or operator typed once is offered to the next reading, so the same
// value is not stored under two spellings.
func TestHourMeterSuggestsShiftsAndOperatorsAlreadyUsed(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	postHourMeter(t, client, testServer, hourMeterFields()).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	for _, fragment := range []string{`<datalist id="shiftList">`, `<option value="Shift 1">`, `<option value="kadal">`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the form does not suggest %q: %s", fragment, page)
		}
	}
}

// Choosing a unit fills the opening reading, so the page carries each machine's
// last closing reading with its option.
func TestHourMeterCarriesTheLastReadingPerUnit(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	seedNamedMachine(t, store, 2, "bul02", "Bulldozer D6 CAT (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	postHourMeter(t, client, testServer, hourMeterFields()).Body.Close()

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, `value="exc01" data-nama-unit="Excavator PC200 Kobelco (Rent)" data-hm-awal="1208"`) {
		t.Fatalf("the fuelled machine does not carry its last reading: %s", page)
	}
	// A machine with no history offers nothing rather than a made-up figure.
	if !strings.Contains(page, `<option value="bul02" data-nama-unit="Bulldozer D6 CAT (Rent)">`) {
		t.Fatalf("a machine with no history was given a reading: %s", page)
	}
}

// With no machines registered the form is not drawn, and the page says where to
// go instead of failing on submit.
func TestHourMeterPointsAtTheRegisterWhenNoMachineExists(t *testing.T) {
	testServer := newTestServer(t)
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, `href="/unit-a2b"`) || !strings.Contains(page, "Belum ada unit A2B terdaftar") {
		t.Fatalf("the empty page does not point at the register: %s", page)
	}
	if strings.Contains(page, `name="hm_awal"`) {
		t.Fatal("the form was drawn with nothing to book hours against")
	}
}

// An Indonesian keyboard produces a decimal comma. The form accepts either, and
// the sheet is written with a point regardless.
func TestHourMeterAcceptsADecimalComma(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := hourMeterFields()
	fields["hm_awal"] = "5064,30"
	fields["hm_akhir"] = "5100,75"
	fields["fuel_liter"] = "245,5"

	response := postHourMeter(t, client, testServer, fields)
	requireFuelResponse(t, response, http.StatusOK, "36.45 jam kerja")

	stored := store.HourMeterList()[0]
	if stored.HMAwal != 5064.3 || stored.HMAkhir != 5100.75 {
		t.Fatalf("readings = %v, %v", stored.HMAwal, stored.HMAkhir)
	}
	if stored.TotalHM != 36.45 {
		t.Fatalf("total = %v, want 36.45", stored.TotalHM)
	}
	if stored.FuelLiter != 245.5 {
		t.Fatalf("fuel = %v, want 245.5", stored.FuelLiter)
	}
}

// postHourMeterWithStandby submits the reading together with its standby lines,
// which arrive as two parallel arrays.
func postHourMeterWithStandby(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string, variables, menit []string) *http.Response {
	t.Helper()
	return postHourMeterWithLines(t, client, testServer, fields, "standby", variables, menit)
}

func postHourMeterWithBreakdown(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string, variables, menit []string) *http.Response {
	t.Helper()
	return postHourMeterWithLines(t, client, testServer, fields, "breakdown", variables, menit)
}

func postHourMeterWithLines(t *testing.T, client *http.Client, testServer *httptest.Server, fields map[string]string, block string, variables, menit []string) *http.Response {
	t.Helper()
	values := urlValues(fields)
	values.Set("csrf_token", csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")))
	for _, variable := range variables {
		values.Add(block+"_variable", variable)
	}
	for _, value := range menit {
		values.Add(block+"_menit", value)
	}
	response, err := client.PostForm(testServer.URL+"/a2b/hm", values)
	if err != nil {
		t.Fatalf("post hour meter with %s lines: %v", block, err)
	}
	return response
}

// The standby lines are stored in order with their own minutes, and the total
// is their sum.
func TestHourMeterStoresStandbyLinesAndTheirTotal(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	// Seven hours worked leaves an hour to account for.
	response := postHourMeterWithStandby(t, client, testServer, hourMeterFieldsWorking("1207"),
		[]string{"P2H", "HUJAN", "ISTIRAHAT"}, []string{"15", "30,5", "14,5"})
	requireFuelResponse(t, response, http.StatusOK, "60 menit standby")

	stored := store.HourMeterList()[0]
	if stored.TotalStandby != 60 {
		t.Fatalf("total standby = %v, want 60", stored.TotalStandby)
	}
	if len(stored.Standby) != 3 {
		t.Fatalf("stored %d standby lines: %+v", len(stored.Standby), stored.Standby)
	}
	for index, want := range []struct {
		variable string
		menit    float64
	}{{"P2H", 15}, {"HUJAN", 30.5}, {"ISTIRAHAT", 14.5}} {
		line := stored.Standby[index]
		if line.Variable != want.variable || line.Menit != want.menit {
			t.Fatalf("standby line %d = %+v, want %s %v", index+1, line, want.variable, want.menit)
		}
	}
}

// A shift where the machine never stopped has no standby at all.
func TestHourMeterAcceptsAShiftWithoutStandby(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	// One blank row is what the form always renders, and an untouched row is not
	// an error.
	response := postHourMeterWithStandby(t, client, testServer, hourMeterFields(), []string{""}, []string{""})
	requireFuelResponse(t, response, http.StatusOK, "0 menit standby")

	stored := store.HourMeterList()[0]
	if stored.TotalStandby != 0 || len(stored.Standby) != 0 {
		t.Fatalf("an empty row was stored: %+v", stored.Standby)
	}
}

// The variable list is closed, so a direct post cannot invent a reason.
func TestHourMeterRefusesAnUnknownStandbyVariable(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithStandby(t, client, testServer, hourMeterFields(),
		[]string{"MAKAN SIANG"}, []string{"30"})
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "variable standby baris 1 tidak terdaftar")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("an unknown standby reason was stored: %+v", rows)
	}
}

// A reason with no minutes against it says nothing, so it is refused rather
// than stored as zero.
func TestHourMeterRefusesAStandbyLineWithoutMinutes(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithStandby(t, client, testServer, hourMeterFields(),
		[]string{"ISTIRAHAT"}, []string{""})
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "Menit standby baris 1")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("a standby line without minutes was stored: %+v", rows)
	}
}

// A rejected form comes back with the standby lines already typed, so nothing
// has to be entered twice.
func TestHourMeterKeepsStandbyLinesOnARejectedForm(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")
	fields := hourMeterFields()
	fields["hm_akhir"] = "1"

	response := postHourMeterWithStandby(t, client, testServer, fields,
		[]string{"P2H", "HUJAN"}, []string{"15", "30"})
	page := requireFuelResponse(t, response, http.StatusUnprocessableEntity, "HM akhir tidak boleh lebih kecil")
	if strings.Count(page, `name="standby_variable"`) != 2 {
		t.Fatalf("the typed standby lines were not returned: %s", page)
	}
	for _, fragment := range []string{`<option value="P2H" selected>`, `<option value="HUJAN" selected>`, `value="15"`, `value="30"`} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the form lost %q: %s", fragment, page)
		}
	}
}

// One reason, one line. A shift that stopped for rain twice is recorded as the
// minutes added up, not as two lines nobody can tell apart.
func TestHourMeterRefusesTheSameStandbyVariableTwice(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithStandby(t, client, testServer, hourMeterFieldsWorking("1207"),
		[]string{"HUJAN", "P2H", "HUJAN"}, []string{"30", "15", "20"})
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "variable standby HUJAN sudah dipakai")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("a duplicated standby reason was stored: %+v", rows)
	}
}

// The dropdown shows the code beside the reason, because the paper timesheet
// is filed by code and the two have to be matched up by eye.
func TestHourMeterStandbyOptionsShowTheirCode(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	for _, option := range []string{
		`<option value="P2H">P2H (D01)</option>`,
		`<option value="ISI BBM">ISI BBM (D02)</option>`,
		`<option value="HUJAN">HUJAN (I15)</option>`,
		`<option value="KABUT">KABUT (I20)</option>`,
	} {
		if !strings.Contains(page, option) {
			t.Fatalf("the standby dropdown is missing %s: %s", option, page)
		}
	}
	// The order is the timesheet's own, so D01 comes before I15.
	if strings.Index(page, "P2H (D01)") > strings.Index(page, "HUJAN (I15)") {
		t.Fatal("the standby dropdown is out of timesheet order")
	}
}

// Breakdown works the same way standby does: its own reasons, its own minutes,
// its own total.
func TestHourMeterStoresBreakdownLinesAndTheirTotal(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithBreakdown(t, client, testServer, hourMeterFieldsWorking("1207"),
		[]string{"SCM", "NO OPR"}, []string{"39,5", "20,5"})
	requireFuelResponse(t, response, http.StatusOK, "60 menit breakdown")

	stored := store.HourMeterList()[0]
	if stored.TotalBreakdown != 60 {
		t.Fatalf("total breakdown = %v, want 60", stored.TotalBreakdown)
	}
	if len(stored.Breakdown) != 2 {
		t.Fatalf("stored %d breakdown lines: %+v", len(stored.Breakdown), stored.Breakdown)
	}
	if stored.Breakdown[0].Variable != "SCM" || stored.Breakdown[0].Menit != 39.5 {
		t.Fatalf("first line = %+v", stored.Breakdown[0])
	}
	if stored.Breakdown[1].Variable != "NO OPR" || stored.Breakdown[1].Menit != 20.5 {
		t.Fatalf("second line = %+v", stored.Breakdown[1])
	}
}

// The breakdown list is closed and each reason may be given once, on the same
// terms as standby.
func TestHourMeterRefusesBadBreakdownLines(t *testing.T) {
	cases := map[string]struct {
		variables []string
		menit     []string
		message   string
	}{
		"unknown reason":    {[]string{"BANJIR"}, []string{"30"}, "variable breakdown baris 1 tidak terdaftar"},
		"same reason twice": {[]string{"SCM", "scm"}, []string{"30", "20"}, "variable breakdown SCM sudah dipakai"},
		"no minutes":        {[]string{"USM"}, []string{""}, "Menit breakdown baris 1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			testServer, store := newTestServerWithStore(t)
			seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
			client := loggedInClientAs(t, testServer, "Logistik")

			response := postHourMeterWithBreakdown(t, client, testServer, hourMeterFieldsWorking("1207"), tc.variables, tc.menit)
			requireFuelResponse(t, response, http.StatusUnprocessableEntity, tc.message)
			if rows := store.HourMeterList(); len(rows) != 0 {
				t.Fatalf("the reading was stored anyway: %+v", rows)
			}
		})
	}
}

// A shift where nothing broke has no breakdown at all.
func TestHourMeterAcceptsAShiftWithoutBreakdown(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithBreakdown(t, client, testServer, hourMeterFields(), []string{""}, []string{""})
	requireFuelResponse(t, response, http.StatusOK, "0 menit breakdown")

	stored := store.HourMeterList()[0]
	if stored.TotalBreakdown != 0 || len(stored.Breakdown) != 0 {
		t.Fatalf("an empty row was stored: %+v", stored.Breakdown)
	}
}

// The breakdown dropdown offers the five reasons and nothing else.
func TestHourMeterBreakdownOptionsAreTheClosedList(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, `name="breakdown_variable"`) {
		t.Fatalf("the breakdown section is missing: %s", page)
	}
	for _, variable := range []string{"SCM", "USM", "TRM", "ICM", "NO OPR"} {
		if !strings.Contains(page, `<option value="`+variable+`">`+variable+`</option>`) {
			t.Fatalf("the breakdown dropdown is missing %s", variable)
		}
	}
}

// A machine that worked the whole shift has nothing to account for, so the two
// sections are not drawn at all and the page says why.
func TestHourMeterHidesTheSectionsWhenTheShiftIsFull(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	// Nothing typed yet: the whole shift is still to be accounted for.
	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, `<div data-idle-sections>`) {
		t.Fatalf("the sections are hidden before anything was typed: %s", page)
	}
	if !strings.Contains(page, "480") {
		t.Fatalf("the page does not say how long a shift is: %s", page)
	}

	// A rejected full shift comes back with the sections closed.
	fields := hourMeterFields()
	fields["fuel_liter"] = ""
	response := postHourMeter(t, client, testServer, fields)
	page = requireFuelResponse(t, response, http.StatusUnprocessableEntity, "Fuel")
	if !strings.Contains(page, `<div data-idle-sections hidden>`) {
		t.Fatalf("a full shift still shows the standby and breakdown sections: %s", page)
	}
	if !strings.Contains(page, "tidak perlu diisi") {
		t.Fatalf("the page does not explain why the sections are gone: %s", page)
	}
}

// Standby against a shift already fully worked is refused, whatever the form
// chose to draw.
func TestHourMeterRefusesStandbyOnAFullShift(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeterWithStandby(t, client, testServer, hourMeterFields(),
		[]string{"ISTIRAHAT"}, []string{"30"})
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "harus kosong")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("standby was recorded against a full shift: %+v", rows)
	}
}

// Short of the shift, the remaining minutes have to be accounted for.
func TestHourMeterDemandsTheRemainingMinutes(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	response := postHourMeter(t, client, testServer, hourMeterFieldsWorking("1207"))
	requireFuelResponse(t, response, http.StatusUnprocessableEntity, "sisa 60 menit")
	if rows := store.HourMeterList(); len(rows) != 0 {
		t.Fatalf("an unaccounted shift was stored: %+v", rows)
	}
}

// The three figures are coloured by whether they met their target, which is the
// only thing anyone reads them for at a glance.
func TestHourMeterColoursTheFiguresByTarget(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	// Half the shift lost to breakdown: availability and its mirror both miss,
	// and the machine used all of what was left.
	fields := hourMeterFieldsWorking("1204")
	response := postHourMeterWithBreakdown(t, client, testServer, fields,
		[]string{"SCM"}, []string{"240"})
	requireFuelResponse(t, response, http.StatusOK, "240 menit breakdown")

	stored := store.HourMeterList()[0]
	if stored.PA != 50 || stored.BDPersen != 50 || stored.UA != 100 {
		t.Fatalf("figures = %v / %v / %v", stored.PA, stored.BDPersen, stored.UA)
	}

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	for _, chip := range []string{
		`<span class="figure-chip short">50%</span>`,
		`<span class="figure-chip good">100%</span>`,
	} {
		if !strings.Contains(page, chip) {
			t.Fatalf("the history is missing %s: %s", chip, page)
		}
	}
	if strings.Count(page, `figure-chip short`) != 2 {
		t.Fatalf("PA and BD should both read short: %s", page)
	}
}

// The remark is stored with the reading and shown against it.
func TestHourMeterStoresARemark(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	fields := hourMeterFields()
	fields["remark"] = "Ganti hose hidrolik"
	response := postHourMeter(t, client, testServer, fields)
	requireFuelResponse(t, response, http.StatusOK, "8 jam kerja")

	if stored := store.HourMeterList()[0]; stored.Remark != "Ganti hose hidrolik" {
		t.Fatalf("remark = %q", stored.Remark)
	}
	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, "Ganti hose hidrolik") {
		t.Fatalf("the remark is missing from the history: %s", page)
	}
}

// The remainder is spelled out in two states: the one still to be filled, and
// the settled one that replaces it. Both ship with the page so the swap costs
// no round trip.
func TestHourMeterCarriesBothRemainderStates(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	seedNamedMachine(t, store, 1, "exc01", "Excavator PC200 Kobelco (Rent)")
	client := loggedInClientAs(t, testServer, "Logistik")

	page := fetchAuthedPage(t, client, testServer.URL+"/a2b/hm")
	if !strings.Contains(page, `<p class="alert info idle-note" data-idle-short>`) {
		t.Fatalf("the outstanding remainder is missing: %s", page)
	}
	if !strings.Contains(page, `data-idle-filled hidden>Semua 480 menit kerja sudah terisi.`) {
		t.Fatalf("the settled remainder is missing or starts visible: %s", page)
	}
}
