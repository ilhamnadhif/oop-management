package repository

import (
	"testing"
	"time"
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
