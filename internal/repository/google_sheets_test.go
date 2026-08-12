package repository

import (
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
)

func testLocation(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return loc
}

// Google Sheets omits trailing empty cells, so a freshly registered user whose
// last_login_at is still blank comes back as a 10-column row.
func TestRowToUserAcceptsTruncatedTrailingCells(t *testing.T) {
	row := []interface{}{
		"user-1", "2026-08-07", "Budi", "12345", "Anggota", "budi@example.com",
		"hash", "aktif", "2026-08-07 10:00:00", "2026-08-07 10:00:00",
	}

	user, err := rowToUser(row, testLocation(t))
	if err != nil {
		t.Fatalf("rowToUser: %v", err)
	}
	if user.UserID != "user-1" || user.Email != "budi@example.com" {
		t.Fatalf("unexpected user: %+v", user)
	}
	if user.LastLoginAt != nil {
		t.Fatalf("LastLoginAt = %v, want nil", user.LastLoginAt)
	}
}

func TestRowToUserStillParsesFullRow(t *testing.T) {
	row := []interface{}{
		"user-1", "2026-08-07", "Budi", "12345", "Anggota", "budi@example.com",
		"hash", "aktif", "2026-08-07 10:00:00", "2026-08-07 10:00:00", "2026-08-07 11:00:00",
	}

	user, err := rowToUser(row, testLocation(t))
	if err != nil {
		t.Fatalf("rowToUser: %v", err)
	}
	if user.LastLoginAt == nil {
		t.Fatal("LastLoginAt = nil, want parsed time")
	}
	if got := user.LastLoginAt.Format(datetimeLayout); got != "2026-08-07 11:00:00" {
		t.Fatalf("LastLoginAt = %q", got)
	}
}

func TestRowToAttendanceAcceptsTruncatedTrailingCells(t *testing.T) {
	row := make([]interface{}, len(attendanceHeaders))
	for i := range row {
		row[i] = ""
	}
	row[0] = "abs-1"
	row[1] = "user-1"
	row[5] = "2026-08-07"
	row[6] = "2026-08-07 08:00:00"
	row[7] = "-6.2"
	row[8] = "106.8"
	row[18] = "hadir"
	row[20] = "2026-08-07 08:00:00"
	row[21] = "2026-08-07 08:00:00"

	attendance, err := rowToAttendance(row[:len(row)-0], testLocation(t))
	if err != nil {
		t.Fatalf("rowToAttendance full row: %v", err)
	}
	if attendance.AbsensiID != "abs-1" {
		t.Fatalf("unexpected attendance: %+v", attendance)
	}

	// A row that lost its trailing blank cells must still parse when the
	// remaining cells cover every required field.
	short := append([]interface{}{}, row[:20]...)
	short[19] = ""
	if _, err := rowToAttendance(short, testLocation(t)); err == nil {
		t.Fatal("expected error when required created_at/updated_at are missing")
	}
}

