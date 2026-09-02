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
	userSheet          = "user"
	activitySheet      = "activity login"
	jabatanAccessSheet = "jabatan akses"
	jabatanSheet       = "jabatan"
	exportConfigSheet  = "export config"
	attendanceSheet    = "absensi data"
	unitDTSheet        = "Unit DT"
	produksiSheet      = "Produksi"
	planSheet          = "Produksi Plan"
	unitA2BSheet       = "Unit A2B"
	notaSheet          = "Nota"
	notaItemSheet      = "Nota Item"
	leaveSheet         = "Leave"
	fuelMasukSheet     = "Fuel Masuk"
	fuelKeluarSheet    = "Fuel Keluar"
	hourMeterSheet     = "Input HM"
	produksiScanSheet  = "Produksi Scan"
	projectSheet       = "Project"

	datetimeLayout = "2006-01-02 15:04:05"
)

var userHeaders = []string{
	"user_id", "tanggal_gabung", "nama_lengkap", "nrp", "jabatan", "email",
	"password_hash", "status_pengguna", "created_at", "updated_at", "last_login_at",
	// Maintained from the profile dialog. punya_foto is read with the rest of
	// the row; foto_profil is a base64 image and is fetched only by ReadUserPhoto.
	"no_telp", "tanggal_lahir", "punya_foto", "foto_profil",
	// The project this account works in, or "*" for an account that reaches
	// every one of them.
	"project",
}

// userReadColumn stops one short of the photo. Every listing and every lookup
// uses it: the session user is loaded on each authenticated request, and
// dragging one base64 image per row through that would cost megabytes an hour.
//
// The project column sits on the far side of the photo, because appending a
// column costs an existing sheet nothing while inserting one in the middle
// means moving every row's data. Reads fetch the two sides in one request and
// stitch them, which is what the fuel sheets already do for the same reason.
const (
	userReadColumn    = "N"
	userPhotoColumn   = "O"
	userProjectColumn = "P"
)

// userPhotoIndex is where the photo sits in a stitched user row: the column the
// read deliberately skips, left blank so the indexes after it still line up
// with the header.
const userPhotoIndex = 14

var activityHeaders = []string{
	"activity_id", "user_id", "nrp", "email", "activity_type", "activity_time",
	"status", "ip_address", "user_agent", "message",
}

// jabatanAccessHeaders hold one row per position that has custom menu rights.
// menu_aktif is the comma-separated list of top-level menu keys the position
// may open; a row absent from the sheet means the position follows the
// built-in defaults.
// The project column came after the rest. A row that leaves it blank is the
// rule the whole app follows, which is what every row written before positions
// belonged to a project holds.
var jabatanAccessHeaders = []string{
	"jabatan", "menu_aktif", "project",
}

var jabatanHeaders = []string{
	"project", "nama", "dibuat_oleh", "created_at",
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

// The plan sheet mirrors the columns the planners already keep, plus the audit
// trail every other sheet carries.
var produksiPlanHeaders = []string{
	"plan_id", "tanggal", "project", "supplier", "lokasi", "volume_m3",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

// The photo is last on purpose: the duplicate check reads up to created_at and
// never pays for tens of thousands of base64 characters it does not need.
var produksiScanHeaders = []string{
	"scan_id", "sidik_sha256", "baris_masuk", "baris_ditolak",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "foto_lembar",
}

// The project sheet lives in the master spreadsheet only. It is the one sheet
// that says where the others are: every row names a spreadsheet of its own, and
// the first project's row names the master itself.
//
// The settings columns are all optional. A blank cell means "use the deployment
// default", so a project added with nothing but a name and a spreadsheet id
// behaves exactly as the app did before it had settings.
var projectHeaders = []string{
	"project_id", "nama", "spreadsheet_id", "menu_aktif", "status",
	"work_start", "work_end", "late_tolerance_minutes", "a2b_work_minutes",
	"company", "signatory_name", "signatory_title", "signatory_place",
	"created_at", "updated_at",
}

// exportConfigHeaders hold one row per project-and-export setting. The three
// slots are left, centre and right in sheet order; TTD count says how many of
// them print.
var exportConfigHeaders = []string{
	"project_id", "export_key", "aktif", "ttd_count",
	"slot1_nama", "slot1_jabatan", "slot2_nama", "slot2_jabatan",
	"slot3_nama", "slot3_jabatan",
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

// The attachment is deliberately last. List and lookup operations stop at S,
// avoiding large base64 image data unless an authorized handler asks for one
// specific attachment.
var leaveHeaders = []string{
	"leave_id", "user_id", "nrp", "nama_lengkap", "jabatan", "jenis_leave",
	"tanggal_mulai", "tanggal_selesai", "jumlah_hari", "alasan", "status",
	"catatan_approval", "diproses_oleh", "diproses_oleh_user_id", "diproses_pada",
	"dibatalkan_pada", "created_at", "updated_at", "has_bukti_pendukung", "bukti_pendukung",
}

// The first thirteen columns are the delivery sheet as the site already keeps
// it, photos and all. Everything this app adds - the approval note and the
// audit trail - is appended after them, so an existing sheet keeps its layout.
var fuelMasukHeaders = []string{
	"no_transaksi", "tanggal_waktu_input", "vendor", "nama_driver", "nopol_truck",
	"jumlah_fuel_masuk_liter", "keterangan", "liter_tidak_sesuai", "status_approval",
	"foto_truck_tampak_depan", "foto_tangki_sebelum_bongkar", "foto_flowmeter",
	"foto_tangki_setelah_bongkar",
	"catatan_approval", "diproses_oleh", "diproses_oleh_user_id", "diproses_pada",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

// The hour meter log carries no photo, so it is read in one range. Every
// standby reason has a column of its own, left empty when it did not happen:
// that is the shape the site already keeps its timesheet in, and it is why a
// reason may be given only once per reading.
var hourMeterHeaders = buildHourMeterHeaders()

func buildHourMeterHeaders() []string {
	headers := []string{
		"no_transaksi", "tanggal", "shift", "id_unit", "nama_unit", "operator",
		"hm_awal_jam", "hm_akhir_jam", "total_hm_jam", "fuel_liter",
		"total_standby_menit",
	}
	for _, variable := range model.StandbyVariables {
		headers = append(headers, variable.StandbyColumn())
	}
	headers = append(headers, "total_bd_menit")
	for _, variable := range model.BreakdownVariables {
		headers = append(headers, model.BreakdownColumn(variable))
	}
	headers = append(headers, "pa_persen", "bd_persen", "ua_persen", "remark")
	return append(headers, "dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at")
}

// Where the standby block starts, and the last column of the row. Both are
// derived so adding a reason cannot leave the reader looking at the wrong cell.
var (
	hourMeterStandbyOffset = 11
	// The breakdown total sits between the two blocks, so its own column is one
	// past the standby block and its reasons start after that.
	hourMeterBreakdownTotalOffset = hourMeterStandbyOffset + len(model.StandbyVariables)
	hourMeterBreakdownOffset      = hourMeterBreakdownTotalOffset + 1
	// The three figures and the remark sit between the breakdown block and the
	// audit trail.
	hourMeterSummaryOffset = hourMeterBreakdownOffset + len(model.BreakdownVariables)
	hourMeterAuditOffset   = hourMeterSummaryOffset + 4
	hourMeterLastColumn    = columnName(len(hourMeterHeaders))
)

// columnName turns a 1-based column number into its spreadsheet letters.
func columnName(number int) string {
	name := ""
	for number > 0 {
		number--
		name = string(rune('A'+number%26)) + name
		number /= 26
	}
	return name
}

// The first ten columns are the dispensing log as the site keeps it; the audit
// trail this app adds follows them.
var fuelKeluarHeaders = []string{
	"no_transaksi", "tanggal_input", "id_unit", "nama_unit",
	"hm_awal_flow_meter", "hm_akhir_flow_meter", "fuel_liter",
	"hm_alat_berat_pengisian", "operator", "foto_akhir_flow_meter",
	"dibuat_oleh", "dibuat_oleh_user_id", "created_at", "updated_at",
}

// The photo sits at J, so reads stop at I and resume at K for the same reason
// the fuel masuk listing does.
const (
	fuelOutHeadColumn  = "I"
	fuelOutTailRange   = "K:N"
	fuelOutHeadWidth   = 9
	fuelOutTailWidth   = 4
	fuelOutPhotoColumn = "J"
)

// Reads stop at I and resume at N: J to M hold four base64 photos, and a list
// of a hundred deliveries would otherwise be several hundred megabytes.
const (
	fuelHeadColumn = "I"
	fuelTailRange  = "N:U"
	// Columns in the head range, and the count of the tail range, so the two
	// reads can be stitched back into one row.
	fuelHeadWidth  = 9
	fuelPhotoCount = 4
	fuelTailWidth  = 8
	// The photos sit at J, K, L and M.
	fuelPhotoFirstColumn = 'J'
	fuelStatusColumn     = "I"
	fuelUpdatedColumn    = "U"
)

// sheetDefinition pairs a sheet with the header it must carry.
type sheetDefinition struct {
	name    string
	headers []string
}

// masterSheets hold identity and the list of projects. They live in one
// spreadsheet no matter how many projects there are: an account has to be
// recognised before anybody knows which project the person belongs to.
var masterSheets = []sheetDefinition{
	{name: userSheet, headers: userHeaders},
	{name: activitySheet, headers: activityHeaders},
	{name: jabatanAccessSheet, headers: jabatanAccessHeaders},
	{name: jabatanSheet, headers: jabatanHeaders},
	{name: exportConfigSheet, headers: exportConfigHeaders},
	{name: projectSheet, headers: projectHeaders},
}

// projectSheets hold the books of one project. Every project has its own
// spreadsheet of these, which is what keeps one project's rows away from
// another's: they are never in the same file, so no filter has to hold.
var projectSheets = []sheetDefinition{
	{name: attendanceSheet, headers: attendanceHeaders},
	{name: unitDTSheet, headers: unitDTHeaders},
	{name: produksiSheet, headers: produksiHeaders},
	{name: planSheet, headers: produksiPlanHeaders},
	{name: produksiScanSheet, headers: produksiScanHeaders},
	{name: unitA2BSheet, headers: unitA2BHeaders},
	{name: notaSheet, headers: notaHeaders},
	{name: notaItemSheet, headers: notaItemHeaders},
	{name: leaveSheet, headers: leaveHeaders},
	{name: fuelMasukSheet, headers: fuelMasukHeaders},
	{name: fuelKeluarSheet, headers: fuelKeluarHeaders},
	{name: hourMeterSheet, headers: hourMeterHeaders},
}

// EnsureSchema prepares the sheets one project's books need. It is what a
// freshly created spreadsheet is handed to become a project store, and it is
// safe to run against one that is already set up.
func (r *GoogleSheetsRepository) EnsureSchema(ctx context.Context) error {
	return r.ensureSheets(ctx, projectSheets)
}

// EnsureMasterSchema prepares the sheets that are not any one project's. The
// first project's spreadsheet is usually also the master, in which case both
// calls run against the same file and between them cover all of it.
func (r *GoogleSheetsRepository) EnsureMasterSchema(ctx context.Context) error {
	return r.ensureSheets(ctx, masterSheets)
}

func (r *GoogleSheetsRepository) ensureSheets(ctx context.Context, definitions []sheetDefinition) error {
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

	requests := make([]*sheets.Request, 0, len(definitions))
	for _, definition := range definitions {
		if existing[definition.name] {
			continue
		}
		requests = append(requests, &sheets.Request{
			AddSheet: &sheets.AddSheetRequest{Properties: &sheets.SheetProperties{Title: definition.name}},
		})
	}
	if len(requests) > 0 {
		if _, err := r.service.Spreadsheets.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateSpreadsheetRequest{Requests: requests}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("create missing sheets: %w", err)
		}
	}

	return r.ensureHeaders(ctx, definitions)
}

// ensureHeaders reads every header row in one request and writes back only the
// ones that need it, in one more.
//
// It used to be a read and a write per sheet, which on a brand new project
// spreadsheet was two dozen round trips to Google - long enough that somebody
// naming a project sat watching a button, and close enough to the server's write
// deadline to be worth not finding out about the hard way.
func (r *GoogleSheetsRepository) ensureHeaders(ctx context.Context, definitions []sheetDefinition) error {
	if len(definitions) == 0 {
		return nil
	}
	ranges := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		ranges = append(ranges, fmt.Sprintf("%s!1:1", quoteSheet(definition.name)))
	}
	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(ranges...).ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("read headers: %w", err)
	}
	// The API answers in the order the ranges were asked for.
	if len(response.ValueRanges) != len(definitions) {
		return fmt.Errorf("read headers: asked for %d ranges, got %d", len(definitions), len(response.ValueRanges))
	}

	writes := make([]*sheets.ValueRange, 0, len(definitions))
	for i, definition := range definitions {
		var actual []interface{}
		if values := response.ValueRanges[i].Values; len(values) > 0 {
			actual = values[0]
		}
		write, err := headerNeedsWriting(definition.name, actual, definition.headers)
		if err != nil {
			return err
		}
		if write {
			writes = append(writes, &sheets.ValueRange{
				Range:  fmt.Sprintf("%s!A1", quoteSheet(definition.name)),
				Values: [][]interface{}{stringsToInterfaces(definition.headers)},
			})
		}
	}
	if len(writes) == 0 {
		return nil
	}
	_, err = r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             writes,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("write headers: %w", err)
	}
	return nil
}

