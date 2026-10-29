package handler

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/widya"
)

// ProduksiNewExportPageData drives the Produksi New export page: the range and
// vendor picked, and what the download would hold.
type ProduksiNewExportPageData struct {
	ShellPageData
	Configured    bool
	OtherProject  string
	Aktif         bool
	From          string
	To            string
	VendorOptions []VendorOption
	// VendorSummary words what is ticked while the list is closed.
	VendorSummary string
	Today         string
	Ritase        string
	Volume        string
	Units         string
	Empty         bool
	Error         string
	// Downloads is a line per table and one for all four, each with its two
	// buttons.
	Downloads []ProduksiNewDownload
}

// ProduksiNewDownload is one line of the download list. XLSX and PDF are the
// buttons' addresses, carrying the range and vendor shown. They are built
// whole here: a query string dropped into an href after the "?" is escaped by
// the template, "&" and "=" included, and the link would carry no filter at
// all.
type ProduksiNewDownload struct {
	Label string
	Hint  string
	XLSX  template.URL
	PDF   template.URL
}

// produksiNewPart is one table the export can hand out alone. Index is its
// place among the sections produksiNewSections lays out.
type produksiNewPart struct {
	Key   string
	Index int
	Label string
	Hint  string
}

var produksiNewParts = []produksiNewPart{
	{Key: "summary", Index: 0, Label: "Summary Ritase", Hint: "Total ritase, volume, unit aktif, vendor dan jam operasi."},
	{Key: "rekap", Index: 1, Label: "Rekap Vendor & Armada", Hint: "Ritase per vendor dan jenis DT."},
	{Key: "detail", Index: 2, Label: "Detail Unit per Vendor", Hint: "Ritase per unit dengan jam tiap rit dan interval rata-rata."},
	{Key: "rit", Index: 3, Label: "Detail Rit per Unit", Hint: "Setiap rit per unit, jam sampai detik dan volumenya."},
}

// partFor is the table a download asks for; nothing, or a name it does not
// know, is all four.
func partFor(key string) (produksiNewPart, bool) {
	for _, part := range produksiNewParts {
		if part.Key == key {
			return part, true
		}
	}
	return produksiNewPart{}, false
}

func (s *Server) handleProduksiNewExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-new-export")
	if !ok {
		return
	}
	data := ProduksiNewExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-new-export"),
		Configured:    s.widyaHere(),
		Aktif:         s.exportAktif(model.ExportProduksiNew),
		Today:         s.now().In(s.location).Format("2006-01-02"),
	}
	data.From, data.To = s.ritaseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if !data.Configured {
		if s.widya != nil {
			data.OtherProject = s.widyaProject
		}
		s.render(w, "produksi_new_export", data, http.StatusOK)
		return
	}
	all := s.widya.Vendors(r.Context())
	picked := pickVendors(r.URL.Query()["vendor"], all)
	for _, vendor := range all {
		data.VendorOptions = append(data.VendorOptions, VendorOption{Name: vendor, Dipilih: containsFold(picked, vendor)})
	}
	data.VendorSummary = vendorSummary(picked)

	tickets, has, err := s.produksiNewTickets(r.Context(), data.From, data.To, picked)
	if err != nil {
		log.Printf("read widya tickets for export: %v", err)
	}
	if !has {
		data.Error = "Tiket Widya belum bisa dibaca. Coba lagi sebentar."
		s.render(w, "produksi_new_export", data, http.StatusOK)
		return
	}
	summary := widya.Summarize(tickets, data.From, data.To, s.location)
	data.Ritase = formatCount(summary.TotalRitase)
	data.Volume = formatRitaseVolume(summary.TotalVolume)
	data.Units = formatCount(summary.UnitAktif)
	data.Empty = summary.TotalRitase == 0
	for _, part := range produksiNewParts {
		data.Downloads = append(data.Downloads, ProduksiNewDownload{
			Label: part.Label, Hint: part.Hint,
			XLSX: produksiNewExportQuery("xlsx", part.Key, data.From, data.To, picked),
			PDF:  produksiNewExportQuery("pdf", part.Key, data.From, data.To, picked),
		})
	}
	data.Downloads = append(data.Downloads, ProduksiNewDownload{
		Label: "Semua bagian", Hint: "Keempat tabel di atas dalam satu berkas: sheet per bagian di XLSX, halaman per bagian di PDF.",
		XLSX: produksiNewExportQuery("xlsx", "", data.From, data.To, picked),
		PDF:  produksiNewExportQuery("pdf", "", data.From, data.To, picked),
	})
	s.render(w, "produksi_new_export", data, http.StatusOK)
}

