package handler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"opp-management/internal/export"
	"opp-management/internal/widya"
)

// WidyaFeed is what Produksi New reads the weighbridge through. The feed caches,
// so a page refreshing itself does not read the scanner on every tick.
type WidyaFeed interface {
	Latest(ctx context.Context) (widya.Snapshot, bool, error)
	Ritase(ctx context.Context, from, to string, location *time.Location) (widya.RitaseSummary, bool, error)
	MaxAge() time.Duration
}

// WithWidya enables Produksi New. Like the scanners it is optional: without an
// account the page renders and says the source has not been set up.
func (s *Server) WithWidya(feed WidyaFeed) *Server {
	s.widya = feed
	return s
}

// WidyaStat is one figure off the scanner's dashboard, already written the way
// the page shows it.
type WidyaStat struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Note  string `json:"note"`
	Icon  string `json:"-"`
	Tone  string `json:"-"`
	Value string `json:"value"`
}

// RitaseRow is one line of the summary table. Tone colours the value: "good"
// for a vendor that hauled, "bad" for a class or vendor with nothing, "peak"
// for the busiest hour, "muted" for the break.
type RitaseRow struct {
	Label  string
	Value  string
	Satuan string
	Tone   string
	// Child rows sit indented under the line before them.
	Child bool
	// Head rows are the bold section lines.
	Head bool
}

// RitaseView is one day's summary, ready to draw.
type RitaseView struct {
	DateLabel string
	Rows      []RitaseRow
	Empty     bool
}

// RekapRow is one line of the vendor and fleet table. A vendor line heads its
// classes; Bad marks a vendor with nothing hauled, Muted a class with nothing.
type RekapRow struct {
	Label   string
	Unit    string
	Rit     string
	Volume  string
	Porsi   string
	Avg     string
	RitUnit string
	Vendor  bool
	Total   bool
	Bad     bool
	Muted   bool
}

// RekapView is the vendor and fleet table, ready to draw.
type RekapView struct {
	DateLabel string
	Rows      []RekapRow
}

// ProduksiNewView is the figures as both the page and its refresh read them,
// so the first paint and every refresh after it say the same thing.
type ProduksiNewView struct {
	Configured bool        `json:"configured"`
	Available  bool        `json:"available"`
	UpdatedAt  string      `json:"updatedAt"`
	Error      string      `json:"error"`
	Stats      []WidyaStat `json:"stats"`
	From       string      `json:"from"`
	To         string      `json:"to"`
	Ritase     *RitaseView `json:"-"`
	Rekap      *RekapView  `json:"-"`
	// RitaseHTML is the summary table as the page draws it, so a refresh swaps
	// in exactly what a reload would have shown.
	RitaseHTML string `json:"ritaseHtml"`
	RekapHTML  string `json:"rekapHtml"`
}

type ProduksiNewPageData struct {
	ShellPageData
	View ProduksiNewView
	// RefreshMS is how often the page asks again: the feed's own interval, so
	// the page never asks more often than there is anything new to show.
	RefreshMS      int64
	RefreshSeconds int64
	Today          string
}

func (s *Server) handleProduksiNewOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-new-overview")
	if !ok {
		return
	}
	data := ProduksiNewPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-new-overview"),
		// The figures are one small cached read; the ritase summary can be a
		// month of tickets. The page goes out with the figures and a loading
		// summary, and its script fetches the summary straight away.
		View:  s.produksiNewView(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"), false),
		Today: s.now().In(s.location).Format("2006-01-02"),
	}
	if s.widya != nil {
		data.RefreshMS = s.widya.MaxAge().Milliseconds()
		data.RefreshSeconds = int64(s.widya.MaxAge().Seconds())
	}
	s.render(w, "produksi_new_overview", data, http.StatusOK)
}

// handleProduksiNewData is the page's refresh: the same view, as JSON.
func (s *Server) handleProduksiNewData(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "produksi-new-overview")
	if !ok {
		return
	}
	view := s.produksiNewView(r.Context(), r.URL.Query().Get("from"), r.URL.Query().Get("to"), true)
	// Nothing to draw stays an empty string, which the page reads as "keep
	// what you have".
	if view.Ritase != nil {
		view.RitaseHTML = s.renderFragment("produksiNewRitase", view.Ritase)
	}
	if view.Rekap != nil {
		view.RekapHTML = s.renderFragment("produksiNewRekap", view.Rekap)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, view)
}

