package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/sheets/v4"

	"opp-management/internal/model"
)

const (
	userSheet       = "user"
	activitySheet   = "activity login"
	attendanceSheet = "absensi data"
	unitDTSheet     = "Unit DT"

	datetimeLayout = "2006-01-02 15:04:05"
)

var userHeaders = []string{
	"user_id", "tanggal_gabung", "nama_lengkap", "nrp", "jabatan", "email",
	"password_hash", "status_pengguna", "created_at", "updated_at", "last_login_at",
}

var activityHeaders = []string{
	"activity_id", "user_id", "nrp", "email", "activity_type", "activity_time",
	"status", "ip_address", "user_agent", "message",
}

var attendanceHeaders = []string{
	"absensi_id", "user_id", "nrp", "nama_lengkap", "jabatan", "tanggal_absensi",
	"clock_in_at", "clock_in_lat", "clock_in_lng", "clock_in_accuracy", "clock_in_photo",
	"clock_in_ip", "clock_out_at", "clock_out_lat", "clock_out_lng", "clock_out_accuracy",
	"clock_out_photo", "clock_out_ip", "status_absensi", "durasi_menit", "created_at", "updated_at",
}

var unitDTHeaders = []string{
	"unit_id", "nopol", "panjang_m", "lebar_m", "tinggi_m", "driver",
	"keterangan", "foto_unit", "dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

type GoogleSheetsRepository struct {
	service       *sheets.Service
	spreadsheetID string
	location      *time.Location
}

func NewGoogleSheetsRepository(service *sheets.Service, spreadsheetID string, location *time.Location) *GoogleSheetsRepository {
	return &GoogleSheetsRepository{service: service, spreadsheetID: spreadsheetID, location: location}
}

func (r *GoogleSheetsRepository) EnsureSchema(ctx context.Context) error {
	spreadsheet, err := r.service.Spreadsheets.Get(r.spreadsheetID).Fields("sheets.properties").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("get spreadsheet metadata: %w", err)
	}

	existing := make(map[string]bool)
	for _, sheet := range spreadsheet.Sheets {
		if sheet.Properties != nil {
			existing[sheet.Properties.Title] = true
		}
	}

	missing := []string{userSheet, activitySheet, attendanceSheet, unitDTSheet}
	requests := make([]*sheets.Request, 0, len(missing))
	for _, name := range missing {
		if existing[name] {
			continue
		}
		requests = append(requests, &sheets.Request{
			AddSheet: &sheets.AddSheetRequest{Properties: &sheets.SheetProperties{Title: name}},
		})
	}
	if len(requests) > 0 {
		if _, err := r.service.Spreadsheets.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("create missing sheets: %w", err)
		}
	}

	for _, definition := range []struct {
		name    string
		headers []string
	}{
		{name: userSheet, headers: userHeaders},
		{name: activitySheet, headers: activityHeaders},
		{name: attendanceSheet, headers: attendanceHeaders},
		{name: unitDTSheet, headers: unitDTHeaders},
	} {
		if err := r.ensureHeader(ctx, definition.name, definition.headers); err != nil {
			return err
		}
	}
	return nil
}

func (r *GoogleSheetsRepository) ensureHeader(ctx context.Context, sheetName string, expected []string) error {
	rangeName := fmt.Sprintf("%s!1:1", quoteSheet(sheetName))
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("read header for %s: %w", sheetName, err)
	}

	if len(values.Values) == 0 || len(values.Values[0]) == 0 {
		if err := r.writeRange(ctx, sheetName, "A1", expected); err != nil {
			return fmt.Errorf("write header for %s: %w", sheetName, err)
		}
		return nil
	}

	actual := values.Values[0]
	if len(actual) != len(expected) {
		return fmt.Errorf("header mismatch in sheet %q: expected %d columns, got %d", sheetName, len(expected), len(actual))
	}
	for i, header := range expected {
		if cellString(actual[i]) != header {
			return fmt.Errorf("header mismatch in sheet %q at column %d: expected %q, got %q", sheetName, i+1, header, cellString(actual[i]))
		}
	}
	return nil
}