// headerNeedsWriting compares one header row against the one the code expects
// and reports whether it has to be written. A header that disagrees is an error
// rather than something to overwrite: the columns are read by position, and
// rewriting the row would leave every existing value under the wrong name.
func headerNeedsWriting(sheetName string, actual []interface{}, expected []string) (bool, error) {
	if len(actual) == 0 {
		return true, nil
	}
	if len(actual) > len(expected) {
		return false, fmt.Errorf("header mismatch in sheet %q: expected %d columns, got %d", sheetName, len(expected), len(actual))
	}
	for i, header := range actual {
		if cellString(header) != expected[i] {
			return false, fmt.Errorf("header mismatch in sheet %q at column %d: expected %q, got %q", sheetName, i+1, expected[i], cellString(header))
		}
	}
	// A sheet written before a column existed is short, not wrong: the new
	// columns are appended rather than treated as a schema conflict, so an
	// existing spreadsheet keeps working across a release that adds one.
	return len(actual) < len(expected), nil
}

// ListProjects reads every project, in the order the sheet holds them. That
// order is the order the switcher offers, so a site can be moved up the list by
// moving its row.
func (r *GoogleSheetsRepository) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := r.readRows(ctx, projectSheet, "O")
	if err != nil {
		return nil, err
	}
	result := make([]model.Project, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, len(projectHeaders))
		projectID := strings.TrimSpace(cellString(row[0]))
		if projectID == "" {
			continue
		}
		// The audit stamps are read leniently: a row typed straight into the
		// sheet has no timestamps, and refusing it would hide a real project.
		createdAt, _ := parseDateTime(cellString(row[13]), r.location)
		updatedAt, _ := parseDateTime(cellString(row[14]), r.location)
		result = append(result, model.Project{
			ProjectID:     projectID,
			Nama:          strings.TrimSpace(cellString(row[1])),
			SpreadsheetID: strings.TrimSpace(cellString(row[2])),
			MenuAktif:     splitMenus(cellString(row[3])),
			Status:        strings.TrimSpace(cellString(row[4])),
			Settings: model.ProjectSettings{
				WorkStart:            strings.TrimSpace(cellString(row[5])),
				WorkEnd:              strings.TrimSpace(cellString(row[6])),
				LateToleranceMinutes: int(parseFloatCell(row[7])),
				A2BWorkMinutes:       int(parseFloatCell(row[8])),
				Company:              strings.TrimSpace(cellString(row[9])),
				SignatoryName:        strings.TrimSpace(cellString(row[10])),
				SignatoryTitle:       strings.TrimSpace(cellString(row[11])),
				SignatoryPlace:       strings.TrimSpace(cellString(row[12])),
			},
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}
	return result, nil
}

func (r *GoogleSheetsRepository) CreateProject(ctx context.Context, project *model.Project) error {
	return r.appendRow(ctx, projectSheet, projectToRow(project))
}

// FindProjectRow locates a project and reports the row it sits on, so the
// settings screen can write back to it.
func (r *GoogleSheetsRepository) FindProjectRow(ctx context.Context, projectID string) (*model.Project, int, error) {
	rows, err := r.readRows(ctx, projectSheet, "O")
	if err != nil {
		return nil, 0, err
	}
	projectID = strings.TrimSpace(projectID)
	for _, row := range dataRowsWithIndex(rows) {
		values := padRow(row.values, len(projectHeaders))
		if !strings.EqualFold(strings.TrimSpace(cellString(values[0])), projectID) {
			continue
		}
		projects, err := r.ListProjects(ctx)
		if err != nil {
			return nil, 0, err
		}
		for i := range projects {
			if strings.EqualFold(projects[i].ProjectID, projectID) {
				return &projects[i], row.rowNumber, nil
			}
		}
	}
	return nil, 0, fmt.Errorf("project %s not found", projectID)
}

// UpdateProject rewrites everything the settings screen owns. The spreadsheet
// id is written too but the screen does not offer it for editing: repointing a
// project at another file would orphan every row already in this one.
func (r *GoogleSheetsRepository) UpdateProject(ctx context.Context, rowNumber int, project *model.Project) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, projectSheet)
	}
	rangeName := fmt.Sprintf("%s!B%d:O%d", quoteSheet(projectSheet), rowNumber, rowNumber)
	_, err := r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName,
		&sheets.ValueRange{Values: [][]interface{}{projectToRow(project)[1:]}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update project row %d: %w", rowNumber, err)
	}
	return nil
}

func (r *GoogleSheetsRepository) MaxProjectSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, projectSheet, "A")
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