// produksiNewView reads the figures, and the ritase summary when withRitase is
// set.
func (s *Server) produksiNewView(ctx context.Context, from, to string, withRitase bool) ProduksiNewView {
	from, to = s.ritaseRange(from, to)
	if s.widya == nil {
		return ProduksiNewView{From: from, To: to}
	}
	view := ProduksiNewView{Configured: true, From: from, To: to}
	var problems []string

	snapshot, has, err := s.widya.Latest(ctx)
	if err != nil {
		log.Printf("read widya: %v", err)
		problems = append(problems, "statistik")
	}
	if has {
		view.Available = true
		view.UpdatedAt = snapshot.FetchedAt.In(s.location).Format("02 Jan 2006 15:04:05")
		stats := snapshot.Stats
		// Four figures, one row: what crossed the scanner and what it was
		// carried in. The scanner's own bookkeeping - suppliers, operators,
		// users, webhooks - is not what a production page is opened for.
		view.Stats = []WidyaStat{
			{Key: "tickets", Label: "TOTAL TIKET", Note: "Tiket timbang tercatat", Icon: "receipt", Tone: "blue", Value: formatCount(stats.Tickets)},
			{Key: "vehicles", Label: "TOTAL KENDARAAN", Note: "Kendaraan terdaftar", Icon: "truck", Tone: "green", Value: formatCount(stats.Vehicles)},
			{Key: "materials", Label: "TOTAL MATERIAL", Note: "Jenis material", Icon: "layers", Tone: "purple", Value: formatCount(stats.Materials)},
			{Key: "units", Label: "TOTAL UNIT", Note: "Unit scanner", Icon: "cube", Tone: "amber", Value: formatCount(stats.Units)},
		}
	}

	hasDay := false
	if withRitase {
		var summary widya.RitaseSummary
		var err error
		summary, hasDay, err = s.widya.Ritase(ctx, from, to, s.location)
		if err != nil {
			log.Printf("read widya tickets %s - %s: %v", from, to, err)
			problems = append(problems, "tiket")
		}
		if hasDay {
			view.Ritase = ritaseView(summary, s.location)
			view.Rekap = rekapView(summary, s.location)
		}
	}

	if len(problems) > 0 {
		view.Error = "Widya Load Scanner tidak bisa dibaca saat ini (" + strings.Join(problems, " dan ") + ")."
		if has || hasDay {
			view.Error += " Angka di bawah adalah pembacaan terakhir."
		}
	}
	return view
}

// ritaseView lays one day out the way the site's own summary sheet does.
func ritaseView(summary widya.RitaseSummary, location *time.Location) *RitaseView {
	view := &RitaseView{Empty: summary.TotalRitase == 0, DateLabel: rangeLabel(summary.From, summary.To, location)}
	zeroBad := func(count int) string {
		if count == 0 {
			return "bad"
		}
		return ""
	}
	rows := []RitaseRow{
		{Label: "Total Ritase", Value: formatCount(summary.TotalRitase), Satuan: "Rit", Head: true, Tone: "accent"},
		{Label: "Total Volume", Value: formatRitaseVolume(summary.TotalVolume), Satuan: "m³", Head: true, Tone: "accent"},
		{Label: "Unit Aktif", Value: formatCount(summary.UnitAktif), Satuan: "Unit", Head: true},
		{Label: "DTK", Value: formatCount(summary.DTK), Satuan: "Unit", Child: true, Tone: zeroBad(summary.DTK)},
		{Label: "DTE", Value: formatCount(summary.DTE), Satuan: "Unit", Child: true, Tone: zeroBad(summary.DTE)},
		{Label: "DTT", Value: formatCount(summary.DTT), Satuan: "Unit", Child: true, Tone: zeroBad(summary.DTT)},
		{Label: "Vendor", Head: true},
	}
	if len(summary.Vendors) == 0 {
		rows = append(rows, RitaseRow{Label: "-", Value: "Tidak ada vendor aktif", Child: true, Tone: "bad"})
	}
	for _, vendor := range summary.Vendors {
		rows = append(rows, RitaseRow{Label: vendor.Name, Value: "Aktif",
			Satuan: formatCount(vendor.Ritase) + " Rit", Child: true, Tone: "good"})
	}
	operasi, puncak, puncakRit := "-", "-", "0 Rit"
	if summary.TotalRitase > 0 {
		operasi = summary.FirstScan + " – " + summary.LastScan
	}
	if summary.PeakHour >= 0 {
		puncak = hourRange(summary.PeakHour, summary.PeakHour+1)
		puncakRit = formatCount(summary.PeakRitase) + " Rit"
	}
	rows = append(rows,
		RitaseRow{Label: "Rata-rata volume/rit", Value: formatRitaseVolume(summary.AvgVolume), Satuan: "m³", Head: true},
		RitaseRow{Label: "Jam Operasi", Value: operasi, Head: true},
		RitaseRow{Label: "Jam Puncak", Value: puncak, Satuan: puncakRit, Head: true, Tone: "peak"},
		RitaseRow{Label: "Rest 1", Value: hourRange(summary.RestStart, summary.RestEnd),
			Satuan: formatCount(summary.RestRitase) + " Rit", Head: true, Tone: "muted"},
	)
	view.Rows = rows
	return view
}

