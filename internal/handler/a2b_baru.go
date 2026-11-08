package handler

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// A2B Baru is the fuel book kept the way the site's own workbook keeps it. Its
// pages read and write the A2B Baru sheets only.

// a2bBaruNotice is what a page has to say about the last submission.
type a2bBaruNotice struct {
	errMessage string
	success    string
}

// a2bBaruPost checks a form submission the way every A2B Baru form needs: a
// signed-in active user who may reach the page in this project, a parsed form
// and a valid CSRF token. It writes the refusal itself when any fails.
func (s *Server) a2bBaruPost(w http.ResponseWriter, r *http.Request, key string) (*Server, *model.User, session.Session, bool) {
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return nil, nil, session.Session{}, false
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return nil, nil, session.Session{}, false
	}
	bound, sessionValue, ok := s.allowedIn(w, r, user, sessionValue, key)
	if !ok {
		return nil, nil, session.Session{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return nil, nil, session.Session{}, false
	}
	if !bound.sessions.ValidCSRFToken(r.FormValue("csrf_token"), sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return nil, nil, session.Session{}, false
	}
	return bound, user, sessionValue, true
}

// a2bBaruFailure turns a service error into what the page says and the status
// it answers with.
func a2bBaruFailure(action string, err error) (string, int) {
	if errors.Is(err, service.ErrValidation) {
		return strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity
	}
	log.Printf("%s: %v", action, err)
	return "Terjadi kesalahan saat menyimpan. Coba lagi.", http.StatusInternalServerError
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET, POST")
	w.WriteHeader(http.StatusMethodNotAllowed)
}

// ---- Unit ----

type A2BBaruUnitForm struct {
	IDUnit   string
	NamaUnit string
}

// A2BBaruUnitRow is one register line, numbered.
type A2BBaruUnitRow struct {
	No int
	model.A2BBaruUnit
}

type A2BBaruUnitPageData struct {
	ShellPageData
	Form    A2BBaruUnitForm
	Units   []A2BBaruUnitRow
	Error   string
	Success string
}

func (s *Server) handleA2BBaruUnit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-unit")
		if !ok {
			return
		}
		bound.renderA2BBaruUnit(w, r, user, sessionValue, A2BBaruUnitForm{}, a2bBaruNotice{}, http.StatusOK)
	case http.MethodPost:
		bound, user, sessionValue, ok := s.a2bBaruPost(w, r, "a2b-baru-unit")
		if !ok {
			return
		}
		form := A2BBaruUnitForm{IDUnit: strings.TrimSpace(r.FormValue("id_unit")), NamaUnit: strings.TrimSpace(r.FormValue("nama_unit"))}
		unit, err := bound.a2bBaru.CreateUnit(r.Context(), user, service.A2BBaruUnitInput{IDUnit: form.IDUnit, NamaUnit: form.NamaUnit})
		if err != nil {
			message, status := a2bBaruFailure("create unit a2b baru", err)
			bound.renderA2BBaruUnit(w, r, user, sessionValue, form, a2bBaruNotice{errMessage: message}, status)
			return
		}
		bound.renderA2BBaruUnit(w, r, user, sessionValue, A2BBaruUnitForm{},
			a2bBaruNotice{success: fmt.Sprintf("Unit %s (%s) terdaftar.", unit.IDUnit, unit.NamaUnit)}, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) renderA2BBaruUnit(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form A2BBaruUnitForm, notice a2bBaruNotice, status int) {
	data := A2BBaruUnitPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-baru-unit"),
		Form:          form, Error: notice.errMessage, Success: notice.success,
	}
	units, err := s.a2bBaru.Units(r.Context())
	if err != nil {
		log.Printf("list unit a2b baru: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat daftar unit"
		}
	}
	for index, unit := range units {
		data.Units = append(data.Units, A2BBaruUnitRow{No: index + 1, A2BBaruUnit: unit})
	}
	s.render(w, "a2b_baru_unit", data, status)
}

// ---- Fuel masuk ----