// UpdateUserPassword writes the password cell and the audit stamp, and nothing
// else. Rewriting the whole row would blank the profile photo, which no read
// that reaches here has fetched.
func (r *GoogleSheetsRepository) UpdateUserPassword(ctx context.Context, rowNumber int, passwordHash string, at time.Time) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, userSheet)
	}
	sheet := quoteSheet(userSheet)
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data: []*sheets.ValueRange{
			{Range: fmt.Sprintf("%s!G%d", sheet, rowNumber), Values: [][]interface{}{{passwordHash}}},
			{Range: fmt.Sprintf("%s!J%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(at)}}},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update password row %d: %w", rowNumber, err)
	}
	return nil
}

// CountUsers reports how many accounts exist. It answers one question: whether
// this deployment has anybody yet, which is what decides if the bootstrap
// registration page is open.
func (r *GoogleSheetsRepository) CountUsers(ctx context.Context) (int, error) {
	rows, err := r.readRows(ctx, userSheet, "A")
	if err != nil {
		return 0, err
	}
	return len(dataRows(rows)), nil
}

// UpdateUserJabatan writes the position cell and the audit stamp, and nothing
// else. Like UpdateUserPassword it leaves the profile photo and password alone.
func (r *GoogleSheetsRepository) UpdateUserJabatan(ctx context.Context, rowNumber int, jabatan string, at time.Time) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, userSheet)
	}
	sheet := quoteSheet(userSheet)
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data: []*sheets.ValueRange{
			{Range: fmt.Sprintf("%s!E%d", sheet, rowNumber), Values: [][]interface{}{{jabatan}}},
			{Range: fmt.Sprintf("%s!J%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(at)}}},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update user jabatan row %d: %w", rowNumber, err)
	}
	return nil
}

// ListJabatanAccess reads every position's configured menu rights, in sheet
// order. The sheet lives in the master spreadsheet beside the accounts: the
// rights are about who a position is, not which site they work in.
func (r *GoogleSheetsRepository) ListJabatanAccess(ctx context.Context) ([]model.JabatanAccess, error) {
	rows, err := r.readRows(ctx, jabatanAccessSheet, "C")
	if err != nil {
		return nil, err
	}
	access := make([]model.JabatanAccess, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, len(jabatanAccessHeaders))
		jabatan := strings.TrimSpace(cellString(row[0]))
		if jabatan == "" {
			continue
		}
		access = append(access, model.JabatanAccess{
			Jabatan:   jabatan,
			MenuAktif: splitMenus(cellString(row[1])),
			Project:   strings.TrimSpace(cellString(row[2])),
		})
	}
	return access, nil
}

// SaveJabatanAccess writes one position's menu rights, creating the row when
// it is new and replacing the list when it exists.
func (r *GoogleSheetsRepository) SaveJabatanAccess(ctx context.Context, project, jabatan string, menus []string) error {
	rows, err := r.readRows(ctx, jabatanAccessSheet, "C")
	if err != nil {
		return err
	}
	project = strings.TrimSpace(project)
	jabatan = strings.TrimSpace(jabatan)
	rowNumber := 0
	// Keyed by both columns: one position holds a different rule in each
	// project, and the app-wide row is the one whose project is blank.
	for _, row := range dataRowsWithIndex(rows) {
		values := padRow(row.values, len(jabatanAccessHeaders))
		if strings.EqualFold(strings.TrimSpace(cellString(values[0])), jabatan) &&
			strings.EqualFold(strings.TrimSpace(cellString(values[2])), project) {
			rowNumber = row.rowNumber
			break
		}
	}
	value := strings.Join(menus, ",")
	if rowNumber == 0 {
		return r.appendRow(ctx, jabatanAccessSheet, []interface{}{jabatan, value, project})
	}
	rangeName := fmt.Sprintf("%s!A%d:C%d", quoteSheet(jabatanAccessSheet), rowNumber, rowNumber)
	_, err = r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName,
		&sheets.ValueRange{Values: [][]interface{}{{jabatan, value, project}}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update jabatan access row %d: %w", rowNumber, err)
	}
	return nil
}

// ListJabatan reads the positions projects have made for themselves.
func (r *GoogleSheetsRepository) ListJabatan(ctx context.Context) ([]model.Jabatan, error) {
	rows, err := r.readRows(ctx, jabatanSheet, "D")
	if err != nil {
		return nil, err
	}
	list := make([]model.Jabatan, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, len(jabatanHeaders))
		nama := strings.TrimSpace(cellString(row[1]))
		if nama == "" {
			continue
		}
		createdAt, _ := parseDateTime(cellString(row[3]), r.location)
		list = append(list, model.Jabatan{
			Project:    strings.TrimSpace(cellString(row[0])),
			Nama:       nama,
			DibuatOleh: strings.TrimSpace(cellString(row[2])),
			// Read leniently: a row typed straight into the sheet has no
			// timestamp, and refusing it would hide a real position.
			CreatedAt: createdAt,
		})
	}
	return list, nil
}

// CreateJabatan appends one position. The service decides whether the name may
// be used; this only writes the row.
func (r *GoogleSheetsRepository) CreateJabatan(ctx context.Context, jabatan *model.Jabatan) error {
	return r.appendRow(ctx, jabatanSheet, []interface{}{
		jabatan.Project, jabatan.Nama, jabatan.DibuatOleh, formatDateTime(jabatan.CreatedAt),
	})
}

// ListExportConfigs reads every project-and-export setting, in sheet order.
func (r *GoogleSheetsRepository) ListExportConfigs(ctx context.Context) ([]model.ExportConfig, error) {
	rows, err := r.readRows(ctx, exportConfigSheet, "J")
	if err != nil {
		return nil, err
	}
	configs := make([]model.ExportConfig, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, len(exportConfigHeaders))
		projectID := strings.TrimSpace(cellString(row[0]))
		if projectID == "" {
			continue
		}
		configs = append(configs, model.ExportConfig{
			ProjectID: projectID,
			ExportKey: strings.TrimSpace(cellString(row[1])),
			Aktif:     parseBoolLoose(cellString(row[2])),
			TTDCount:  parseIntCell(row[3]),
			Slots: [3]model.ExportSlot{
				{Nama: strings.TrimSpace(cellString(row[4])), Jabatan: strings.TrimSpace(cellString(row[5]))},
				{Nama: strings.TrimSpace(cellString(row[6])), Jabatan: strings.TrimSpace(cellString(row[7]))},
				{Nama: strings.TrimSpace(cellString(row[8])), Jabatan: strings.TrimSpace(cellString(row[9]))},
			},
		})
	}
	return configs, nil
}

// SaveExportConfig writes one project-and-export setting, creating the row when
// it is new and replacing it when it exists. The row is keyed by both columns,
// since one project configures several exports.
func (r *GoogleSheetsRepository) SaveExportConfig(ctx context.Context, config model.ExportConfig) error {
	rows, err := r.readRows(ctx, exportConfigSheet, "A")
	if err != nil {
		return err
	}
	rowNumber := 0
	for _, row := range dataRowsWithIndex(rows) {
		if len(row.values) < 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(cellString(row.values[0])), strings.TrimSpace(config.ProjectID)) &&
			strings.EqualFold(strings.TrimSpace(cellString(row.values[1])), strings.TrimSpace(config.ExportKey)) {
			rowNumber = row.rowNumber
			break
		}
	}
	values := exportConfigToRow(config)
	if rowNumber == 0 {
		return r.appendRow(ctx, exportConfigSheet, values)
	}
	rangeName := fmt.Sprintf("%s!A%d:J%d", quoteSheet(exportConfigSheet), rowNumber, rowNumber)
	_, err = r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName,
		&sheets.ValueRange{Values: [][]interface{}{values}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update export config row %d: %w", rowNumber, err)
	}
	return nil
}

func exportConfigToRow(config model.ExportConfig) []interface{} {
	return []interface{}{
		config.ProjectID, config.ExportKey, boolCell(config.Aktif), strconv.Itoa(config.TTDCount),
		config.Slots[0].Nama, config.Slots[0].Jabatan,
		config.Slots[1].Nama, config.Slots[1].Jabatan,
		config.Slots[2].Nama, config.Slots[2].Jabatan,
	}
}

// boolCell writes a boolean the way the sheets in this app read them.
func boolCell(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// parseBoolLoose reads a boolean cell without rejecting an empty value, which
// a row typed straight into the sheet may well have.
func parseBoolLoose(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "ya", "yes":
		return true
	default:
		return false
	}
}

// UpdateUserProject writes the one cell that says which project an account
// belongs to. It is its own call because assigning somebody to a site must not
// rewrite their password hash or blank their photo.
func (r *GoogleSheetsRepository) UpdateUserProject(ctx context.Context, rowNumber int, project string, at time.Time) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, userSheet)
	}
	sheet := quoteSheet(userSheet)
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data: []*sheets.ValueRange{
			{Range: fmt.Sprintf("%s!%s%d", sheet, userProjectColumn, rowNumber), Values: [][]interface{}{{project}}},
			{Range: fmt.Sprintf("%s!J%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(at)}}},
		},
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update user project row %d: %w", rowNumber, err)
	}
	return nil
}

