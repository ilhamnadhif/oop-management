package handler

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
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
	Detail(ctx context.Context, day string, location *time.Location) (widya.UnitDetailReport, bool, error)
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

// DetailTime is one load's time in the per-rit columns. Rest marks a load
// scanned during the midday break; an empty Clock pads a truck with fewer
// loads than the busiest.
type DetailTime struct {
	Clock string
	Rest  bool
}

// DetailRow is one line of the unit detail: a vendor heading, one of its
// trucks, or the total at the foot.
type DetailRow struct {
	Vendor   bool
	Total    bool
	No       int
	Label    string
	Kategori string
	Rit      string
	Volume   string
	Avg      string
	Porsi    string
	// Note runs across the per-rit columns on the vendor and total lines.
	Note     string
	Times    []DetailTime
	Interval string
	// Single marks a truck that made one load all day.
	Single bool
}

// DetailView is one day, vendor by vendor and truck by truck.
type DetailView struct {
	DateLabel string
	// RitColumns numbers the per-rit columns, as many as the busiest truck
	// made.
	RitColumns []int
	Rows       []DetailRow
}

// RitRow is one line of the per-load list: a vendor, one of its trucks, or
// one load. Key names the line and Parent the line it folds under, which is
// what the page's expand and collapse work by.
type RitRow struct {
	Kind     string
	Key      string
	Parent   string
	Label    string
	Rit      string
	Volume   string
	Kategori string
}

// RitView is one day's loads, vendor by vendor and truck by truck.
type RitView struct {
	DateLabel string
	Rows      []RitRow
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
	RekapFrom  string      `json:"rekapFrom"`
	RekapTo    string      `json:"rekapTo"`
	Ritase     *RitaseView `json:"-"`
	Rekap      *RekapView  `json:"-"`
	Day        string      `json:"day"`
	Detail     *DetailView `json:"-"`
	RitDay     string      `json:"ritDay"`
	Rit        *RitView    `json:"-"`
	// RitaseHTML is the summary table as the page draws it, so a refresh swaps
	// in exactly what a reload would have shown.
	RitaseHTML string `json:"ritaseHtml"`
	RekapHTML  string `json:"rekapHtml"`
	DetailHTML string `json:"detailHtml"`
	RitHTML    string `json:"ritHtml"`
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
		View:  s.produksiNewView(r.Context(), r.URL.Query(), false),
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
	view := s.produksiNewView(r.Context(), r.URL.Query(), true)
	// Nothing to draw stays an empty string, which the page reads as "keep
	// what you have".
	if view.Ritase != nil {
		view.RitaseHTML = s.renderFragment("produksiNewRitase", view.Ritase)
	}
	if view.Rekap != nil {
		view.RekapHTML = s.renderFragment("produksiNewRekap", view.Rekap)
	}
	if view.Detail != nil {
		view.DetailHTML = s.renderFragment("produksiNewDetail", view.Detail)
	}
	if view.Rit != nil {
		view.RitHTML = s.renderFragment("produksiNewRit", view.Rit)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, view)
}