type A2BBaruFuelMasukForm struct {
	Phase           string
	Tanggal         string
	NoPO            string
	POVolume        string
	POHargaSatuan   string
	SiteVolume      string
	SiteHargaSatuan string
}

// A2BBaruOrderRow is one purchase order as the table prints it.
type A2BBaruOrderRow struct {
	Phase, Tanggal, NoPO              string
	POVolume, POHarga, POJumlah       string
	SiteVolume, SiteHarga, SiteJumlah string
	DevVolume, DevHarga, DevJumlah    string
	DevNegative                       bool
}

// A2BBaruOrderTable is the purchase orders with their totals on top and the
// share of the order that arrived, as the workbook heads it.
type A2BBaruOrderTable struct {
	Rows      []A2BBaruOrderRow
	Total     A2BBaruOrderRow
	SitePct   string
	DevPct    string
	HasOrders bool
}

type A2BBaruFuelMasukPageData struct {
	ShellPageData
	Form    A2BBaruFuelMasukForm
	Table   A2BBaruOrderTable
	Error   string
	Success string
}

func (s *Server) handleA2BBaruFuelMasuk(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-fuel-masuk")
		if !ok {
			return
		}
		bound.renderA2BBaruFuelMasuk(w, r, user, sessionValue, A2BBaruFuelMasukForm{}, a2bBaruNotice{}, http.StatusOK)
	case http.MethodPost:
		bound, user, sessionValue, ok := s.a2bBaruPost(w, r, "a2b-baru-fuel-masuk")
		if !ok {
			return
		}
		form := A2BBaruFuelMasukForm{
			Phase: strings.TrimSpace(r.FormValue("phase")), Tanggal: strings.TrimSpace(r.FormValue("tanggal")),
			NoPO:     strings.TrimSpace(r.FormValue("no_po")),
			POVolume: strings.TrimSpace(r.FormValue("po_volume")), POHargaSatuan: strings.TrimSpace(r.FormValue("po_harga")),
			SiteVolume: strings.TrimSpace(r.FormValue("site_volume")), SiteHargaSatuan: strings.TrimSpace(r.FormValue("site_harga")),
		}
		order, err := bound.a2bBaru.CreateFuelMasuk(r.Context(), user, service.A2BBaruFuelMasukInput(form))
		if err != nil {
			message, status := a2bBaruFailure("create fuel masuk a2b baru", err)
			bound.renderA2BBaruFuelMasuk(w, r, user, sessionValue, form, a2bBaruNotice{errMessage: message}, status)
			return
		}
		bound.renderA2BBaruFuelMasuk(w, r, user, sessionValue, A2BBaruFuelMasukForm{}, a2bBaruNotice{
			success: fmt.Sprintf("%s (%s) tersimpan: PO %s L, diterima %s L.",
				order.Phase, order.NoPO, formatWhole(order.POVolume), formatWhole(order.SiteVolume)),
		}, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) renderA2BBaruFuelMasuk(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form A2BBaruFuelMasukForm, notice a2bBaruNotice, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.a2bBaru.Today()
	}
	if form.Phase == "" {
		if phase, err := s.a2bBaru.NextPhase(r.Context()); err == nil {
			form.Phase = phase
		}
	}
	data := A2BBaruFuelMasukPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-baru-fuel-masuk"),
		Form:          form, Error: notice.errMessage, Success: notice.success,
	}
	overview, err := s.a2bBaru.Overview(r.Context())
	if err != nil {
		log.Printf("read a2b baru: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat daftar fuel masuk"
		}
	}
	data.Table = orderTable(overview)
	s.render(w, "a2b_baru_fuel_masuk", data, status)
}

