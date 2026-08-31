package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// MonthlyAbsensi is the whole monthly attendance report: one row per active
// employee, one column per day of the month. Days holds the count so the export
// can name the columns 1..Days.
type MonthlyAbsensi struct {
	Month   string
	Jabatan string
	Days    int
	Rows    []MonthlyAbsensiRow
}

// MonthlyAbsensiRow is one employee's month. Hari runs 1..Days; each cell is
// the check for hadir, S/I/C for an approved leave, or blank.
type MonthlyAbsensiRow struct {
	No         int
	Nama       string
	Jabatan    string
	Hari       []string
	M1         int
	M2         int
	M3         int
	M4         int
	Hadir      int
	Sakit      int
	Izin       int
	Cuti       int
	TidakAbsen int
	Persentase float64
}

// The marker written into a day that has attendance, and the shorthand for the
// approved leave types. These are exactly the letters the report's legend uses.
const (
	absensiHadirMark = "✓"
)

func leaveShortMark(jenis string) string {
	switch jenis {
	case model.LeaveJenisCutiSakit:
		return "S"
	case model.LeaveJenisIzin:
		return "I"
	default:
		return "C"
	}
}

// BuildMonthlyAbsensi counts one month of attendance and approved leave per
// employee. Every day the employee is active counts as a legal work day: a day
// with attendance is hadir, a day inside an approved leave is S/I/C, and any
// other active day is "tidak absen" - weekends included, because the operation
// runs all week. Days before the employee joined are not owed, so they sit in
// neither the counts nor the percentage. M1-M4 split the month into four weeks
// of seven days for the weekly read.
func (s *AttendanceService) BuildMonthlyAbsensi(ctx context.Context, month, jabatan string) (*MonthlyAbsensi, error) {
	month = strings.TrimSpace(month)
	jabatan = strings.TrimSpace(jabatan)
	start, err := time.ParseInLocation("2006-01", month, s.location)
	if err != nil {
		return nil, fmt.Errorf("%w: bulan tidak valid", ErrValidation)
	}
	end := start.AddDate(0, 1, -1)
	from := start.Format("2006-01-02")
	to := end.Format("2006-01-02")

	users, err := s.listUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	attendance, err := s.store.ListAttendanceBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("read attendance: %w", err)
	}
	leaves, err := s.store.ListLeave(ctx)
	if err != nil {
		return nil, fmt.Errorf("read leave requests: %w", err)
	}

	present := make(map[string]map[string]bool)
	for _, row := range attendance {
		if row.TanggalAbsensi < from || row.TanggalAbsensi > to {
			continue
		}
		if present[row.UserID] == nil {
			present[row.UserID] = make(map[string]bool)
		}
		present[row.UserID][row.TanggalAbsensi] = true
	}

	// The leave type is kept, not just the fact of it, so the day column can
	// say S, I or C rather than one generic "leave". Every day of the approved
	// range counts, weekend included.
	approvedLeave := make(map[string]map[string]string)
	for _, row := range leaves {
		if row.Status != model.LeaveStatusDisetujui || row.TanggalSelesai < from || row.TanggalMulai > to {
			continue
		}
		low := maxDateString(row.TanggalMulai, from)
		high := minDateString(row.TanggalSelesai, to)
		for _, day := range dateStringsInRange(low, high) {
			if approvedLeave[row.UserID] == nil {
				approvedLeave[row.UserID] = make(map[string]string)
			}
			approvedLeave[row.UserID][day] = row.JenisLeave
		}
	}

	report := &MonthlyAbsensi{Month: month, Jabatan: jabatan, Days: end.Day()}
	days := dateStringsInRange(from, to)

	for _, user := range users {
		if user.StatusPengguna != model.StatusAktif || strings.TrimSpace(user.UserID) == "" {
			continue
		}
		if jabatan != "" && user.Jabatan != jabatan {
			continue
		}
		row := MonthlyAbsensiRow{
			Nama:    strings.TrimSpace(user.NamaLengkap),
			Jabatan: strings.TrimSpace(user.Jabatan),
			Hari:    make([]string, len(days)),
		}
		if row.Nama == "" {
			row.Nama = user.UserID
		}
		for i, day := range days {
			// Someone who joined after the day did not owe that day's presence,
			// so it is neither counted nor marked.
			if !isActiveUserOn(user, day) {
				continue
			}
			switch {
			case present[user.UserID][day]:
				row.Hari[i] = absensiHadirMark
				row.Hadir++
			case approvedLeave[user.UserID][day] != "":
				jenis := approvedLeave[user.UserID][day]
				row.Hari[i] = leaveShortMark(jenis)
				switch jenis {
				case model.LeaveJenisCutiSakit:
					row.Sakit++
				case model.LeaveJenisIzin:
					row.Izin++
				default:
					row.Cuti++
				}
			default:
				row.TidakAbsen++
			}
		}
		for i, mark := range row.Hari {
			if mark != absensiHadirMark {
				continue
			}
			switch i / 7 {
			case 0:
				row.M1++
			case 1:
				row.M2++
			case 2:
				row.M3++
			default:
				row.M4++
			}
		}
		// Kehadiran is hadir plus any approved leave, over the days that were
		// actually owed (each active day lands in exactly one column).
		if total := row.Hadir + row.Sakit + row.Izin + row.Cuti + row.TidakAbsen; total > 0 {
			row.Persentase = round2(float64(row.Hadir+row.Sakit+row.Izin+row.Cuti) * 100 / float64(total))
		}
		report.Rows = append(report.Rows, row)
	}

	sort.SliceStable(report.Rows, func(i, j int) bool {
		if !strings.EqualFold(report.Rows[i].Nama, report.Rows[j].Nama) {
			return strings.ToLower(report.Rows[i].Nama) < strings.ToLower(report.Rows[j].Nama)
		}
		return report.Rows[i].Jabatan < report.Rows[j].Jabatan
	})
	for i := range report.Rows {
		report.Rows[i].No = i + 1
	}
	return report, nil
}