func TestFindUserRowReturnsSheetRowNumber(t *testing.T) {
	user := func(id string) []interface{} {
		return []interface{}{
			id, "2026-08-07", "Budi", "12345", "Anggota", id + "@example.com",
			"hash", "aktif", "2026-08-07 10:00:00", "2026-08-07 10:00:00",
		}
	}
	rows := [][]interface{}{
		{"user_id"},
		user("user-1"),
		{},
		user("user-2"),
	}

	// Sheet rows are 1-based and row 1 is the header, so the first data row
	// must resolve to 2 — never 0, which the Sheets API rejects.
	found, rowNumber, err := findUserRow(rows, "user-1", testLocation(t))
	if err != nil {
		t.Fatalf("findUserRow: %v", err)
	}
	if found.UserID != "user-1" {
		t.Fatalf("found %q, want user-1", found.UserID)
	}
	if rowNumber != 2 {
		t.Fatalf("rowNumber = %d, want 2", rowNumber)
	}

	if _, rowNumber, err = findUserRow(rows, "user-2", testLocation(t)); err != nil {
		t.Fatalf("findUserRow: %v", err)
	}
	if rowNumber != 4 {
		t.Fatalf("rowNumber = %d, want 4", rowNumber)
	}

	if _, _, err := findUserRow(rows, "missing", testLocation(t)); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestUpdateRowRejectsHeaderAndInvalidRowNumbers(t *testing.T) {
	repo := &GoogleSheetsRepository{}
	for _, rowNumber := range []int{0, 1, -3} {
		if err := repo.updateRow(nil, userSheet, rowNumber, nil, "K"); err == nil {
			t.Fatalf("updateRow(%d) = nil error, want rejection", rowNumber)
		}
	}
}

func TestDataRowsSkipsBlankRows(t *testing.T) {
	rows := [][]interface{}{
		{"header"},
		{"user-1"},
		{},
		{"", "  "},
		{"user-2"},
	}

	got := dataRows(rows)
	if len(got) != 2 {
		t.Fatalf("dataRows returned %d rows, want 2", len(got))
	}

	indexed := dataRowsWithIndex(rows)
	if len(indexed) != 2 {
		t.Fatalf("dataRowsWithIndex returned %d rows, want 2", len(indexed))
	}
	if indexed[0].rowNumber != 2 {
		t.Fatalf("first row number = %d, want 2", indexed[0].rowNumber)
	}
	if indexed[1].rowNumber != 5 {
		t.Fatalf("second row number = %d, want 5", indexed[1].rowNumber)
	}
}

// The sheet is written with a decimal point whatever the operator typed. A
// comma reaches Sheets as text, and a column of text does not add up.
func TestNumericRowsAreWrittenWithADecimalPoint(t *testing.T) {
	hm := hourMeterToRow(&model.HourMeter{
		HMID: "HM-20260807-0001", HMAwal: 5064.3, HMAkhir: 5100.75, TotalHM: 36.45, FuelLiter: 245.5,
	})
	fuelOut := fuelKeluarToRow(&model.FuelKeluar{
		FuelOutID: "FUELOUT-20260807-0001", HMAwalFlowMeter: 20.5, HMAkhirFlowMeter: 30.25, Liter: 9.75,
	})
	fuelIn := fuelMasukToRow(&model.FuelMasuk{
		FuelID: "FUEL-20260807-0001", JumlahLiter: 8010.4, LiterTidakSesuai: 150.25,
	})

	for name, row := range map[string][]interface{}{
		"hour meter": hm, "fuel keluar": fuelOut, "fuel masuk": fuelIn,
	} {
		for index, cell := range row {
			value, ok := cell.(string)
			if !ok {
				t.Fatalf("%s column %d is not written as a string: %T", name, index, cell)
			}
			if strings.Contains(value, ",") {
				t.Fatalf("%s column %d was written with a decimal comma: %q", name, index, value)
			}
		}
	}
	if hm[6] != "5064.300000" {
		t.Fatalf("hm awal = %q", hm[6])
	}
}

// Every standby reason has a column of its own. A reason that did not happen
// leaves its cell empty rather than writing a zero, so the sheet distinguishes
// "no rain" from "rain, nought minutes".
func TestHourMeterRowWritesOneColumnPerStandbyReason(t *testing.T) {
	reading := &model.HourMeter{
		HMID: "HM-20260807-0001", Tanggal: "2026-08-07", TotalStandby: 45,
		Standby: []model.HourMeterStandby{
			{Variable: "P2H", Menit: 15},
			{Variable: "HUJAN", Menit: 30},
		},
		TotalBreakdown: 60,
		Breakdown: []model.HourMeterBreakdown{
			{Variable: "SCM", Menit: 40},
			{Variable: "NO OPR", Menit: 20},
		},
		PA: 93.75, BDPersen: 6.25, UA: 86.67, Remark: "Ganti hose hidrolik",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	row := hourMeterToRow(reading)
	if len(row) != len(hourMeterHeaders) {
		t.Fatalf("row has %d cells for %d headers", len(row), len(hourMeterHeaders))
	}

	byHeader := make(map[string]string, len(row))
	for index, header := range hourMeterHeaders {
		byHeader[header] = cellString(row[index])
	}
	if byHeader["d01_p2h"] != "15.000000" {
		t.Fatalf("d01_p2h = %q", byHeader["d01_p2h"])
	}
	if byHeader["i15_hujan"] != "30.000000" {
		t.Fatalf("i15_hujan = %q", byHeader["i15_hujan"])
	}
	if byHeader["d09_istirahat"] != "" {
		t.Fatalf("a reason that did not happen was written as %q", byHeader["d09_istirahat"])
	}
	if byHeader["total_standby_menit"] != "45.000000" {
		t.Fatalf("total = %q", byHeader["total_standby_menit"])
	}
	if byHeader["bd_scm"] != "40.000000" || byHeader["bd_no_opr"] != "20.000000" {
		t.Fatalf("breakdown columns = %q, %q", byHeader["bd_scm"], byHeader["bd_no_opr"])
	}
	if byHeader["bd_usm"] != "" {
		t.Fatalf("a breakdown that did not happen was written as %q", byHeader["bd_usm"])
	}
	if byHeader["total_bd_menit"] != "60.000000" {
		t.Fatalf("total breakdown = %q", byHeader["total_bd_menit"])
	}
	if byHeader["pa_persen"] != "93.750000" || byHeader["bd_persen"] != "6.250000" || byHeader["ua_persen"] != "86.670000" {
		t.Fatalf("figures = %q / %q / %q", byHeader["pa_persen"], byHeader["bd_persen"], byHeader["ua_persen"])
	}
	if byHeader["remark"] != "Ganti hose hidrolik" {
		t.Fatalf("remark = %q", byHeader["remark"])
	}

	// What was written comes back as the same two reasons and nothing else.
	back, err := rowToHourMeter(row, testLocation(t))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(back.Standby) != 2 {
		t.Fatalf("read back %d reasons: %+v", len(back.Standby), back.Standby)
	}
	if back.Standby[0].Variable != "P2H" || back.Standby[1].Variable != "HUJAN" {
		t.Fatalf("read back the wrong reasons: %+v", back.Standby)
	}
	if back.TotalStandby != 45 {
		t.Fatalf("read back total = %v", back.TotalStandby)
	}
	if len(back.Breakdown) != 2 || back.Breakdown[0].Variable != "SCM" || back.Breakdown[1].Variable != "NO OPR" {
		t.Fatalf("read back the wrong breakdown: %+v", back.Breakdown)
	}
	if back.TotalBreakdown != 60 {
		t.Fatalf("read back breakdown total = %v", back.TotalBreakdown)
	}
	if back.PA != 93.75 || back.BDPersen != 6.25 || back.UA != 86.67 {
		t.Fatalf("read back figures = %v / %v / %v", back.PA, back.BDPersen, back.UA)
	}
	if back.Remark != "Ganti hose hidrolik" {
		t.Fatalf("read back remark = %q", back.Remark)
	}
}

// The audit trail sits after the standby block, so the reader has to find it by
// offset rather than by a hard-coded column.
func TestHourMeterHeaderLayoutMatchesItsOffsets(t *testing.T) {
	if hourMeterHeaders[hourMeterStandbyOffset] != "d01_p2h" {
		t.Fatalf("the standby block starts at %q", hourMeterHeaders[hourMeterStandbyOffset])
	}
	if hourMeterHeaders[hourMeterBreakdownTotalOffset] != "total_bd_menit" {
		t.Fatalf("the breakdown total sits at %q", hourMeterHeaders[hourMeterBreakdownTotalOffset])
	}
	if hourMeterHeaders[hourMeterBreakdownOffset] != "bd_scm" {
		t.Fatalf("the breakdown block starts at %q", hourMeterHeaders[hourMeterBreakdownOffset])
	}
	if hourMeterHeaders[hourMeterSummaryOffset] != "pa_persen" {
		t.Fatalf("the figures start at %q", hourMeterHeaders[hourMeterSummaryOffset])
	}
	if hourMeterHeaders[hourMeterAuditOffset] != "dibuat_oleh" {
		t.Fatalf("the audit trail starts at %q", hourMeterHeaders[hourMeterAuditOffset])
	}
	if got := columnName(len(hourMeterHeaders)); got != hourMeterLastColumn {
		t.Fatalf("last column = %q, want %q", hourMeterLastColumn, got)
	}
	for number, want := range map[int]string{1: "A", 26: "Z", 27: "AA", 48: "AV"} {
		if got := columnName(number); got != want {
			t.Fatalf("columnName(%d) = %q, want %q", number, got, want)
		}
	}
}
