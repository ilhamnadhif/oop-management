package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func seedMonthlyAttendance(t *testing.T, store *repository.TestRepository, user *model.User, date string) {
	t.Helper()
	location := time.FixedZone("WIB", 7*60*60)
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("seed attendance date: %v", err)
	}
	in := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, location)
	out := in.Add(9 * time.Hour)
	minutes := 540
	attendance := &model.Attendance{
		AbsensiID: "ABS-" + user.UserID + "-" + date,
		UserID:    user.UserID, NRP: user.NRP, NamaLengkap: user.NamaLengkap, Jabatan: user.Jabatan,
		TanggalAbsensi: date, ClockInAt: in, ClockOutAt: &out, DurasiMenit: &minutes,
		StatusAbsensi: model.AttendanceSelesai,
	}
	if err := store.CreateAttendance(context.Background(), attendance); err != nil {
		t.Fatalf("seed attendance %s: %v", date, err)
	}
}

func seedApprovedLeave(t *testing.T, store *repository.TestRepository, leaveID string, user *model.User, jenis, from, to string) {
	t.Helper()
	location := time.FixedZone("WIB", 7*60*60)
	leave := &model.Leave{
		LeaveID: leaveID, UserID: user.UserID, NRP: user.NRP, NamaLengkap: user.NamaLengkap, Jabatan: user.Jabatan,
		JenisLeave: jenis, TanggalMulai: from, TanggalSelesai: to, JumlahHari: 1,
		Status: model.LeaveStatusDisetujui, CreatedAt: time.Date(2026, 8, 1, 9, 0, 0, 0, location),
	}
	if err := store.CreateLeave(context.Background(), leave); err != nil {
		t.Fatalf("seed leave %s: %v", leaveID, err)
	}
}

func newMonthlyAttendanceService(store repository.Store) *AttendanceService {
	return newMonthlyAttendanceServiceOn(store, time.Date(2026, 8, 10, 12, 0, 0, 0, time.FixedZone("WIB", 7*60*60)))
}

// newMonthlyAttendanceServiceOn fixes the day the report is read on. It matters
// because a month still running only owes the days that have passed, so a
// fixture whose attendance falls after "today" is recording days nobody has
// lived through.
func newMonthlyAttendanceServiceOn(store repository.Store, now time.Time) *AttendanceService {
	location := time.FixedZone("WIB", 7*60*60)
	return NewAttendanceService(store, location, func() time.Time { return now })
}

// afterAugust reads August once it is over, which is what the tests about the
// shape of a whole month want. It is the first of September rather than the
// last of August on purpose: the last day of a month is still running, and a
// day still running owes nobody an absence.
func afterAugust() time.Time {
	return time.Date(2026, 9, 1, 8, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
}

func TestBuildMonthlyAbsensiCountsDaysWeeksAndLeaves(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceServiceOn(store, afterAugust())

	surveyor := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	produksi := leaveUser("usr_prod", "2002", "Citra Ayu", "Produksi", "2026-01-02")
	createFixtureUser(t, store, surveyor)
	createFixtureUser(t, store, produksi)

	// Budi works the whole first week (Mon-Fri, 3-7 Agustus 2026).
	for _, date := range []string{"2026-08-03", "2026-08-04", "2026-08-05", "2026-08-06", "2026-08-07"} {
		seedMonthlyAttendance(t, store, surveyor, date)
	}

	// Citra has no attendance, only approved leaves across the month.
	seedApprovedLeave(t, store, "LVE-S", produksi, model.LeaveJenisCutiSakit, "2026-08-10", "2026-08-12")
	seedApprovedLeave(t, store, "LVE-I", produksi, model.LeaveJenisIzin, "2026-08-17", "2026-08-17")
	seedApprovedLeave(t, store, "LVE-C", produksi, model.LeaveJenisCutiTahunan, "2026-08-24", "2026-08-25")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if report.Days != 31 {
		t.Fatalf("days = %d, want 31", report.Days)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(report.Rows))
	}

	budi := report.Rows[0]
	if budi.Nama != "Budi Hartono" || budi.Hadir != 5 || budi.M1 != 5 || budi.M2 != 0 || budi.M4 != 0 {
		t.Fatalf("budi nama=%q hadir=%d M1=%d M2=%d M4=%d, want 5/5/0/0",
			budi.Nama, budi.Hadir, budi.M1, budi.M2, budi.M4)
	}
	// Every day is a legal work day, so Budi owes all 31: five are present and
	// the rest, weekends included, are not.
	if budi.TidakAbsen != 26 || budi.Persentase != 16.13 {
		t.Fatalf("budi tidak absen=%d persentase=%v, want 26 and 16.13",
			budi.TidakAbsen, budi.Persentase)
	}
	for day := 3; day <= 7; day++ {
		if budi.Hari[day-1] != absensiHadirMark {
			t.Fatalf("budi day %d = %q, want the hadir check", day, budi.Hari[day-1])
		}
	}
	// A weekend without attendance is "tidak absen": the cell stays blank.
	if budi.Hari[1] != "" {
		t.Fatalf("budi day 2 (a Sunday) should be blank, got %q", budi.Hari[1])
	}

	citra := report.Rows[1]
	if citra.Hadir != 0 || citra.Sakit != 3 || citra.Izin != 1 || citra.Cuti != 2 {
		t.Fatalf("citra hadir=%d sakit=%d izin=%d cuti=%d, want 0/3/1/2",
			citra.Hadir, citra.Sakit, citra.Izin, citra.Cuti)
	}
	// Kehadiran counts leave too, so Citra is 6 of 31 days.
	if citra.TidakAbsen != 25 || citra.Persentase != 19.35 {
		t.Fatalf("citra tidak absen=%d persentase=%v, want 25 and 19.35",
			citra.TidakAbsen, citra.Persentase)
	}
	if citra.Hari[9] != "S" || citra.Hari[10] != "S" || citra.Hari[16] != "I" || citra.Hari[23] != "C" || citra.Hari[24] != "C" {
		t.Fatalf("citra day marks are wrong: %v", citra.Hari[8:25])
	}
}