// produksiNewView reads the figures, and the ticket tables when withTickets is
// set. Each table reads dates of its own: the summary the from-to range, the
// rekap the rekap_from-rekap_to range, the unit detail the one day in day, and
// the per-load list the one day in rit_day.
func (s *Server) produksiNewView(ctx context.Context, query url.Values, withTickets bool) ProduksiNewView {
	from, to := s.ritaseRange(query.Get("from"), query.Get("to"))
	day := s.detailDay(query.Get("day"))
	ritDay := s.detailDay(query.Get("rit_day"))
	rekapFrom, rekapTo := s.ritaseRange(query.Get("rekap_from"), query.Get("rekap_to"))
	view := ProduksiNewView{Configured: s.widya != nil, From: from, To: to,
		RekapFrom: rekapFrom, RekapTo: rekapTo, Day: day, RitDay: ritDay}
	if s.widya == nil {
		return view
	}
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
	if withTickets {
		var summary widya.RitaseSummary
		var err error
		summary, hasDay, err = s.widya.Ritase(ctx, from, to, s.location)
		if err != nil {
			log.Printf("read widya tickets %s - %s: %v", from, to, err)
			problems = append(problems, "tiket")
		}
		if hasDay {
			view.Ritase = ritaseView(summary, s.location)
		}
		// The feed keeps each day's loads, so a rekap range that overlaps the
		// summary's costs no second read of the scanner.
		rekap, hasRekap, err := s.widya.Ritase(ctx, rekapFrom, rekapTo, s.location)
		if err != nil {
			log.Printf("read widya tickets %s - %s: %v", rekapFrom, rekapTo, err)
			if len(problems) == 0 || problems[len(problems)-1] != "tiket" {
				problems = append(problems, "tiket")
			}
		}
		if hasRekap {
			view.Rekap = rekapView(rekap, s.location)
		}
		report, hasDetail, err := s.widya.Detail(ctx, day, s.location)
		if err != nil {
			log.Printf("read widya tickets %s: %v", day, err)
			if len(problems) == 0 || problems[len(problems)-1] != "tiket" {
				problems = append(problems, "tiket")
			}
		}
		if hasDetail {
			view.Detail = detailView(report, s.location)
		}
		// The per-load list has a day of its own. The feed keeps each day's
		// loads, so the same day twice is read once.
		ritReport, hasRit, err := s.widya.Detail(ctx, ritDay, s.location)
		if err != nil {
			log.Printf("read widya tickets %s: %v", ritDay, err)
			if len(problems) == 0 || problems[len(problems)-1] != "tiket" {
				problems = append(problems, "tiket")
			}
		}
		if hasRit {
			view.Rit = ritView(ritReport, s.location)
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

// detailDay settles the one day the unit detail shows: today when nothing or
// no date is given, and never a day still to come.
func (s *Server) detailDay(value string) string {
	today := s.now().In(s.location).Format("2006-01-02")
	day, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), s.location)
	if err != nil || day.Format("2006-01-02") > today {
		return today
	}
	return day.Format("2006-01-02")
}

// detailView lays one day out like the site's detail sheet: a heading line per
// vendor with its first and last load, its trucks under it with the time of
// every load, and the total at the foot.
func detailView(report widya.UnitDetailReport, location *time.Location) *DetailView {
	view := &DetailView{DateLabel: rangeLabel(report.Day, report.Day, location)}
	columns := report.MaxRitase
	if columns < 1 {
		columns = 1
	}
	for column := 1; column <= columns; column++ {
		view.RitColumns = append(view.RitColumns, column)
	}
	porsi := func(volume float64) string {
		if report.Volume <= 0 {
			return "0.0%"
		}
		return fmt.Sprintf("%.1f%%", volume/report.Volume*100)
	}
	avg := func(volume float64, rit int) string {
		if rit == 0 {
			return "-"
		}
		return formatRitaseVolume(volume / float64(rit))
	}
	number := 0
	for _, vendor := range report.Vendors {
		view.Rows = append(view.Rows, DetailRow{
			Vendor: true, Label: vendor.Name, Kategori: fmt.Sprintf("%d unit", len(vendor.Units)),
			Rit: formatCount(vendor.Ritase), Volume: formatRitaseVolume(vendor.Volume),
			Avg: avg(vendor.Volume, vendor.Ritase), Porsi: porsi(vendor.Volume),
			Note: "Mulai " + vendor.FirstScan + " · selesai " + vendor.LastScan,
		})
		for _, unit := range vendor.Units {
			number++
			row := DetailRow{
				No: number, Label: unit.Vehicle, Kategori: unit.Class, Rit: formatCount(unit.Ritase),
				Volume: formatRitaseVolume(unit.Volume), Avg: avg(unit.Volume, unit.Ritase),
				Porsi: porsi(unit.Volume), Interval: "-", Single: unit.Ritase == 1,
			}
			if unit.IntervalMinutes >= 0 {
				row.Interval = formatCount(unit.IntervalMinutes)
			}
			for column := 0; column < columns; column++ {
				var cell DetailTime
				if column < len(unit.Times) {
					cell = DetailTime{Clock: unit.Times[column], Rest: unit.RestLoads[column]}
				}
				row.Times = append(row.Times, cell)
			}
			view.Rows = append(view.Rows, row)
		}
	}
	operasi := "-"
	if report.Ritase > 0 {
		operasi = report.FirstScan + " – " + report.LastScan
	}
	view.Rows = append(view.Rows, DetailRow{
		Total: true, Label: "TOTAL", Kategori: fmt.Sprintf("%d unit", report.Units),
		Rit: formatCount(report.Ritase), Volume: formatRitaseVolume(report.Volume),
		Avg: avg(report.Volume, report.Ritase), Porsi: porsi(report.Volume),
		Note: "Jam operasi " + operasi,
	})
	return view
}

// ritView lists every load of the day under its truck and vendor, like the
// site's per-rit sheet.
func ritView(report widya.UnitDetailReport, location *time.Location) *RitView {
	view := &RitView{DateLabel: rangeLabel(report.Day, report.Day, location)}
	// Keys are names rather than positions, so a line stays open across a
	// refresh that reorders the trucks.
	for _, vendor := range report.Vendors {
		vendorKey := "v:" + vendor.Name
		view.Rows = append(view.Rows, RitRow{Kind: "vendor", Key: vendorKey, Label: vendor.Name,
			Rit: formatCount(vendor.Ritase), Volume: formatRitaseVolume(vendor.Volume)})
		for _, unit := range vendor.Units {
			unitKey := vendorKey + "|" + unit.Vehicle
			view.Rows = append(view.Rows, RitRow{Kind: "unit", Key: unitKey, Parent: vendorKey,
				Label: unit.Vehicle, Rit: formatCount(unit.Ritase), Volume: formatRitaseVolume(unit.Volume), Kategori: unit.Class})
			for l, load := range unit.Loads {
				view.Rows = append(view.Rows, RitRow{Kind: "load", Parent: unitKey,
					Label: load.Clock, Rit: formatCount(l + 1), Volume: formatRitaseVolume(load.Volume)})
			}
		}
	}
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
