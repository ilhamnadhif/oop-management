package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/service"
)

// AbsensiExportPageData drives the absensi export page: a month and a jabatan
// filter, then a single XLSX download. The column layout of the file is the
// month's days plus the totals, so the page only needs to name the filter.
type AbsensiExportPageData struct {
	ShellPageData
	Month          string
	Jabatan        string
	JabatanOptions []string
	Rows           int
	Note           string
	Error          string
	// Aktif says the project allows this report to be downloaded at all.
	Aktif bool
}

func (s *Server) handleAbsensiExportPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-export")
	if !ok {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = s.now().In(s.location).Format("2006-01")
	}
	jabatan := strings.TrimSpace(r.URL.Query().Get("jabatan"))

	data := AbsensiExportPageData{
		ShellPageData:  s.shellData(user, sessionValue, "hr-export"),
		Month:          month,
		Jabatan:        jabatan,
		JabatanOptions: service.JabatanOptions,
		Aktif:          s.exportAktif(model.ExportAbsensi),
		Note: "Matriks absensi bulanan: satu baris per karyawan. Kolom tanggal 1 sampai akhir " +
			"bulan berisi ✓ (hadir), S (sakit), I (izin), C (cuti), lalu total per minggu " +
			"(M1-M4), total kehadiran, sakit, izin, cuti, tidak absen, dan presentase kehadiran.",
	}

	report, err := s.attendance.BuildMonthlyAbsensi(r.Context(), month, jabatan)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build monthly absensi for export: %v", err)
			data.Error = "Gagal memuat data absensi"
		}
		s.render(w, "absensi_export", data, http.StatusOK)
		return
	}
	data.Month = report.Month
	data.Jabatan = report.Jabatan
	data.Rows = len(report.Rows)
	s.render(w, "absensi_export", data, http.StatusOK)
}

// handleAbsensiExportDownload streams the XLSX or the PDF itself.
func (s *Server) handleAbsensiExportDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "hr-export")
	if !ok {
		return
	}
	if !s.requireExportAktif(w, model.ExportAbsensi) {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "xlsx"
	}
	if format != "xlsx" && format != "pdf" {
		http.Error(w, "format tidak dikenal", http.StatusBadRequest)
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	if month == "" {
		month = s.now().In(s.location).Format("2006-01")
	}
	jabatan := strings.TrimSpace(r.URL.Query().Get("jabatan"))

	report, err := s.attendance.BuildMonthlyAbsensi(r.Context(), month, jabatan)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read monthly absensi for export: %v", err)
		http.Error(w, "Gagal memuat data absensi", http.StatusInternalServerError)
		return
	}

	meta := s.exportMetaFor(model.ExportAbsensi, "Rekap Absensi Bulanan", report.Month+"-01", report.Month+"-31")
	meta.Period = monthLabel(report.Month)

	var payload []byte
	var contentType, extension string
	if format == "xlsx" {
		payload, err = export.AbsensiXLSX(report, meta)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		extension = "xlsx"
	} else {
		payload, err = export.AbsensiPDF(report, meta)
		contentType = "application/pdf"
		extension = "pdf"
	}
	if err != nil {
		log.Printf("build absensi %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}

	filename := "rekap-absensi-" + report.Month
	if report.Jabatan != "" {
		filename += "-" + strings.ToLower(report.Jabatan)
	}
	filename += "." + extension
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	// A report is a snapshot of a moving sheet; a cached copy would quietly go
	// stale behind the person downloading it.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// monthLabel renders "2026-08" as "Agustus 2026" for the letterhead.
func monthLabel(month string) string {
	date, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return fmt.Sprintf("%s %d", indonesianMonths[date.Month()], date.Year())
}
