package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

type leaveFixture struct {
	service  *LeaveService
	store    *repository.TestRepository
	location *time.Location
	now      *time.Time
	user     *model.User
	hr       *model.User
}

func newLeaveFixture(t *testing.T) leaveFixture {
	t.Helper()
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, location)
	user := leaveUser("usr_1", "1001", "Budi", "Produksi", "2026-01-02")
	hr := leaveUser("usr_hr", "9001", "Rina HR", "HR", "2025-01-02")
	createFixtureUser(t, store, user)
	createFixtureUser(t, store, hr)
	service := NewLeaveService(store, location, func() time.Time { return now })
	return leaveFixture{service: service, store: store, location: location, now: &now, user: user, hr: hr}
}

func leaveUser(id, nrp, name, position, joined string) *model.User {
	return &model.User{
		UserID: id, NRP: nrp, NamaLengkap: name, Jabatan: position,
		Email: nrp + "@example.test", TanggalGabung: joined,
		StatusPengguna: model.StatusAktif,
	}
}

func createFixtureUser(t *testing.T, store *repository.TestRepository, user *model.User) {
	t.Helper()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create fixture user %s: %v", user.UserID, err)
	}
}

func annualLeaveInput() LeaveInput {
	return LeaveInput{
		JenisLeave:   model.LeaveJenisCutiTahunan,
		TanggalMulai: "2026-08-07", TanggalSelesai: "2026-08-10",
		Alasan: "Keperluan keluarga",
	}
}

func TestLeaveCreateDerivesIdentityStatusWeekdaysAndSequence(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()

	first, err := fixture.service.Create(ctx, fixture.user, annualLeaveInput())
	if err != nil {
		t.Fatalf("create first request: %v", err)
	}
	secondInput := annualLeaveInput()
	secondInput.JenisLeave = "izin"
	second, err := fixture.service.Create(ctx, fixture.user, secondInput)
	if err != nil {
		t.Fatalf("create overlapping request: %v", err)
	}

	if first.LeaveID != "LVE-20260810-0001" || second.LeaveID != "LVE-20260810-0002" {
		t.Fatalf("unexpected IDs: %q, %q", first.LeaveID, second.LeaveID)
	}
	if first.JumlahHari != 2 { // Friday and Monday; the weekend is excluded.
		t.Fatalf("weekday count = %d, want 2", first.JumlahHari)
	}
	if first.UserID != fixture.user.UserID || first.NRP != fixture.user.NRP || first.NamaLengkap != fixture.user.NamaLengkap {
		t.Fatalf("requester snapshot is incomplete: %+v", first)
	}
	if first.Status != model.LeaveStatusMenunggu || second.JenisLeave != model.LeaveJenisIzin {
		t.Fatalf("server-derived values are wrong: first=%+v second=%+v", first, second)
	}
	if got := len(fixture.store.LeaveList()); got != 2 {
		t.Fatalf("stored requests = %d, want 2", got)
	}
}

func TestCountWeekdaysHandlesMultiCenturyRange(t *testing.T) {
	start := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2400, time.December, 31, 0, 0, 0, 0, time.UTC)
	if got, want := countWeekdays(start, end), 104615; got != want {
		t.Fatalf("multi-century weekday count = %d, want %d", got, want)
	}
}

