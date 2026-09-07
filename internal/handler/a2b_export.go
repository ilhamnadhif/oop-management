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
	// PerfUnits are the machines picked, empty meaning the whole fleet.
	PerfUnits []string
	// PerfPeriod is the range actually used, worded for the person reading it.
	PerfPeriod string
	PerfRows   int
	// The three machine filters, one per card, in register order.
	PerfPicker UnitPicker
	HMPicker   UnitPicker
	FKPicker   UnitPicker
	// HMFrom, HMTo and HMUnit filter the hour meter report the same way the
	// performance report is filtered; each left empty means all of it. They
	// carry their own names in the query because one page holds three filters.
	HMFrom  string
	HMTo    string
	HMUnits []string
	// HMPeriod is the range actually used, worded for the person reading it.
	HMPeriod string
	HMRows   int
	HMNote   string

	// The dispensing sheet is filtered the same way, under its own names.
	FKAktif  bool
	FKFrom   string
	FKTo     string
	FKUnits  []string
	FKPeriod string
	FKRows   int
	FKNote   string

	Error string
}

// UnitOption is one machine in a filter's list, and whether this report was
// narrowed to it.
type UnitOption struct {
	ID      string
	Nama    string
	Dipilih bool
}

// UnitPicker is one card's machine filter as the template draws it: the field
// the ticks post under, every machine with its own tick, and the line the
// summary shows while the list is closed.
type UnitPicker struct {
	Field   string
	Options []UnitOption
	// SummaryLabel says what is picked without the list having to be open.
	SummaryLabel string
}

// unitPickerFor builds one card's filter. The label is the whole point of the
// summary: a closed list that says nothing forces it open to find out what the
// report was narrowed to.
func unitPickerFor(field string, units []model.UnitA2B, picked []string) UnitPicker {
	chosen := make(map[string]bool, len(picked))
	for _, id := range picked {
		chosen[strings.ToLower(strings.TrimSpace(id))] = true
	}
	picker := UnitPicker{Field: field, Options: make([]UnitOption, 0, len(units))}
	names := make([]string, 0, len(picked))
	for _, unit := range units {
		selected := chosen[strings.ToLower(strings.TrimSpace(unit.IDUnit))]
		picker.Options = append(picker.Options, UnitOption{
			ID: unit.IDUnit, Nama: unit.NamaUnit, Dipilih: selected,
		})
		if selected {
			names = append(names, unit.IDUnit)
		}
	}
	picker.SummaryLabel = unitSummaryLabel(names)
	return picker
}

// unitSummaryLabel words what is picked. Past two it counts instead: a summary
// line listing ten ids is a list, not a summary.
func unitSummaryLabel(picked []string) string {
	switch len(picked) {
	case 0:
		return "Semua unit"
	case 1, 2:
		return strings.Join(picked, ", ")
	default:
		return fmt.Sprintf("%d unit dipilih", len(picked))
	}
}

// handleA2BExport renders the A2B export page: the performance report over the
// range and machine asked for, and the hour meter readings for the month filter
// beside it.
func (s *Server) handleA2BExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
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
		PerfUnits: pickedUnits(r, "unit"),
		HMFrom:    strings.TrimSpace(r.URL.Query().Get("hm_from")),
		HMTo:      strings.TrimSpace(r.URL.Query().Get("hm_to")),
		HMUnits:   pickedUnits(r, "hm_unit"),
		FKAktif:   s.exportAktif(model.ExportFuelKeluar),
		FKFrom:    strings.TrimSpace(r.URL.Query().Get("fk_from")),
		FKTo:      strings.TrimSpace(r.URL.Query().Get("fk_to")),
		FKUnits:   pickedUnits(r, "fk_unit"),
		FKNote: "Fuel keluar: satu baris per pemakaian, berisi pembacaan flow meter, " +
			"liter yang keluar, hour meter alat berat, dan operatornya.",
		HMNote: "Input hour meter: satu baris per pembacaan, berisi tanggal, HM awal, " +
			"HM akhir, total HM, PA, dan UA.",
	}
	units, err := s.unitA2B.List(r.Context())
	if err != nil {
		log.Printf("read unit a2b for export: %v", err)
		data.Error = "Gagal memuat data unit"
	}
	data.PerfPicker = unitPickerFor("unit", units, data.PerfUnits)
	data.HMPicker = unitPickerFor("hm_unit", units, data.HMUnits)
	data.FKPicker = unitPickerFor("fk_unit", units, data.FKUnits)

	report, err := s.a2bPerformance(r.Context(), data.PerfFrom, data.PerfTo, data.PerfUnits)
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

	readings, err := s.hourMeter.ExportRows(r.Context(), data.HMFrom, data.HMTo, data.HMUnits)
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
		data.HMRows = len(readings.Rows)
		data.HMPeriod = exportPeriodLabel(readings.From, readings.To)
	}

	dispenses, err := s.fuelKeluar.ExportRows(r.Context(), data.FKFrom, data.FKTo, data.FKUnits)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			if data.Error == "" {
				data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
			}
		} else {
			log.Printf("count fuel keluar for export: %v", err)
			if data.Error == "" {
				data.Error = "Gagal memuat data fuel keluar"
			}
		}
	} else {
		data.FKRows = len(dispenses.Rows)
		data.FKPeriod = exportPeriodLabel(dispenses.From, dispenses.To)
	}
	s.render(w, "a2b_export", data, http.StatusOK)
}

