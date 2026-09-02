package service

import (
	"context"
	"testing"
	"time"

	"opp-management/internal/model"
)

// Somebody who clocked in and never clocked out is present, not absent. HR
// wants the name so it can be chased up, but the day's attendance figures must
// not change: the person was at work.
func TestHROverviewNamesWhoHasNotClockedOut(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	pulang := leaveUser("usr_pulang", "9201", "Ani Lestari", "Logistik", "2025-01-01")
	lupa := leaveUser("usr_lupa", "9202", "Cahyo Nugroho", "Security", "2025-01-01")
	createFixtureUser(t, fixture.store, pulang)
	createFixtureUser(t, fixture.store, lupa)

	out := time.Date(2026, 8, 10, 17, 0, 0, 0, fixture.location)
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-OUT", UserID: pulang.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 7, 0, 0, 0, fixture.location), ClockOutAt: &out,
	}); err != nil {
		t.Fatalf("seed closed shift: %v", err)
	}
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: lupa.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 7, 5, 0, 0, fixture.location),
	}); err != nil {
		t.Fatalf("seed open shift: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}

	if len(overview.BelumClockOutNama) != 1 || overview.BelumClockOutNama[0].NamaLengkap != "Cahyo Nugroho" {
		t.Fatalf("open shift list is wrong: %+v", overview.BelumClockOutNama)
	}
	// Both clocked in, so both count as present whatever they did at the end of
	// the day.
	if overview.HadirHariAkhir != 2 {
		t.Fatalf("hadir = %d, want both people who clocked in", overview.HadirHariAkhir)
	}
	// The person who never turned up belongs to the other list and only there.
	for _, person := range overview.BelumClockOutNama {
		for _, absent := range overview.BelumAbsenNama {
			if person.UserID == absent.UserID {
				t.Fatalf("%s is in both lists", person.NamaLengkap)
			}
		}
	}
	if len(overview.BelumAbsenNama) == 0 {
		t.Fatal("the HR fixture never clocked in and is missing from the absent list")
	}
}

// A day everybody closed leaves the list empty rather than holding whoever was
// last.
func TestHROverviewLeavesTheClockOutListEmptyWhenEverybodyClosed(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	out := time.Date(2026, 8, 10, 17, 0, 0, 0, fixture.location)
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-OUT", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 7, 0, 0, 0, fixture.location), ClockOutAt: &out,
	}); err != nil {
		t.Fatalf("seed closed shift: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if len(overview.BelumClockOutNama) != 0 {
		t.Fatalf("open shift list = %+v, want none", overview.BelumClockOutNama)
	}
}

// One person may have more than one row in a day. One row still open is enough
// to be chased up: the point is the shift nobody closed.
func TestHROverviewFlagsAnyUnclosedRowInTheDay(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	out := time.Date(2026, 8, 10, 12, 0, 0, 0, fixture.location)
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-1", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 7, 0, 0, 0, fixture.location), ClockOutAt: &out,
	}); err != nil {
		t.Fatalf("seed closed shift: %v", err)
	}
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-2", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 13, 0, 0, 0, fixture.location),
	}); err != nil {
		t.Fatalf("seed second shift: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if len(overview.BelumClockOutNama) != 1 || overview.BelumClockOutNama[0].UserID != fixture.user.UserID {
		t.Fatalf("a second open row went unnoticed: %+v", overview.BelumClockOutNama)
	}
}

// The list is sorted the same way the others are, so a reload does not
// reshuffle it and read as if the people had changed.
func TestHROverviewSortsTheClockOutList(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	for _, person := range []struct{ id, nrp, nama string }{
		{"usr_z", "9301", "Zulfikar Rahman"},
		{"usr_a", "9302", "Andi Pratama"},
	} {
		user := leaveUser(person.id, person.nrp, person.nama, "Logistik", "2025-01-01")
		createFixtureUser(t, fixture.store, user)
		if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
			AbsensiID: "ABS-" + person.id, UserID: user.UserID, TanggalAbsensi: "2026-08-10",
			ClockInAt: time.Date(2026, 8, 10, 7, 0, 0, 0, fixture.location),
		}); err != nil {
			t.Fatalf("seed open shift: %v", err)
		}
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if len(overview.BelumClockOutNama) != 2 ||
		overview.BelumClockOutNama[0].NamaLengkap != "Andi Pratama" {
		t.Fatalf("open shift list is not sorted by name: %+v", overview.BelumClockOutNama)
	}
}

// HR wants to see who stayed on, and for how long. The overview reports the
// last day of the range, the same day its other lists report.
func TestHROverviewNamesWhoWorkedOvertime(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	lembur := leaveUser("usr_lembur", "9401", "Ani Lestari", "Logistik", "2025-01-01")
	pulang := leaveUser("usr_pulang", "9402", "Cahyo Nugroho", "Security", "2025-01-01")
	createFixtureUser(t, fixture.store, lembur)
	createFixtureUser(t, fixture.store, pulang)
	fixture.service.WithSchedule(Schedule{Start: 8 * 60, End: 17 * 60, Tolerance: 15 * time.Minute})

	late := time.Date(2026, 8, 10, 19, 0, 0, 0, fixture.location)
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-LEMBUR", UserID: lembur.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 8, 0, 0, 0, fixture.location), ClockOutAt: &late,
	}); err != nil {
		t.Fatalf("seed overtime: %v", err)
	}
	onTime := time.Date(2026, 8, 10, 17, 0, 0, 0, fixture.location)
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-ONTIME", UserID: pulang.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 8, 0, 0, 0, fixture.location), ClockOutAt: &onTime,
	}); err != nil {
		t.Fatalf("seed on-time day: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if len(overview.LemburNama) != 1 || overview.LemburNama[0].NamaLengkap != "Ani Lestari" {
		t.Fatalf("overtime list is wrong: %+v", overview.LemburNama)
	}
	if overview.LemburNama[0].Keterangan != "2j" {
		t.Fatalf("keterangan = %q, want the hours stayed on", overview.LemburNama[0].Keterangan)
	}
	if overview.LemburMenitHariAkhir != 120 {
		t.Fatalf("total lembur = %d menit, want 120", overview.LemburMenitHariAkhir)
	}
}

// A shift nobody closed has no departure to measure, so it earns no overtime
// however late the overview is read.
func TestHROverviewCountsNoOvertimeForAnOpenShift(t *testing.T) {
	fixture := newLeaveFixture(t)
	ctx := context.Background()
	fixture.service.WithSchedule(Schedule{Start: 8 * 60, End: 17 * 60, Tolerance: 15 * time.Minute})
	if err := fixture.store.CreateAttendance(ctx, &model.Attendance{
		AbsensiID: "ABS-OPEN", UserID: fixture.user.UserID, TanggalAbsensi: "2026-08-10",
		ClockInAt: time.Date(2026, 8, 10, 8, 0, 0, 0, fixture.location),
	}); err != nil {
		t.Fatalf("seed open shift: %v", err)
	}

	overview, err := fixture.service.BuildHROverview(ctx, "2026-08-10", "2026-08-10")
	if err != nil {
		t.Fatalf("build overview: %v", err)
	}
	if len(overview.LemburNama) != 0 || overview.LemburMenitHariAkhir != 0 {
		t.Fatalf("an open shift earned overtime: %+v", overview.LemburNama)
	}
}
