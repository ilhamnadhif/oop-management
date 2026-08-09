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
	produksiSheet   = "Produksi"
	unitA2BSheet    = "Unit A2B"
	notaSheet       = "Nota"
	notaItemSheet   = "Nota Item"

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

var produksiHeaders = []string{
	"produksi_id", "tanggal", "project", "supplier", "quary", "kategori", "lokasi", "layer",
	"unit_id", "nopol", "driver", "jenis_dt",
	"panjang_m", "lebar_m", "tinggi_m", "tt_m", "tf_m",
	"volume_m3", "volume_opp_m3", "deviasi_m3",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

var unitA2BHeaders = []string{
	"no_urut", "tanggal_input", "id_unit", "nama_unit", "merek_type",
	"fuel_storage_liter", "fr_unit_liter_per_jam", "lokasi_unit", "hm_awal",
	"foto_unit", "dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

type GoogleSheetsRepository struct {
	service       *sheets.Service
	spreadsheetID string
	location      *time.Location
}

func NewGoogleSheetsRepository(service *sheets.Service, spreadsheetID string, location *time.Location) *GoogleSheetsRepository {
	return &GoogleSheetsRepository{service: service, spreadsheetID: spreadsheetID, location: location}
}

// The attachments sit at the end of the header sheet so the columns before
// them can be read without dragging base64 images along.
var notaHeaders = []string{
	"nota_id", "tanggal", "pic", "metode_pembayaran", "status_pembayaran",
	"penerima_reimburse", "kategori", "sub_kategori", "jenis_perjalanan_dinas",
	"total", "foto_kwitansi", "bukti_transfer",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
	// Filled in at reconciliation, after the row already exists.
	"bukti_bayar", "dibayar_pada", "direkonsiliasi_oleh", "direkonsiliasi_oleh_user_id",
}

// The columns the settlement writes, so a payment never has to rewrite the
// attachments sitting in between.
const (
	notaStatusColumn    = "E"
	notaUpdatedColumn   = "P"
	notaSettlementRange = "Q:T"
)

var notaItemHeaders = []string{
	"nota_id", "baris", "nama_produk", "satuan", "volume", "harga", "subtotal",
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

	missing := []string{userSheet, activitySheet, attendanceSheet, unitDTSheet, produksiSheet, unitA2BSheet, notaSheet, notaItemSheet}
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
		{name: produksiSheet, headers: produksiHeaders},
		{name: unitA2BSheet, headers: unitA2BHeaders},
		{name: notaSheet, headers: notaHeaders},
		{name: notaItemSheet, headers: notaItemHeaders},
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
	if len(actual) > len(expected) {
		return fmt.Errorf("header mismatch in sheet %q: expected %d columns, got %d", sheetName, len(expected), len(actual))
	}
	for i, header := range actual {
		if cellString(header) != expected[i] {
			return fmt.Errorf("header mismatch in sheet %q at column %d: expected %q, got %q", sheetName, i+1, expected[i], cellString(header))
		}
	}
	// A sheet written before a column existed is short, not wrong: the new
	// columns are appended rather than treated as a schema conflict, so an
	// existing spreadsheet keeps working across a release that adds one.
	if len(actual) < len(expected) {
		if err := r.writeRange(ctx, sheetName, "A1", expected); err != nil {
			return fmt.Errorf("extend header for %s: %w", sheetName, err)
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

// ListUnitDT reads columns A:G, deliberately stopping before the foto column.
// The production form only needs the plate and its dimensions, and pulling
// every base64 photo would make the page weigh megabytes.
func (r *GoogleSheetsRepository) ListUnitDT(ctx context.Context) ([]model.UnitDT, error) {
	rows, err := r.readRows(ctx, unitDTSheet, "G")
	if err != nil {
		return nil, err
	}
	units := make([]model.UnitDT, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, 7)
		nopol := strings.TrimSpace(cellString(row[1]))
		if nopol == "" {
			continue
		}
		units = append(units, model.UnitDT{
			UnitID:     cellString(row[0]),
			Nopol:      nopol,
			Panjang:    parseFloatCell(row[2]),
			Lebar:      parseFloatCell(row[3]),
			Tinggi:     parseFloatCell(row[4]),
			Driver:     cellString(row[5]),
			Keterangan: cellString(row[6]),
		})
	}
	return units, nil
}

func parseFloatCell(cell interface{}) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(cellString(cell)), 64)
	if err != nil {
		return 0
	}
	return value
}

func (r *GoogleSheetsRepository) MaxProduksiSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, produksiSheet, "A")
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, row := range dataRows(rows) {
		if len(row) == 0 {
			continue
		}
		if sequence, ok := unitSequence(cellString(row[0]), prefix); ok && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *GoogleSheetsRepository) CreateProduksi(ctx context.Context, produksi *model.Produksi) error {
	return r.appendRow(ctx, produksiSheet, produksiToRow(produksi))
}

// produksiBatchSize caps how many rows travel in one append. Importing a
// backlog one row at a time would take an hour and trip the per-minute write
// quota; sending everything in a single request risks a payload the API
// rejects outright.
const produksiBatchSize = 500

func (r *GoogleSheetsRepository) CreateProduksiBatch(ctx context.Context, rows []*model.Produksi) error {
	for start := 0; start < len(rows); start += produksiBatchSize {
		end := start + produksiBatchSize
		if end > len(rows) {
			end = len(rows)
		}
		values := make([][]interface{}, 0, end-start)
		for _, produksi := range rows[start:end] {
			values = append(values, produksiToRow(produksi))
		}
		rangeName := fmt.Sprintf("%s!A:A", quoteSheet(produksiSheet))
		_, err := r.service.Spreadsheets.Values.Append(r.spreadsheetID, rangeName, &sheets.ValueRange{Values: values}).
			ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("append produksi rows %d-%d: %w", start+1, end, err)
		}
	}
	return nil
}

