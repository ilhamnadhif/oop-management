package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
)

func postLeaveRequest(t *testing.T, client *http.Client, testServer *httptest.Server, csrf string, fields map[string]string, image []byte) *http.Response {
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
		part, err := writer.CreateFormFile("bukti_pendukung", "bukti.jpg")
		if err != nil {
			t.Fatalf("create leave attachment: %v", err)
		}
		if _, err := part.Write(image); err != nil {
			t.Fatalf("write leave attachment: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close leave request: %v", err)
	}

	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/leave/request", &body)
	if err != nil {
		t.Fatalf("create leave request: %v", err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("post leave request: %v", err)
	}
	return response
}

func leaveFields(kind, from, to, reason string) map[string]string {
	return map[string]string{
		"operation":       "create",
		"jenis_leave":     kind,
		"tanggal_mulai":   from,
		"tanggal_selesai": to,
		"alasan":          reason,
	}
}

func leaveFormCSRF(t *testing.T, client *http.Client, testServer *httptest.Server) string {
	t.Helper()
	return csrfFromForm(t, fetchAuthedPage(t, client, testServer.URL+"/leave/request"))
}

func requireLeaveResponse(t *testing.T, response *http.Response, status int, contains ...string) string {
	t.Helper()
	body := readBody(t, response)
	if response.StatusCode != status {
		t.Fatalf("leave response status = %d, want %d; body=%s", response.StatusCode, status, body)
	}
	for _, fragment := range contains {
		if !strings.Contains(body, fragment) {
			t.Fatalf("leave response missing %q: %s", fragment, body)
		}
	}
	return body
}

func TestRequestLeaveIsDirectMenuForEveryRoleAndHRPagesStayRestricted(t *testing.T) {
	roles := []string{"Flagman", "Security", "SHE", "Surveyor", "Logistik", "HR", "SPV", "Management", "Produksi"}
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			testServer := newTestServer(t)
			client := loggedInClientAs(t, testServer, role)
			response, err := client.Get(testServer.URL + "/leave/request")
			if err != nil {
				t.Fatalf("get request leave: %v", err)
			}
			page := requireLeaveResponse(t, response, http.StatusOK, "REQUEST LEAVE")
			nav := navSection(t, page)
			leaveAt := strings.Index(nav, `href="/leave/request"`)
			firstGroupAt := strings.Index(nav, `<details class="sidebar-group`)
			if leaveAt < 0 {
				t.Fatal("Request Leave is missing from the sidebar")
			}
			if firstGroupAt >= 0 && leaveAt > firstGroupAt {
				t.Fatal("Request Leave is nested inside or placed after a submenu group")
			}

			wantHR := role == "HR" || role == "Management"
			for _, path := range []string{"/hr/overview", "/hr/approval-leave"} {
				status := statusOf(t, client, testServer.URL+path)
				if wantHR && status != http.StatusOK {
					t.Fatalf("%s should reach %s, got %d", role, path, status)
				}
				if !wantHR && status != http.StatusForbidden {
					t.Fatalf("%s should be refused %s, got %d", role, path, status)
				}
			}
		})
	}
}

func TestLeavePagesRedirectAnonymousUsersToLogin(t *testing.T) {
	testServer := newTestServer(t)
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, path := range []string{"/leave/request", "/leave/attachment?leave_id=LVE-UNKNOWN", "/hr/overview", "/hr/approval-leave"} {
		response, err := client.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther && response.StatusCode != http.StatusFound {
			t.Fatalf("%s status = %d, want redirect", path, response.StatusCode)
		}
		if location := response.Header.Get("Location"); location != "/login" {
			t.Fatalf("%s redirected to %q, want /login", path, location)
		}
	}
}