func projectToRow(project *model.Project) []interface{} {
	return []interface{}{
		project.ProjectID, project.Nama, project.SpreadsheetID,
		strings.Join(project.MenuAktif, ","), project.Status,
		project.Settings.WorkStart, project.Settings.WorkEnd,
		formatOptionalInt(project.Settings.LateToleranceMinutes),
		formatOptionalInt(project.Settings.A2BWorkMinutes),
		project.Settings.Company, project.Settings.SignatoryName,
		project.Settings.SignatoryTitle, project.Settings.SignatoryPlace,
		formatDateTime(project.CreatedAt), formatDateTime(project.UpdatedAt),
	}
}

// formatOptionalInt writes an unset figure as an empty cell rather than a zero,
// because zero and "not configured" mean different things here: one is a
// tolerance of no minutes, the other is the deployment default.
func formatOptionalInt(value int) interface{} {
	if value <= 0 {
		return ""
	}
	return value
}

// splitMenus reads the comma-separated menu list. An empty cell yields no
// entries, which model.Project reads as every menu.
func splitMenus(value string) []string {
	var menus []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			menus = append(menus, trimmed)
		}
	}
	return menus
}

func (r *GoogleSheetsRepository) FindUserByID(ctx context.Context, userID string) (*model.User, error) {
	rows, err := r.readUserRows(ctx)
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
	rows, err := r.readUserRows(ctx)
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

func (r *GoogleSheetsRepository) ListUsers(ctx context.Context) ([]model.User, error) {
	rows, err := r.readUserRows(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]model.User, 0, len(rows))
	for _, row := range dataRows(rows) {
		user, err := rowToUser(row, r.location)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(user.UserID) == "" {
			continue
		}
		users = append(users, *user)
	}
	return users, nil
}

func (r *GoogleSheetsRepository) UserExists(ctx context.Context, nrp, email string) (bool, error) {
	rows, err := r.readUserRows(ctx)
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
	rows, err := r.readUserRows(ctx)
	if err != nil {
		return err
	}
	_, rowNumber, err := findUserRow(rows, userID, r.location)
	if err != nil {
		return err
	}
	// Only the two timestamp cells are written. Rewriting the whole row would
	// blank the profile photo, which this read deliberately did not fetch.
	rangeName := fmt.Sprintf("%s!J%d:K%d", quoteSheet(userSheet), rowNumber, rowNumber)
	_, err = r.service.Spreadsheets.Values.Update(r.spreadsheetID, rangeName,
		&sheets.ValueRange{Values: [][]interface{}{{formatDateTime(at), formatDateTime(at)}}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update last login row %d: %w", rowNumber, err)
	}
	return nil
}

// FindUserRow locates a user and reports the row it sits on. Like every other
// user read it stops before the photo.
func (r *GoogleSheetsRepository) FindUserRow(ctx context.Context, userID string) (*model.User, int, error) {
	rows, err := r.readUserRows(ctx)
	if err != nil {
		return nil, 0, err
	}
	return findUserRow(rows, userID, r.location)
}

// UpdateUserProfile writes only the cells the profile dialog owns. The photo is
// written only when a new one was supplied, so saving a phone number does not
// have to carry the existing image back up to the sheet.
func (r *GoogleSheetsRepository) UpdateUserProfile(ctx context.Context, rowNumber int, user *model.User, updatePhoto bool) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, userSheet)
	}
	sheet := quoteSheet(userSheet)
	data := []*sheets.ValueRange{
		{Range: fmt.Sprintf("%s!C%d", sheet, rowNumber), Values: [][]interface{}{{user.NamaLengkap}}},
		{Range: fmt.Sprintf("%s!J%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(user.UpdatedAt)}}},
		{
			Range:  fmt.Sprintf("%s!L%d:M%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{user.NoTelp, user.TanggalLahir}},
		},
	}
	if updatePhoto {
		data = append(data, &sheets.ValueRange{
			Range:  fmt.Sprintf("%s!N%d:O%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{user.PunyaFoto, user.FotoProfil}},
		})
	}
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update profile row %d: %w", rowNumber, err)
	}
	return nil
}

// ReadUserPhoto fetches one profile photo, the only read that touches that
// column.
func (r *GoogleSheetsRepository) ReadUserPhoto(ctx context.Context, rowNumber int) (string, error) {
	if rowNumber < 2 {
		return "", fmt.Errorf("invalid row number %d for sheet %q", rowNumber, userSheet)
	}
	rangeName := fmt.Sprintf("%s!%s%d", quoteSheet(userSheet), userPhotoColumn, rowNumber)
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("read user photo row %d: %w", rowNumber, err)
	}
	if len(values.Values) == 0 || len(values.Values[0]) == 0 {
		return "", nil
	}
	return strings.TrimSpace(cellString(values.Values[0][0])), nil
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

func parseIntCell(cell interface{}) int {
	value, err := strconv.Atoi(strings.TrimSpace(cellString(cell)))
	if err != nil {
		return 0
	}
	return value
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

func (r *GoogleSheetsRepository) MaxProduksiPlanSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, planSheet, "A")
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

func (r *GoogleSheetsRepository) CreateProduksiPlan(ctx context.Context, plan *model.ProduksiPlan) error {
	return r.appendRow(ctx, planSheet, produksiPlanToRow(plan))
}

func (r *GoogleSheetsRepository) ListProduksiPlan(ctx context.Context) ([]model.ProduksiPlan, error) {
	rows, err := r.readRows(ctx, planSheet, "J")
	if err != nil {
		return nil, err
	}
	result := make([]model.ProduksiPlan, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, len(produksiPlanHeaders))
		planID := strings.TrimSpace(cellString(row[0]))
		if planID == "" {
			continue
		}
		// The audit stamps are read leniently: a row typed straight into the
		// sheet has no timestamps, and refusing it would hide a real plan.
		createdAt, _ := parseDateTime(cellString(row[8]), r.location)
		updatedAt, _ := parseDateTime(cellString(row[9]), r.location)
		result = append(result, model.ProduksiPlan{
			PlanID:      planID,
			Tanggal:     strings.TrimSpace(cellString(row[1])),
			Project:     cellString(row[2]),
			Supplier:    cellString(row[3]),
			Lokasi:      cellString(row[4]),
			Volume:      parseFloatCell(row[5]),
			CreatedBy:   cellString(row[6]),
			CreatedByID: cellString(row[7]),
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
	}
	return result, nil
}

func produksiPlanToRow(plan *model.ProduksiPlan) []interface{} {
	return []interface{}{
		plan.PlanID, plan.Tanggal, plan.Project, plan.Supplier, plan.Lokasi,
		formatFloat(plan.Volume), plan.CreatedBy, plan.CreatedByID,
		formatDateTime(plan.CreatedAt), formatDateTime(plan.UpdatedAt),
	}
}

// UnitA2BExists reads columns A:C only. The foto column carries a base64 data
// URL, so a full-width read just to compare identifiers would pull megabytes.
// MaxProduksiScanSequence reads only the id column: the sheet holds a photo per
// row, and numbering the next scan must not drag every image across the wire.
func (r *GoogleSheetsRepository) MaxProduksiScanSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, produksiScanSheet, "A")
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

func (r *GoogleSheetsRepository) CreateProduksiScan(ctx context.Context, scan *model.ProduksiScan) error {
	return r.appendRow(ctx, produksiScanSheet, produksiScanToRow(scan))
}

// FindProduksiScan looks a fingerprint up and stops at created_at, so the answer
// to "has this file been filed before" costs the metadata and not the photos.
func (r *GoogleSheetsRepository) FindProduksiScan(ctx context.Context, sidik string) (*model.ProduksiScan, error) {
	sidik = strings.TrimSpace(sidik)
	if sidik == "" {
		return nil, nil
	}
	rows, err := r.readRows(ctx, produksiScanSheet, "G")
	if err != nil {
		return nil, err
	}
	for _, row := range dataRows(rows) {
		scan := rowToProduksiScan(row, r.location)
		if scan.ScanID == "" {
			continue
		}
		if strings.EqualFold(scan.Sidik, sidik) {
			return &scan, nil
		}
	}
	return nil, nil
}

func produksiScanToRow(scan *model.ProduksiScan) []interface{} {
	return []interface{}{
		scan.ScanID, scan.Sidik,
		strconv.Itoa(scan.BarisMasuk), strconv.Itoa(scan.BarisDitolak),
		scan.DibuatOleh, scan.DibuatOlehID, formatDateTime(scan.CreatedAt), scan.Foto,
	}
}

// rowToProduksiScan tolerates a row cut short by a narrow read, which is how the
// duplicate check gets its answer without the photo column.
func rowToProduksiScan(row []interface{}, location *time.Location) model.ProduksiScan {
	row = padRow(row, len(produksiScanHeaders))
	createdAt, _ := parseDateTime(cellString(row[6]), location)
	return model.ProduksiScan{
		ScanID:       strings.TrimSpace(cellString(row[0])),
		Sidik:        strings.TrimSpace(cellString(row[1])),
		BarisMasuk:   parseIntCell(row[2]),
		BarisDitolak: parseIntCell(row[3]),
		DibuatOleh:   cellString(row[4]),
		DibuatOlehID: cellString(row[5]),
		CreatedAt:    createdAt,
		Foto:         cellString(row[7]),
	}
}

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
	return r.listNota(ctx, false)
}

// ListNotaWithAttachments reads one column further, for the receipt photo the
// export prints. It costs a base64 image per nota, which is why the screens
// that only list notes call ListNota instead. The transfer and settlement
// proofs are deliberately left behind: nothing prints them.
func (r *GoogleSheetsRepository) ListNotaWithAttachments(ctx context.Context) ([]model.Nota, error) {
	return r.listNota(ctx, true)
}

func (r *GoogleSheetsRepository) listNota(ctx context.Context, withAttachments bool) ([]model.Nota, error) {
	lastColumn, width := "J", 10
	if withAttachments {
		lastColumn, width = "K", 11
	}
	rows, err := r.readRows(ctx, notaSheet, lastColumn)
	if err != nil {
		return nil, err
	}
	notas := make([]model.Nota, 0, len(rows))
	for _, row := range dataRows(rows) {
		row = padRow(row, width)
		notaID := strings.TrimSpace(cellString(row[0]))
		if notaID == "" {
			continue
		}
		nota := model.Nota{
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
		}
		if withAttachments {
			nota.FotoKwitansi = cellString(row[10])
		}
		notas = append(notas, nota)
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

func (r *GoogleSheetsRepository) MaxLeaveSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, leaveSheet, "A")
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

func (r *GoogleSheetsRepository) CreateLeave(ctx context.Context, leave *model.Leave) error {
	return r.appendRow(ctx, leaveSheet, leaveToRow(leave))
}

// ListLeave deliberately stops at S. Column T can contain a multi-megabyte
// base64 image and is fetched only by ReadLeaveAttachment.
func (r *GoogleSheetsRepository) ListLeave(ctx context.Context) ([]model.Leave, error) {
	rows, err := r.readRows(ctx, leaveSheet, "S")
	if err != nil {
		return nil, err
	}
	leaves := make([]model.Leave, 0, len(rows))
	for _, row := range dataRows(rows) {
		leave, err := rowToLeave(row, r.location)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(leave.LeaveID) == "" {
			continue
		}
		leave.BuktiPendukung = ""
		leaves = append(leaves, *leave)
	}
	return leaves, nil
}

func (r *GoogleSheetsRepository) FindLeaveRow(ctx context.Context, leaveID string) (*model.Leave, int, error) {
	rows, err := r.readRows(ctx, leaveSheet, "S")
	if err != nil {
		return nil, 0, err
	}
	wanted := strings.ToUpper(strings.TrimSpace(leaveID))
	for _, row := range dataRowsWithIndex(rows) {
		values := padRow(row.values, 19)
		if strings.ToUpper(strings.TrimSpace(cellString(values[0]))) != wanted {
			continue
		}
		leave, err := rowToLeave(values, r.location)
		if err != nil {
			return nil, 0, err
		}
		leave.BuktiPendukung = ""
		return leave, row.rowNumber, nil
	}
	return nil, 0, ErrNotFound
}

// UpdateLeaveRequest changes only requester-editable fields. Snapshot, status,
// audit and creation columns remain untouched; the attachment is rewritten
// only when the caller explicitly requests it.
func (r *GoogleSheetsRepository) UpdateLeaveRequest(ctx context.Context, rowNumber int, leave *model.Leave, updateAttachment bool) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, leaveSheet)
	}
	sheet := quoteSheet(leaveSheet)
	data := []*sheets.ValueRange{
		{
			Range: fmt.Sprintf("%s!F%d:J%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{
				leave.JenisLeave, leave.TanggalMulai, leave.TanggalSelesai,
				strconv.Itoa(leave.JumlahHari), leave.Alasan,
			}},
		},
		{
			Range:  fmt.Sprintf("%s!R%d", sheet, rowNumber),
			Values: [][]interface{}{{formatDateTime(leave.UpdatedAt)}},
		},
	}
	if updateAttachment {
		data = append(data, &sheets.ValueRange{
			Range: fmt.Sprintf("%s!S%d:T%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{
				leave.HasBuktiPendukung, leave.BuktiPendukung,
			}},
		})
	}
	return r.batchUpdateLeave(ctx, rowNumber, "update request", data)
}

