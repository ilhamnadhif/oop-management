package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// AttendanceDay is one day of someone's own attendance, formatted the way the
// dashboard prints it.
type AttendanceDay struct {
	Tanggal  string
	Label    string
	ClockIn  string
	ClockOut string
	Durasi   string
	Jam      float64
	Status   string
	// Selesai separates a finished day from one still open, which is the
	// difference between a record and a reminder.
	Selesai bool
	// Rule is how the day measured against the working hours.
	Rule AttendanceRule
	// Catatan spells the rule out in words, e.g. "Terlambat 12m".
	Catatan []string
}

// AttendanceSummary is the signed-in person's own attendance, never anyone
// else's: this dashboard answers "how am I doing", not "how is the team doing".
type AttendanceSummary struct {
	Hari        string
	SudahMasuk  bool
	SudahPulang bool
	MasukPukul  string
	PulangPukul string

	BulanLabel  string
	HariHadir   int
	TotalJam    float64
	RataJam     float64
	BelumPulang int

	// Counted over the same month, against the working day in force.
	Jadwal      Schedule
	TepatWaktu  int
	Terlambat   int
	MasukAwal   int
	PulangCepat int
	// Overtime is judged by the schedule but not reported: the dashboards do
	// not account for it, and a figure nobody acts on invites being read as
	// hours owed.
	TerlambatMenit int

	Series  []AttendanceDay
	Riwayat []AttendanceDay
}

// seriesDays is how far back the chart looks. Two working weeks is enough to
// see a pattern and short enough to read on a phone.
const seriesDays = 14

// riwayatRows is how many days the history table lists.
const riwayatRows = 10

// Summary builds one person's attendance dashboard.
func (s *AttendanceService) Summary(ctx context.Context, userID string) (*AttendanceSummary, error) {
	rows, err := s.store.ListAttendanceByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read attendance history: %w", err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TanggalAbsensi > rows[j].TanggalAbsensi })

	now := s.now().In(s.location)
	today := now.Format("2006-01-02")
	month := now.Format("2006-01")

	summary := &AttendanceSummary{
		Hari:        today,
		MasukPukul:  "--:--",
		PulangPukul: "--:--",
		BulanLabel:  indonesianMonthLabel(now),
	}

	byDate := make(map[string]AttendanceDay, len(rows))
	for _, row := range rows {
		day := s.attendanceDay(row)
		if _, seen := byDate[day.Tanggal]; !seen {
			byDate[day.Tanggal] = day
		}

		if day.Tanggal == today {
			summary.SudahMasuk = true
			summary.MasukPukul = day.ClockIn
			if day.Selesai {
				summary.SudahPulang = true
				summary.PulangPukul = day.ClockOut
			}
		}

		if strings.HasPrefix(day.Tanggal, month) {
			summary.HariHadir++
			summary.TotalJam += day.Jam
			if !day.Selesai {
				summary.BelumPulang++
			}
			if day.Rule.EarlyIn {
				summary.MasukAwal++
			}
			if day.Rule.Late {
				summary.Terlambat++
				summary.TerlambatMenit += day.Rule.LateMinutes
			}
			if day.Rule.EarlyLeave {
				summary.PulangCepat++
			}
			if day.Rule.OnTime(day.Selesai) {
				summary.TepatWaktu++
			}
		}
		if len(summary.Riwayat) < riwayatRows {
			summary.Riwayat = append(summary.Riwayat, day)
		}
	}
	summary.Jadwal = s.schedule
	summary.TotalJam = round2(summary.TotalJam)
	if summary.HariHadir > 0 {
		summary.RataJam = round2(summary.TotalJam / float64(summary.HariHadir))
	}

	// The chart runs day by day, including the days with no record: a gap is
	// itself the reading, and skipping it would draw an unbroken run of work.
	summary.Series = make([]AttendanceDay, 0, seriesDays)
	for offset := seriesDays - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		key := date.Format("2006-01-02")
		if day, ok := byDate[key]; ok {
			day.Label = date.Format("2 Jan")
			summary.Series = append(summary.Series, day)
			continue
		}
		summary.Series = append(summary.Series, AttendanceDay{
			Tanggal: key, Label: date.Format("2 Jan"),
			ClockIn: "--:--", ClockOut: "--:--", Durasi: "-",
		})
	}
	return summary, nil
}

func (s *AttendanceService) attendanceDay(row model.Attendance) AttendanceDay {
	day := AttendanceDay{
		Tanggal:  row.TanggalAbsensi,
		Label:    row.TanggalAbsensi,
		ClockIn:  "--:--",
		ClockOut: "--:--",
		Durasi:   "-",
		Status:   row.StatusAbsensi,
	}
	if !row.ClockInAt.IsZero() {
		day.ClockIn = row.ClockInAt.In(s.location).Format("15:04")
	}
	if row.ClockOutAt != nil && !row.ClockOutAt.IsZero() {
		day.ClockOut = row.ClockOutAt.In(s.location).Format("15:04")
		day.Selesai = true
	}
	if row.DurasiMenit != nil && *row.DurasiMenit > 0 {
		day.Jam = round2(float64(*row.DurasiMenit) / 60)
		day.Durasi = formatDuration(*row.DurasiMenit)
	}

	var clockOut *time.Time
	if row.ClockOutAt != nil && !row.ClockOutAt.IsZero() {
		local := row.ClockOutAt.In(s.location)
		clockOut = &local
	}
	day.Rule = s.schedule.Judge(row.ClockInAt.In(s.location), clockOut)
	day.Catatan = ruleNotes(day.Rule, day.Selesai)
	return day
}

// ruleNotes turns the measurements into the words the pages print. A day with
// nothing to explain says so rather than staying blank, which would read as a
// day nobody looked at.
func ruleNotes(rule AttendanceRule, closed bool) []string {
	notes := make([]string, 0, 3)
	if rule.EarlyIn {
		notes = append(notes, "Masuk lebih awal")
	}
	if rule.Late {
		notes = append(notes, "Terlambat "+formatDuration(rule.LateMinutes))
	}
	if rule.EarlyLeave {
		notes = append(notes, "Pulang cepat "+formatDuration(rule.EarlyOutMinutes))
	}
	if len(notes) == 0 && closed {
		notes = append(notes, "Tepat waktu")
	}
	return notes
}

// formatDuration writes 545 minutes as "9j 5m", which is how a working day is
// spoken about here.
func formatDuration(minutes int) string {
	if minutes <= 0 {
		return "-"
	}
	hours := minutes / 60
	rest := minutes % 60
	if hours == 0 {
		return fmt.Sprintf("%dm", rest)
	}
	if rest == 0 {
		return fmt.Sprintf("%dj", hours)
	}
	return fmt.Sprintf("%dj %dm", hours, rest)
}

var indonesianMonthNames = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func indonesianMonthLabel(value time.Time) string {
	return fmt.Sprintf("%s %d", indonesianMonthNames[int(value.Month())-1], value.Year())
}
