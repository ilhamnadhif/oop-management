package handler

import (
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

// A2BExportPageData drives the A2B export page: the machine register download
// and the hour meter readings export on one page. The register is a snapshot;
// the readings are filterable by month, with an empty month meaning every one.
type A2BExportPageData struct {
	ShellPageData
	Register string
	Rows     int
	BasePath string
	Note     string
	Company  string
	// RegisterAktif and HMAktif say the project allows each report to be
	// downloaded at all, since one page carries two exports.
	RegisterAktif bool
	HMAktif       bool
	// HMMonth filters the hour meter report; empty means every month.
	HMMonth string
	HMRows  int
	HMNote  string
	Error   string
}

// handleA2BExport renders the A2B export page. It answers the register card the
// same way it always has, and counts the hour meter readings for the month
// filter beside it.
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
		Register:      "Unit A2B",
		Note:          "Daftar alat berat lengkap dengan kapasitas tangki, konsumsi per jam, dan lokasinya.",
		RegisterAktif: s.exportAktif(model.ExportUnitA2B),
		HMAktif:       s.exportAktif(model.ExportInputHM),
		HMMonth:       month,
		HMNote: "Input hour meter: satu baris per pembacaan, berisi tanggal, HM awal, " +
			"HM akhir, total HM, PA, dan UA.",
	}
	units, err := s.unitA2B.List(r.Context())
	if err != nil {
		log.Printf("count unit a2b for export: %v", err)
		data.Error = "Gagal memuat data unit"
	}
	data.Rows = len(units)

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