func TestLeaveCreateValidatesDatesTypesReasonAndSickEvidence(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()

	tests := []struct {
		name  string
		input LeaveInput
		want  error
	}{
		{"unknown type", LeaveInput{JenisLeave: "Liburan", TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: "x"}, ErrValidation},
		{"missing reason", LeaveInput{JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10"}, ErrValidation},
		{"reason too long", LeaveInput{JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: strings.Repeat("a", LeaveTextMaxLength+1)}, ErrValidation},
		{"reversed", LeaveInput{JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-11", TanggalSelesai: "2026-08-10", Alasan: "x"}, ErrValidation},
		{"weekend only", LeaveInput{JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-08", TanggalSelesai: "2026-08-09", Alasan: "x"}, ErrValidation},
		{"sick without proof", LeaveInput{JenisLeave: model.LeaveJenisCutiSakit, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: "Sakit"}, ErrValidation},
		{"invalid proof", LeaveInput{JenisLeave: model.LeaveJenisCutiSakit, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: "Sakit", BuktiPendukung: "data:image/jpeg;base64,bukan-gambar"}, ErrInvalidPhoto},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := fixture.service.Create(ctx, fixture.user, tc.input); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}

	valid := LeaveInput{
		JenisLeave: model.LeaveJenisCutiSakit, TanggalMulai: "2025-12-29", TanggalSelesai: "2026-01-02",
		Alasan: "Rawat jalan", BuktiPendukung: testPhoto(t),
	}
	leave, err := fixture.service.Create(ctx, fixture.user, valid)
	if err != nil {
		t.Fatalf("valid historical sick leave rejected: %v", err)
	}
	if leave.JumlahHari != 5 || !leave.HasBuktiPendukung {
		t.Fatalf("unexpected sick request: %+v", leave)
	}
}

func TestLeaveUpdateAttachmentRulesOwnershipAndFinality(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	photoOne := testPhoto(t)
	input := LeaveInput{
		JenisLeave: model.LeaveJenisCutiSakit, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-12",
		Alasan: "Demam", BuktiPendukung: photoOne,
	}
	leave, err := fixture.service.Create(ctx, fixture.user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	input.Alasan = "Kontrol dokter"
	kept, err := fixture.service.Update(ctx, fixture.user, leave.LeaveID, input, "", LeaveAttachmentKeep)
	if err != nil {
		t.Fatalf("keep attachment: %v", err)
	}
	if !kept.HasBuktiPendukung || fixture.store.LeaveList()[0].BuktiPendukung != photoOne {
		t.Fatal("keep action changed the stored attachment")
	}
	if _, err := fixture.service.Update(ctx, fixture.user, leave.LeaveID, input, "", LeaveAttachmentRemove); !errors.Is(err, ErrValidation) {
		t.Fatalf("sick proof removal error = %v, want validation", err)
	}

	input.JenisLeave = model.LeaveJenisIzin
	removed, err := fixture.service.Update(ctx, fixture.user, leave.LeaveID, input, "", LeaveAttachmentRemove)
	if err != nil {
		t.Fatalf("remove optional attachment: %v", err)
	}
	stored := fixture.store.LeaveList()[0]
	if removed.HasBuktiPendukung || stored.HasBuktiPendukung || stored.BuktiPendukung != "" {
		t.Fatalf("attachment was not removed: returned=%+v stored=%+v", removed, stored)
	}

	other := leaveUser("usr_other", "1002", "Sari", "Produksi", "2026-01-02")
	createFixtureUser(t, fixture.store, other)
	if _, err := fixture.service.Update(ctx, other, leave.LeaveID, input, "", LeaveAttachmentKeep); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign edit error = %v, want forbidden", err)
	}

	cancelled, err := fixture.service.Cancel(ctx, fixture.user, leave.LeaveID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if cancelled.Status != model.LeaveStatusDibatalkan || cancelled.DibatalkanPada == nil {
		t.Fatalf("cancel audit missing: %+v", cancelled)
	}
	if _, err := fixture.service.Update(ctx, fixture.user, leave.LeaveID, input, "", LeaveAttachmentKeep); !errors.Is(err, ErrConflict) {
		t.Fatalf("edit final request error = %v, want conflict", err)
	}
	if _, err := fixture.service.Cancel(ctx, fixture.user, leave.LeaveID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second cancel error = %v, want conflict", err)
	}
}

func TestLeaveDecisionRoleNoteSelfApprovalAndFinality(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()

	ordinary, err := fixture.service.Create(ctx, fixture.user, annualLeaveInput())
	if err != nil {
		t.Fatalf("create ordinary request: %v", err)
	}
	if _, err := fixture.service.Decide(ctx, fixture.user, ordinary.LeaveID, LeaveDecisionApprove, ""); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary approver error = %v, want forbidden", err)
	}
	if _, err := fixture.service.Decide(ctx, fixture.hr, ordinary.LeaveID, LeaveDecisionReject, " "); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank rejection note error = %v, want validation", err)
	}
	if _, err := fixture.service.Decide(ctx, fixture.hr, ordinary.LeaveID, LeaveDecisionApprove, strings.Repeat("a", LeaveTextMaxLength+1)); !errors.Is(err, ErrValidation) {
		t.Fatalf("long approval note error = %v, want validation", err)
	}
	rejected, err := fixture.service.Decide(ctx, fixture.hr, ordinary.LeaveID, LeaveDecisionReject, "Dokumen belum lengkap")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != model.LeaveStatusDitolak || rejected.DiprosesOlehUserID != fixture.hr.UserID || rejected.DiprosesPada == nil {
		t.Fatalf("decision audit missing: %+v", rejected)
	}
	if _, err := fixture.service.Decide(ctx, fixture.hr, ordinary.LeaveID, LeaveDecisionApprove, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("second decision error = %v, want conflict", err)
	}

	selfInput := annualLeaveInput()
	self, err := fixture.service.Create(ctx, fixture.hr, selfInput)
	if err != nil {
		t.Fatalf("create HR's own request: %v", err)
	}
	approved, err := fixture.service.Decide(ctx, fixture.hr, self.LeaveID, LeaveDecisionApprove, "Disetujui")
	if err != nil {
		t.Fatalf("self approval: %v", err)
	}
	if approved.Status != model.LeaveStatusDisetujui {
		t.Fatalf("self approval status = %q", approved.Status)
	}
}

func TestLeaveAttachmentIsOnlyForOwnerOrHR(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	proof := testPhoto(t)
	input := annualLeaveInput()
	input.BuktiPendukung = proof
	leave, err := fixture.service.Create(ctx, fixture.user, input)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	other := leaveUser("usr_other", "1002", "Sari", "Produksi", "2026-01-02")
	createFixtureUser(t, fixture.store, other)

	if _, err := fixture.service.Attachment(ctx, other, leave.LeaveID, false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("foreign attachment error = %v, want forbidden", err)
	}
	for name, testCase := range map[string]struct {
		user  *model.User
		canHR bool
	}{
		"owner": {fixture.user, false},
		"HR":    {fixture.hr, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := fixture.service.Attachment(ctx, testCase.user, leave.LeaveID, testCase.canHR)
			if err != nil || got != proof {
				t.Fatalf("attachment = %q, %v", got, err)
			}
		})
	}
}

func TestApprovalRowsFiltersOverlapAndPrioritizesOldestPending(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	base := fixture.now.Add(-48 * time.Hour)
	rows := []*model.Leave{
		{LeaveID: "LVE-OLD", UserID: fixture.user.UserID, NRP: "1001", NamaLengkap: "Budi", Jabatan: "Produksi", JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-08", TanggalSelesai: "2026-08-12", Alasan: "Keluarga", Status: model.LeaveStatusMenunggu, CreatedAt: base, UpdatedAt: base},
		{LeaveID: "LVE-NEW", UserID: fixture.hr.UserID, NRP: "9001", NamaLengkap: "Rina HR", Jabatan: "HR", JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: "Dokumen", Status: model.LeaveStatusMenunggu, CreatedAt: base.Add(time.Hour), UpdatedAt: base.Add(time.Hour)},
		{LeaveID: "LVE-DONE", UserID: fixture.user.UserID, NRP: "1001", NamaLengkap: "Budi", Jabatan: "Produksi", JenisLeave: model.LeaveJenisCutiTahunan, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Alasan: "Libur", Status: model.LeaveStatusDisetujui, CreatedAt: base.Add(-time.Hour), UpdatedAt: base.Add(2 * time.Hour)},
	}
	for _, row := range rows {
		if err := fixture.store.CreateLeave(ctx, row); err != nil {
			t.Fatalf("seed leave: %v", err)
		}
	}

	all, err := fixture.service.ApprovalRows(ctx, LeaveFilters{})
	if err != nil {
		t.Fatalf("all rows: %v", err)
	}
	if len(all) != 3 || all[0].LeaveID != "LVE-OLD" || all[1].LeaveID != "LVE-NEW" {
		t.Fatalf("pending order is wrong: %+v", all)
	}
	filtered, err := fixture.service.ApprovalRows(ctx, LeaveFilters{Q: "rina", Status: "menunggu", JenisLeave: "izin", From: "2026-08-11", To: "2026-08-09"})
	if err != nil {
		t.Fatalf("filtered rows: %v", err)
	}
	if len(filtered) != 1 || filtered[0].LeaveID != "LVE-NEW" {
		t.Fatalf("filter result = %+v", filtered)
	}
	if _, err := fixture.service.ApprovalRows(ctx, LeaveFilters{Status: "UNKNOWN"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("unknown status error = %v, want validation", err)
	}
}

func TestPersonalSummaryCountsUniqueApprovedWeekdaysAndPending(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	rows := []*model.Leave{
		{LeaveID: "LVE-A", UserID: fixture.user.UserID, JenisLeave: model.LeaveJenisCutiTahunan, TanggalMulai: "2025-12-29", TanggalSelesai: "2026-01-02", Status: model.LeaveStatusDisetujui},
		{LeaveID: "LVE-B", UserID: fixture.user.UserID, JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-01-02", TanggalSelesai: "2026-01-05", Status: model.LeaveStatusDisetujui},
		{LeaveID: "LVE-TODAY", UserID: fixture.user.UserID, JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Status: model.LeaveStatusDisetujui},
		{LeaveID: "LVE-P", UserID: fixture.user.UserID, JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-11", Status: model.LeaveStatusMenunggu},
		{LeaveID: "LVE-OTHER", UserID: fixture.hr.UserID, JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-02-02", TanggalSelesai: "2026-02-02", Status: model.LeaveStatusDisetujui},
	}
	for _, row := range rows {
		if err := fixture.store.CreateLeave(ctx, row); err != nil {
			t.Fatalf("seed leave: %v", err)
		}
	}
	summary, err := fixture.service.PersonalSummary(ctx, fixture.user.UserID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	// Jan 1, Jan 2, Jan 5 and Aug 10. Jan 2 appears in two requests but counts once.
	if summary.Year != 2026 || summary.ApprovedDays != 4 || summary.PendingCount != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if summary.TodayStatus != model.LeaveStatusDisetujui || summary.TodayLeave == nil || summary.TodayLeave.LeaveID != "LVE-TODAY" {
		t.Fatalf("today precedence is wrong: %+v", summary)
	}
}

// The KPI cards say how many; these lists say who. Nobody can be chased up
// from a count, which is the whole reason the names are carried.
func TestHROverviewNamesWhoIsAbsentAndWhoIsOnLeave(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	sakit := leaveUser("usr_sakit", "9101", "Ani Lestari", "Logistik", "2025-01-01")
	absent := leaveUser("usr_absent", "9102", "Cahyo Nugroho", "Security", "2025-01-01")
	createFixtureUser(t, fixture.store, sakit)
	createFixtureUser(t, fixture.store, absent)

	// Budi is present; the other three active employees are not.
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-1", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 7, 0, 0, 0, fixture.location),
	}); err != nil {
		t.Fatalf("seed attendance: %v", err)
	}
	if err := fixture.store.CreateLeave(ctx, &model.Leave{
		LeaveID: "LVE-SAKIT", UserID: sakit.UserID, NamaLengkap: sakit.NamaLengkap,
		JenisLeave: model.LeaveJenisCutiSakit, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10",
		Status: model.LeaveStatusDisetujui,
	}); err != nil {
		t.Fatalf("seed leave: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}

	// Someone on approved leave is away, not missing: the two lists never hold
	// the same person.
	if len(overview.CutiHariAkhirNama) != 1 ||
		overview.CutiHariAkhirNama[0].NamaLengkap != "Ani Lestari" ||
		overview.CutiHariAkhirNama[0].Keterangan != model.LeaveJenisCutiSakit ||
		overview.CutiHariAkhirNama[0].Jabatan != "Logistik" {
		t.Fatalf("leave list is wrong: %+v", overview.CutiHariAkhirNama)
	}
	// Sorted by name, so a reload does not reshuffle the list: "Cahyo Nugroho"
	// then the HR fixture, "Rina HR".
	names := make([]string, 0, len(overview.BelumAbsenNama))
	for _, person := range overview.BelumAbsenNama {
		names = append(names, person.NamaLengkap)
		if person.Keterangan != "" {
			t.Fatalf("an absent person carries a leave type: %+v", person)
		}
	}
	if len(names) != 2 || names[0] != "Cahyo Nugroho" {
		t.Fatalf("absent list is wrong: %v", names)
	}
	if len(overview.BelumAbsenNama) != overview.TidakHadirHariAkhir ||
		len(overview.CutiHariAkhirNama) != overview.CutiHariAkhir {
		t.Fatalf("the lists disagree with the cards: %d/%d absent, %d/%d on leave",
			len(overview.BelumAbsenNama), overview.TidakHadirHariAkhir,
			len(overview.CutiHariAkhirNama), overview.CutiHariAkhir)
	}
}

func TestHROverviewAggregatesPresenceLeaveAbsenceAndPeople(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	// The fixture has Budi (joined Jan 2) and HR (joined 2025). Add someone who
	// joins on the report end date, plus inactive and future employees.
	management := leaveUser("usr_management", "9002", "Dewi", "Management", "2026-08-10")
	inactive := leaveUser("usr_inactive", "1003", "Nonaktif", "Produksi", "2025-01-01")
	inactive.StatusPengguna = model.StatusTidakAktif
	future := leaveUser("usr_future", "1004", "Belum Bergabung", "Produksi", "2026-08-11")
	createFixtureUser(t, fixture.store, management)
	createFixtureUser(t, fixture.store, inactive)
	createFixtureUser(t, fixture.store, future)

	clockIn := time.Date(2026, 8, 10, 9, 0, 0, 0, fixture.location)
	clockOutLate := time.Date(2026, 8, 10, 19, 0, 0, 0, fixture.location)
	clockOutNormal := time.Date(2026, 8, 10, 17, 0, 0, 0, fixture.location)
	for _, row := range []*model.Attendance{
		{AbsensiID: "ABS-1", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10", ClockInAt: clockIn, ClockOutAt: &clockOutLate},
		{AbsensiID: "ABS-2", UserID: fixture.hr.UserID, TanggalAbsensi: "2026-08-10", ClockInAt: clockIn, ClockOutAt: &clockOutNormal},
		{AbsensiID: "ABS-INACTIVE", UserID: inactive.UserID, TanggalAbsensi: "2026-08-10", ClockInAt: clockIn, ClockOutAt: &clockOutLate},
	} {
		if err := fixture.store.CreateAttendance(ctx, row); err != nil {
			t.Fatalf("seed attendance: %v", err)
		}
	}
	created := fixture.now.Add(-2 * time.Hour)
	for _, row := range []*model.Leave{
		// HR's attendance on the same date wins over this approved leave.
		{LeaveID: "LVE-HR", UserID: fixture.hr.UserID, NamaLengkap: fixture.hr.NamaLengkap, JenisLeave: model.LeaveJenisIzin, TanggalMulai: "2026-08-10", TanggalSelesai: "2026-08-10", Status: model.LeaveStatusDisetujui, CreatedAt: created},
		{LeaveID: "LVE-MGMT", UserID: management.UserID, NamaLengkap: management.NamaLengkap, JenisLeave: model.LeaveJenisCutiTahunan, TanggalMulai: "2026-08-08", TanggalSelesai: "2026-08-10", Status: model.LeaveStatusDisetujui, CreatedAt: created.Add(time.Hour)},
	} {
		if err := fixture.store.CreateLeave(ctx, row); err != nil {
			t.Fatalf("seed leave: %v", err)
		}
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-09", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if overview.TotalKaryawan != 3 || overview.HadirHariAkhir != 2 || overview.CutiHariAkhir != 1 || overview.TidakHadirHariAkhir != 0 {
		t.Fatalf("end-date KPIs are wrong: %+v", overview)
	}
	// The KPI figures are counts; the lists are the people they stand for, and
	// the two have to agree or one of them is lying about the same day.
	if len(overview.BelumAbsenNama) != overview.TidakHadirHariAkhir {
		t.Fatalf("absent list holds %d names against a count of %d",
			len(overview.BelumAbsenNama), overview.TidakHadirHariAkhir)
	}
	if len(overview.CutiHariAkhirNama) != 1 ||
		overview.CutiHariAkhirNama[0].NamaLengkap != management.NamaLengkap ||
		overview.CutiHariAkhirNama[0].Keterangan != model.LeaveJenisCutiTahunan {
		t.Fatalf("leave list is wrong: %+v", overview.CutiHariAkhirNama)
	}
	if len(overview.Series) != 2 || overview.Series[0].Tanggal != "2026-08-09" || overview.Series[0].Cuti != 0 || overview.Series[0].TidakHadir != 2 {
		t.Fatalf("weekend/series calculation is wrong: %+v", overview.Series)
	}
	if len(overview.JabatanShares) != 3 || overview.JabatanShares[0].Percent < 33 || overview.JabatanShares[0].Percent > 34 {
		t.Fatalf("position distribution is wrong: %+v", overview.JabatanShares)
	}
	if len(overview.KaryawanBaru) != 3 || overview.KaryawanBaru[0].UserID != management.UserID {
		t.Fatalf("new employee order is wrong: %+v", overview.KaryawanBaru)
	}
	if len(overview.PengajuanTerbaru) != 2 || overview.PengajuanTerbaru[0].LeaveID != "LVE-MGMT" {
		t.Fatalf("latest leave order is wrong: %+v", overview.PengajuanTerbaru)
	}

	defaultOverview, err := fixture.service.BuildHROverview(ctx, "", "")
	if err != nil {
		t.Fatalf("default overview: %v", err)
	}
	if defaultOverview.From != "2026-08-04" || defaultOverview.To != "2026-08-10" || len(defaultOverview.Series) != 7 {
		t.Fatalf("default range is wrong: %+v", defaultOverview)
	}
}

func TestLeaveConcurrentCreateHasUniqueSequences(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	const workers = 16
	ids := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			leave, err := fixture.service.Create(ctx, fixture.user, annualLeaveInput())
			if err != nil {
				errs <- err
				return
			}
			ids <- leave.LeaveID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent create: %v", err)
	}
	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID %s", id)
		}
		seen[id] = true
	}
	if len(seen) != workers {
		t.Fatalf("created %d unique IDs, want %d", len(seen), workers)
	}
}

func TestLeaveConcurrentDecisionOnlyOneWins(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	leave, err := fixture.service.Create(ctx, fixture.user, annualLeaveInput())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i, decision := range []LeaveDecision{LeaveDecisionApprove, LeaveDecisionReject} {
		wg.Add(1)
		go func(index int, decision LeaveDecision) {
			defer wg.Done()
			note := ""
			if decision == LeaveDecisionReject {
				note = fmt.Sprintf("Ditolak %d", index)
			}
			_, err := fixture.service.Decide(ctx, fixture.hr, leave.LeaveID, decision, note)
			results <- err
		}(i, decision)
	}
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected decision error: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d, want one each", success, conflicts)
	}
}