func orderTable(overview service.A2BBaruOverview) A2BBaruOrderTable {
	table := A2BBaruOrderTable{HasOrders: len(overview.Orders) > 0}
	for _, order := range overview.Orders {
		row := A2BBaruOrderRow{
			Phase: order.Phase, Tanggal: dateOnlyLabel(order.Tanggal), NoPO: order.NoPO,
			POVolume: formatWhole(order.POVolume), POHarga: formatWhole(order.POHargaSatuan), POJumlah: formatWhole(order.POJumlah()),
			SiteVolume: formatWhole(order.SiteVolume), SiteHarga: formatWhole(order.SiteHargaSatuan), SiteJumlah: formatWhole(order.SiteJumlah()),
			DevVolume: formatWhole(order.DeviasiVolume()), DevJumlah: formatWhole(order.DeviasiJumlah()),
			DevNegative: order.DeviasiVolume() < 0, DevHarga: "-",
		}
		if order.DeviasiVolume() != 0 {
			row.DevHarga = formatWhole(order.DeviasiJumlah() / order.DeviasiVolume())
		}
		table.Rows = append(table.Rows, row)
	}
	totals := overview.Totals
	table.Total = A2BBaruOrderRow{
		POVolume: formatWhole(totals.POVolume), POHarga: formatWhole(totals.POHarga()), POJumlah: formatWhole(totals.POJumlah),
		SiteVolume: formatWhole(totals.SiteVolume), SiteHarga: formatWhole(totals.SiteHarga()), SiteJumlah: formatWhole(totals.SiteJumlah),
		DevVolume: formatWhole(totals.DevVolume), DevHarga: formatWhole(totals.DevHarga()), DevJumlah: formatWhole(totals.DevJumlah),
		DevNegative: totals.DevVolume < 0,
	}
	if totals.POVolume > 0 {
		table.SitePct = formatPercent(totals.SiteVolume / totals.POVolume * 100)
		table.DevPct = formatPercent(totals.DevVolume / totals.POVolume * 100)
	}
	return table
}

// ---- Fuel keluar ----

type A2BBaruFuelKeluarForm struct {
	Tanggal    string
	IDUnit     string
	HMAwalA2B  string
	HMAkhirA2B string
	HMAwalFM   string
	HMAkhirFM  string
	FuelLiter  string
	HMSaatIsi  string
	JamKerja   string
	Operator   string
}

// A2BBaruFillRow is one dispense as the tables print it.
type A2BBaruFillRow struct {
	No                                   int
	FuelOutID, Tanggal, IDUnit, NamaUnit string
	HMAwalA2B, HMAkhirA2B, HMAwalFM      string
	HMAkhirFM, Fuel, HMSaatIsi, JamKerja string
	Operator, Ratio                      string
}

// A2BBaruFillTable is the dispense list with its total, for the shared table.
type A2BBaruFillTable struct {
	Rows  []A2BBaruFillRow
	Total string
}

type A2BBaruFuelKeluarPageData struct {
	ShellPageData
	Form            A2BBaruFuelKeluarForm
	Units           []model.A2BBaruUnit
	OperatorOptions []string
	NextFuelOutID   string
	Fills           A2BBaruFillTable
	Error           string
	Success         string
}