func TestLeaveCreateAllowsBackdatedOverlappingRangesAndCountsWeekdays(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	csrf := leaveFormCSRF(t, owner, testServer)

	first := postLeaveRequest(t, owner, testServer, csrf,
		leaveFields(model.LeaveJenisCutiTahunan, "2026-07-31", "2026-08-03", "Keperluan keluarga lama"), nil)
	requireLeaveResponse(t, first, http.StatusOK, "berhasil diajukan")

	second := postLeaveRequest(t, owner, testServer, csrf,
		leaveFields(model.LeaveJenisIzin, "2026-08-03", "2026-08-05", "Pengajuan yang periodenya bersinggungan"), nil)
	requireLeaveResponse(t, second, http.StatusOK, "berhasil diajukan")

	rows := store.LeaveList()
	if len(rows) != 2 {
		t.Fatalf("stored leave count = %d, want 2", len(rows))
	}
	if rows[0].JumlahHari != 2 {
		t.Fatalf("Friday through Monday count = %d, want 2 weekdays", rows[0].JumlahHari)
	}
	if rows[1].JumlahHari != 3 {
		t.Fatalf("Monday through Wednesday count = %d, want 3 weekdays", rows[1].JumlahHari)
	}
	if rows[0].LeaveID == rows[1].LeaveID || !strings.HasPrefix(rows[0].LeaveID, "LVE-20260807-") {
		t.Fatalf("leave IDs were not assigned independently: %+v", rows)
	}

	history := fetchAuthedPage(t, owner, testServer.URL+"/leave/request")
	for _, row := range rows {
		if !strings.Contains(history, row.LeaveID) || !strings.Contains(history, row.Alasan) {
			t.Fatalf("owner history is missing %+v", row)
		}
	}

	unrelated := loggedInClientAs(t, testServer, "Security")
	otherHistory := fetchAuthedPage(t, unrelated, testServer.URL+"/leave/request")
	for _, row := range rows {
		if strings.Contains(otherHistory, row.LeaveID) {
			t.Fatalf("another employee can see owner request %s", row.LeaveID)
		}
	}
}

func TestSickLeaveRequiresPhotoAndAttachmentAccessIsPrivate(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	csrf := leaveFormCSRF(t, owner, testServer)
	fields := leaveFields(model.LeaveJenisCutiSakit, "2026-08-05", "2026-08-07", "Pemulihan setelah pemeriksaan dokter")

	missing := postLeaveRequest(t, owner, testServer, csrf, fields, nil)
	requireLeaveResponse(t, missing, http.StatusUnprocessableEntity, "bukti pendukung wajib untuk cuti sakit")
	if got := len(store.LeaveList()); got != 0 {
		t.Fatalf("invalid sick leave stored %d rows", got)
	}

	created := postLeaveRequest(t, owner, testServer, csrf, fields, testJPEG(t))
	requireLeaveResponse(t, created, http.StatusOK, "berhasil diajukan")
	rows := store.LeaveList()
	if len(rows) != 1 || !rows[0].HasBuktiPendukung || !strings.HasPrefix(rows[0].BuktiPendukung, photo.DataURLPrefix) {
		t.Fatalf("sick leave attachment was not normalized and stored: %+v", rows)
	}
	attachmentURL := testServer.URL + "/leave/attachment?leave_id=" + url.QueryEscape(rows[0].LeaveID)

	for label, client := range map[string]*http.Client{
		"owner": owner,
		"HR":    loggedInClientAs(t, testServer, "HR"),
	} {
		response, err := client.Get(attachmentURL)
		if err != nil {
			t.Fatalf("%s get attachment: %v", label, err)
		}
		payload := readBody(t, response)
		if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "image/jpeg" || payload == "" {
			t.Fatalf("%s attachment response: status=%d type=%q bytes=%d", label, response.StatusCode, response.Header.Get("Content-Type"), len(payload))
		}
		if cache := response.Header.Get("Cache-Control"); !strings.Contains(cache, "no-store") {
			t.Fatalf("%s attachment cache policy = %q", label, cache)
		}
	}

	unrelated := loggedInClientAs(t, testServer, "Security")
	response, err := unrelated.Get(attachmentURL)
	if err != nil {
		t.Fatalf("unrelated get attachment: %v", err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unrelated attachment status = %d, want 404", response.StatusCode)
	}
}

func TestPendingLeaveCanBeEditedAndCancelledOnlyByItsOwner(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	ownerCSRF := leaveFormCSRF(t, owner, testServer)
	created := postLeaveRequest(t, owner, testServer, ownerCSRF,
		leaveFields(model.LeaveJenisCutiTahunan, "2026-08-03", "2026-08-04", "Alasan awal"), nil)
	requireLeaveResponse(t, created, http.StatusOK, "berhasil diajukan")
	row := store.LeaveList()[0]

	editPageResponse, err := owner.Get(testServer.URL + "/leave/request?edit=" + url.QueryEscape(row.LeaveID))
	if err != nil {
		t.Fatalf("owner get edit page: %v", err)
	}
	editPage := requireLeaveResponse(t, editPageResponse, http.StatusOK, row.LeaveID, "Alasan awal", "Simpan perubahan")
	ownerCSRF = csrfFromForm(t, editPage)

	other := loggedInClientAs(t, testServer, "Security")
	otherCSRF := leaveFormCSRF(t, other, testServer)
	otherEdit, err := other.Get(testServer.URL + "/leave/request?edit=" + url.QueryEscape(row.LeaveID))
	if err != nil {
		t.Fatalf("other user get edit page: %v", err)
	}
	requireLeaveResponse(t, otherEdit, http.StatusNotFound, "Pengajuan leave tidak ditemukan")

	unauthorizedUpdate := leaveFields(model.LeaveJenisIzin, "2026-08-05", "2026-08-07", "Tidak boleh tersimpan")
	unauthorizedUpdate["operation"] = "update"
	unauthorizedUpdate["leave_id"] = row.LeaveID
	requireLeaveResponse(t, postLeaveRequest(t, other, testServer, otherCSRF, unauthorizedUpdate, nil),
		http.StatusForbidden, "tidak berhak")

	unauthorizedCancel, err := other.PostForm(testServer.URL+"/leave/request", url.Values{
		"csrf_token": {otherCSRF}, "operation": {"cancel"}, "leave_id": {row.LeaveID},
	})
	if err != nil {
		t.Fatalf("other user cancel: %v", err)
	}
	requireLeaveResponse(t, unauthorizedCancel, http.StatusForbidden, "tidak berhak")

	update := leaveFields(model.LeaveJenisIzin, "2026-08-05", "2026-08-07", "Alasan sudah direvisi")
	update["operation"] = "update"
	update["leave_id"] = row.LeaveID
	requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, ownerCSRF, update, nil),
		http.StatusOK, "berhasil diperbarui")
	stored := store.LeaveList()[0]
	if stored.JenisLeave != model.LeaveJenisIzin || stored.JumlahHari != 3 || stored.Alasan != "Alasan sudah direvisi" || stored.Status != model.LeaveStatusMenunggu {
		t.Fatalf("pending update stored unexpected fields: %+v", stored)
	}

	cancelled, err := owner.PostForm(testServer.URL+"/leave/request", url.Values{
		"csrf_token": {ownerCSRF}, "operation": {"cancel"}, "leave_id": {row.LeaveID},
	})
	if err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
	requireLeaveResponse(t, cancelled, http.StatusOK, "berhasil dibatalkan")
	if got := store.LeaveList()[0]; got.Status != model.LeaveStatusDibatalkan || got.DibatalkanPada == nil {
		t.Fatalf("cancel did not preserve its lifecycle audit: %+v", got)
	}

	closedEdit, err := owner.Get(testServer.URL + "/leave/request?edit=" + url.QueryEscape(row.LeaveID))
	if err != nil {
		t.Fatalf("owner get cancelled edit: %v", err)
	}
	requireLeaveResponse(t, closedEdit, http.StatusConflict, "Hanya pengajuan yang masih menunggu")
}