// handleProduksiNewExportDownload streams the Produksi New tables for a range
// and a vendor: one table when the part asks for one, otherwise all four, a
// sheet each in the workbook and a run of pages each in the PDF.
func (s *Server) handleProduksiNewExportDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "produksi-new-export")
	if !ok {
		return
	}
	if !s.widyaHere() {
		http.Error(w, "Widya Load Scanner tidak terhubung ke project ini", http.StatusNotFound)
		return
	}
	if !s.requireExportAktif(w, model.ExportProduksiNew) {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	from, to := s.ritaseRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	vendors := pickVendors(r.URL.Query()["vendor"], s.widya.Vendors(r.Context()))
	tickets, has, err := s.produksiNewTickets(r.Context(), from, to, vendors)
	if !has {
		log.Printf("read widya tickets for export: %v", err)
		http.Error(w, "Tiket Widya belum bisa dibaca", http.StatusServiceUnavailable)
		return
	}

	sections := s.produksiNewSections(tickets, from, to)
	title := "Laporan Ritase Widya · " + vendorLabel(vendors)
	filename := "ritase-widya-"
	if part, one := partFor(r.URL.Query().Get("part")); one {
		sections = sections[part.Index : part.Index+1]
		title = part.Label + " · " + vendorLabel(vendors)
		filename += part.Key + "-"
	}
	meta := s.exportMetaFor(model.ExportProduksiNew, title, from, to)
	var payload []byte
	if format == "xlsx" {
		payload, err = export.ReportXLSX(sections, meta)
	} else {
		payload, err = export.ReportPDF(sections, meta)
	}
	if err != nil {
		log.Printf("build produksi new %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}
	contentType := "application/pdf"
	if format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	filename += vendorFilename(vendors) + "-" + from
	if to != from {
		filename += "_" + to
	}
	filename += "." + format
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

// VendorOption is one vendor in the export's picker.
type VendorOption struct {
	Name    string
	Dipilih bool
}

// produksiNewTickets reads the kept loads for a range, keeping only the
// vendors picked when any are.
func (s *Server) produksiNewTickets(ctx context.Context, from, to string, vendors []string) ([]widya.Ticket, bool, error) {
	tickets, has, err := s.widya.TicketsBetween(ctx, from, to, s.location)
	if !has || len(vendors) == 0 {
		return tickets, has, err
	}
	kept := tickets[:0:0]
	for _, ticket := range tickets {
		if containsFold(vendors, strings.TrimSpace(ticket.Supplier)) {
			kept = append(kept, ticket)
		}
	}
	return kept, true, err
}

// pickVendors keeps the vendors asked for that the tickets carry, spelled the
// way they spell them and in their order; none left means every vendor.
func pickVendors(values []string, vendors []string) []string {
	var picked []string
	for _, vendor := range vendors {
		for _, value := range values {
			if strings.EqualFold(vendor, strings.TrimSpace(value)) {
				picked = append(picked, vendor)
				break
			}
		}
	}
	return picked
}

func containsFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

// vendorLabel names the vendors a report covers, for its title.
func vendorLabel(vendors []string) string {
	switch len(vendors) {
	case 0:
		return "Semua vendor"
	case 1:
		return "Vendor " + vendors[0]
	default:
		return "Vendor " + strings.Join(vendors, ", ")
	}
}

// vendorSummary is the picker's closed label, worded as the script words it.
func vendorSummary(vendors []string) string {
	switch {
	case len(vendors) == 0:
		return "Semua vendor"
	case len(vendors) <= 2:
		return strings.Join(vendors, ", ")
	default:
		return fmt.Sprintf("%d vendor dipilih", len(vendors))
	}
}

// vendorFilename names the vendors in a file name: each by name up to two,
// then a count, so the name stays short.
func vendorFilename(vendors []string) string {
	switch {
	case len(vendors) == 0:
		return "semua-vendor"
	case len(vendors) <= 2:
		return "vendor-" + filenameSlug(strings.Join(vendors, " "))
	default:
		return fmt.Sprintf("%d-vendor", len(vendors))
	}
}

var slugCleaner = regexp.MustCompile(`[^a-z0-9]+`)

func filenameSlug(text string) string {
	return strings.Trim(slugCleaner.ReplaceAllString(strings.ToLower(text), "-"), "-")
}

// produksiNewSections lays the four Produksi New tables out for export. They
// are built from the same rows the page draws, so the report says exactly what
// the screen does, column for column. The two tables of one day each - the
// unit detail and the per-load list - run day by day across the range.
func (s *Server) produksiNewSections(tickets []widya.Ticket, from, to string) []export.Section {
	summary := widya.Summarize(tickets, from, to, s.location)
	sections := []export.Section{
		summarySection(ritaseView(summary, s.location), s.rangeTitle("Summary Ritase", from, to)),
		rekapSection(rekapView(summary, s.location), s.rangeTitle("Ritase Per Vendor dan Jenis DT,", from, to)),
	}

	var days []widya.UnitDetailReport
	maxRit := 1
	start, _ := time.ParseInLocation("2006-01-02", from, s.location)
	end, _ := time.ParseInLocation("2006-01-02", to, s.location)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		report := widya.DetailUnits(tickets, day.Format("2006-01-02"), s.location)
		if report.Ritase == 0 {
			continue
		}
		days = append(days, report)
		if report.MaxRitase > maxRit {
			maxRit = report.MaxRitase
		}
	}
	sections = append(sections,
		s.detailSection(days, maxRit, from, to, summary),
		s.ritSection(days, from, to, summary),
	)
	return sections
}

func summarySection(view *RitaseView, title string) export.Section {
	section := export.Section{
		SheetName: "Summary Ritase",
		Title:     title,
		Columns: []export.Column{
			{Header: "Deskripsi", Width: 110},
			{Header: "Nilai", Width: 70, Center: true},
			{Header: "Satuan", Width: 45},
		},
	}
	for _, row := range view.Rows {
		line := export.ReportRow{Cells: []string{row.Label, row.Value, row.Satuan}}
		if row.Head {
			line.Kind = export.RowGroup
		}
		if row.Child {
			line.Indent = 1
		}
		// The value takes the row's tone, and the unit too where the page
		// colours it: the busy hour and the break.
		tone := pageTone(row.Tone)
		line.Tones = []export.Tone{export.ToneNone, tone, export.ToneNone}
		if row.Tone == "peak" || row.Tone == "muted" {
			line.Tones[2] = tone
		}
		section.Rows = append(section.Rows, line)
	}
	section.Legend = []export.LegendItem{
		{Tone: export.ToneGood, Label: "Hijau", Text: "Vendor aktif"},
		{Tone: export.ToneBad, Label: "Merah", Text: "Tidak ada vendor aktif / unit 0"},
		{Tone: export.TonePeak, Label: "Kuning", Text: "Jam puncak"},
	}
	section.Notes = []string{
		"Sumber: tiket Widya Load Scanner, dihitung berdasarkan jam scan penuh pada rentang yang dipilih " +
			"(maksimal 62 hari). Lebih dari satu hari, jam operasi dan jam puncak dibaca sebagai jam dalam sehari. " +
			"Jenis DT dari rata-rata volume per rit: DTK di bawah 14 m³, DTE 14–20 m³, DTT di atas 20 m³. " +
			"Rest 1 ditetapkan pukul 12:00–13:00.",
	}
	return section
}

// pageTone is the export tone for a tone the overview's summary table sets.
func pageTone(tone string) export.Tone {
	switch tone {
	case "good":
		return export.ToneGood
	case "bad":
		return export.ToneBad
	case "peak":
		return export.TonePeak
	case "accent":
		return export.ToneAccent
	case "muted":
		return export.ToneMuted
	}
	return export.ToneNone
}

func rekapSection(view *RekapView, title string) export.Section {
	section := export.Section{
		SheetName: "Rekap Vendor & Armada",
		Title:     title,
		Columns: []export.Column{
			{Header: "Vendor / Armada", Width: 70},
			{Header: "Unit", Width: 22, Numeric: true},
			{Header: "Rit", Width: 22, Numeric: true},
			{Header: "Volume (m³)", Width: 32, Numeric: true, Decimals: 2},
			{Header: "Porsi Vol", Width: 28, Numeric: true, Percent: true},
			{Header: "Avg m³/rit", Width: 30, Numeric: true, Decimals: 2},
			{Header: "Rit/unit", Width: 28, Numeric: true, Decimals: 2},
		},
	}
	for _, row := range view.Rows {
		cells := []string{row.Label, row.Unit, row.Rit, row.Volume, row.Porsi, row.Avg, row.RitUnit}
		line := export.ReportRow{Cells: cells, Values: []interface{}{
			row.Label, countValue(row.Unit), countValue(row.Rit), decimalValue(row.Volume),
			percentValue(row.Porsi), decimalValue(row.Avg), decimalValue(row.RitUnit),
		}}
		switch {
		case row.Vendor:
			line.Kind = export.RowGroup
			if row.Bad {
				// A vendor that hauled nothing reads red across the line.
				line.Tones = make([]export.Tone, len(cells))
				for i := range line.Tones {
					line.Tones[i] = export.ToneBadText
				}
			}
		case row.Total:
			line.Kind = export.RowTotal
		case row.Muted:
			line.Kind, line.Indent = export.RowMuted, 1
		default:
			line.Indent = 1
		}
		section.Rows = append(section.Rows, line)
	}
	section.Legend = []export.LegendItem{
		{Tone: export.ToneGroup, Label: "Vendor", Text: "Baris subtotal per vendor"},
		{Tone: export.TonePlain, Label: "Armada", Text: "Rincian per jenis DT (abu-abu = tidak ada ritase)"},
		{Tone: export.ToneBad, Label: "Merah", Text: "Vendor tidak aktif"},
	}
	section.Notes = []string{
		"Unit dihitung sekali per vendor; truk yang bekerja untuk dua vendor muncul di keduanya, " +
			"tetapi dihitung sekali di total.",
	}
	return section
}

// detailSection lays the unit detail out day after day. Every day has the
// same per-rit columns, as many as the busiest truck of the range made, so
// the sheet stays one table.
func (s *Server) detailSection(days []widya.UnitDetailReport, maxRit int, from, to string, summary widya.RitaseSummary) export.Section {
	columns := []export.Column{
		{Header: "No", Width: 9, Numeric: true},
		{Header: "Unit", Width: 26},
		{Header: "Kategori", Width: 16},
		{Header: "Rit", Width: 10, Numeric: true},
		{Header: "Volume (m³)", Width: 18, Numeric: true, Decimals: 2},
		{Header: "Avg m³/rit", Width: 16, Numeric: true, Decimals: 2},
		{Header: "Porsi Vol", Width: 15, Numeric: true, Percent: true},
	}
	ritWidth := (281.0 - 128.0) / float64(maxRit)
	if ritWidth > 14 {
		ritWidth = 14
	}
	for column := 1; column <= maxRit; column++ {
		columns = append(columns, export.Column{Header: fmt.Sprintf("Rit %d", column), Width: ritWidth})
	}
	columns = append(columns, export.Column{Header: "Interval rata-rata (menit)", Width: 18, Numeric: true})
	section := export.Section{
		SheetName: "Detail Unit per Vendor",
		Title:     s.rangeTitle("Detail Unit per Vendor", from, to),
		Columns:   columns,
	}
	tail := maxRit + 1
	for _, report := range days {
		view := detailView(report, s.location, s.rangeTitle("", report.Day, report.Day))
		for _, row := range view.Rows {
			switch {
			case row.Caption:
				section.Rows = append(section.Rows, export.ReportRow{Kind: export.RowCaption, Cells: []string{row.Label}})
			case row.Vendor, row.Total:
				label := row.Label
				kind := export.RowGroup
				if row.Total {
					// The day is named by the titles over its blocks.
					kind = export.RowTotal
					label = "TOTAL"
				}
				// The note runs across the rit columns; the interval keeps its own.
				cells := []string{"", label, row.Kategori, row.Rit, row.Volume, row.Avg, row.Porsi, row.Note, row.Interval}
				values := []interface{}{"", label, row.Kategori, countValue(row.Rit), decimalValue(row.Volume),
					decimalValue(row.Avg), percentValue(row.Porsi), row.Note, countValue(row.Interval)}
				section.Rows = append(section.Rows, export.ReportRow{Kind: kind, Cells: cells, Values: values, SpanTail: maxRit, SpanKeep: 1})
			default:
				cells := []string{strconv.Itoa(row.No), row.Label, row.Kategori, row.Rit, row.Volume, row.Avg, row.Porsi}
				values := []interface{}{row.No, row.Label, row.Kategori, countValue(row.Rit), decimalValue(row.Volume),
					decimalValue(row.Avg), percentValue(row.Porsi)}
				for column := 0; column < maxRit; column++ {
					clock := ""
					if column < len(row.Times) {
						clock = row.Times[column].Clock
					}
					cells = append(cells, clock)
					values = append(values, clock)
				}
				cells = append(cells, row.Interval)
				values = append(values, countValue(row.Interval))
				// A load in the midday break is amber, a truck with one load
				// all day has its count in red, as on the page.
				tones := make([]export.Tone, len(cells))
				if row.Single {
					tones[3] = export.ToneBadText
				}
				for column, cell := range row.Times {
					if cell.Rest && 7+column < len(tones) {
						tones[7+column] = export.TonePeak
					}
				}
				section.Rows = append(section.Rows, export.ReportRow{Cells: cells, Values: values, Tones: tones})
			}
		}
	}
	// Over more than one day, the range closes with its own total.
	if len(days) > 1 {
		label := "TOTAL RENTANG"
		note := "Jam operasi " + summary.FirstScan + " – " + summary.LastScan
		volume := formatRitaseVolume(summary.TotalVolume)
		avg := formatRitaseVolume(summary.AvgVolume)
		section.Rows = append(section.Rows, export.ReportRow{
			Kind:     export.RowTotal,
			Cells:    []string{"", label, fmt.Sprintf("%d unit", summary.UnitAktif), formatCount(summary.TotalRitase), volume, avg, "100.0%", note},
			Values:   []interface{}{"", label, fmt.Sprintf("%d unit", summary.UnitAktif), summary.TotalRitase, summary.TotalVolume, summary.AvgVolume, 1.0, note},
			SpanTail: tail,
		})
	}
	section.Legend = []export.LegendItem{
		{Tone: export.TonePeak, Label: "Kuning", Text: "Rit yang tercatat saat Rest 1 (12:00–13:00)"},
		{Tone: export.ToneBad, Label: "Merah", Text: "Unit dengan hanya 1 rit"},
	}
	section.Notes = []string{
		"Jam per rit berdasarkan jam scan penuh. Interval rata-rata = (rit terakhir − rit pertama) ÷ (jumlah rit − 1), " +
			"termasuk waktu istirahat dan antrean, jadi bukan cycle time murni. Jenis DT dari rata-rata muatan unit pada hari itu.",
	}
	return section
}

// ritSection lists every load of the range, day by day under a title naming
// the day, each day closing with its total.
func (s *Server) ritSection(days []widya.UnitDetailReport, from, to string, summary widya.RitaseSummary) export.Section {
	section := export.Section{
		SheetName: "Detail Rit per Unit",
		Title:     s.rangeTitle("Detail Rit per Unit", from, to),
		Columns: []export.Column{
			{Header: "Vendor / Unit / Waktu", Width: 90},
			{Header: "Rit ke", Width: 25, Numeric: true},
			{Header: "Volume (m³)", Width: 35, Numeric: true, Decimals: 2},
			{Header: "Kategori", Width: 30, Center: true},
		},
	}
	for _, report := range days {
		section.Rows = append(section.Rows, export.ReportRow{Kind: export.RowCaption,
			Cells: []string{s.rangeTitle("Ritase", report.Day, report.Day)}})
		for _, row := range ritView(report, s.location).Rows {
			line := export.ReportRow{
				Cells:  []string{row.Label, row.Rit, row.Volume, row.Kategori},
				Values: []interface{}{row.Label, countValue(row.Rit), decimalValue(row.Volume), row.Kategori},
			}
			switch row.Kind {
			case "vendor":
				line.Kind = export.RowGroup
			case "unit":
				line.Kind, line.Indent = export.RowSub, 1
			case "load":
				line.Indent = 2
			case "total":
				line.Kind = export.RowTotal
			}
			section.Rows = append(section.Rows, line)
		}
	}
	if len(days) > 1 {
		label := "TOTAL RENTANG"
		units := fmt.Sprintf("%d unit", summary.UnitAktif)
		section.Rows = append(section.Rows, export.ReportRow{
			Kind:   export.RowTotal,
			Cells:  []string{label, formatCount(summary.TotalRitase), formatRitaseVolume(summary.TotalVolume), units},
			Values: []interface{}{label, summary.TotalRitase, summary.TotalVolume, units},
		})
	}
	section.Notes = []string{"Jam sampai detik berdasarkan jam scan penuh."}
	return section
}

// countValue reads a count the page wrote grouped ("1.234") back into a
// number, so the spreadsheet can sum it. A dash or a blank stays as it is.
func countValue(text string) interface{} {
	value, err := strconv.Atoi(strings.ReplaceAll(strings.TrimSpace(text), ".", ""))
	if err != nil {
		return text
	}
	return value
}

// decimalValue reads a figure written with two decimals ("807.52").
func decimalValue(text string) interface{} {
	value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return text
	}
	return value
}

// percentValue reads "82.2%" as the fraction the spreadsheet formats as one.
func percentValue(text string) interface{} {
	value, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(text), "%"), 64)
	if err != nil {
		return text
	}
	return value / 100
}

// produksiNewExportQuery is a download link carrying the table, the range and
// the vendor; no part is every table. Everything is encoded here, so the
// address is safe to hand the template as it stands.
func produksiNewExportQuery(format, part, from, to string, vendors []string) template.URL {
	query := url.Values{}
	query.Set("format", format)
	if part != "" {
		query.Set("part", part)
	}
	query.Set("from", from)
	query.Set("to", to)
	for _, vendor := range vendors {
		query.Add("vendor", vendor)
	}
	return template.URL("/produksi-new/export/download?" + query.Encode())
}