func (r *GoogleSheetsRepository) FindUserByID(ctx context.Context, userID string) (*model.User, error) {
	rows, err := r.readRows(ctx, userSheet, "K")
	if err != nil {
		return nil, err
	}
	for _, row := range dataRows(rows) {
		user, err := rowToUser(row, r.location)
		if err != nil {
			return nil, err
		}
		if user.UserID == userID {
			return user, nil
		}
	}
	return nil, ErrNotFound
}

func (r *GoogleSheetsRepository) FindUserByIdentifier(ctx context.Context, identifier string) (*model.User, error) {
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	rows, err := r.readRows(ctx, userSheet, "K")
	if err != nil {
		return nil, err
	}
	for _, row := range dataRows(rows) {
		user, err := rowToUser(row, r.location)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(user.NRP) == identifier || strings.ToLower(user.Email) == identifier {
			return user, nil
		}
	}
	return nil, ErrNotFound
}

func (r *GoogleSheetsRepository) UserExists(ctx context.Context, nrp, email string) (bool, error) {
	rows, err := r.readRows(ctx, userSheet, "K")
	if err != nil {
		return false, err
	}
	nrp = strings.ToLower(strings.TrimSpace(nrp))
	email = strings.ToLower(strings.TrimSpace(email))
	for _, row := range dataRows(rows) {
		user, err := rowToUser(row, r.location)
		if err != nil {
			return false, err
		}
		if strings.ToLower(user.NRP) == nrp || strings.ToLower(user.Email) == email {
			return true, nil
		}
	}
	return false, nil
}

func (r *GoogleSheetsRepository) CreateUser(ctx context.Context, user *model.User) error {
	return r.appendRow(ctx, userSheet, userToRow(user))
}

func (r *GoogleSheetsRepository) UpdateLastLogin(ctx context.Context, userID string, at time.Time) error {
	rows, err := r.readRows(ctx, userSheet, "K")
	if err != nil {
		return err
	}
	user, rowNumber, err := findUserRow(rows, userID, r.location)
	if err != nil {
		return err
	}
	user.LastLoginAt = &at
	user.UpdatedAt = at
	return r.updateRow(ctx, userSheet, rowNumber, userToRow(user), "K")
}

// findUserRow returns the user and its 1-based sheet row number, which is what
// the Sheets API expects in an update range.
func findUserRow(rows [][]interface{}, userID string, location *time.Location) (*model.User, int, error) {
	for _, row := range dataRowsWithIndex(rows) {
		user, err := rowToUser(row.values, location)
		if err != nil {
			return nil, 0, err
		}
		if user.UserID == userID {
			return user, row.rowNumber, nil
		}
	}
	return nil, 0, ErrNotFound
}

// UnitDTExists reads only columns A:B. The foto column holds a base64 data URL
// of up to 45k characters per row, so a full-width read just to compare plates
// would pull megabytes for nothing.
func (r *GoogleSheetsRepository) UnitDTExists(ctx context.Context, nopol string) (bool, error) {
	rows, err := r.readRows(ctx, unitDTSheet, "B")
	if err != nil {
		return false, err
	}
	nopol = strings.ToUpper(strings.TrimSpace(nopol))
	for _, row := range dataRows(rows) {
		if len(row) < 2 {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(cellString(row[1]))) == nopol {
			return true, nil
		}
	}
	return false, nil
}