func TestHRCanApproveOwnLeave(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	created := postLeaveRequest(t, hr, testServer, leaveFormCSRF(t, hr, testServer),
		leaveFields(model.LeaveJenisCutiTahunan, "2026-08-10", "2026-08-11", "Cuti HR sendiri"), nil)
	requireLeaveResponse(t, created, http.StatusOK, "berhasil diajukan")
	row := store.LeaveList()[0]

	approvalPage := fetchAuthedPage(t, hr, testServer.URL+"/hr/approval-leave")
	response, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"csrf_token": {csrfFromForm(t, approvalPage)},
		"leave_id":   {row.LeaveID},
		"decision":   {"approve"},
	})
	if err != nil {
		t.Fatalf("approve own leave: %v", err)
	}
	requireLeaveResponse(t, response, http.StatusOK, "berhasil ditandai disetujui")
	stored := store.LeaveList()[0]
	users := store.UserList()
	if stored.Status != model.LeaveStatusDisetujui || stored.DiprosesPada == nil || len(users) != 1 || stored.DiprosesOlehUserID != users[0].UserID {
		t.Fatalf("self approval audit is incomplete: leave=%+v users=%+v", stored, users)
	}
}

func TestStaleEditReturnsToFinalReadOnlyHistory(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	ownerCSRF := leaveFormCSRF(t, owner, testServer)
	requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, ownerCSRF,
		leaveFields(model.LeaveJenisIzin, "2026-08-07", "2026-08-07", "Alasan awal"), nil),
		http.StatusOK, "berhasil diajukan")
	row := store.LeaveList()[0]

	hr := loggedInClientAs(t, testServer, "HR")
	hrCSRF := csrfFromForm(t, fetchAuthedPage(t, hr, testServer.URL+"/hr/approval-leave"))
	decision, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"csrf_token": {hrCSRF}, "leave_id": {row.LeaveID}, "decision": {"approve"},
	})
	if err != nil {
		t.Fatalf("approve while owner is editing: %v", err)
	}
	requireLeaveResponse(t, decision, http.StatusOK, "berhasil ditandai disetujui")

	stale := leaveFields(model.LeaveJenisIzin, "2026-08-08", "2026-08-10", "Perubahan dari tab lama")
	stale["operation"] = "update"
	stale["leave_id"] = row.LeaveID
	body := requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, ownerCSRF, stale, nil),
		http.StatusConflict, "sudah diproses", "Buat pengajuan baru", model.LeaveStatusDisetujui)
	if strings.Contains(body, "Simpan perubahan") || strings.Contains(body, `class="status-chip pending">MENUNGGU`) {
		t.Fatalf("stale final request remained editable: %s", body)
	}
}