// Every day of an approved leave counts, so a request that spans Saturday and
// Sunday covers those days too.
func TestBuildMonthlyAbsensiCountsLeaveAcrossTheWeekend(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	user := leaveUser("usr_weekend", "2003", "Dedi Kurniawan", "SPV", "2026-01-02")
	createFixtureUser(t, store, user)

	// Friday 7 to Monday 10 August 2026: four days, Saturday and Sunday among
	// them.
	seedApprovedLeave(t, store, "LVE-W", user, model.LeaveJenisCutiTahunan, "2026-08-07", "2026-08-10")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	if row.Cuti != 4 {
		t.Fatalf("cuti = %d, want 4 (the weekend included)", row.Cuti)
	}
	if row.Hari[6] != "C" || row.Hari[7] != "C" || row.Hari[8] != "C" || row.Hari[9] != "C" {
		t.Fatalf("weekend leave marks are wrong: %v", row.Hari[6:10])
	}
}

// Days before an employee joined are not owed: they sit in neither the tidak
// absen count nor the percentage.
func TestBuildMonthlyAbsensiCountsOnlyActiveDays(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceServiceOn(store, afterAugust())
	user := leaveUser("usr_late", "2004", "Eka Putri", "Produksi", "2026-08-20")
	createFixtureUser(t, store, user)

	seedMonthlyAttendance(t, store, user, "2026-08-20")
	seedMonthlyAttendance(t, store, user, "2026-08-21")
	seedMonthlyAttendance(t, store, user, "2026-08-22")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	// Active from the 20th: 12 days. Three present, nine not.
	if row.Hadir != 3 || row.TidakAbsen != 9 {
		t.Fatalf("hadir=%d tidak absen=%d, want 3 and 9", row.Hadir, row.TidakAbsen)
	}
	if row.Persentase != 25 {
		t.Fatalf("persentase = %v, want 25 (3/12)", row.Persentase)
	}
	if row.M3 != 2 || row.M4 != 1 {
		t.Fatalf("M3=%d M4=%d, want 2 and 1", row.M3, row.M4)
	}
	// The days before joining carry no marks and no counts.
	for i := 0; i < 19; i++ {
		if row.Hari[i] != "" {
			t.Fatalf("pre-join day %d = %q, want blank", i+1, row.Hari[i])
		}
	}
}

func TestBuildMonthlyAbsensiFiltersByJabatan(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	for _, user := range []*model.User{
		leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02"),
		leaveUser("usr_prod", "2002", "Citra Ayu", "Produksi", "2026-01-02"),
	} {
		createFixtureUser(t, store, user)
	}

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "Surveyor")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(report.Rows) != 1 || report.Rows[0].Nama != "Budi Hartono" {
		t.Fatalf("rows = %+v, want only the surveyor", report.Rows)
	}
}

func TestBuildMonthlyAbsensiRejectsAnInvalidMonth(t *testing.T) {
	service := newMonthlyAttendanceService(repository.NewTestRepository())
	for _, month := range []string{"2026-13", "agustus", "08/2026"} {
		if _, err := service.BuildMonthlyAbsensi(context.Background(), month, ""); !errors.Is(err, ErrValidation) {
			t.Fatalf("month %q: err = %v, want ErrValidation", month, err)
		}
	}
}