func (s *Server) handleA2BBaruFuelKeluar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-fuel-keluar")
		if !ok {
			return
		}
		bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, A2BBaruFuelKeluarForm{}, a2bBaruNotice{}, http.StatusOK)
	case http.MethodPost:
		bound, user, sessionValue, ok := s.a2bBaruPost(w, r, "a2b-baru-fuel-keluar")
		if !ok {
			return
		}
		form := A2BBaruFuelKeluarForm{
			Tanggal: strings.TrimSpace(r.FormValue("tanggal")), IDUnit: strings.TrimSpace(r.FormValue("id_unit")),
			HMAwalA2B: strings.TrimSpace(r.FormValue("hm_awal_a2b")), HMAkhirA2B: strings.TrimSpace(r.FormValue("hm_akhir_a2b")),
			HMAwalFM: strings.TrimSpace(r.FormValue("hm_awal_fm")), HMAkhirFM: strings.TrimSpace(r.FormValue("hm_akhir_fm")),
			FuelLiter: strings.TrimSpace(r.FormValue("fuel_liter")), HMSaatIsi: strings.TrimSpace(r.FormValue("hm_saat_isi")),
			JamKerja: strings.TrimSpace(r.FormValue("jam_kerja")), Operator: strings.TrimSpace(r.FormValue("operator")),
		}
		fill, err := bound.a2bBaru.CreateFuelKeluar(r.Context(), user, service.A2BBaruFuelKeluarInput(form))
		if err != nil {
			message, status := a2bBaruFailure("create fuel keluar a2b baru", err)
			bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, form, a2bBaruNotice{errMessage: message}, status)
			return
		}
		bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, A2BBaruFuelKeluarForm{}, a2bBaruNotice{
			success: fmt.Sprintf("Fuel keluar %s tersimpan: %s L untuk %s.", fill.FuelOutID, formatDecimal(fill.FuelLiter, 2), fill.NamaUnit),
		}, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) renderA2BBaruFuelKeluar(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form A2BBaruFuelKeluarForm, notice a2bBaruNotice, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.a2bBaru.Today()
	}
	data := A2BBaruFuelKeluarPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-baru-fuel-keluar"),
		Form:          form, Error: notice.errMessage, Success: notice.success,
	}
	fail := func(message string, err error) {
		log.Printf("%s: %v", message, err)
		if data.Error == "" {
			data.Error = message
		}
	}
	units, err := s.a2bBaru.Units(r.Context())
	if err != nil {
		fail("Gagal memuat daftar unit", err)
	}
	data.Units = units
	if operators, err := s.a2bBaru.Operators(r.Context()); err == nil {
		data.OperatorOptions = operators
	}
	if next, err := s.a2bBaru.NextFuelOutID(r.Context()); err == nil {
		data.NextFuelOutID = next
	}
	// The pump's meter carries on from the last dispense; offered, not forced.
	if data.Form.HMAwalFM == "" {
		if last, err := s.a2bBaru.LastFlowMeter(r.Context()); err == nil && last > 0 {
			data.Form.HMAwalFM = formatLiter(last)
		}
	}
	fills, err := s.a2bBaru.FuelKeluar(r.Context())
	if err != nil {
		fail("Gagal memuat riwayat fuel keluar", err)
	}
	data.Fills.Rows, data.Fills.Total = fillRows(fills, true)
	s.render(w, "a2b_baru_fuel_keluar", data, status)
}

// fillRows prints the dispenses, newest first when asked, numbered in the
// order they were made, with their total.
func fillRows(fills []model.A2BBaruFuelKeluar, newestFirst bool) ([]A2BBaruFillRow, string) {
	rows := make([]A2BBaruFillRow, 0, len(fills))
	total := 0.0
	for index, fill := range fills {
		total += fill.FuelLiter
		row := A2BBaruFillRow{
			No: index + 1, FuelOutID: fill.FuelOutID, Tanggal: dateOnlyLabel(fill.Tanggal),
			IDUnit: fill.IDUnit, NamaUnit: fill.NamaUnit,
			HMAwalA2B: optionalDecimal(fill.HMAwalA2B, 1), HMAkhirA2B: optionalDecimal(fill.HMAkhirA2B, 1),
			HMAwalFM: optionalDecimal(fill.HMAwalFM, 0), HMAkhirFM: optionalDecimal(fill.HMAkhirFM, 0),
			Fuel: formatDecimal(fill.FuelLiter, 2), HMSaatIsi: optionalDecimal(fill.HMSaatIsi, 1),
			JamKerja: formatDecimal(fill.JamKerja, 1), Operator: fill.Operator, Ratio: "-",
		}
		if ratio, ok := fill.Ratio(); ok {
			row.Ratio = formatDecimal(ratio, 2)
		}
		if row.Operator == "" {
			row.Operator = "-"
		}
		rows = append(rows, row)
	}
	if newestFirst {
		for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
			rows[i], rows[j] = rows[j], rows[i]
		}
	}
	return rows, formatDecimal(total, 2)
}

