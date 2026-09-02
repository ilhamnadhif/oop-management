package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// seedOpenAttendance files a day that was clocked in and never clocked out.
func seedOpenAttendance(t *testing.T, store *repository.TestRepository, user *model.User, date string) {
	t.Helper()
	location := time.FixedZone("WIB", 7*60*60)
	day, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Fatalf("seed attendance date: %v", err)
	}
	attendance := &model.Attendance{
		AbsensiID: "ABS-OPEN-" + user.UserID + "-" + date,
		UserID:    user.UserID, NRP: user.NRP, NamaLengkap: user.NamaLengkap, Jabatan: user.Jabatan,
		TanggalAbsensi: date,
		ClockInAt:      time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, location),
		StatusAbsensi:  model.AttendanceBelumClockOut,
	}
	if err := store.CreateAttendance(context.Background(), attendance); err != nil {
		t.Fatalf("seed open attendance %s: %v", date, err)
	}
}

// A shift nobody closed is still a day present. The count sits beside the
// attendance figures rather than changing them: it explains a low hours total,
// it does not correct it.
func TestMonthlyAbsensiCountsDaysLeftWithoutAClockOut(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	seedMonthlyAttendance(t, store, user, "2026-08-03")
	seedOpenAttendance(t, store, user, "2026-08-04")
	seedOpenAttendance(t, store, user, "2026-08-05")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	if row.BelumClockOut != 2 {
		t.Fatalf("belum clock out = %d, want 2", row.BelumClockOut)
	}
	if row.Hadir != 3 {
		t.Fatalf("hadir = %d, want all three days counted present", row.Hadir)
	}
}

// One person may have two rows in a day. The column counts days, not rows: a
// day is chased up once.
func TestMonthlyAbsensiCountsOpenDaysNotOpenRows(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	seedOpenAttendance(t, store, user, "2026-08-04")
	// A second open row on the same day, filed under its own id.
	location := time.FixedZone("WIB", 7*60*60)
	if err := store.CreateAttendance(context.Background(), &model.Attendance{
		AbsensiID: "ABS-OPEN-2", UserID: user.UserID, NRP: user.NRP,
		NamaLengkap: user.NamaLengkap, Jabatan: user.Jabatan,
		TanggalAbsensi: "2026-08-04",
		ClockInAt:      time.Date(2026, 8, 4, 13, 0, 0, 0, location),
		StatusAbsensi:  model.AttendanceBelumClockOut,
	}); err != nil {
		t.Fatalf("seed second open row: %v", err)
	}

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := report.Rows[0].BelumClockOut; got != 1 {
		t.Fatalf("belum clock out = %d, want the day counted once", got)
	}
}

// A day that was closed does not appear, however it was closed.
func TestMonthlyAbsensiIgnoresClosedDays(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	seedMonthlyAttendance(t, store, user, "2026-08-03")
	seedMonthlyAttendance(t, store, user, "2026-08-04")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := report.Rows[0].BelumClockOut; got != 0 {
		t.Fatalf("belum clock out = %d, want none", got)
	}
}

// A day outside the month cannot be chased up on this month's page.
func TestMonthlyAbsensiIgnoresOpenDaysOutsideTheMonth(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store)
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	seedOpenAttendance(t, store, user, "2026-07-31")
	seedOpenAttendance(t, store, user, "2026-08-04")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := report.Rows[0].BelumClockOut; got != 1 {
		t.Fatalf("belum clock out = %d, want only the day inside the month", got)
	}
}

// A month still running only owes the days that have happened. Counting the
// rest as absent read a perfect attendance record as a handful of percent,
// because the whole month sat in the denominator on the second of the month.
func TestMonthlyAbsensiOnlyOwesTheDaysThatHavePassed(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store) // now: 2026-08-10
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	// Present every day of the month so far, today included.
	for day := 1; day <= 10; day++ {
		seedMonthlyAttendance(t, store, user, fmt.Sprintf("2026-08-%02d", day))
	}

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	if row.Hadir != 10 {
		t.Fatalf("hadir = %d, want 10", row.Hadir)
	}
	if row.TidakAbsen != 0 {
		t.Fatalf("tidak absen = %d, want none: the rest of the month has not happened", row.TidakAbsen)
	}
	if row.Persentase != 100 {
		t.Fatalf("persentase = %v, want 100 for somebody present every day so far", row.Persentase)
	}
	// The matrix still spans the month, so the export prints a full calendar.
	if report.Days != 31 || len(row.Hari) != 31 {
		t.Fatalf("days = %d, cells = %d, want the whole month", report.Days, len(row.Hari))
	}
}

// A month already over owes every one of its days, whatever today is.
func TestMonthlyAbsensiOwesEveryDayOfAFinishedMonth(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store) // now: 2026-08-10
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	for day := 1; day <= 10; day++ {
		seedMonthlyAttendance(t, store, user, fmt.Sprintf("2026-07-%02d", day))
	}

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-07", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	if row.TidakAbsen != 21 {
		t.Fatalf("tidak absen = %d, want the remaining 21 days of July", row.TidakAbsen)
	}
	if row.Persentase != 32.26 {
		t.Fatalf("persentase = %v, want 32.26", row.Persentase)
	}
}

// Leave is attendance for this figure: somebody who was present or accounted
// for every day is at a hundred, whichever of the two each day was.
func TestMonthlyAbsensiCountsApprovedLeaveAsAttendance(t *testing.T) {
	store := repository.NewTestRepository()
	service := newMonthlyAttendanceService(store) // now: 2026-08-10
	user := leaveUser("usr_survey", "2001", "Budi Hartono", "Surveyor", "2026-01-02")
	createFixtureUser(t, store, user)

	for day := 1; day <= 5; day++ {
		seedMonthlyAttendance(t, store, user, fmt.Sprintf("2026-08-%02d", day))
	}
	seedApprovedLeave(t, store, "LVE-I", user, model.LeaveJenisIzin, "2026-08-06", "2026-08-07")
	seedApprovedLeave(t, store, "LVE-S", user, model.LeaveJenisCutiSakit, "2026-08-08", "2026-08-08")
	seedApprovedLeave(t, store, "LVE-C", user, model.LeaveJenisCutiTahunan, "2026-08-09", "2026-08-10")

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	row := report.Rows[0]
	if row.Hadir != 5 || row.Izin != 2 || row.Sakit != 1 || row.Cuti != 2 {
		t.Fatalf("hadir=%d izin=%d sakit=%d cuti=%d, want 5/2/1/2",
			row.Hadir, row.Izin, row.Sakit, row.Cuti)
	}
	if row.Persentase != 100 {
		t.Fatalf("persentase = %v, want 100: approved leave does not lower it", row.Persentase)
	}
}