// ListProduksi reads the columns the dashboard aggregates over, stopping at T.
// The trailing audit columns are of no use to a chart and this sheet already
// holds thousands of rows.
func (r *GoogleSheetsRepository) ListProduksi(ctx context.Context) ([]model.Produksi, error) {
	rows, err := r.readRows(ctx, produksiSheet, "T")
	if err != nil {
		return nil, err
	}
	result := make([]model.Produksi, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, 20)
		tanggal := strings.TrimSpace(cellString(row[1]))
		if tanggal == "" {
			continue
		}
		result = append(result, model.Produksi{
			ProduksiID: cellString(row[0]),
			Tanggal:    tanggal,
			Project:    cellString(row[2]),
			Supplier:   cellString(row[3]),
			Quary:      cellString(row[4]),
			Kategori:   cellString(row[5]),
			Lokasi:     cellString(row[6]),
			Layer:      cellString(row[7]),
			UnitID:     cellString(row[8]),
			Nopol:      cellString(row[9]),
			Driver:     cellString(row[10]),
			JenisDT:    cellString(row[11]),
			Panjang:    parseFloatCell(row[12]),
			Lebar:      parseFloatCell(row[13]),
			Tinggi:     parseFloatCell(row[14]),
			TT:         parseFloatCell(row[15]),
			TF:         parseFloatCell(row[16]),
			Volume:     parseFloatCell(row[17]),
			VolumeOPP:  parseFloatCell(row[18]),
			Deviasi:    parseFloatCell(row[19]),
		})
	}
	return result, nil
}

func produksiToRow(produksi *model.Produksi) []interface{} {
	return []interface{}{
		produksi.ProduksiID, produksi.Tanggal, produksi.Project, produksi.Supplier,
		produksi.Quary, produksi.Kategori, produksi.Lokasi, produksi.Layer,
		produksi.UnitID, produksi.Nopol, produksi.Driver, produksi.JenisDT,
		formatFloat(produksi.Panjang), formatFloat(produksi.Lebar), formatFloat(produksi.Tinggi),
		formatFloat(produksi.TT), formatFloat(produksi.TF),
		formatFloat(produksi.Volume), formatFloat(produksi.VolumeOPP), formatFloat(produksi.Deviasi),
		produksi.CreatedBy, produksi.CreatedByID,
		formatDateTime(produksi.CreatedAt), formatDateTime(produksi.UpdatedAt),
	}
}

// UnitA2BExists reads columns A:C only. The foto column carries a base64 data
// URL, so a full-width read just to compare identifiers would pull megabytes.
func (r *GoogleSheetsRepository) UnitA2BExists(ctx context.Context, idUnit string) (bool, error) {
	rows, err := r.readRows(ctx, unitA2BSheet, "C")
	if err != nil {
		return false, err
	}
	idUnit = strings.ToUpper(strings.TrimSpace(idUnit))
	for _, row := range dataRows(rows) {
		row = padRow(row, 3)
		if strings.ToUpper(strings.TrimSpace(cellString(row[2]))) == idUnit {
			return true, nil
		}
	}
	return false, nil
}

// MaxUnitA2BNumber returns the highest running number in use. It takes the
// maximum rather than a row count so a deleted row cannot make the next number
// collide with a surviving one.
func (r *GoogleSheetsRepository) MaxUnitA2BNumber(ctx context.Context) (int, error) {
	rows, err := r.readRows(ctx, unitA2BSheet, "A")
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, row := range dataRows(rows) {
		if len(row) == 0 {
			continue
		}
		number, err := strconv.Atoi(strings.TrimSpace(cellString(row[0])))
		if err == nil && number > highest {
			highest = number
		}
	}
	return highest, nil
}