// MaxUnitDTSequence returns the highest sequence number already used under the
// given ID prefix. It reads column A only, and takes the maximum rather than a
// row count so a deleted row can never make the next ID collide.
func (r *GoogleSheetsRepository) MaxUnitDTSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, unitDTSheet, "A")
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, row := range dataRows(rows) {
		if len(row) == 0 {
			continue
		}
		sequence, ok := unitSequence(cellString(row[0]), prefix)
		if ok && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func unitSequence(unitID, prefix string) (int, bool) {
	trimmed := strings.TrimSpace(unitID)
	if !strings.HasPrefix(trimmed, prefix) {
		return 0, false
	}
	sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
	if err != nil || sequence <= 0 {
		return 0, false
	}
	return sequence, true
}

func (r *GoogleSheetsRepository) CreateUnitDT(ctx context.Context, unit *model.UnitDT) error {
	return r.appendRow(ctx, unitDTSheet, unitDTToRow(unit))
}

func unitDTToRow(unit *model.UnitDT) []interface{} {
	return []interface{}{
		unit.UnitID, unit.Nopol, formatFloat(unit.Panjang), formatFloat(unit.Lebar),
		formatFloat(unit.Tinggi), unit.Driver, unit.Keterangan, unit.Foto,
		unit.CreatedBy, unit.CreatedByID, formatDateTime(unit.CreatedAt), formatDateTime(unit.UpdatedAt),
	}
}

func (r *GoogleSheetsRepository) AppendActivity(ctx context.Context, activity *model.LoginActivity) error {
	return r.appendRow(ctx, activitySheet, activityToRow(activity))
}

func (r *GoogleSheetsRepository) FindAttendanceByUserDate(ctx context.Context, userID, date string) (*model.Attendance, int, error) {
	rows, err := r.readRows(ctx, attendanceSheet, "V")
	if err != nil {
		return nil, 0, err
	}
	for _, row := range dataRowsWithIndex(rows) {
		attendance, err := rowToAttendance(row.values, r.location)
		if err != nil {
			return nil, 0, err
		}
		if attendance.UserID == userID && attendance.TanggalAbsensi == date {
			return attendance, row.rowNumber, nil
		}
	}
	return nil, 0, nil
}

func (r *GoogleSheetsRepository) CreateAttendance(ctx context.Context, attendance *model.Attendance) error {
	return r.appendRow(ctx, attendanceSheet, attendanceToRow(attendance))
}

func (r *GoogleSheetsRepository) UpdateAttendance(ctx context.Context, rowNumber int, attendance *model.Attendance) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid attendance row number %d", rowNumber)
	}
	return r.updateRow(ctx, attendanceSheet, rowNumber, attendanceToRow(attendance), "V")
}

func (r *GoogleSheetsRepository) readRows(ctx context.Context, sheetName, endColumn string) ([][]interface{}, error) {
	rangeName := fmt.Sprintf("%s!A:%s", quoteSheet(sheetName), endColumn)
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", sheetName, err)
	}
	return values.Values, nil
}

func (r *GoogleSheetsRepository) appendRow(ctx context.Context, sheetName string, row []interface{}) error {
	rangeName := fmt.Sprintf("%s!A:A", quoteSheet(sheetName))
	_, err := r.service.Spreadsheets.Values.Append(r.spreadsheetID, rangeName, &sheets.ValueRange{Values: [][]interface{}{row}}).
		ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("append row to %s: %w", sheetName, err)
	}
	return nil
}

func (r *GoogleSheetsRepository) updateRow(ctx context.Context, sheetName string, rowNumber int, row []interface{}, endColumn string) error {
	// Row 1 holds the header and the Sheets API rejects row 0 outright, so a
	// row number below 2 always means the caller computed it wrong.
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, sheetName)
	}
	rangeName := fmt.Sprintf("%s!A%d:%s%d", quoteSheet(sheetName), rowNumber, endColumn, rowNumber)
	_, err := r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName, &sheets.ValueRange{Values: [][]interface{}{row}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update row %s %d: %w", sheetName, rowNumber, err)
	}
	return nil
}