// CancelLeave touches only lifecycle fields. In particular, it never reads or
// rewrites the attachment.
func (r *GoogleSheetsRepository) CancelLeave(ctx context.Context, rowNumber int, leave *model.Leave) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, leaveSheet)
	}
	sheet := quoteSheet(leaveSheet)
	data := []*sheets.ValueRange{
		{Range: fmt.Sprintf("%s!K%d", sheet, rowNumber), Values: [][]interface{}{{leave.Status}}},
		{Range: fmt.Sprintf("%s!P%d", sheet, rowNumber), Values: [][]interface{}{{formatNullableDateTime(leave.DibatalkanPada)}}},
		{Range: fmt.Sprintf("%s!R%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(leave.UpdatedAt)}}},
	}
	return r.batchUpdateLeave(ctx, rowNumber, "cancel", data)
}

// UpdateLeaveDecision writes the decision and its audit stamp, leaving request
// contents, cancellation data and attachment intact.
func (r *GoogleSheetsRepository) UpdateLeaveDecision(ctx context.Context, rowNumber int, leave *model.Leave) error {
	if rowNumber < 2 {
		return fmt.Errorf("invalid row number %d for sheet %q", rowNumber, leaveSheet)
	}
	sheet := quoteSheet(leaveSheet)
	data := []*sheets.ValueRange{
		{
			Range: fmt.Sprintf("%s!K%d:O%d", sheet, rowNumber, rowNumber),
			Values: [][]interface{}{{
				leave.Status, leave.CatatanApproval, leave.DiprosesOleh,
				leave.DiprosesOlehUserID, formatNullableDateTime(leave.DiprosesPada),
			}},
		},
		{Range: fmt.Sprintf("%s!R%d", sheet, rowNumber), Values: [][]interface{}{{formatDateTime(leave.UpdatedAt)}}},
	}
	return r.batchUpdateLeave(ctx, rowNumber, "update decision", data)
}

func (r *GoogleSheetsRepository) batchUpdateLeave(ctx context.Context, rowNumber int, action string, data []*sheets.ValueRange) error {
	_, err := r.service.Spreadsheets.Values.BatchUpdate(r.spreadsheetID, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("%s leave row %d: %w", action, rowNumber, err)
	}
	return nil
}

func (r *GoogleSheetsRepository) ReadLeaveAttachment(ctx context.Context, rowNumber int) (string, error) {
	if rowNumber < 2 {
		return "", fmt.Errorf("invalid row number %d for sheet %q", rowNumber, leaveSheet)
	}
	rangeName := fmt.Sprintf("%s!T%d", quoteSheet(leaveSheet), rowNumber)
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("read leave attachment row %d: %w", rowNumber, err)
	}
	if len(values.Values) == 0 || len(values.Values[0]) == 0 {
		return "", nil
	}
	return cellString(values.Values[0][0]), nil
}

func leaveToRow(leave *model.Leave) []interface{} {
	return []interface{}{
		leave.LeaveID, leave.UserID, leave.NRP, leave.NamaLengkap, leave.Jabatan,
		leave.JenisLeave, leave.TanggalMulai, leave.TanggalSelesai,
		strconv.Itoa(leave.JumlahHari), leave.Alasan, leave.Status, leave.CatatanApproval,
		leave.DiprosesOleh, leave.DiprosesOlehUserID, formatNullableDateTime(leave.DiprosesPada),
		formatNullableDateTime(leave.DibatalkanPada), formatDateTime(leave.CreatedAt),
		formatDateTime(leave.UpdatedAt), leave.HasBuktiPendukung, leave.BuktiPendukung,
	}
}

func (r *GoogleSheetsRepository) MaxFuelMasukSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, fuelMasukSheet, "A")
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

func (r *GoogleSheetsRepository) CreateFuelMasuk(ctx context.Context, fuel *model.FuelMasuk) error {
	return r.appendRow(ctx, fuelMasukSheet, fuelMasukToRow(fuel))
}

func (r *GoogleSheetsRepository) ListFuelMasuk(ctx context.Context) ([]model.FuelMasuk, error) {
	rows, err := r.readFuelMasukRows(ctx)
	if err != nil {
		return nil, err
	}
	deliveries := make([]model.FuelMasuk, 0, len(rows))
	for _, row := range rows {
		fuel, err := rowToFuelMasuk(row.values, r.location)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(fuel.FuelID) == "" {
			continue
		}
		deliveries = append(deliveries, *fuel)
	}
	return deliveries, nil
}