func TestRejectRequiresNoteAndASecondDecisionConflicts(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, leaveFormCSRF(t, owner, testServer),
		leaveFields(model.LeaveJenisIzin, "2026-08-06", "2026-08-07", "Urusan administrasi"), nil),
		http.StatusOK, "berhasil diajukan")
	row := store.LeaveList()[0]

	hr := loggedInClientAs(t, testServer, "HR")
	csrf := csrfFromForm(t, fetchAuthedPage(t, hr, testServer.URL+"/hr/approval-leave"))
	missingNote, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"csrf_token": {csrf}, "leave_id": {row.LeaveID}, "decision": {"reject"},
	})
	if err != nil {
		t.Fatalf("reject without note: %v", err)
	}
	requireLeaveResponse(t, missingNote, http.StatusUnprocessableEntity, "catatan wajib diisi saat menolak")
	if got := store.LeaveList()[0].Status; got != model.LeaveStatusMenunggu {
		t.Fatalf("invalid rejection changed status to %q", got)
	}

	rejected, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"csrf_token": {csrf}, "leave_id": {row.LeaveID}, "decision": {"reject"},
		"catatan_approval": {"Dokumen pendukung belum lengkap"},
	})
	if err != nil {
		t.Fatalf("reject with note: %v", err)
	}
	requireLeaveResponse(t, rejected, http.StatusOK, "berhasil ditandai ditolak")
	stored := store.LeaveList()[0]
	if stored.Status != model.LeaveStatusDitolak || stored.CatatanApproval != "Dokumen pendukung belum lengkap" || stored.DiprosesPada == nil {
		t.Fatalf("rejection audit is incomplete: %+v", stored)
	}

	second, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"csrf_token": {csrf}, "leave_id": {row.LeaveID}, "decision": {"approve"},
	})
	if err != nil {
		t.Fatalf("second decision: %v", err)
	}
	requireLeaveResponse(t, second, http.StatusConflict, "sudah diproses")
	if got := store.LeaveList()[0].Status; got != model.LeaveStatusDitolak {
		t.Fatalf("second decision changed final status to %q", got)
	}
}

// chipsOn lists the status chips a page rendered, for a failure message that
// says what was there instead.
func chipsOn(page string) []string {
	var chips []string
	for _, part := range strings.Split(page, `class="status-chip `)[1:] {
		if end := strings.Index(part, "</span>"); end >= 0 {
			chips = append(chips, part[:end])
		}
	}
	return chips
}