// ListUnitA2B reads columns A:I, stopping before the foto column so the picker
// suggestions do not drag base64 images along with them.
func (r *GoogleSheetsRepository) ListUnitA2B(ctx context.Context) ([]model.UnitA2B, error) {
	rows, err := r.readRows(ctx, unitA2BSheet, "I")
	if err != nil {
		return nil, err
	}
	units := make([]model.UnitA2B, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, 9)
		idUnit := strings.TrimSpace(cellString(row[2]))
		if idUnit == "" {
			continue
		}
		number, _ := strconv.Atoi(strings.TrimSpace(cellString(row[0])))
		units = append(units, model.UnitA2B{
			NoUrut:      number,
			TanggalIn:   cellString(row[1]),
			IDUnit:      idUnit,
			NamaUnit:    cellString(row[3]),
			MerekType:   cellString(row[4]),
			FuelStorage: parseFloatCell(row[5]),
			FRUnit:      parseFloatCell(row[6]),
			Lokasi:      cellString(row[7]),
			HMAwal:      parseFloatCell(row[8]),
		})
	}
	return units, nil
}

func (r *GoogleSheetsRepository) CreateUnitA2B(ctx context.Context, unit *model.UnitA2B) error {
	return r.appendRow(ctx, unitA2BSheet, unitA2BToRow(unit))
}

func unitA2BToRow(unit *model.UnitA2B) []interface{} {
	return []interface{}{
		strconv.Itoa(unit.NoUrut), unit.TanggalIn, unit.IDUnit, unit.NamaUnit, unit.MerekType,
		formatFloat(unit.FuelStorage), formatFloat(unit.FRUnit), unit.Lokasi, formatFloat(unit.HMAwal),
		unit.Foto, unit.CreatedBy, unit.CreatedByID,
		formatDateTime(unit.CreatedAt), formatDateTime(unit.UpdatedAt),
	}
}

// MaxNotaSequence reports the highest number issued under a prefix, which is
// one day's worth of notes. Taking the maximum rather than a row count keeps a
// deleted row from handing its number to the next nota.
func (r *GoogleSheetsRepository) MaxNotaSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, notaSheet, "A")
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, row := range dataRows(rows) {
		if len(row) == 0 {
			continue
		}
		if sequence, ok := unitSequence(cellString(row[0]), prefix); ok && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

// CreateNota writes the header first and the lines after it. Sheets has no
// transaction: a header without its lines reads as a nota totalling more than
// its detail, which is visible and fixable, while orphaned lines belong to
// nothing and cannot be traced back.
func (r *GoogleSheetsRepository) CreateNota(ctx context.Context, nota *model.Nota) error {
	if err := r.appendRow(ctx, notaSheet, notaToRow(nota)); err != nil {
		return err
	}
	if len(nota.Items) == 0 {
		return nil
	}
	rows := make([][]interface{}, 0, len(nota.Items))
	for _, item := range nota.Items {
		rows = append(rows, notaItemToRow(nota.NotaID, item))
	}
	return r.appendRows(ctx, notaItemSheet, rows)
}

// ListNota reads the header columns only, stopping before the attachments.
func (r *GoogleSheetsRepository) ListNota(ctx context.Context) ([]model.Nota, error) {
	rows, err := r.readRows(ctx, notaSheet, "J")
	if err != nil {
		return nil, err
	}
	notas := make([]model.Nota, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, 10)
		notaID := strings.TrimSpace(cellString(row[0]))
		if notaID == "" {
			continue
		}
		notas = append(notas, model.Nota{
			NotaID:            notaID,
			Tanggal:           cellString(row[1]),
			PIC:               cellString(row[2]),
			MetodePembayaran:  cellString(row[3]),
			StatusPembayaran:  cellString(row[4]),
			PenerimaReimburse: cellString(row[5]),
			Kategori:          cellString(row[6]),
			SubKategori:       cellString(row[7]),
			JenisPerjalanan:   cellString(row[8]),
			Total:             parseFloatCell(row[9]),
		})
	}
	return notas, nil
}

