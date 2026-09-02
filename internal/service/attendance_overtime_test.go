package service

import (
	"context"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// overtimeSchedule is a plain eight-to-five day, which is what overtime is
// measured against.
func overtimeSchedule() Schedule {
	return Schedule{Start: 8 * 60, End: 17 * 60, Tolerance: 15 * time.Minute}
}

// seedShift files one attendance day. A nil clockOut is a day still open.
func seedShift(t *testing.T, store *repository.TestRepository, user *model.User, id, date string, in time.Time, out *time.Time) {
	t.Helper()
	attendance := &model.Attendance{
		AbsensiID: id, UserID: user.UserID, NRP: user.NRP,
		NamaLengkap: user.NamaLengkap, Jabatan: user.Jabatan,
		TanggalAbsensi: date, ClockInAt: in,
		StatusAbsensi: model.AttendanceBelumClockOut,
	}
	if out != nil {
		minutes := int(out.Sub(in).Minutes())
		attendance.ClockOutAt = out
		attendance.DurasiMenit = &minutes
		attendance.StatusAbsensi = model.AttendanceSelesai
	}
	if err := store.CreateAttendance(context.Background(), attendance); err != nil {
		t.Fatalf("seed shift %s: %v", id, err)
	}
}

func overtimeUser() *model.User {
	return &model.User{
		UserID: "usr_ot", NRP: "NRP800", NamaLengkap: "Budi Hartono", Jabatan: "Surveyor",
		Email: "usr_ot@example.test", TanggalGabung: "2026-01-02", StatusPengguna: model.StatusAktif,
	}
}

// Staying past the end of the working day is overtime, and the dashboard says
// how much rather than only that it happened.
func TestSummaryReportsOvertimePerDayAndForTheMonth(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 10, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	// Out at 18:30, an hour and a half past closing.
	in := time.Date(2026, 8, 3, 8, 0, 0, 0, location)
	out := time.Date(2026, 8, 3, 18, 30, 0, 0, location)
	seedShift(t, store, user, "ABS-1", "2026-08-03", in, &out)
	// Out on time: nothing to add.
	in2 := time.Date(2026, 8, 4, 8, 0, 0, 0, location)
	out2 := time.Date(2026, 8, 4, 17, 0, 0, 0, location)
	seedShift(t, store, user, "ABS-2", "2026-08-04", in2, &out2)

	summary, err := service.Summary(context.Background(), user.UserID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.LemburMenit != 90 {
		t.Fatalf("lembur bulan = %d menit, want 90", summary.LemburMenit)
	}
	var overtimeDay AttendanceDay
	for _, day := range summary.Riwayat {
		if day.Tanggal == "2026-08-03" {
			overtimeDay = day
		}
	}
	if overtimeDay.LemburMenit != 90 || overtimeDay.Lembur != "1j 30m" {
		t.Fatalf("day = %d menit / %q, want 90 and \"1j 30m\"", overtimeDay.LemburMenit, overtimeDay.Lembur)
	}
	var note string
	for _, line := range overtimeDay.Catatan {
		if len(line) >= 6 && line[:6] == "Lembur" {
			note = line
		}
	}
	if note != "Lembur 1j 30m" {
		t.Fatalf("catatan = %v, want it to name the overtime", overtimeDay.Catatan)
	}
}

// A day nobody closed has no overtime: there is no departure to measure, and
// guessing one would pay for hours nobody recorded.
func TestSummaryCountsNoOvertimeForAnOpenDay(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 10, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	seedShift(t, store, user, "ABS-OPEN", "2026-08-03",
		time.Date(2026, 8, 3, 8, 0, 0, 0, location), nil)

	summary, err := service.Summary(context.Background(), user.UserID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.LemburMenit != 0 {
		t.Fatalf("lembur = %d, want none for a day left open", summary.LemburMenit)
	}
	if summary.Riwayat[0].Lembur != "-" {
		t.Fatalf("day lembur = %q, want a dash", summary.Riwayat[0].Lembur)
	}
}

// A shift that runs past midnight is worked into the next day, not eighteen
// hours short of the last one.
func TestSummaryCountsOvertimePastMidnight(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 10, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	in := time.Date(2026, 8, 3, 8, 0, 0, 0, location)
	out := time.Date(2026, 8, 4, 1, 0, 0, 0, location)
	seedShift(t, store, user, "ABS-NIGHT", "2026-08-03", in, &out)

	summary, err := service.Summary(context.Background(), user.UserID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	// 01:00 the next day is 25:00, eight hours past a five o'clock finish.
	if summary.LemburMenit != 480 {
		t.Fatalf("lembur = %d menit, want 480", summary.LemburMenit)
	}
}

// The monthly report totals each employee's overtime, so a month can be read
// without opening every day.
func TestMonthlyAbsensiTotalsOvertime(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 31, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	for _, shift := range []struct {
		id, date string
		outHour  int
	}{
		{"ABS-1", "2026-08-03", 18},
		{"ABS-2", "2026-08-04", 17},
		{"ABS-3", "2026-08-05", 19},
	} {
		day, _ := time.Parse("2006-01-02", shift.date)
		in := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, location)
		out := time.Date(day.Year(), day.Month(), day.Day(), shift.outHour, 0, 0, 0, location)
		seedShift(t, store, user, shift.id, shift.date, in, &out)
	}

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := report.Rows[0].LemburMenit; got != 180 {
		t.Fatalf("lembur = %d menit, want 180", got)
	}
}

// Two shifts in a day are one working day. Judging each on its own would count
// the same evening twice.
func TestMonthlyAbsensiJudgesTheWholeDayNotEachShift(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 31, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	morningIn := time.Date(2026, 8, 3, 8, 0, 0, 0, location)
	morningOut := time.Date(2026, 8, 3, 18, 0, 0, 0, location)
	seedShift(t, store, user, "ABS-AM", "2026-08-03", morningIn, &morningOut)
	eveningIn := time.Date(2026, 8, 3, 18, 30, 0, 0, location)
	eveningOut := time.Date(2026, 8, 3, 19, 0, 0, 0, location)
	seedShift(t, store, user, "ABS-PM", "2026-08-03", eveningIn, &eveningOut)

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Eight in the morning to seven at night is two hours past closing, once.
	if got := report.Rows[0].LemburMenit; got != 120 {
		t.Fatalf("lembur = %d menit, want 120 counted once for the day", got)
	}
}

// A day left open contributes nothing to the month's overtime either.
func TestMonthlyAbsensiIgnoresOvertimeOnAnOpenDay(t *testing.T) {
	store := repository.NewTestRepository()
	location := time.FixedZone("WIB", 7*60*60)
	user := overtimeUser()
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	service := NewAttendanceService(store, location,
		func() time.Time { return time.Date(2026, 8, 31, 20, 0, 0, 0, location) }).
		WithSchedule(overtimeSchedule())

	seedShift(t, store, user, "ABS-OPEN", "2026-08-03",
		time.Date(2026, 8, 3, 8, 0, 0, 0, location), nil)

	report, err := service.BuildMonthlyAbsensi(context.Background(), "2026-08", "")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := report.Rows[0].LemburMenit; got != 0 {
		t.Fatalf("lembur = %d, want none for a day left open", got)
	}
}
