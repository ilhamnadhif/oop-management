package handler

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/service"
)

// The A2B Baru export: the three tables of the overview - the purchase
// orders, the ratio per unit, every dispense - as a workbook or a signed PDF,
// over a range of dates and a set of units.

// A2BBaruExportPageData drives the export page.
type A2BBaruExportPageData struct {
	ShellPageData
	Aktif  bool
	From   string
	To     string
	Picker UnitPicker
	// The figures the range and units hold, so the page says what a download
	// would carry before it is taken.
	Orders  string
	Fills   string
	Keluar  string
	Empty   bool
	Error   string
	Filter  A2BBaruExportFilter
	Parts   []ProduksiNewDownload
	Reset   string
	Applied bool
}

// A2BBaruExportFilter is the range and units a download is for.
type A2BBaruExportFilter struct {
	From, To string
	Units    []string
}

// a2bBaruPart is one table the export can hand out alone. Index is its place
// among the sections a2bBaruSections lays out.
type a2bBaruPart struct {
	Key   string
	Index int
	Label string
	Hint  string
}

var a2bBaruParts = []a2bBaruPart{
	{Key: "fuel-masuk", Index: 0, Label: "Fuel Masuk", Hint: "Purchase order, diterima site dan deviasinya. Filter unit tidak berlaku: PO tidak terikat unit."},
	{Key: "ratio", Index: 1, Label: "Ratio Fuel per Unit", Hint: "BBM keluar, BBM untuk ratio, jam kerja dan ratio rata-rata, juga per bulan."},
	{Key: "fuel-keluar", Index: 2, Label: "Fuel Keluar & HM", Hint: "Setiap pengisian dengan HM A2B, flow meter, jam kerja, operator dan ratio."},
}

func a2bBaruPartFor(key string) (a2bBaruPart, bool) {
	for _, part := range a2bBaruParts {
		if part.Key == key {
			return part, true
		}
	}
	return a2bBaruPart{}, false
}

// a2bBaruExportFilterFrom reads the range and units. A date that is not one is
// dropped and a range given backwards is turned round, as on the overview.
func a2bBaruExportFilterFrom(query url.Values) A2BBaruExportFilter {
	own := a2bBaruRange(url.Values{"x_from": query["from"], "x_to": query["to"], "x_unit": query["unit"]}, "x")
	return A2BBaruExportFilter{From: own.From, To: own.To, Units: own.Units}
}

func (f A2BBaruExportFilter) overview() service.A2BBaruOverviewFilter {
	dates := service.A2BBaruRange{From: f.From, To: f.To}
	withUnits := service.A2BBaruRange{From: f.From, To: f.To, Units: f.Units}
	return service.A2BBaruOverviewFilter{Orders: dates, Ratios: withUnits, Fills: withUnits}
}

func a2bBaruExportQuery(format, part string, filter A2BBaruExportFilter) template.URL {
	query := url.Values{}
	query.Set("format", format)
	if part != "" {
		query.Set("part", part)
	}
	if filter.From != "" {
		query.Set("from", filter.From)
	}
	if filter.To != "" {
		query.Set("to", filter.To)
	}
	for _, unit := range filter.Units {
		query.Add("unit", unit)
	}
	return template.URL("/a2b-baru/export/download?" + query.Encode())
}

func (s *Server) handleA2BBaruExport(w http.ResponseWriter, r *http.Request) {
	bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-export")
	if !ok {
		return
	}
	filter := a2bBaruExportFilterFrom(r.URL.Query())
	data := A2BBaruExportPageData{
		ShellPageData: bound.shellData(user, sessionValue, "a2b-baru-export"),
		Aktif:         bound.exportAktif(model.ExportA2BBaru),
		From:          filter.From, To: filter.To, Filter: filter,
		Applied: filter.From != "" || filter.To != "" || len(filter.Units) > 0,
	}
	overview, err := bound.a2bBaru.Overview(r.Context(), filter.overview())
	if err != nil {
		log.Printf("read a2b baru for export: %v", err)
		data.Error = "Data A2B Baru gagal dimuat"
	}
	register := make([]model.UnitA2B, 0, len(overview.UnitList))
	for _, unit := range overview.UnitList {
		register = append(register, model.UnitA2B{IDUnit: unit.IDUnit, NamaUnit: unit.NamaUnit})
	}
	data.Picker = unitPickerFor("unit", register, filter.Units)
	keluar := 0.0
	for _, fill := range overview.Fills {
		keluar += fill.FuelLiter
	}
	data.Orders = formatCount(len(overview.Orders))
	data.Fills = formatCount(len(overview.Fills))
	data.Keluar = formatDecimal(keluar, 2)
	data.Empty = len(overview.Orders) == 0 && len(overview.Fills) == 0
	for _, part := range a2bBaruParts {
		data.Parts = append(data.Parts, ProduksiNewDownload{Label: part.Label, Hint: part.Hint,
			XLSX: a2bBaruExportQuery("xlsx", part.Key, filter), PDF: a2bBaruExportQuery("pdf", part.Key, filter)})
	}
	data.Parts = append(data.Parts, ProduksiNewDownload{
		Label: "Semua bagian", Hint: "Ketiga tabel di atas dalam satu berkas: sheet per bagian di XLSX, halaman per bagian di PDF.",
		XLSX: a2bBaruExportQuery("xlsx", "", filter), PDF: a2bBaruExportQuery("pdf", "", filter),
	})
	bound.render(w, "a2b_baru_export", data, http.StatusOK)
}