// The card tells the two apart by the badge alone, so the colours have to be
// the ones the rest of the app uses for "wrong" and for "wait": red for nobody
// clocked in, amber for a shift still open.
func TestHROverviewBadgesAbsenceAndOpenShiftsApart(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	// Two accounts, one for each half of the card: HR clocks in and leaves the
	// shift open, and the storekeeper never clocks in at all.
	loggedInClientAs(t, testServer, "Logistik")
	users := store.UserList()
	var hrUser model.User
	for _, user := range users {
		if user.Jabatan == "HR" {
			hrUser = user
		}
	}
	if hrUser.UserID == "" {
		t.Fatal("the HR account was not registered")
	}
	location := time.FixedZone("WIB", 7*60*60)
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: hrUser.UserID, NRP: hrUser.NRP,
		NamaLengkap: hrUser.NamaLengkap, Jabatan: hrUser.Jabatan,
		TanggalAbsensi: "2026-08-07", ClockInAt: time.Date(2026, 8, 7, 7, 0, 0, 0, location),
		StatusAbsensi: model.AttendanceBelumClockOut,
	}); err != nil {
		t.Fatalf("seed open shift: %v", err)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/overview?from=2026-08-07&to=2026-08-07")
	for _, want := range []string{
		`<span class="status-chip rejected">Belum absen</span>`,
		`<span class="status-chip pending">Belum clock out</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("the card is missing %q; chips on page: %v", want, chipsOn(page))
		}
	}
	if !strings.Contains(page, "belum clock out per 2026-08-07") {
		t.Fatalf("the heading does not count the open shifts:\n%s", firstLines(page))
	}
}

// The overview names who stayed on and for how long, and a shift nobody closed
// is not overtime: there is no departure to measure it from.
func TestHROverviewNamesWhoWorkedOvertime(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	hr := loggedInClientAs(t, testServer, "HR")
	loggedInClientAs(t, testServer, "Logistik")

	users := store.UserList()
	var stayed, open model.User
	for _, user := range users {
		switch user.Jabatan {
		case "HR":
			stayed = user
		case "Logistik":
			open = user
		}
	}
	location := time.FixedZone("WIB", 7*60*60)
	// The default working day ends at 17:00, so 19:00 is two hours past it.
	out := time.Date(2026, 8, 7, 19, 0, 0, 0, location)
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-OT", UserID: stayed.UserID, NRP: stayed.NRP,
		NamaLengkap: stayed.NamaLengkap, Jabatan: stayed.Jabatan,
		TanggalAbsensi: "2026-08-07",
		ClockInAt:      time.Date(2026, 8, 7, 8, 0, 0, 0, location), ClockOutAt: &out,
		StatusAbsensi: model.AttendanceSelesai,
	}); err != nil {
		t.Fatalf("seed overtime day: %v", err)
	}
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: open.UserID, NRP: open.NRP,
		NamaLengkap: open.NamaLengkap, Jabatan: open.Jabatan,
		TanggalAbsensi: "2026-08-07",
		ClockInAt:      time.Date(2026, 8, 7, 8, 0, 0, 0, location),
		StatusAbsensi:  model.AttendanceBelumClockOut,
	}); err != nil {
		t.Fatalf("seed open day: %v", err)
	}

	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/overview?from=2026-08-07&to=2026-08-07")
	if !strings.Contains(page, "LEMBUR") {
		t.Fatal("the overview has no overtime card")
	}
	if !strings.Contains(page, `<span class="status-chip approved">2j</span>`) {
		t.Fatalf("the overtime card does not say how long:\n%s", firstLines(page))
	}
	if !strings.Contains(page, "1 orang per 2026-08-07") {
		t.Fatal("the open shift was counted as overtime")
	}
}

func TestHROverviewRendersCoreDashboardAndLatestRequest(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, leaveFormCSRF(t, owner, testServer),
		leaveFields(model.LeaveJenisCutiTahunan, "2026-08-06", "2026-08-07", "Terlihat di overview HR"), nil),
		http.StatusOK, "berhasil diajukan")
	leaveID := store.LeaveList()[0].LeaveID

	hr := loggedInClientAs(t, testServer, "HR")
	page := fetchAuthedPage(t, hr, testServer.URL+"/hr/overview")
	for _, fragment := range []string{
		"TOTAL KARYAWAN", "HADIR", "TIDAK HADIR", "CUTI &amp; IZIN",
		"RINGKASAN ABSENSI", "DISTRIBUSI KARYAWAN", "KARYAWAN BARU", "PENGAJUAN TERBARU",
		// A count says how many were missing; the lists say who.
		"ABSENSI BELUM LENGKAP", "Belum absen",
		leaveID, "2026-08-01", "2026-08-07",
	} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("HR overview missing %q", fragment)
		}
	}
	if !strings.Contains(page, "LEMBUR") {
		t.Fatal("the HR overview does not report overtime")
	}

	invalid, err := hr.Get(testServer.URL + "/hr/overview?from=invalid&to=2026-08-07")
	if err != nil {
		t.Fatalf("get invalid overview filter: %v", err)
	}
	requireLeaveResponse(t, invalid, http.StatusOK, "tanggal awal tidak valid")
}

// A request just filed shows up on the page that filed it, waiting for a
// decision. That summary used to sit on the dashboard as well; leave has its
// own menu now, and this checks the one place that still carries it.
func TestLeaveRequestPageShowsTrackedSummary(t *testing.T) {
	testServer, _ := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	requireLeaveResponse(t, postLeaveRequest(t, owner, testServer, leaveFormCSRF(t, owner, testServer),
		leaveFields(model.LeaveJenisIzin, "2026-08-07", "2026-08-07", "Izin hari ini"), nil),
		http.StatusOK, "berhasil diajukan")

	page := fetchAuthedPage(t, owner, testServer.URL+"/leave/request")
	for _, fragment := range []string{"Hari disetujui", "Menunggu approval", "MENUNGGU"} {
		if !strings.Contains(page, fragment) {
			t.Fatalf("the leave request page is missing %q", fragment)
		}
	}
	for _, obsolete := range []string{"belum dicatat di aplikasi ini", "Ringkasan cuti dan izin belum tersedia"} {
		if strings.Contains(page, obsolete) {
			t.Fatalf("the page still says leave is untracked: %q", obsolete)
		}
	}
}

func TestLeavePOSTsRequireCSRFAndDeclaredMethods(t *testing.T) {
	testServer := newTestServer(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	withoutCSRF := postLeaveRequest(t, owner, testServer, "",
		leaveFields(model.LeaveJenisCutiTahunan, "2026-08-07", "2026-08-07", "Tanpa CSRF"), nil)
	requireLeaveResponse(t, withoutCSRF, http.StatusForbidden, "CSRF token tidak valid")

	hr := loggedInClientAs(t, testServer, "HR")
	decisionWithoutCSRF, err := hr.PostForm(testServer.URL+"/hr/approval-leave", url.Values{
		"leave_id": {"LVE-UNKNOWN"}, "decision": {"approve"},
	})
	if err != nil {
		t.Fatalf("approval without csrf: %v", err)
	}
	requireLeaveResponse(t, decisionWithoutCSRF, http.StatusForbidden, "CSRF token tidak valid")

	for _, test := range []struct {
		method string
		path   string
		allow  string
	}{
		{http.MethodPut, "/leave/request", "GET, POST"},
		{http.MethodPut, "/hr/approval-leave", "GET, POST"},
		{http.MethodPost, "/leave/attachment?leave_id=LVE-UNKNOWN", "GET"},
		{http.MethodPost, "/hr/overview", "GET"},
	} {
		request, err := http.NewRequest(test.method, testServer.URL+test.path, nil)
		if err != nil {
			t.Fatalf("new %s %s: %v", test.method, test.path, err)
		}
		response, err := hr.Do(request)
		if err != nil {
			t.Fatalf("%s %s: %v", test.method, test.path, err)
		}
		body := readBody(t, response)
		if response.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s status = %d, want 405; body=%s", test.method, test.path, response.StatusCode, body)
		}
		if allow := response.Header.Get("Allow"); allow != test.allow {
			t.Fatalf("%s %s Allow = %q, want %q", test.method, test.path, allow, test.allow)
		}
	}
}

func TestLeaveURLEncodedPOSTBodiesAreBounded(t *testing.T) {
	testServer := newTestServer(t)
	oversized := "padding=" + strings.Repeat("x", 100*1024)
	for _, tc := range []struct {
		name   string
		client *http.Client
		path   string
	}{
		{name: "request", client: loggedInClientAs(t, testServer, "Produksi"), path: "/leave/request"},
		{name: "approval", client: loggedInClientAs(t, testServer, "HR"), path: "/hr/approval-leave"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, testServer.URL+tc.path, strings.NewReader(oversized))
			if err != nil {
				t.Fatalf("new oversized request: %v", err)
			}
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response, err := tc.client.Do(request)
			if err != nil {
				t.Fatalf("post oversized request: %v", err)
			}
			body := readBody(t, response)
			if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Form tidak valid") {
				t.Fatalf("oversized %s response = %d %q", tc.name, response.StatusCode, body)
			}
		})
	}
}

func TestLeaveStoreRemainsEmptyAfterUnknownOperation(t *testing.T) {
	testServer, store := newTestServerWithStore(t)
	owner := loggedInClientAs(t, testServer, "Produksi")
	fields := leaveFields(model.LeaveJenisCutiTahunan, "2026-08-07", "2026-08-07", "Tidak boleh tersimpan")
	fields["operation"] = "arbitrary-operation"
	response := postLeaveRequest(t, owner, testServer, leaveFormCSRF(t, owner, testServer), fields, nil)
	requireLeaveResponse(t, response, http.StatusUnprocessableEntity, "aksi pengajuan tidak dikenal")
	if rows := store.LeaveList(); len(rows) != 0 {
		t.Fatalf("unknown operation stored rows: %+v", rows)
	}
}