func (r *GoogleSheetsRepository) FindFuelMasukRow(ctx context.Context, fuelID string) (*model.FuelMasuk, int, error) {
	rows, err := r.readFuelMasukRows(ctx)
	if err != nil {
		return nil, 0, err
	}
	wanted := strings.ToUpper(strings.TrimSpace(fuelID))
	for _, row := range rows {
		if strings.ToUpper(strings.TrimSpace(cellString(row.values[0]))) != wanted {
			continue
		}
		fuel, err := rowToFuelMasuk(row.values, r.location)
		if err != nil {
			return nil, 0, err
		}
		return fuel, row.rowNumber, nil
	}
	return nil, 0, ErrNotFound
}

// readFuelMasukRows fetches the two ranges either side of the photos and puts
// them back together, leaving the four photo cells empty.
func (r *GoogleSheetsRepository) readFuelMasukRows(ctx context.Context) ([]indexedRow, error) {
	sheet := quoteSheet(fuelMasukSheet)
	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(sheet+"!A:"+fuelHeadColumn, sheet+"!"+fuelTailRange).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", fuelMasukSheet, err)
	}
	if len(response.ValueRanges) != 2 {
		return nil, fmt.Errorf("read rows from %s: expected 2 ranges, got %d", fuelMasukSheet, len(response.ValueRanges))
	}
	head, tail := response.ValueRanges[0], response.ValueRanges[1]

	rows := make([]indexedRow, 0, len(head.Values))
	// Row 1 is the header and both ranges start there, so one index walks both.
	for index := 1; index < len(head.Values); index++ {
		values := padRow(head.Values[index], fuelHeadWidth)[:fuelHeadWidth]
		if isBlankRow(values) {
			continue
		}
		merged := make([]interface{}, 0, len(fuelMasukHeaders))
		merged = append(merged, values...)
		for photo := 0; photo < fuelPhotoCount; photo++ {
			merged = append(merged, "")
		}
		if index < len(tail.Values) {
			merged = append(merged, padRow(tail.Values[index], fuelTailWidth)[:fuelTailWidth]...)
		}
		rows = append(rows, indexedRow{values: padRow(merged, len(fuelMasukHeaders)), rowNumber: index + 1})
	}
	return rows, nil
}

func (r *GoogleSheetsRepository) ReadFuelMasukPhoto(ctx context.Context, rowNumber, photoIndex int) (string, error) {
	if rowNumber < 2 {
		return "", fmt.Errorf("invalid row number %d for sheet %q", rowNumber, fuelMasukSheet)
	}
	if photoIndex < 0 || photoIndex >= fuelPhotoCount {
		return "", fmt.Errorf("invalid fuel photo index %d", photoIndex)
	}
	column := string(rune(fuelPhotoFirstColumn + photoIndex))
	rangeName := fmt.Sprintf("%s!%s%d", quoteSheet(fuelMasukSheet), column, rowNumber)
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("read fuel masuk photo row %d: %w", rowNumber, err)
	}
	if len(values.Values) == 0 || len(values.Values[0]) == 0 {
		return "", nil
	}
	return cellString(values.Values[0][0]), nil
}

func fuelMasukToRow(fuel *model.FuelMasuk) []interface{} {
	return []interface{}{
		fuel.FuelID, formatDateTime(fuel.TanggalInput), fuel.Vendor, fuel.Driver, fuel.Nopol,
		formatFloat(fuel.JumlahLiter), fuel.Keterangan, formatFloat(fuel.LiterTidakSesuai),
		fuel.StatusApproval,
		fuel.FotoTruckDepan, fuel.FotoTangkiSebelum, fuel.FotoFlowmeter, fuel.FotoTangkiSetelah,
		fuel.CatatanApproval, fuel.DiprosesOleh, fuel.DiprosesOlehUserID,
		formatNullableDateTime(fuel.DiprosesPada),
		fuel.CreatedBy, fuel.CreatedByID, formatDateTime(fuel.CreatedAt), formatDateTime(fuel.UpdatedAt),
	}
}