// ---- Overview ----

type A2BBaruRatioRow struct {
	IDUnit, NamaUnit      string
	FuelKeluar, FuelRatio string
	JamKerja, Ratio       string
	Monthly               []string
}

type A2BBaruOverviewPageData struct {
	ShellPageData
	Error        string
	Units        string
	Stock        string
	StockLow     bool
	FuelKeluar   string
	FuelDiterima string
	Orders       A2BBaruOrderTable
	Months       []string
	Ratios       []A2BBaruRatioRow
	RatioTotal   A2BBaruRatioRow
	Fills        A2BBaruFillTable
}

func (s *Server) handleA2BBaruOverview(w http.ResponseWriter, r *http.Request) {
	bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-overview")
	if !ok {
		return
	}
	data := A2BBaruOverviewPageData{ShellPageData: bound.shellData(user, sessionValue, "a2b-baru-overview")}
	overview, err := bound.a2bBaru.Overview(r.Context())
	if err != nil {
		log.Printf("read a2b baru overview: %v", err)
		data.Error = "Data A2B Baru gagal dimuat"
	}
	data.Units = formatCount(overview.Units)
	data.Stock = formatDecimal(overview.Stock, 2)
	data.StockLow = overview.Stock < 0
	data.FuelKeluar = formatDecimal(overview.FuelKeluar, 2)
	data.FuelDiterima = formatDecimal(overview.FuelDiterima, 2)
	data.Orders = orderTable(overview)
	for _, month := range overview.Months {
		data.Months = append(data.Months, monthLabel(month))
	}
	var keluar, forRatio, jam float64
	for _, unit := range overview.Ratios {
		row := A2BBaruRatioRow{
			IDUnit: unit.IDUnit, NamaUnit: unit.NamaUnit,
			FuelKeluar: formatDecimal(unit.FuelKeluar, 2), FuelRatio: formatDecimal(unit.FuelRatio, 2),
			JamKerja: formatDecimal(unit.JamKerja, 1), Ratio: "-",
		}
		if unit.HasRatio {
			row.Ratio = formatDecimal(unit.Ratio, 2)
		}
		for _, month := range unit.Monthly {
			cell := ""
			if month.HasRatio {
				cell = formatDecimal(month.Ratio, 2)
			}
			row.Monthly = append(row.Monthly, cell)
		}
		keluar += unit.FuelKeluar
		forRatio += unit.FuelRatio
		jam += unit.JamKerja
		data.Ratios = append(data.Ratios, row)
	}
	data.RatioTotal = A2BBaruRatioRow{FuelKeluar: formatDecimal(keluar, 2), FuelRatio: formatDecimal(forRatio, 2),
		JamKerja: formatDecimal(jam, 1), Ratio: "-"}
	if jam > 0 {
		data.RatioTotal.Ratio = formatDecimal(forRatio/jam, 2)
	}
	data.Fills.Rows, data.Fills.Total = fillRows(overview.Fills, true)
	bound.render(w, "a2b_baru_overview", data, http.StatusOK)
}

// ---- Formatting ----

// formatDecimal writes a number the Indonesian way, 1.234,5, with at most the
// decimals given and none that are only zeros.
func formatDecimal(value float64, decimals int) string {
	scale := math.Pow(10, float64(decimals))
	rounded := math.Round(value*scale) / scale
	whole := math.Trunc(rounded)
	text := export.FormatMoney(whole)
	if whole == 0 && rounded < 0 {
		text = "-0"
	}
	fraction := math.Abs(rounded - whole)
	if decimals <= 0 || fraction*scale < 0.5 {
		return text
	}
	digits := fmt.Sprintf("%0*d", decimals, int64(math.Round(fraction*scale)))
	return text + "," + strings.TrimRight(digits, "0")
}

func formatWhole(value float64) string { return export.FormatMoney(value) }

func optionalDecimal(value *float64, decimals int) string {
	if value == nil {
		return "-"
	}
	return formatDecimal(*value, decimals)
}
