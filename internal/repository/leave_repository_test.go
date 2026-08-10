package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/model"
)

func TestLeaveHeadersKeepAttachmentLast(t *testing.T) {
	want := []string{
		"leave_id", "user_id", "nrp", "nama_lengkap", "jabatan", "jenis_leave",
		"tanggal_mulai", "tanggal_selesai", "jumlah_hari", "alasan", "status",
		"catatan_approval", "diproses_oleh", "diproses_oleh_user_id", "diproses_pada",
		"dibatalkan_pada", "created_at", "updated_at", "has_bukti_pendukung", "bukti_pendukung",
	}
	if !reflect.DeepEqual(leaveHeaders, want) {
		t.Fatalf("leaveHeaders = %#v, want %#v", leaveHeaders, want)
	}
	if len(leaveHeaders) != 20 || leaveHeaders[len(leaveHeaders)-1] != "bukti_pendukung" {
		t.Fatalf("attachment must be column 20, got %#v", leaveHeaders)
	}
}

func TestLeaveRowRoundTripAndAttachmentProjection(t *testing.T) {
	loc := testLocation(t)
	processed := time.Date(2026, 8, 10, 14, 5, 6, 0, loc)
	created := time.Date(2026, 8, 9, 9, 30, 0, 0, loc)
	leave := &model.Leave{
		LeaveID:            "LVE-20260809-0001",
		UserID:             "user-1",
		NRP:                "2307099",
		NamaLengkap:        "Yusuf",
		Jabatan:            "Management",
		JenisLeave:         model.LeaveJenisCutiTahunan,
		TanggalMulai:       "2026-08-11",
		TanggalSelesai:     "2026-08-13",
		JumlahHari:         3,
		Alasan:             "Keperluan keluarga",
		Status:             model.LeaveStatusDisetujui,
		CatatanApproval:    "Disetujui",
		DiprosesOleh:       "HR Satu",
		DiprosesOlehUserID: "hr-1",
		DiprosesPada:       &processed,
		CreatedAt:          created,
		UpdatedAt:          processed,
		HasBuktiPendukung:  true,
		BuktiPendukung:     "data:image/jpeg;base64,abc",
	}

	row := leaveToRow(leave)
	if len(row) != len(leaveHeaders) {
		t.Fatalf("leaveToRow width = %d, want %d", len(row), len(leaveHeaders))
	}
	parsed, err := rowToLeave(row, loc)
	if err != nil {
		t.Fatalf("rowToLeave: %v", err)
	}
	if parsed.LeaveID != leave.LeaveID || parsed.JumlahHari != 3 || parsed.DiprosesPada == nil {
		t.Fatalf("unexpected parsed leave: %+v", parsed)
	}
	if !parsed.HasBuktiPendukung || parsed.BuktiPendukung != leave.BuktiPendukung {
		t.Fatalf("attachment fields not preserved: %+v", parsed)
	}

	// List/lookup reads stop at S. A truncated row must still parse and expose
	// the lightweight attachment flag without the image itself.
	projected, err := rowToLeave(row[:19], loc)
	if err != nil {
		t.Fatalf("rowToLeave projected row: %v", err)
	}
	if !projected.HasBuktiPendukung || projected.BuktiPendukung != "" {
		t.Fatalf("projected attachment = (%v, %q)", projected.HasBuktiPendukung, projected.BuktiPendukung)
	}
}