// handleA2BBaruExportDownload streams the tables for a range and units: one
// when the part asks for one, otherwise all three.
func (s *Server) handleA2BBaruExportDownload(w http.ResponseWriter, r *http.Request) {
	bound, _, _, ok := s.requireAccess(w, r, "a2b-baru-export")
	if !ok {
		return
	}
	if !bound.requireExportAktif(w, model.ExportA2BBaru) {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	filter := a2bBaruExportFilterFrom(r.URL.Query())
	overview, err := bound.a2bBaru.Overview(r.Context(), filter.overview())
	if err != nil {
		log.Printf("read a2b baru for export: %v", err)
		http.Error(w, "Data A2B Baru gagal dimuat", http.StatusInternalServerError)
		return
	}

	units := a2bBaruUnitLabel(filter.Units)
	sections := a2bBaruSections(overview, units)
	title := "Laporan Fuel A2B Baru · " + units
	filename := "fuel-a2b-baru"
	if part, one := a2bBaruPartFor(r.URL.Query().Get("part")); one {
		sections = sections[part.Index : part.Index+1]
		title = part.Label + " · " + units
		filename += "-" + part.Key
	}
	meta := bound.exportMetaFor(model.ExportA2BBaru, title, filter.From, filter.To)
	var payload []byte
	if format == "xlsx" {
		payload, err = export.ReportXLSX(sections, meta)
	} else {
		payload, err = export.ReportPDF(sections, meta)
	}
	if err != nil {
		log.Printf("build a2b baru %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}
	contentType := "application/pdf"
	if format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	filename += unitFilenameSuffix(filter.Units) + "-" + a2bBaruPeriodSlug(filter) + "." + format
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

func a2bBaruUnitLabel(units []string) string {
	switch len(units) {
	case 0:
		return "Semua unit"
	case 1, 2:
		return strings.Join(units, ", ")
	default:
		return fmt.Sprintf("%d unit", len(units))
	}
}

func a2bBaruPeriodSlug(filter A2BBaruExportFilter) string {
	switch {
	case filter.From != "" && filter.To != "":
		if filter.From == filter.To {
			return filter.From
		}
		return filter.From + "_" + filter.To
	case filter.From != "":
		return "sejak-" + filter.From
	case filter.To != "":
		return "sampai-" + filter.To
	default:
		return "semua"
	}
}

// a2bBaruSections lays the three tables out, in the order of a2bBaruParts.
func a2bBaruSections(overview service.A2BBaruOverview, units string) []export.Section {
	return []export.Section{
		a2bBaruOrderSection(overview),
		a2bBaruRatioSection(overview, units),
		a2bBaruFillSection(overview, units),
	}
}

func a2bBaruOrderSection(overview service.A2BBaruOverview) export.Section {
	money := func(header string, width float64) export.Column {
		return export.Column{Header: header, Width: width, Numeric: true, Money: true}
	}
	section := export.Section{
		SheetName: "Fuel Masuk",
		Title:     "Fuel Masuk: Purchase Order dan Diterima Site",
		Columns: []export.Column{
			{Header: "Phase", Width: 14}, {Header: "Tanggal", Width: 20}, {Header: "No. PO", Width: 20},
			money("PO Vol. (L)", 16), money("PO Harga Satuan", 20), money("PO Jumlah Harga", 26),
			money("Site Vol. (L)", 16), money("Site Harga Satuan", 20), money("Site Jumlah Harga", 26),
			money("Deviasi Vol. (L)", 18), money("Deviasi Harga Satuan", 22), money("Deviasi Jumlah Harga", 26),
		},
	}
	line := func(kind export.RowKind, label, tanggal, po string, values ...float64) export.ReportRow {
		row := export.ReportRow{Kind: kind, Cells: []string{label, tanggal, po}, Values: []interface{}{label, tanggal, po}}
		for _, value := range values {
			row.Cells = append(row.Cells, formatWhole(value))
			row.Values = append(row.Values, math.Round(value))
		}
		return row
	}
	for _, order := range overview.Orders {
		devHarga := 0.0
		if order.DeviasiVolume() != 0 {
			devHarga = order.DeviasiJumlah() / order.DeviasiVolume()
		}
		section.Rows = append(section.Rows, line(export.RowPlain, order.Phase, dateOnlyLabel(order.Tanggal), order.NoPO,
			order.POVolume, order.POHargaSatuan, order.POJumlah(),
			order.SiteVolume, order.SiteHargaSatuan, order.SiteJumlah(),
			order.DeviasiVolume(), devHarga, order.DeviasiJumlah()))
	}
	totals := overview.Totals
	section.Rows = append(section.Rows, line(export.RowTotal, "TOTAL", "", "",
		totals.POVolume, totals.POHarga(), totals.POJumlah,
		totals.SiteVolume, totals.SiteHarga(), totals.SiteJumlah,
		totals.DevVolume, totals.DevHarga(), totals.DevJumlah))
	if totals.POVolume > 0 {
		section.Notes = append(section.Notes, fmt.Sprintf("Diterima site %s dari volume PO, deviasi %s.",
			formatPercent(totals.SiteVolume/totals.POVolume*100), formatPercent(totals.DevVolume/totals.POVolume*100)))
	}
	section.Notes = append(section.Notes,
		"Jumlah harga = volume x harga satuan. Deviasi = PO dikurangi diterima site; harga satuan deviasi = jumlah deviasi / volume deviasi.")
	return section
}

func a2bBaruRatioSection(overview service.A2BBaruOverview, units string) export.Section {
	section := export.Section{
		SheetName: "Ratio Fuel per Unit",
		Title:     "Ratio Fuel Rata-rata per Unit · " + units,
		Columns: []export.Column{
			{Header: "ID Unit", Width: 20}, {Header: "Nama Unit", Width: 42},
			{Header: "Total BBM Keluar (L)", Width: 22, Numeric: true, Decimals: 2},
			{Header: "Total BBM untuk Ratio (L)", Width: 24, Numeric: true, Decimals: 2},
			{Header: "Total Jam Kerja", Width: 20, Numeric: true, Decimals: 1},
			{Header: "Ratio Rata-rata (L/jam)", Width: 22, Numeric: true, Decimals: 2},
		},
	}
	// The months share what is left of the landscape page, at most the width
	// they read well in.
	monthWidth := 16.0
	if len(overview.Months) > 0 {
		monthWidth = math.Min(16, (281-150)/float64(len(overview.Months)))
	}
	for _, month := range overview.Months {
		section.Columns = append(section.Columns, export.Column{Header: shortMonthLabel(month), Width: monthWidth, Numeric: true, Decimals: 2})
	}
	ratioCell := func(value float64, has bool) (string, interface{}) {
		if !has {
			return "-", nil
		}
		return formatDecimal(value, 2), round2(value)
	}
	var keluar, forRatio, jam float64
	for _, unit := range overview.Ratios {
		row := export.ReportRow{
			Cells:  []string{unit.IDUnit, unit.NamaUnit, formatDecimal(unit.FuelKeluar, 2), formatDecimal(unit.FuelRatio, 2), formatDecimal(unit.JamKerja, 1)},
			Values: []interface{}{unit.IDUnit, unit.NamaUnit, round2(unit.FuelKeluar), round2(unit.FuelRatio), round2(unit.JamKerja)},
		}
		text, value := ratioCell(unit.Ratio, unit.HasRatio)
		row.Cells, row.Values = append(row.Cells, text), append(row.Values, value)
		for _, month := range unit.Monthly {
			text, value := ratioCell(month.Ratio, month.HasRatio)
			row.Cells, row.Values = append(row.Cells, text), append(row.Values, value)
		}
		// A value left nil writes the printed "-" into the sheet.
		for index, value := range row.Values {
			if value == nil {
				row.Values[index] = "-"
			}
		}
		keluar += unit.FuelKeluar
		forRatio += unit.FuelRatio
		jam += unit.JamKerja
		section.Rows = append(section.Rows, row)
	}
	total := export.ReportRow{Kind: export.RowTotal,
		Cells:  []string{"TOTAL", "", formatDecimal(keluar, 2), formatDecimal(forRatio, 2), formatDecimal(jam, 1)},
		Values: []interface{}{"TOTAL", "", round2(keluar), round2(forRatio), round2(jam)},
	}
	text, value := ratioCell(forRatio/math.Max(jam, 1e-9), jam > 0)
	if value == nil {
		value = "-"
	}
	total.Cells, total.Values = append(total.Cells, text), append(total.Values, value)
	for range overview.Months {
		total.Cells, total.Values = append(total.Cells, ""), append(total.Values, "")
	}
	section.Rows = append(section.Rows, total)
	section.Notes = []string{
		"BBM untuk ratio hanya pengisian yang punya jam kerja; ratio = BBM untuk ratio / jam kerja. Unit tanpa HM seperti genset tidak punya ratio.",
	}
	return section
}

func a2bBaruFillSection(overview service.A2BBaruOverview, units string) export.Section {
	section := export.Section{
		SheetName: "Fuel Keluar & HM",
		Title:     "Fuel Keluar & HM · " + units,
		Columns: []export.Column{
			{Header: "No", Width: 9, Numeric: true}, {Header: "No Transaksi", Width: 36}, {Header: "Tanggal", Width: 19},
			{Header: "ID Unit", Width: 18}, {Header: "Nama Unit", Width: 38},
			{Header: "HM Awal A2B", Width: 16, Numeric: true, Decimals: 1}, {Header: "HM Akhir A2B", Width: 16, Numeric: true, Decimals: 1},
			{Header: "HM Awal FM", Width: 16, Numeric: true}, {Header: "HM Akhir FM", Width: 16, Numeric: true},
			{Header: "Fuel (L)", Width: 14, Numeric: true, Decimals: 2}, {Header: "HM A2B Saat Isi", Width: 18, Numeric: true, Decimals: 1},
			{Header: "Jam Kerja", Width: 13, Numeric: true, Decimals: 1}, {Header: "Operator", Width: 20},
			{Header: "Ratio Fuel", Width: 13, Numeric: true, Decimals: 2},
		},
	}
	optional := func(value *float64, decimals int) (string, interface{}) {
		if value == nil {
			return "-", "-"
		}
		return formatDecimal(*value, decimals), round2(*value)
	}
	total := 0.0
	for index, fill := range overview.Fills {
		total += fill.FuelLiter
		row := export.ReportRow{
			Cells:  []string{strconv.Itoa(index + 1), fill.FuelOutID, dateOnlyLabel(fill.Tanggal), fill.IDUnit, fill.NamaUnit},
			Values: []interface{}{index + 1, fill.FuelOutID, dateOnlyLabel(fill.Tanggal), fill.IDUnit, fill.NamaUnit},
		}
		for _, reading := range []struct {
			value    *float64
			decimals int
		}{{fill.HMAwalA2B, 1}, {fill.HMAkhirA2B, 1}, {fill.HMAwalFM, 0}, {fill.HMAkhirFM, 0}} {
			text, value := optional(reading.value, reading.decimals)
			row.Cells, row.Values = append(row.Cells, text), append(row.Values, value)
		}
		row.Cells, row.Values = append(row.Cells, formatDecimal(fill.FuelLiter, 2)), append(row.Values, round2(fill.FuelLiter))
		text, value := optional(fill.HMSaatIsi, 1)
		row.Cells, row.Values = append(row.Cells, text), append(row.Values, value)
		operator := fill.Operator
		if operator == "" {
			operator = "-"
		}
		row.Cells = append(row.Cells, formatDecimal(fill.JamKerja, 1), operator)
		row.Values = append(row.Values, round2(fill.JamKerja), operator)
		if ratio, ok := fill.Ratio(); ok {
			row.Cells, row.Values = append(row.Cells, formatDecimal(ratio, 2)), append(row.Values, round2(ratio))
		} else {
			row.Cells, row.Values = append(row.Cells, "-"), append(row.Values, "-")
		}
		section.Rows = append(section.Rows, row)
	}
	blank := func(n int) []string { return make([]string, n) }
	totalCells := append(append([]string{"", "TOTAL"}, blank(7)...), formatDecimal(total, 2))
	totalCells = append(totalCells, blank(4)...)
	totalValues := make([]interface{}, len(totalCells))
	for index, cell := range totalCells {
		totalValues[index] = cell
	}
	totalValues[9] = round2(total)
	section.Rows = append(section.Rows, export.ReportRow{Kind: export.RowTotal, Cells: totalCells, Values: totalValues})
	section.Notes = []string{
		"Urut dari pengisian terlama. Ratio fuel = fuel / jam kerja; pengisian tanpa jam kerja tidak punya ratio.",
	}
	return section
}

func round2(value float64) float64 { return math.Round(value*100) / 100 }