// ListNotaItems reads every line in the detail sheet.
func (r *GoogleSheetsRepository) ListNotaItems(ctx context.Context) ([]model.NotaItem, error) {
	rows, err := r.readRows(ctx, notaItemSheet, "G")
	if err != nil {
		return nil, err
	}
	items := make([]model.NotaItem, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, 7)
		notaID := strings.TrimSpace(cellString(row[0]))
		if notaID == "" {
			continue
		}
		baris, _ := strconv.Atoi(strings.TrimSpace(cellString(row[1])))
		items = append(items, model.NotaItem{
			NotaID:     notaID,
			Baris:      baris,
			NamaProduk: cellString(row[2]),
			Satuan:     cellString(row[3]),
			Volume:     parseFloatCell(row[4]),
			Harga:      parseFloatCell(row[5]),
			Subtotal:   parseFloatCell(row[6]),
		})
	}
	return items, nil
}

func notaToRow(nota *model.Nota) []interface{} {
	return []interface{}{
		nota.NotaID, nota.Tanggal, nota.PIC, nota.MetodePembayaran, nota.StatusPembayaran,
		nota.PenerimaReimburse, nota.Kategori, nota.SubKategori, nota.JenisPerjalanan,
		formatFloat(nota.Total), nota.FotoKwitansi, nota.BuktiTransfer,
		nota.CreatedBy, nota.CreatedByID,
		formatDateTime(nota.CreatedAt), formatDateTime(nota.UpdatedAt),
		nota.BuktiBayar, formatNullableDateTime(nota.DibayarPada),
		nota.DirekonsiliasiOleh, nota.DirekonsiliasiOlehID,
	}
}

// FindNotaRow locates one nota by its identifier and reports the row it sits
// on. It stops before the attachment columns: finding a nota must not drag two
// base64 images along with it.
func (r *GoogleSheetsRepository) FindNotaRow(ctx context.Context, notaID string) (*model.Nota, int, error) {
	rows, err := r.readRows(ctx, notaSheet, "J")
	if err != nil {
		return nil, 0, err
	}
	wanted := strings.ToUpper(strings.TrimSpace(notaID))
	for _, row := range dataRowsWithIndex(rows) {
		values := padRow(row.values, 10)
		if strings.ToUpper(strings.TrimSpace(cellString(values[0]))) != wanted {
			continue
		}
		return &model.Nota{
			NotaID:            cellString(values[0]),
			Tanggal:           cellString(values[1]),
			PIC:               cellString(values[2]),
			MetodePembayaran:  cellString(values[3]),
			StatusPembayaran:  cellString(values[4]),
			PenerimaReimburse: cellString(values[5]),
			Kategori:          cellString(values[6]),
			SubKategori:       cellString(values[7]),
			JenisPerjalanan:   cellString(values[8]),
			Total:             parseFloatCell(values[9]),
		}, row.rowNumber, nil
	}
	return nil, 0, ErrNotFound
}

// SettleNota records a payment against an existing row. Only the status, the
// audit stamp and the settlement columns are written: rewriting the whole row
// would mean reading the attachments back and sending them again.
func (r *GoogleSheetsRepository) SettleNota(ctx context.Context, rowNumber int, nota *model.Nota) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, notaSheet)
	}
	sheet := quoteSheet(notaSheet)
	data := []*sheets.ValueRange{
		{
			Range:  fmt.Sprintf("%s!%s%d", sheet, notaStatusColumn, rowNumber),
			Values: [][]interface{}{{nota.StatusPembayaran}},
		},
		{
			Range:  fmt.Sprintf("%s!%s%d", sheet, notaUpdatedColumn, rowNumber),
			Values: [][]interface{}{{formatDateTime(nota.UpdatedAt)}},
		},
		{
			Range: fmt.Sprintf("%s!Q%d:T%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{
				nota.BuktiBayar, formatNullableDateTime(nota.DibayarPada),
				nota.DirekonsiliasiOleh, nota.DirekonsiliasiOlehID,
			}},
		},
	}
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("settle nota row %d: %w", rowNumber, err)
	}
	return nil
}

func notaItemToRow(notaID string, item model.NotaItem) []interface{} {
	return []interface{}{
		notaID, strconv.Itoa(item.Baris), item.NamaProduk, item.Satuan,
		formatFloat(item.Volume), formatFloat(item.Harga), formatFloat(item.Subtotal),
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

// appendRows writes several rows in one request. One request per row would
// spend a round trip per line of a nota and burn the per-minute write quota.
func (r *GoogleSheetsRepository) appendRows(ctx context.Context, sheetName string, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	rangeName := fmt.Sprintf("%s!A:A", quoteSheet(sheetName))
	_, err := r.service.Spreadsheets.Values.Append(r.spreadsheetID, rangeName, &sheets.ValueRange{Values: rows}).
		ValueInputOption("RAW").InsertDataOption("INSERT_ROWS").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("append rows to %s: %w", sheetName, err)
	}
	return nil
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

// formatNullableDateTime leaves the cell empty rather than writing a zero time,
// which would read as a payment made in year one.
func formatNullableDateTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return formatDateTime(*value)
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