func TestTestRepositoryLeaveLifecycleUsesPartialUpdates(t *testing.T) {
	ctx := context.Background()
	loc := testLocation(t)
	created := time.Date(2026, 8, 10, 8, 0, 0, 0, loc)
	store := NewTestRepository()
	original := &model.Leave{
		LeaveID:           "LVE-20260810-0007",
		UserID:            "user-1",
		NRP:               "1001",
		NamaLengkap:       "Budi",
		Jabatan:           "Driver",
		JenisLeave:        model.LeaveJenisCutiSakit,
		TanggalMulai:      "2026-08-10",
		TanggalSelesai:    "2026-08-10",
		JumlahHari:        1,
		Alasan:            "Sakit",
		Status:            model.LeaveStatusMenunggu,
		CreatedAt:         created,
		UpdatedAt:         created,
		HasBuktiPendukung: true,
		BuktiPendukung:    "data:image/png;base64,old",
	}
	if err := store.CreateLeave(ctx, original); err != nil {
		t.Fatalf("CreateLeave: %v", err)
	}
	sequence, err := store.MaxLeaveSequence(ctx, "LVE-20260810-")
	if err != nil || sequence != 7 {
		t.Fatalf("MaxLeaveSequence = (%d, %v), want (7, nil)", sequence, err)
	}

	listed, err := store.ListLeave(ctx)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListLeave = (%+v, %v)", listed, err)
	}
	if !listed[0].HasBuktiPendukung || listed[0].BuktiPendukung != "" {
		t.Fatalf("ListLeave leaked or lost attachment metadata: %+v", listed[0])
	}
	found, rowNumber, err := store.FindLeaveRow(ctx, "lve-20260810-0007")
	if err != nil || rowNumber != 2 || found.BuktiPendukung != "" {
		t.Fatalf("FindLeaveRow = (%+v, %d, %v)", found, rowNumber, err)
	}
	attachment, err := store.ReadLeaveAttachment(ctx, rowNumber)
	if err != nil || attachment != original.BuktiPendukung {
		t.Fatalf("ReadLeaveAttachment = (%q, %v)", attachment, err)
	}

	editedAt := created.Add(time.Hour)
	edit := *original
	edit.UserID = "must-not-overwrite"
	edit.Status = model.LeaveStatusDitolak
	edit.JenisLeave = model.LeaveJenisIzin
	edit.TanggalMulai = "2026-08-12"
	edit.TanggalSelesai = "2026-08-13"
	edit.JumlahHari = 2
	edit.Alasan = "Urusan keluarga"
	edit.UpdatedAt = editedAt
	edit.BuktiPendukung = "data:image/png;base64,new-but-not-requested"
	if err := store.UpdateLeaveRequest(ctx, rowNumber, &edit, false); err != nil {
		t.Fatalf("UpdateLeaveRequest: %v", err)
	}
	stored := store.LeaveList()[0]
	if stored.UserID != original.UserID || stored.Status != model.LeaveStatusMenunggu {
		t.Fatalf("request edit overwrote immutable fields: %+v", stored)
	}
	if stored.JenisLeave != model.LeaveJenisIzin || stored.JumlahHari != 2 || stored.Alasan != "Urusan keluarga" {
		t.Fatalf("request edit did not update editable fields: %+v", stored)
	}
	if stored.BuktiPendukung != original.BuktiPendukung {
		t.Fatalf("attachment changed without updateAttachment: %q", stored.BuktiPendukung)
	}

	edit.HasBuktiPendukung = false
	edit.BuktiPendukung = ""
	if err := store.UpdateLeaveRequest(ctx, rowNumber, &edit, true); err != nil {
		t.Fatalf("UpdateLeaveRequest attachment: %v", err)
	}
	stored = store.LeaveList()[0]
	if stored.HasBuktiPendukung || stored.BuktiPendukung != "" {
		t.Fatalf("attachment was not cleared: %+v", stored)
	}

	processed := editedAt.Add(time.Hour)
	decision := &model.Leave{
		Status:             model.LeaveStatusDisetujui,
		CatatanApproval:    "OK",
		DiprosesOleh:       "HR",
		DiprosesOlehUserID: "hr-1",
		DiprosesPada:       &processed,
		UpdatedAt:          processed,
	}
	if err := store.UpdateLeaveDecision(ctx, rowNumber, decision); err != nil {
		t.Fatalf("UpdateLeaveDecision: %v", err)
	}
	stored = store.LeaveList()[0]
	if stored.Status != model.LeaveStatusDisetujui || stored.DiprosesPada == nil || stored.TanggalMulai != edit.TanggalMulai {
		t.Fatalf("decision update was not partial: %+v", stored)
	}

	cancelled := processed.Add(time.Hour)
	cancel := &model.Leave{Status: model.LeaveStatusDibatalkan, DibatalkanPada: &cancelled, UpdatedAt: cancelled}
	if err := store.CancelLeave(ctx, rowNumber, cancel); err != nil {
		t.Fatalf("CancelLeave: %v", err)
	}
	stored = store.LeaveList()[0]
	if stored.Status != model.LeaveStatusDibatalkan || stored.DibatalkanPada == nil || stored.DiprosesOleh != "HR" {
		t.Fatalf("cancel update was not partial: %+v", stored)
	}
}

func TestTestRepositoryListAttendanceBetweenIsInclusiveAndOmitsPhotos(t *testing.T) {
	ctx := context.Background()
	store := NewTestRepository()
	for index, date := range []string{"2026-08-01", "2026-08-02", "2026-08-03", "2026-08-04"} {
		if err := store.CreateAttendance(ctx, &model.Attendance{
			AbsensiID:      date,
			UserID:         "user-1",
			TanggalAbsensi: date,
			ClockInPhoto:   "in-photo",
			ClockOutPhoto:  "out-photo",
			DurasiMenit:    intPtrForTest(index),
		}); err != nil {
			t.Fatalf("CreateAttendance: %v", err)
		}
	}
	rows, err := store.ListAttendanceBetween(ctx, "2026-08-02", "2026-08-03")
	if err != nil {
		t.Fatalf("ListAttendanceBetween: %v", err)
	}
	if len(rows) != 2 || rows[0].TanggalAbsensi != "2026-08-02" || rows[1].TanggalAbsensi != "2026-08-03" {
		t.Fatalf("unexpected date range: %+v", rows)
	}
	for _, row := range rows {
		if row.ClockInPhoto != "" || row.ClockOutPhoto != "" {
			t.Fatalf("aggregate attendance leaked photos: %+v", row)
		}
	}
}

