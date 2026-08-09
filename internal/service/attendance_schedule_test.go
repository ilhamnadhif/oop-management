package service

import (
	"testing"
	"time"
)

func at(hour, minute int) time.Time {
	return time.Date(2026, 8, 7, hour, minute, 0, 0, time.UTC)
}

func closedAt(hour, minute int) *time.Time {
	value := at(hour, minute)
	return &value
}

// The working day is 09:00 to 17:00 with fifteen minutes of grace.
func TestScheduleJudgesTheWorkingDay(t *testing.T) {
	schedule := DefaultSchedule()

	cases := map[string]struct {
		in   time.Time
		out  *time.Time
		want AttendanceRule
	}{
		"early in, on time out": {
			at(8, 30), closedAt(17, 0),
			AttendanceRule{EarlyIn: true},
		},
		"inside the grace period is not late": {
			at(9, 15), closedAt(17, 0),
			AttendanceRule{},
		},
		"a minute past the grace period is late from nine": {
			at(9, 16), closedAt(17, 0),
			AttendanceRule{Late: true, LateMinutes: 16},
		},
		"leaving before five is early leave": {
			at(9, 0), closedAt(16, 30),
			AttendanceRule{EarlyLeave: true, EarlyOutMinutes: 30},
		},
		"staying past five is overtime": {
			at(9, 0), closedAt(18, 45),
			AttendanceRule{Overtime: true, OvertimeMinutes: 105},
		},
		"late and overtime happen on the same day": {
			at(10, 0), closedAt(19, 0),
			AttendanceRule{Late: true, LateMinutes: 60, Overtime: true, OvertimeMinutes: 120},
		},
		// A day still open is not a day left early.
		"open day is judged on arrival only": {
			at(9, 30), nil,
			AttendanceRule{Late: true, LateMinutes: 30},
		},
	}
	for name, tc := range cases {
		if got := schedule.Judge(tc.in, tc.out); got != tc.want {
			t.Fatalf("%s: got %+v, want %+v", name, got, tc.want)
		}
	}
}

// A shift that ends after midnight is time worked, not eighteen hours of early
// leave.
func TestScheduleHandlesAShiftPastMidnight(t *testing.T) {
	schedule := DefaultSchedule()
	out := time.Date(2026, 8, 8, 1, 0, 0, 0, time.UTC)

	rule := schedule.Judge(at(9, 0), &out)
	if !rule.Overtime || rule.OvertimeMinutes != 480 {
		t.Fatalf("got %+v, want 480 minutes of overtime", rule)
	}
	if rule.EarlyLeave {
		t.Fatal("a shift past midnight was read as leaving early")
	}
}

// The schedule is a company setting, so it has to be readable from configuration
// and refuse nonsense rather than silently accept it.
func TestNewScheduleReadsAndValidatesTheWorkingDay(t *testing.T) {
	schedule, err := NewSchedule("08:30", "16:00", 10)
	if err != nil {
		t.Fatalf("new schedule: %v", err)
	}
	if schedule.StartLabel() != "08:30" || schedule.EndLabel() != "16:00" || schedule.ToleranceMinutes() != 10 {
		t.Fatalf("unexpected schedule: %+v", schedule)
	}

	for name, tc := range map[string]struct {
		start, end string
		tolerance  int
	}{
		"end before start": {"17:00", "09:00", 15},
		"same time":        {"09:00", "09:00", 15},
		"malformed":        {"9 pagi", "17:00", 15},
		"negative grace":   {"09:00", "17:00", -1},
	} {
		if _, err := NewSchedule(tc.start, tc.end, tc.tolerance); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
}