func rowToFuelMasuk(row []interface{}, location *time.Location) (*model.FuelMasuk, error) {
	row = padRow(row, len(fuelMasukHeaders))
	tanggalInput, err := parseDateTime(cellString(row[1]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel masuk tanggal_waktu_input: %w", err)
	}
	diprosesPada, err := parseOptionalTime(cellString(row[16]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel masuk diproses_pada: %w", err)
	}
	createdAt, err := parseDateTime(cellString(row[19]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel masuk created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[20]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel masuk updated_at: %w", err)
	}
	return &model.FuelMasuk{
		FuelID:             cellString(row[0]),
		TanggalInput:       tanggalInput,
		Vendor:             cellString(row[2]),
		Driver:             cellString(row[3]),
		Nopol:              cellString(row[4]),
		JumlahLiter:        parseFloatCell(row[5]),
		Keterangan:         cellString(row[6]),
		LiterTidakSesuai:   parseFloatCell(row[7]),
		StatusApproval:     cellString(row[8]),
		FotoTruckDepan:     cellString(row[9]),
		FotoTangkiSebelum:  cellString(row[10]),
		FotoFlowmeter:      cellString(row[11]),
		FotoTangkiSetelah:  cellString(row[12]),
		CatatanApproval:    cellString(row[13]),
		DiprosesOleh:       cellString(row[14]),
		DiprosesOlehUserID: cellString(row[15]),
		DiprosesPada:       diprosesPada,
		CreatedBy:          cellString(row[17]),
		CreatedByID:        cellString(row[18]),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

func (r *GoogleSheetsRepository) MaxFuelKeluarSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, fuelKeluarSheet, "A")
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

func (r *GoogleSheetsRepository) CreateFuelKeluar(ctx context.Context, fuel *model.FuelKeluar) error {
	return r.appendRow(ctx, fuelKeluarSheet, fuelKeluarToRow(fuel))
}

func (r *GoogleSheetsRepository) ListFuelKeluar(ctx context.Context) ([]model.FuelKeluar, error) {
	rows, err := r.readFuelKeluarRows(ctx)
	if err != nil {
		return nil, err
	}
	dispenses := make([]model.FuelKeluar, 0, len(rows))
	for _, row := range rows {
		fuel, err := rowToFuelKeluar(row.values, r.location)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(fuel.FuelOutID) == "" {
			continue
		}
		dispenses = append(dispenses, *fuel)
	}
	return dispenses, nil
}

func (r *GoogleSheetsRepository) FindFuelKeluarRow(ctx context.Context, fuelOutID string) (*model.FuelKeluar, int, error) {
	rows, err := r.readFuelKeluarRows(ctx)
	if err != nil {
		return nil, 0, err
	}
	wanted := strings.ToUpper(strings.TrimSpace(fuelOutID))
	for _, row := range rows {
		if strings.ToUpper(strings.TrimSpace(cellString(row.values[0]))) != wanted {
			continue
		}
		fuel, err := rowToFuelKeluar(row.values, r.location)
		if err != nil {
			return nil, 0, err
		}
		return fuel, row.rowNumber, nil
	}
	return nil, 0, ErrNotFound
}

// readFuelKeluarRows stitches the ranges either side of the photo column back
// into one row, leaving the photo cell empty.
func (r *GoogleSheetsRepository) readFuelKeluarRows(ctx context.Context) ([]indexedRow, error) {
	sheet := quoteSheet(fuelKeluarSheet)
	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(sheet+"!A:"+fuelOutHeadColumn, sheet+"!"+fuelOutTailRange).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", fuelKeluarSheet, err)
	}
	if len(response.ValueRanges) != 2 {
		return nil, fmt.Errorf("read rows from %s: expected 2 ranges, got %d", fuelKeluarSheet, len(response.ValueRanges))
	}
	head, tail := response.ValueRanges[0], response.ValueRanges[1]

	rows := make([]indexedRow, 0, len(head.Values))
	for index := 1; index < len(head.Values); index++ {
		values := padRow(head.Values[index], fuelOutHeadWidth)[:fuelOutHeadWidth]
		if isBlankRow(values) {
			continue
		}
		merged := make([]interface{}, 0, len(fuelKeluarHeaders))
		merged = append(merged, values...)
		merged = append(merged, "")
		if index < len(tail.Values) {
			merged = append(merged, padRow(tail.Values[index], fuelOutTailWidth)[:fuelOutTailWidth]...)
		}
		rows = append(rows, indexedRow{values: padRow(merged, len(fuelKeluarHeaders)), rowNumber: index + 1})
	}
	return rows, nil
}

func (r *GoogleSheetsRepository) ReadFuelKeluarPhoto(ctx context.Context, rowNumber int) (string, error) {
	if rowNumber < 2 {
		return "", fmt.Errorf("invalid row number %d for sheet %q", rowNumber, fuelKeluarSheet)
	}
	rangeName := fmt.Sprintf("%s!%s%d", quoteSheet(fuelKeluarSheet), fuelOutPhotoColumn, rowNumber)
	values, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, rangeName).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("read fuel keluar photo row %d: %w", rowNumber, err)
	}
	if len(values.Values) == 0 || len(values.Values[0]) == 0 {
		return "", nil
	}
	return cellString(values.Values[0][0]), nil
}

func fuelKeluarToRow(fuel *model.FuelKeluar) []interface{} {
	return []interface{}{
		fuel.FuelOutID, fuel.Tanggal, fuel.IDUnit, fuel.NamaUnit,
		formatFloat(fuel.HMAwalFlowMeter), formatFloat(fuel.HMAkhirFlowMeter), formatFloat(fuel.Liter),
		optionalFloat(fuel.HMAlatBerat), fuel.Operator, fuel.FotoAkhirFlowMeter,
		fuel.CreatedBy, fuel.CreatedByID, formatDateTime(fuel.CreatedAt), formatDateTime(fuel.UpdatedAt),
	}
}

func rowToFuelKeluar(row []interface{}, location *time.Location) (*model.FuelKeluar, error) {
	row = padRow(row, len(fuelKeluarHeaders))
	hmAlatBerat, err := parseOptionalFloatCell(row[7])
	if err != nil {
		return nil, fmt.Errorf("parse fuel keluar hm_alat_berat_pengisian: %w", err)
	}
	createdAt, err := parseDateTime(cellString(row[12]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel keluar created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[13]), location)
	if err != nil {
		return nil, fmt.Errorf("parse fuel keluar updated_at: %w", err)
	}
	return &model.FuelKeluar{
		FuelOutID:          cellString(row[0]),
		Tanggal:            cellString(row[1]),
		IDUnit:             cellString(row[2]),
		NamaUnit:           cellString(row[3]),
		HMAwalFlowMeter:    parseFloatCell(row[4]),
		HMAkhirFlowMeter:   parseFloatCell(row[5]),
		Liter:              parseFloatCell(row[6]),
		HMAlatBerat:        hmAlatBerat,
		Operator:           cellString(row[8]),
		FotoAkhirFlowMeter: cellString(row[9]),
		CreatedBy:          cellString(row[10]),
		CreatedByID:        cellString(row[11]),
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// parseOptionalFloatCell keeps an empty cell as "nobody took this reading",
// which a plain zero would quietly turn into a reading of zero hours.
func parseOptionalFloatCell(cell interface{}) (*float64, error) {
	raw := strings.TrimSpace(cellString(cell))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("unsupported number %q", raw)
	}
	return &value, nil
}

func (r *GoogleSheetsRepository) MaxHourMeterSequence(ctx context.Context, prefix string) (int, error) {
	rows, err := r.readRows(ctx, hourMeterSheet, "A")
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

func (r *GoogleSheetsRepository) CreateHourMeter(ctx context.Context, reading *model.HourMeter) error {
	return r.appendRow(ctx, hourMeterSheet, hourMeterToRow(reading))
}

func (r *GoogleSheetsRepository) ListHourMeter(ctx context.Context) ([]model.HourMeter, error) {
	rows, err := r.readRows(ctx, hourMeterSheet, hourMeterLastColumn)
	if err != nil {
		return nil, err
	}
	readings := make([]model.HourMeter, 0, len(rows))
	for _, row := range dataRows(rows) {
		reading, err := rowToHourMeter(row, r.location)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(reading.HMID) == "" {
			continue
		}
		readings = append(readings, *reading)
	}
	return readings, nil
}

func hourMeterToRow(reading *model.HourMeter) []interface{} {
	row := []interface{}{
		reading.HMID, reading.Tanggal, reading.Shift, reading.IDUnit, reading.NamaUnit,
		reading.Operator, formatFloat(reading.HMAwal), formatFloat(reading.HMAkhir),
		formatFloat(reading.TotalHM), formatFloat(reading.FuelLiter),
		formatFloat(reading.TotalStandby),
	}
	// A reason that did not happen leaves its cell empty rather than writing a
	// zero, so the sheet distinguishes "no rain" from "rain, nought minutes".
	minutes := make(map[string]float64, len(reading.Standby))
	for _, standby := range reading.Standby {
		minutes[strings.ToUpper(strings.TrimSpace(standby.Variable))] = standby.Menit
	}
	for _, variable := range model.StandbyVariables {
		value, found := minutes[variable.Nama]
		if !found {
			row = append(row, "")
			continue
		}
		row = append(row, formatFloat(value))
	}

	row = append(row, formatFloat(reading.TotalBreakdown))
	lost := make(map[string]float64, len(reading.Breakdown))
	for _, breakdown := range reading.Breakdown {
		lost[strings.ToUpper(strings.TrimSpace(breakdown.Variable))] = breakdown.Menit
	}
	for _, variable := range model.BreakdownVariables {
		value, found := lost[variable]
		if !found {
			row = append(row, "")
			continue
		}
		row = append(row, formatFloat(value))
	}
	row = append(row,
		formatFloat(reading.PA), formatFloat(reading.BDPersen), formatFloat(reading.UA),
		reading.Remark,
	)
	return append(row,
		reading.CreatedBy, reading.CreatedByID,
		formatDateTime(reading.CreatedAt), formatDateTime(reading.UpdatedAt),
	)
}

func rowToHourMeter(row []interface{}, location *time.Location) (*model.HourMeter, error) {
	row = padRow(row, len(hourMeterHeaders))
	createdAt, err := parseDateTime(cellString(row[hourMeterAuditOffset+2]), location)
	if err != nil {
		return nil, fmt.Errorf("parse hour meter created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[hourMeterAuditOffset+3]), location)
	if err != nil {
		return nil, fmt.Errorf("parse hour meter updated_at: %w", err)
	}
	reading := &model.HourMeter{
		HMID:           cellString(row[0]),
		Tanggal:        cellString(row[1]),
		Shift:          cellString(row[2]),
		IDUnit:         cellString(row[3]),
		NamaUnit:       cellString(row[4]),
		Operator:       cellString(row[5]),
		HMAwal:         parseFloatCell(row[6]),
		HMAkhir:        parseFloatCell(row[7]),
		TotalHM:        parseFloatCell(row[8]),
		FuelLiter:      parseFloatCell(row[9]),
		TotalStandby:   parseFloatCell(row[10]),
		TotalBreakdown: parseFloatCell(row[hourMeterBreakdownTotalOffset]),
		PA:             parseFloatCell(row[hourMeterSummaryOffset]),
		BDPersen:       parseFloatCell(row[hourMeterSummaryOffset+1]),
		UA:             parseFloatCell(row[hourMeterSummaryOffset+2]),
		Remark:         cellString(row[hourMeterSummaryOffset+3]),
		CreatedBy:      cellString(row[hourMeterAuditOffset]),
		CreatedByID:    cellString(row[hourMeterAuditOffset+1]),
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	// An empty cell is a reason that did not happen, so it produces no entry
	// rather than one reading zero minutes.
	for index, variable := range model.StandbyVariables {
		cell := row[hourMeterStandbyOffset+index]
		if strings.TrimSpace(cellString(cell)) == "" {
			continue
		}
		reading.Standby = append(reading.Standby, model.HourMeterStandby{
			Variable: variable.Nama,
			Menit:    parseFloatCell(cell),
		})
	}
	for index, variable := range model.BreakdownVariables {
		cell := row[hourMeterBreakdownOffset+index]
		if strings.TrimSpace(cellString(cell)) == "" {
			continue
		}
		reading.Breakdown = append(reading.Breakdown, model.HourMeterBreakdown{
			Variable: variable,
			Menit:    parseFloatCell(cell),
		})
	}
	return reading, nil
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

// ListAttendanceByUser reads one person's attendance history. It fetches only
// the columns a summary needs: the sheet also holds two base64 photos per row,
// and pulling a year of those back would cost megabytes to count days.
func (r *GoogleSheetsRepository) ListAttendanceByUser(ctx context.Context, userID string) ([]model.Attendance, error) {
	sheet := quoteSheet(attendanceSheet)
	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(sheet+"!B:B", sheet+"!F:G", sheet+"!M:M", sheet+"!S:T").
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read attendance for user: %w", err)
	}
	if len(response.ValueRanges) != 4 {
		return nil, fmt.Errorf("read attendance for user: expected 4 ranges, got %d", len(response.ValueRanges))
	}
	owner, dates, clockOut, status := response.ValueRanges[0], response.ValueRanges[1], response.ValueRanges[2], response.ValueRanges[3]

	rows := make([]model.Attendance, 0, len(owner.Values))
	// Row 1 is the header; every range starts at the same row, so one index
	// walks all four of them.
	for i := 1; i < len(owner.Values); i++ {
		if strings.TrimSpace(cellAt(owner, i, 0)) != userID {
			continue
		}
		tanggal := strings.TrimSpace(cellAt(dates, i, 0))
		if tanggal == "" {
			continue
		}
		clockInAt, err := parseDateTime(cellAt(dates, i, 1), r.location)
		if err != nil {
			return nil, fmt.Errorf("parse attendance clock_in_at: %w", err)
		}
		clockOutAt, err := parseOptionalTime(cellAt(clockOut, i, 0), r.location)
		if err != nil {
			return nil, fmt.Errorf("parse attendance clock_out_at: %w", err)
		}
		var durasi *int
		if minutes, err := strconv.Atoi(strings.TrimSpace(cellAt(status, i, 1))); err == nil {
			durasi = &minutes
		}
		rows = append(rows, model.Attendance{
			UserID:         userID,
			TanggalAbsensi: tanggal,
			ClockInAt:      clockInAt,
			ClockOutAt:     clockOutAt,
			StatusAbsensi:  strings.TrimSpace(cellAt(status, i, 0)),
			DurasiMenit:    durasi,
		})
	}
	return rows, nil
}

// ListAttendanceBetween first reads only the date column to find the smallest
// contiguous row window that can contain the requested period. It then reads
// the fields needed by HR reporting from that window while skipping K and Q,
// the two base64 photo columns. Date strings use YYYY-MM-DD, so lexical
// comparison preserves chronological order.
func (r *GoogleSheetsRepository) ListAttendanceBetween(ctx context.Context, from, to string) ([]model.Attendance, error) {
	sheet := quoteSheet(attendanceSheet)
	dateColumn, err := r.service.Spreadsheets.Values.Get(r.spreadsheetID, sheet+"!F2:F").
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read attendance dates: %w", err)
	}
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	firstRow, lastRow, found := attendanceDateRowBounds(dateColumn.Values, from, to, 2)
	if !found {
		return []model.Attendance{}, nil
	}

	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(
			fmt.Sprintf("%s!A%d:J%d", sheet, firstRow, lastRow),
			fmt.Sprintf("%s!L%d:P%d", sheet, firstRow, lastRow),
			fmt.Sprintf("%s!R%d:V%d", sheet, firstRow, lastRow),
		).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read attendance between dates: %w", err)
	}
	if len(response.ValueRanges) != 3 {
		return nil, fmt.Errorf("read attendance between dates: expected 3 ranges, got %d", len(response.ValueRanges))
	}
	first, middle, last := response.ValueRanges[0], response.ValueRanges[1], response.ValueRanges[2]
	rowCount := len(first.Values)
	if len(middle.Values) > rowCount {
		rowCount = len(middle.Values)
	}
	if len(last.Values) > rowCount {
		rowCount = len(last.Values)
	}
	rows := make([]model.Attendance, 0, rowCount)
	for i := 0; i < rowCount; i++ {
		date := strings.TrimSpace(cellAt(first, i, 5))
		if date == "" || (from != "" && date < from) || (to != "" && date > to) {
			continue
		}
		values := make([]interface{}, len(attendanceHeaders))
		for column := 0; column <= 9; column++ {
			values[column] = cellAt(first, i, column)
		}
		// K (clock_in_photo) remains blank.
		for column := 0; column <= 4; column++ {
			values[11+column] = cellAt(middle, i, column)
		}
		// Q (clock_out_photo) remains blank.
		for column := 0; column <= 4; column++ {
			values[17+column] = cellAt(last, i, column)
		}
		attendance, err := rowToAttendance(values, r.location)
		if err != nil {
			return nil, fmt.Errorf("parse attendance row %d: %w", firstRow+i, err)
		}
		rows = append(rows, *attendance)
	}
	return rows, nil
}

// attendanceDateRowBounds returns sheet row numbers, not slice indexes. Taking
// the minimum and maximum matching row keeps the second fetch correct even if
// old data is not sorted by attendance date. Rows between those bounds are
// filtered again after the detailed fetch.
func attendanceDateRowBounds(values [][]interface{}, from, to string, firstSheetRow int) (int, int, bool) {
	if firstSheetRow < 1 {
		return 0, 0, false
	}
	first := 0
	last := 0
	for index, row := range values {
		if len(row) == 0 {
			continue
		}
		date := strings.TrimSpace(cellString(row[0]))
		if date == "" || (from != "" && date < from) || (to != "" && date > to) {
			continue
		}
		rowNumber := firstSheetRow + index
		if first == 0 || rowNumber < first {
			first = rowNumber
		}
		if rowNumber > last {
			last = rowNumber
		}
	}
	return first, last, first != 0
}

// cellAt reads one cell of a fetched range. Sheets drops trailing empty cells,
// so a row that ends early is short rather than padded.
func cellAt(values *sheets.ValueRange, row, column int) string {
	if values == nil || row >= len(values.Values) {
		return ""
	}
	line := values.Values[row]
	if column >= len(line) {
		return ""
	}
	return cellString(line[column])
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

// readUserRows reads the user sheet either side of the photo and stitches the
// two halves back into whole rows, with the photo left blank. It is one request
// rather than two: the session user is loaded on every authenticated request,
// and a second round trip there would be felt on every page.
func (r *GoogleSheetsRepository) readUserRows(ctx context.Context) ([][]interface{}, error) {
	sheet := quoteSheet(userSheet)
	response, err := r.service.Spreadsheets.Values.BatchGet(r.spreadsheetID).
		Ranges(sheet+"!A:"+userReadColumn, sheet+"!"+userProjectColumn+":"+userProjectColumn).
		ValueRenderOption("UNFORMATTED_VALUE").Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read rows from %s: %w", userSheet, err)
	}
	if len(response.ValueRanges) != 2 {
		return nil, fmt.Errorf("read rows from %s: expected 2 ranges, got %d", userSheet, len(response.ValueRanges))
	}
	head, tail := response.ValueRanges[0], response.ValueRanges[1]

	rows := make([][]interface{}, 0, len(head.Values))
	// Both ranges start at row 1, so one index walks them together.
	for index := 0; index < len(head.Values); index++ {
		merged := padRow(head.Values[index], userPhotoIndex)[:userPhotoIndex]
		// The photo column is skipped, not missing: leaving the slot in keeps
		// every index after it lined up with userHeaders.
		merged = append(merged, "")
		if index < len(tail.Values) && len(tail.Values[index]) > 0 {
			merged = append(merged, tail.Values[index][0])
		}
		rows = append(rows, padRow(merged, len(userHeaders)))
	}
	return rows, nil
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
		user.NoTelp, user.TanggalLahir, user.PunyaFoto, user.FotoProfil, user.Project,
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
	punyaFoto, err := parseBoolCell(row[13])
	if err != nil {
		return nil, fmt.Errorf("parse user punya_foto: %w", err)
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
		NoTelp:         cellString(row[11]),
		TanggalLahir:   cellString(row[12]),
		PunyaFoto:      punyaFoto,
		// Left empty by every read that stops at userReadColumn, which is all
		// of them; ReadUserPhoto fetches the image on its own.
		FotoProfil: cellString(row[14]),
		Project:    strings.TrimSpace(cellString(row[15])),
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

func rowToLeave(row []interface{}, location *time.Location) (*model.Leave, error) {
	row = padRow(row, len(leaveHeaders))
	jumlahHari, err := strconv.Atoi(strings.TrimSpace(cellString(row[8])))
	if err != nil {
		return nil, fmt.Errorf("parse leave jumlah_hari: %w", err)
	}
	diprosesPada, err := parseOptionalTime(cellString(row[14]), location)
	if err != nil {
		return nil, fmt.Errorf("parse leave diproses_pada: %w", err)
	}
	dibatalkanPada, err := parseOptionalTime(cellString(row[15]), location)
	if err != nil {
		return nil, fmt.Errorf("parse leave dibatalkan_pada: %w", err)
	}
	createdAt, err := parseDateTime(cellString(row[16]), location)
	if err != nil {
		return nil, fmt.Errorf("parse leave created_at: %w", err)
	}
	updatedAt, err := parseDateTime(cellString(row[17]), location)
	if err != nil {
		return nil, fmt.Errorf("parse leave updated_at: %w", err)
	}
	hasAttachment, err := parseBoolCell(row[18])
	if err != nil {
		return nil, fmt.Errorf("parse leave has_bukti_pendukung: %w", err)
	}
	return &model.Leave{
		LeaveID:            cellString(row[0]),
		UserID:             cellString(row[1]),
		NRP:                cellString(row[2]),
		NamaLengkap:        cellString(row[3]),
		Jabatan:            cellString(row[4]),
		JenisLeave:         cellString(row[5]),
		TanggalMulai:       cellString(row[6]),
		TanggalSelesai:     cellString(row[7]),
		JumlahHari:         jumlahHari,
		Alasan:             cellString(row[9]),
		Status:             cellString(row[10]),
		CatatanApproval:    cellString(row[11]),
		DiprosesOleh:       cellString(row[12]),
		DiprosesOlehUserID: cellString(row[13]),
		DiprosesPada:       diprosesPada,
		DibatalkanPada:     dibatalkanPada,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
		HasBuktiPendukung:  hasAttachment,
		BuktiPendukung:     cellString(row[19]),
	}, nil
}

func parseBoolCell(cell interface{}) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(cellString(cell))) {
	case "true", "1", "ya", "yes":
		return true, nil
	case "", "false", "0", "tidak", "no":
		return false, nil
	default:
		return false, fmt.Errorf("unsupported boolean %q", cellString(cell))
	}
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