func TestAttendanceDateRowBoundsHandlesEmptyAndUnsortedRows(t *testing.T) {
	rows := [][]interface{}{
		{"2026-08-10"},
		{},
		{"2026-08-02"},
		{"2026-08-12"},
		{"2026-08-03"},
	}

	first, last, found := attendanceDateRowBounds(rows, "2026-08-02", "2026-08-03", 2)
	if !found || first != 4 || last != 6 {
		t.Fatalf("bounds = (%d, %d, %v), want (4, 6, true)", first, last, found)
	}

	first, last, found = attendanceDateRowBounds(rows, "2026-09-01", "2026-09-30", 2)
	if found || first != 0 || last != 0 {
		t.Fatalf("empty bounds = (%d, %d, %v), want (0, 0, false)", first, last, found)
	}

	if _, _, found = attendanceDateRowBounds(rows, "", "", 0); found {
		t.Fatal("invalid first sheet row unexpectedly matched")
	}
}

func TestGoogleSheetsListAttendanceBetweenNarrowsRowsAndSkipsPhotos(t *testing.T) {
	ctx := context.Background()
	var requestedRanges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(request.URL.Path, "/values/'absensi data'!F2:F"):
			_ = json.NewEncoder(w).Encode(&sheets.ValueRange{Values: [][]interface{}{
				{"2026-08-10"}, {}, {"2026-08-02"}, {"2026-08-12"}, {"2026-08-03"},
			}})
		case strings.HasSuffix(request.URL.Path, "/values:batchGet"):
			requestedRanges = append(requestedRanges, request.URL.Query()["ranges"]...)
			first := func(id, date string) []interface{} {
				return []interface{}{id, "user-1", "1001", "Budi", "Produksi", date, date + " 08:00:00", "-6.2", "106.8", "5"}
			}
			middle := func(date string) []interface{} {
				return []interface{}{"127.0.0.1", date + " 17:00:00", "-6.2", "106.8", "5"}
			}
			last := func(date string) []interface{} {
				return []interface{}{"127.0.0.1", "HADIR", "540", date + " 08:00:00", date + " 17:00:00"}
			}
			_ = json.NewEncoder(w).Encode(&sheets.BatchGetValuesResponse{ValueRanges: []*sheets.ValueRange{
				{Values: [][]interface{}{first("ABS-2", "2026-08-02"), first("ABS-12", "2026-08-12"), first("ABS-3", "2026-08-03")}},
				{Values: [][]interface{}{middle("2026-08-02"), middle("2026-08-12"), middle("2026-08-03")}},
				{Values: [][]interface{}{last("2026-08-02"), last("2026-08-12"), last("2026-08-03")}},
			}})
		default:
			http.Error(w, "unexpected request: "+request.URL.String(), http.StatusNotFound)
		}
	}))
	defer server.Close()

	sheetsService, err := sheets.NewService(ctx, option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatalf("create Sheets service: %v", err)
	}
	store := NewGoogleSheetsRepository(sheetsService, "test-sheet", testLocation(t))
	rows, err := store.ListAttendanceBetween(ctx, "2026-08-02", "2026-08-03")
	if err != nil {
		t.Fatalf("ListAttendanceBetween: %v", err)
	}

	wantRanges := []string{
		"'absensi data'!A4:J6",
		"'absensi data'!L4:P6",
		"'absensi data'!R4:V6",
	}
	if !reflect.DeepEqual(requestedRanges, wantRanges) {
		t.Fatalf("detailed ranges = %#v, want %#v", requestedRanges, wantRanges)
	}
	if len(rows) != 2 || rows[0].TanggalAbsensi != "2026-08-02" || rows[1].TanggalAbsensi != "2026-08-03" {
		t.Fatalf("filtered rows = %+v", rows)
	}
	for _, row := range rows {
		if row.ClockInPhoto != "" || row.ClockOutPhoto != "" {
			t.Fatalf("attendance row leaked a photo: %+v", row)
		}
	}
}

func intPtrForTest(value int) *int { return &value }