func (r *GoogleSheetsRepository) writeRange(ctx context.Context, sheetName, cell string, values []string) error {
	rangeName := fmt.Sprintf("%s!%s", quoteSheet(sheetName), cell)
	_, err := r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName, &sheets.ValueRange{Values: [][]interface{}{stringsToInterfaces(values)}}).
		ValueInputOption("RAW").Context(ctx).Do()
	return err
}

type indexedRow struct {
	values    []interface{}
	rowNumber int
}

func dataRows(rows [][]interface{}) [][]interface{} {
	if len(rows) <= 1 {
		return nil
	}
	result := make([][]interface{}, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		result = append(result, row)
	}
	return result
}

func dataRowsWithIndex(rows [][]interface{}) []indexedRow {
	if len(rows) <= 1 {
		return nil
	}
	result := make([]indexedRow, 0, len(rows)-1)
	for index, row := range rows[1:] {
		if isBlankRow(row) {
			continue
		}
		result = append(result, indexedRow{values: row, rowNumber: index + 2})
	}
	return result
}

func isBlankRow(row []interface{}) bool {
	for _, cell := range row {
		if strings.TrimSpace(cellString(cell)) != "" {
			return false
		}
	}
	return true
}

// padRow widens a row to the sheet's full column count. The Sheets API drops
// trailing empty cells, so a record whose last columns are blank comes back
// shorter than its header.
func padRow(row []interface{}, width int) []interface{} {
	if len(row) >= width {
		return row
	}
	padded := make([]interface{}, width)
	copy(padded, row)
	for i := len(row); i < width; i++ {
		padded[i] = ""
	}
	return padded
}

func userToRow(user *model.User) []interface{} {
	lastLogin := ""
	if user.LastLoginAt != nil {
		lastLogin = formatDateTime(*user.LastLoginAt)
	}
	return []interface{}{
		user.UserID, user.TanggalGabung, user.NamaLengkap, user.NRP, user.Jabatan, user.Email,
		user.PasswordHash, user.StatusPengguna, formatDateTime(user.CreatedAt), formatDateTime(user.UpdatedAt), lastLogin,
	}
}

func activityToRow(activity *model.LoginActivity) []interface{} {
	return []interface{}{
		activity.ActivityID, activity.UserID, activity.NRP, activity.Email, activity.ActivityType,
		formatDateTime(activity.ActivityTime), activity.Status, activity.IPAddress, activity.UserAgent, activity.Message,
	}
}

func attendanceToRow(attendance *model.Attendance) []interface{} {
	return []interface{}{
		attendance.AbsensiID, attendance.UserID, attendance.NRP, attendance.NamaLengkap, attendance.Jabatan,
		attendance.TanggalAbsensi, formatDateTime(attendance.ClockInAt), formatFloat(attendance.ClockInLat),
		formatFloat(attendance.ClockInLng), optionalFloat(attendance.ClockInAccuracy), attendance.ClockInPhoto,
		attendance.ClockInIP, optionalTime(attendance.ClockOutAt), optionalFloat(attendance.ClockOutLat),
		optionalFloat(attendance.ClockOutLng), optionalFloat(attendance.ClockOutAccuracy), attendance.ClockOutPhoto,
		attendance.ClockOutIP, attendance.StatusAbsensi, optionalInt(attendance.DurasiMenit),
		formatDateTime(attendance.CreatedAt), formatDateTime(attendance.UpdatedAt),
	}
}