// renderFragment draws one template on its own, for the page's refresh to swap
// in.
func (s *Server) renderFragment(name string, data interface{}) string {
	var buffer bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		return ""
	}
	return buffer.String()
}

// rekapView lays the loads out by vendor and truck class, the way the site's
// own rekap sheet does: a line per vendor, its three classes under it, and the
// total at the foot.
func rekapView(summary widya.RitaseSummary, location *time.Location) *RekapView {
	view := &RekapView{DateLabel: rangeLabel(summary.From, summary.To, location)}
	total := summary.TotalVolume
	line := func(label string, group widya.ArmadaRekap) RekapRow {
		row := RekapRow{Label: label, Unit: formatCount(group.Units), Rit: formatCount(group.Ritase),
			Volume: formatRitaseVolume(group.Volume), Porsi: "0.0%", Avg: "-", RitUnit: "-"}
		if total > 0 {
			row.Porsi = fmt.Sprintf("%.1f%%", group.Volume/total*100)
		}
		if group.Ritase > 0 {
			row.Avg = formatRitaseVolume(group.Volume / float64(group.Ritase))
		}
		if group.Units > 0 {
			row.RitUnit = formatRitaseVolume(float64(group.Ritase) / float64(group.Units))
		}
		return row
	}
	for _, vendor := range summary.Rekap {
		head := line(vendor.Name, vendor.Total)
		head.Vendor, head.Bad = true, vendor.Total.Ritase == 0
		view.Rows = append(view.Rows, head)
		for _, class := range []struct {
			name  string
			group widya.ArmadaRekap
		}{{"DTK", vendor.DTK}, {"DTE", vendor.DTE}, {"DTT", vendor.DTT}} {
			row := line(class.name, class.group)
			row.Muted = class.group.Ritase == 0
			view.Rows = append(view.Rows, row)
		}
	}
	foot := line("TOTAL", widya.ArmadaRekap{Units: summary.RekapTotal.Units,
		Ritase: summary.RekapTotal.Ritase, Volume: summary.RekapTotal.Volume})
	foot.Total = true
	view.Rows = append(view.Rows, foot)
	return view
}

// ritaseRangeDays caps how far back one summary reaches. The ended days are
// read from the scanner once and kept, but the first read of a long range is
// a few megabytes a week, and a page request has thirty seconds.
const ritaseRangeDays = 62

// ritaseRange settles the range the summary covers. Nothing given is the month
// so far; a day that is not a date falls back the same way; the end never runs
// past today, a range given backwards is turned round, and a range longer than
// the cap keeps its end and loses its start.
func (s *Server) ritaseRange(from, to string) (string, string) {
	now := s.now().In(s.location)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	parse := func(value string) (time.Time, bool) {
		day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), s.location)
		return day, err == nil
	}
	start, okStart := parse(from)
	end, okEnd := parse(to)
	if !okEnd || end.After(today) {
		end = today
	}
	if !okStart {
		start = time.Date(end.Year(), end.Month(), 1, 0, 0, 0, 0, s.location)
	}
	if start.After(end) {
		start, end = end, start
	}
	if earliest := end.AddDate(0, 0, -(ritaseRangeDays - 1)); start.Before(earliest) {
		start = earliest
	}
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// rangeLabel heads the summary: one date when the range is a day, else both
// ends, with the month and year said once when they are shared.
func rangeLabel(from, to string, location *time.Location) string {
	start, errStart := time.ParseInLocation("2006-01-02", from, location)
	end, errEnd := time.ParseInLocation("2006-01-02", to, location)
	if errStart != nil || errEnd != nil {
		return ""
	}
	switch {
	case from == to:
		return formatTanggal(start)
	case start.Year() == end.Year() && start.Month() == end.Month():
		return fmt.Sprintf("%d – %s", start.Day(), formatTanggal(end))
	default:
		return formatTanggal(start) + " – " + formatTanggal(end)
	}
}

// formatTanggal writes a date the way the summary sheet heads it.
func formatTanggal(day time.Time) string {
	months := []string{"Januari", "Februari", "Maret", "April", "Mei", "Juni", "Juli",
		"Agustus", "September", "Oktober", "November", "Desember"}
	return fmt.Sprintf("%d %s %d", day.Day(), months[day.Month()-1], day.Year())
}

func hourRange(from, to int) string {
	return fmt.Sprintf("%02d:00 – %02d:00", from, to)
}

// formatRitaseVolume keeps two decimals, the way the scanner's own reports
// print a load.
func formatRitaseVolume(value float64) string { return fmt.Sprintf("%.2f", value) }

// formatCount groups a count the way the rest of the app writes numbers.
func formatCount(value int) string { return export.FormatMoney(float64(value)) }
