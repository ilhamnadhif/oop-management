package handler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/service"
)

// A2BExportPageData drives the A2B export page: the machine performance report
// and the hour meter readings export on one page. Performance is filterable by
// date range and by machine, the readings by month; every filter left empty
// means all of it.
type A2BExportPageData struct {
	ShellPageData
	BasePath string
	Note     string
	Company  string
	// PerfAktif and HMAktif say the project allows each report to be
	// downloaded at all, since one page carries two exports.
	PerfAktif bool
	HMAktif   bool
	// PerfFrom and PerfTo are the range as it was typed, put back into the
	// form. Empty means every reading ever taken.
	PerfFrom string
	PerfTo   string
	// PerfUnit is the machine picked, empty meaning the whole fleet.
	PerfUnit string
	// PerfPeriod is the range actually used, worded for the person reading it.
	PerfPeriod string
	PerfRows   int
	// UnitOptions fill the machine dropdown, in register order.
	UnitOptions []UnitOption
	// HMMonth filters the hour meter report; empty means every month.
	HMMonth string
	HMRows  int
	HMNote  string
	Error   string
}

// UnitOption is one machine in the performance filter's dropdown.
type UnitOption struct {
	ID   string
	Nama string
}

// handleA2BExport renders the A2B export page: the performance report over the
// range and machine asked for, and the hour meter readings for the month filter
// beside it.
func (s *Server) handleA2BExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	data := A2BExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-export"),
		Company:       s.company,
		BasePath:      "/a2b/export",
		Note: "Performance per unit: satu baris per alat, berisi jumlah shift, total HM, " +
			"fuel, fuel ratio, PA, dan UA. Angkanya sama dengan yang ada di Overview A2B.",
		PerfAktif: s.exportAktif(model.ExportUnitA2B),
		HMAktif:   s.exportAktif(model.ExportInputHM),
		PerfFrom:  strings.TrimSpace(r.URL.Query().Get("from")),
		PerfTo:    strings.TrimSpace(r.URL.Query().Get("to")),
		PerfUnit:  strings.TrimSpace(r.URL.Query().Get("unit")),
		HMMonth:   month,
		HMNote: "Input hour meter: satu baris per pembacaan, berisi tanggal, HM awal, " +
			"HM akhir, total HM, PA, dan UA.",
	}
	units, err := s.unitA2B.List(r.Context())
	if err != nil {
		log.Printf("read unit a2b for export: %v", err)
		data.Error = "Gagal memuat data unit"
	}
	for _, unit := range units {
		data.UnitOptions = append(data.UnitOptions, UnitOption{ID: unit.IDUnit, Nama: unit.NamaUnit})
	}

	report, err := s.a2bPerformance(r.Context(), data.PerfFrom, data.PerfTo, data.PerfUnit)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			if data.Error == "" {
				data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
			}
		} else {
			log.Printf("count a2b performance for export: %v", err)
			if data.Error == "" {
				data.Error = "Gagal memuat performance unit"
			}
		}
	} else {
		data.PerfRows = len(report.Units)
		data.PerfPeriod = exportPeriodLabel(report.From, report.To)
	}

	readings, err := s.hourMeter.ExportRows(r.Context(), month)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			if data.Error == "" {
				data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
			}
		} else {
			log.Printf("count hour meter for export: %v", err)
			if data.Error == "" {
				data.Error = "Gagal memuat data hour meter"
			}
		}
	} else {
		data.HMRows = len(readings)
	}
	s.render(w, "a2b_export", data, http.StatusOK)
}

// a2bPerformance builds the performance report the page counts and the download
// streams, so the two never disagree about what the filters mean.
func (s *Server) a2bPerformance(ctx context.Context, from, to, idUnit string) (*service.A2BPerformanceReport, error) {
	return s.unitOverview.A2BPerformance(ctx, from, to, idUnit, s.hourMeter.WorkMinutes())
}

// handleA2BPerformanceDownload streams the performance report as XLSX or PDF.
// The range and the machine travel in the query; both left empty mean every
// reading of every machine.
func (s *Server) handleA2BPerformanceDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	if !s.requireExportAktif(w, model.ExportUnitA2B) {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	report, err := s.a2bPerformance(r.Context(),
		r.URL.Query().Get("from"), r.URL.Query().Get("to"), r.URL.Query().Get("unit"))
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read a2b performance for export: %v", err)
		http.Error(w, "Gagal memuat performance unit", http.StatusInternalServerError)
		return
	}

	title := "Performance Unit A2B"
	if report.IDUnit != "" {
		// The machine is named in the title rather than left to the reader to
		// work out from a table holding one row.
		title += " - " + report.IDUnit
	}
	meta := s.exportMetaFor(model.ExportUnitA2B, title, report.From, report.To)

	var payload []byte
	if format == "xlsx" {
		payload, err = export.A2BPerformanceXLSX(report.Units, meta)
	} else {
		payload, err = export.A2BPerformancePDF(report.Units, meta)
	}
	s.writeRegister(w, "performance-unit-a2b", format, payload, err)
}

// handleA2BHMExportDownload streams the hour meter readings as XLSX or PDF. The
// month filter travels in the query, empty meaning every month.
func (s *Server) handleA2BHMExportDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	if !s.requireExportAktif(w, model.ExportInputHM) {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	month := strings.TrimSpace(r.URL.Query().Get("month"))
	readings, err := s.hourMeter.ExportRows(r.Context(), month)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read hour meter for export: %v", err)
		http.Error(w, "Gagal memuat data hour meter", http.StatusInternalServerError)
		return
	}

	meta := s.exportMetaFor(model.ExportInputHM, "Input HM", "", "")
	if month != "" {
		meta.Period = monthLabel(month)
	}

	var payload []byte
	var contentType, extension string
	if format == "xlsx" {
		payload, err = export.HourMeterXLSX(readings, meta)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		extension = "xlsx"
	} else {
		payload, err = export.HourMeterPDF(readings, meta)
		contentType = "application/pdf"
		extension = "pdf"
	}
	if err != nil {
		log.Printf("build hour meter %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}

	filename := "input-hm"
	if month != "" {
		filename += "-" + month
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