// a2bPerformance builds the performance report the page counts and the download
// streams, so the two never disagree about what the filters mean.
func (s *Server) a2bPerformance(ctx context.Context, from, to string, idUnits []string) (*service.A2BPerformanceReport, error) {
	return s.unitOverview.A2BPerformance(ctx, from, to, idUnits, s.hourMeter.WorkMinutes())
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
		r.URL.Query().Get("from"), r.URL.Query().Get("to"), pickedUnits(r, "unit"))
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read a2b performance for export: %v", err)
		http.Error(w, "Gagal memuat performance unit", http.StatusInternalServerError)
		return
	}

	// The machines are named in the title rather than left to the reader to
	// work out from the rows.
	title := "Performance Unit A2B" + unitTitleSuffix(report.IDUnits)
	meta := s.exportMetaFor(model.ExportUnitA2B, title, report.From, report.To)

	var payload []byte
	if format == "xlsx" {
		payload, err = export.A2BPerformanceXLSX(report, meta)
	} else {
		payload, err = export.A2BPerformancePDF(report, meta)
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
	readings, err := s.hourMeter.ExportRows(r.Context(),
		r.URL.Query().Get("from"), r.URL.Query().Get("to"), pickedUnits(r, "unit"))
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read hour meter for export: %v", err)
		http.Error(w, "Gagal memuat data hour meter", http.StatusInternalServerError)
		return
	}

	// The machines are named in the title rather than left to the reader to
	// work out from a column holding the same ids over and over.
	title := "Input HM" + unitTitleSuffix(readings.IDUnits)
	meta := s.exportMetaFor(model.ExportInputHM, title, readings.From, readings.To)

	var payload []byte
	var contentType, extension string
	if format == "xlsx" {
		payload, err = export.HourMeterXLSX(readings.Rows, meta)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		extension = "xlsx"
	} else {
		payload, err = export.HourMeterPDF(readings.Rows, meta)
		contentType = "application/pdf"
		extension = "pdf"
	}
	if err != nil {
		log.Printf("build hour meter %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}

	// The filename says which slice of the sheet this is, so two downloads do
	// not land in the same folder under the same name.
	filename := "input-hm" + unitFilenameSuffix(readings.IDUnits)
	if readings.From != "" || readings.To != "" {
		filename += "-" + strings.Trim(readings.From+"_"+readings.To, "_")
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

// handleFuelKeluarExportDownload streams the dispensing sheet as XLSX or PDF.
// The range and the machine travel in the query; both left empty mean every
// fill of every machine.
func (s *Server) handleFuelKeluarExportDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	if !s.requireExportAktif(w, model.ExportFuelKeluar) {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	report, err := s.fuelKeluar.ExportRows(r.Context(),
		r.URL.Query().Get("from"), r.URL.Query().Get("to"), pickedUnits(r, "unit"))
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read fuel keluar for export: %v", err)
		http.Error(w, "Gagal memuat data fuel keluar", http.StatusInternalServerError)
		return
	}

	title := "Fuel Keluar" + unitTitleSuffix(report.IDUnits)
	meta := s.exportMetaFor(model.ExportFuelKeluar, title, report.From, report.To)

	var payload []byte
	if format == "xlsx" {
		payload, err = export.FuelKeluarXLSX(report.Rows, meta)
	} else {
		payload, err = export.FuelKeluarPDF(report.Rows, meta)
	}
	s.writeRegister(w, fuelKeluarFilename(report), format, payload, err)
}

// fuelKeluarFilename says which slice of the sheet a download holds, so two of
// them do not land in the same folder under the same name.
func fuelKeluarFilename(report *service.FuelKeluarExport) string {
	name := "fuel-keluar" + unitFilenameSuffix(report.IDUnits)
	if report.From != "" || report.To != "" {
		name += "-" + strings.Trim(report.From+"_"+report.To, "_")
	}
	return name
}

// pickedUnits reads one filter's machines off the query. The parameter repeats,
// one entry per tick, and none of them means every machine.
func pickedUnits(r *http.Request, field string) []string {
	values := r.URL.Query()[field]
	picked := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			picked = append(picked, value)
		}
	}
	return picked
}

// unitTitleSuffix names the machines a report was narrowed to. Past two it
// gives the count instead: a heading listing ten ids stops being a heading.
func unitTitleSuffix(idUnits []string) string {
	switch len(idUnits) {
	case 0:
		return ""
	case 1, 2:
		return " - " + strings.Join(idUnits, ", ")
	default:
		return fmt.Sprintf(" - %d unit", len(idUnits))
	}
}

// unitFilenameSuffix does the same for a file name, where the case for the
// count is stronger still: ten ids in a name is unreadable, and long enough to
// run past what some filesystems will take.
func unitFilenameSuffix(idUnits []string) string {
	switch len(idUnits) {
	case 0:
		return ""
	case 1:
		return "-" + idUnits[0]
	default:
		return fmt.Sprintf("-%d-unit", len(idUnits))
	}
}
