package service

import (
	"fmt"
	"strings"
	"time"
)

// Schedule is the working day every attendance record is judged against. It is
// derived at read time rather than stamped onto a row: a rule that changes must
// change the reading of the whole history at once, not leave old rows judged by
// a rule nobody remembers.
type Schedule struct {
	// Start and End are minutes from midnight, so a comparison is arithmetic
	// rather than date juggling across time zones.
	Start int
	End   int
	// Tolerance is how late someone may clock in before it counts as late. It
	// forgives the walk from the gate, not a habit.
	Tolerance time.Duration
}

// DefaultSchedule is a nine-to-five with a quarter of an hour of grace.
func DefaultSchedule() Schedule {
	return Schedule{Start: 9 * 60, End: 17 * 60, Tolerance: 15 * time.Minute}
}

// NewSchedule reads the working day from "HH:MM" strings.
func NewSchedule(start, end string, toleranceMinutes int) (Schedule, error) {
	startMinutes, err := parseClock(start)
	if err != nil {
		return Schedule{}, fmt.Errorf("jam masuk: %w", err)
	}
	endMinutes, err := parseClock(end)
	if err != nil {
		return Schedule{}, fmt.Errorf("jam pulang: %w", err)
	}
	if endMinutes <= startMinutes {
		return Schedule{}, fmt.Errorf("jam pulang harus setelah jam masuk")
	}
	if toleranceMinutes < 0 {
		return Schedule{}, fmt.Errorf("toleransi keterlambatan tidak boleh minus")
	}
	return Schedule{
		Start:     startMinutes,
		End:       endMinutes,
		Tolerance: time.Duration(toleranceMinutes) * time.Minute,
	}, nil
}

func parseClock(value string) (int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("format jam harus HH:MM")
	}
	return parsed.Hour()*60 + parsed.Minute(), nil
}

// StartLabel and EndLabel print the working day for the pages that state it.
func (s Schedule) StartLabel() string { return clockLabel(s.Start) }
func (s Schedule) EndLabel() string   { return clockLabel(s.End) }

// ToleranceMinutes is the grace period in whole minutes.
func (s Schedule) ToleranceMinutes() int { return int(s.Tolerance / time.Minute) }

func clockLabel(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// minutesOfDay is how far into its own day a moment falls.
func minutesOfDay(value time.Time) int {
	return value.Hour()*60 + value.Minute()
}

// AttendanceRule is what one day amounts to against the schedule. Several can
// be true at once: someone can arrive late and still work past closing.
type AttendanceRule struct {
	EarlyIn         bool
	Late            bool
	EarlyLeave      bool
	Overtime        bool
	LateMinutes     int
	EarlyOutMinutes int
	OvertimeMinutes int
}

// OnTime is a day with nothing to explain: arrived within the grace period and
// left no earlier than closing.
func (r AttendanceRule) OnTime(closed bool) bool {
	return !r.Late && (!closed || !r.EarlyLeave)
}

// Judge measures one day against the schedule. clockOut may be nil, which is a
// day still open rather than a day left early.
func (s Schedule) Judge(clockIn time.Time, clockOut *time.Time) AttendanceRule {
	rule := AttendanceRule{}
	if clockIn.IsZero() {
		return rule
	}

	arrival := minutesOfDay(clockIn)
	switch {
	case arrival < s.Start:
		rule.EarlyIn = true
	case arrival > s.Start+s.ToleranceMinutes():
		rule.Late = true
		// Counted from the start of the day, not from the end of the grace
		// period: the tolerance decides whether it counts, not how late it was.
		rule.LateMinutes = arrival - s.Start
	}

	if clockOut == nil || clockOut.IsZero() {
		return rule
	}
	departure := minutesOfDay(*clockOut)
	// A clock-out past midnight reads as a smaller number than the start of the
	// day; count it as time worked into the next day rather than as leaving
	// eighteen hours early.
	if departure < arrival {
		departure += 24 * 60
	}
	switch {
	case departure < s.End:
		rule.EarlyLeave = true
		rule.EarlyOutMinutes = s.End - departure
	case departure > s.End:
		rule.Overtime = true
		rule.OvertimeMinutes = departure - s.End
	}
	return rule
}