func rowToUser(row []interface{}, location *time.Location) (*model.User, error) {
	row = padRow(row, len(userHeaders))
	createdAt, err := parseDateTime(cellString(row[8]), location)
	if err != nil {
		return nil, fmt.Errorf("parse user created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[9]), location)
	if err != nil {
		return nil, fmt.Errorf("parse user updated_at: %w", err)
	}
	var lastLoginAt *time.Time
	if value := strings.TrimSpace(cellString(row[10])); value != "" {
		parsed, err := parseDateTime(value, location)
		if err != nil {
			return nil, fmt.Errorf("parse user last_login_at: %w", err)
		}
		lastLoginAt = &parsed
	}
	return &model.User{
		UserID:         cellString(row[0]),
		TanggalGabung:  cellString(row[1]),
		NamaLengkap:    cellString(row[2]),
		NRP:            cellString(row[3]),
		Jabatan:        cellString(row[4]),
		Email:          cellString(row[5]),
		PasswordHash:   cellString(row[6]),
		StatusPengguna: cellString(row[7]),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		LastLoginAt:    lastLoginAt,
	}, nil
}

func rowToAttendance(row []interface{}, location *time.Location) (*model.Attendance, error) {
	row = padRow(row, len(attendanceHeaders))
	clockInAt, err := parseDateTime(cellString(row[6]), location)
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_in_at: %w", err)
	}
	createdAt, err := parseDateTime(cellString(row[20]), location)
	if err != nil {
		return nil, fmt.Errorf("parse attendance created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[21]), location)
	if err != nil {
		return nil, fmt.Errorf("parse attendance updated_at: %w", err)
	}
	clockInLat, err := strconv.ParseFloat(cellString(row[7]), 64)
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_in_lat: %w", err)
	}
	clockInLng, err := strconv.ParseFloat(cellString(row[8]), 64)
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_in_lng: %w", err)
	}

	clockInAccuracy, err := parseOptionalFloat(cellString(row[9]))
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_in_accuracy: %w", err)
	}
	clockOutAt, err := parseOptionalTime(cellString(row[12]), location)
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_out_at: %w", err)
	}
	clockOutLat, err := parseOptionalFloat(cellString(row[13]))
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_out_lat: %w", err)
	}
	clockOutLng, err := parseOptionalFloat(cellString(row[14]))
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_out_lng: %w", err)
	}
	clockOutAccuracy, err := parseOptionalFloat(cellString(row[15]))
	if err != nil {
		return nil, fmt.Errorf("parse attendance clock_out_accuracy: %w", err)
	}
	durasi, err := parseOptionalInt(cellString(row[19]))
	if err != nil {
		return nil, fmt.Errorf("parse attendance durasi_menit: %w", err)
	}

	if location == nil {
		location = time.Local
	}
	return &model.Attendance{
		AbsensiID:        cellString(row[0]),
		UserID:           cellString(row[1]),
		NRP:              cellString(row[2]),
		NamaLengkap:      cellString(row[3]),
		Jabatan:          cellString(row[4]),
		TanggalAbsensi:   cellString(row[5]),
		ClockInAt:        clockInAt.In(location),
		ClockInLat:       clockInLat,
		ClockInLng:       clockInLng,
		ClockInAccuracy:  clockInAccuracy,
		ClockInPhoto:     cellString(row[10]),
		ClockInIP:        cellString(row[11]),
		ClockOutAt:       clockOutAt,
		ClockOutLat:      clockOutLat,
		ClockOutLng:      clockOutLng,
		ClockOutAccuracy: clockOutAccuracy,
		ClockOutPhoto:    cellString(row[16]),
		ClockOutIP:       cellString(row[17]),
		StatusAbsensi:    cellString(row[18]),
		DurasiMenit:      durasi,
		CreatedAt:        createdAt.In(location),
		UpdatedAt:        updatedAt.In(location),
	}, nil
}

func parseDateTime(value string, location *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}
	if location == nil {
		location = time.Local
	}
	for _, layout := range []string{datetimeLayout, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported datetime %q", value)
}

func parseOptionalTime(value string, location *time.Location) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseDateTime(value, location)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalFloat(value string) (*float64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func parseOptionalInt(value string) (*int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func formatDateTime(value time.Time) string {
	return value.Format(datetimeLayout)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func optionalFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return formatFloat(*value)
}

func optionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatDateTime(*value)
}

func cellString(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(typed)
	}
}

func stringsToInterfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func quoteSheet(name string) string {
	return "'" + strings.ReplaceAll(name, "'", "''") + "'"
}
