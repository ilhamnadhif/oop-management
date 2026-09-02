package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/service"
)

// HRPerformancePageData is one month of attendance for every employee, read as
// totals rather than as the day-by-day matrix the export prints.
type HRPerformancePageData struct {
	ShellPageData
	Month          string
	Jabatan        string
	JabatanOptions []string
	Rows           []service.MonthlyAbsensiRow
	Error          string
}

// handleHRPerformance renders the monthly performance table. It reads the same
// report the absensi export does: the figures on this page and the ones in that
// file are the same figures, and building them twice would let them drift.
func (s *Server) handleHRPerformance(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-performance")
	if !ok {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = s.now().In(s.location).Format("2006-01")
	}
	jabatan := strings.TrimSpace(r.URL.Query().Get("jabatan"))

	data := HRPerformancePageData{
		ShellPageData:  s.shellData(user, sessionValue, "hr-performance"),
		Month:          month,
		Jabatan:        jabatan,
		JabatanOptions: s.jabatanOptions(r.Context()),
	}
	report, err := s.attendance.BuildMonthlyAbsensi(r.Context(), month, jabatan)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build hr performance: %v", err)
			data.Error = "Gagal memuat data absensi"
		}
		s.render(w, "hr_performance", data, http.StatusOK)
		return
	}
	data.Month = report.Month
	data.Jabatan = report.Jabatan
	data.Rows = report.Rows
	s.render(w, "hr_performance", data, http.StatusOK)
}
