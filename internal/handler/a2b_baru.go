package handler

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
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
	// A form carrying a photo is given room for it; the rest stay small.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		maxBody := bound.maxUploadBytes + 64*1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := r.ParseMultipartForm(maxBody); err != nil {
			http.Error(w, "Form tidak valid atau foto terlalu besar", http.StatusRequestEntityTooLarge)
			return nil, nil, session.Session{}, false
		}
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Form tidak valid", http.StatusBadRequest)
			return nil, nil, session.Session{}, false
		}
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
	// Empty is what the table says when it has no rows.
	Empty string
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
	overview, err := s.a2bBaru.Overview(r.Context(), service.A2BBaruOverviewFilter{})
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
	table := A2BBaruOrderTable{HasOrders: len(overview.Orders) > 0, Empty: "Belum ada fuel masuk tercatat."}
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
	Tanggal   string
	IDUnit    string
	HMAwalFM  string
	HMAkhirFM string
	HMSaatIsi string
	JamKerja  string
	Operator  string
}

// A2BBaruFillRow is one dispense as the tables print it.
type A2BBaruFillRow struct {
	No                                   int
	FuelOutID, Tanggal, IDUnit, NamaUnit string
	HMAwalA2B, HMAkhirA2B, HMAwalFM      string
	HMAkhirFM, Fuel, HMSaatIsi, JamKerja string
	Operator, Ratio                      string
	PhotoURL                             string
}

// A2BBaruFillTable is the dispense list with its total, for the shared table.
type A2BBaruFillTable struct {
	Rows  []A2BBaruFillRow
	Total string
	// Pager is set on the overview, where the list is paged; the input page
	// shows every row.
	Pager *A2BBaruPager
	// Empty is what the table says when it has no rows.
	Empty string
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
	// Warning rides beside Success: the row was saved, but its hour meter is
	// worth a second look.
	Warning string
}

func (s *Server) handleA2BBaruFuelKeluar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-fuel-keluar")
		if !ok {
			return
		}
		bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, A2BBaruFuelKeluarForm{}, a2bBaruNotice{}, "", http.StatusOK)
	case http.MethodPost:
		bound, user, sessionValue, ok := s.a2bBaruPost(w, r, "a2b-baru-fuel-keluar")
		if !ok {
			return
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		form := A2BBaruFuelKeluarForm{
			Tanggal: strings.TrimSpace(r.FormValue("tanggal")), IDUnit: strings.TrimSpace(r.FormValue("id_unit")),
			HMAwalFM: strings.TrimSpace(r.FormValue("hm_awal_fm")), HMAkhirFM: strings.TrimSpace(r.FormValue("hm_akhir_fm")),
			HMSaatIsi: strings.TrimSpace(r.FormValue("hm_saat_isi")), JamKerja: strings.TrimSpace(r.FormValue("jam_kerja")),
			Operator: strings.TrimSpace(r.FormValue("operator")),
		}
		foto, err := bound.readOptionalPhoto(r, "foto_flow_meter")
		if err != nil {
			bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, form, a2bBaruNotice{errMessage: err.Error()}, "", http.StatusUnprocessableEntity)
			return
		}
		fill, warning, err := bound.a2bBaru.CreateFuelKeluar(r.Context(), user, service.A2BBaruFuelKeluarInput{
			Tanggal: form.Tanggal, IDUnit: form.IDUnit, HMAwalFM: form.HMAwalFM, HMAkhirFM: form.HMAkhirFM,
			HMSaatIsi: form.HMSaatIsi, JamKerja: form.JamKerja, Operator: form.Operator, Foto: foto,
		})
		if err != nil {
			message, status := a2bBaruFailure("create fuel keluar a2b baru", err)
			if errors.Is(err, service.ErrInvalidPhoto) {
				message, status = "Foto akhir flow meter tidak valid", http.StatusUnprocessableEntity
			}
			bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, form, a2bBaruNotice{errMessage: message}, "", status)
			return
		}
		bound.renderA2BBaruFuelKeluar(w, r, user, sessionValue, A2BBaruFuelKeluarForm{}, a2bBaruNotice{
			success: fmt.Sprintf("Fuel keluar %s tersimpan: %s liter untuk %s.", fill.FuelOutID, formatLiter(fill.FuelLiter), fill.NamaUnit),
		}, warning, http.StatusOK)
	default:
		methodNotAllowed(w)
	}
}

func (s *Server) renderA2BBaruFuelKeluar(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form A2BBaruFuelKeluarForm, notice a2bBaruNotice, warning string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.a2bBaru.Today()
	}
	data := A2BBaruFuelKeluarPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-baru-fuel-keluar"),
		Form:          form, Error: notice.errMessage, Success: notice.success, Warning: warning,
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
	// The pump's totaliser carries on from the last dispense: offered, and
	// left editable, since the number on the pump is the authority.
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
	data.Fills.Empty = "Belum ada fuel keluar tercatat."
	s.render(w, "a2b_baru_fuel_keluar", data, status)
}

func (s *Server) handleA2BBaruFuelKeluarPhoto(w http.ResponseWriter, r *http.Request) {
	bound, _, _, ok := s.requireAccess(w, r, "a2b-baru-fuel-keluar")
	if !ok {
		return
	}
	fuelOutID := strings.TrimSpace(r.URL.Query().Get("fuel_id"))
	dataURL, err := bound.a2bBaru.FuelKeluarPhoto(r.Context(), fuelOutID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrValidation) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read a2b baru photo: %v", err)
		http.Error(w, "Gagal memuat foto flow meter", http.StatusInternalServerError)
		return
	}
	payload, err := photo.DecodeDataURL(dataURL)
	if err != nil {
		log.Printf("decode a2b baru photo %s: %v", fuelOutID, err)
		http.Error(w, "Foto flow meter rusak", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fuelOutID+"-flowmeter.jpg"))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(payload)
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
		if fill.HasFoto {
			row.PhotoURL = "/a2b-baru/fuel-keluar/foto?fuel_id=" + url.QueryEscape(fill.FuelOutID)
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

// A2BBaruTableFilter is one table's filter form: what it holds, the other
// tables' filters to carry along so they are kept, and the address that
// clears this one alone.
type A2BBaruTableFilter struct {
	// Prefix names this table's fields: "fm", "ratio" or "fk".
	Prefix   string
	Anchor   string
	From     string
	To       string
	HasUnit  bool
	Active   bool
	Keep     []A2BBaruKeep
	ResetURL string
	// Picker is the unit filter: a list of ticks with a search over it, the
	// same one the A2B export uses, so several units can be picked at once.
	Picker UnitPicker
}

// A2BBaruKeep is one other filter's field, carried as a hidden input.
type A2BBaruKeep struct{ Name, Value string }

type A2BBaruOverviewPageData struct {
	ShellPageData
	Error        string
	OrderFilter  A2BBaruTableFilter
	RatioFilter  A2BBaruTableFilter
	FillFilter   A2BBaruTableFilter
	Units        string
	Stock        string
	StockLow     bool
	FuelKeluar   string
	FuelDiterima string
	Orders       A2BBaruOrderTable
	Months       []string
	Ratios       []A2BBaruRatioRow
	RatioTotal   A2BBaruRatioRow
	RatioEmpty   string
	Fills        A2BBaruFillTable
}

func (s *Server) handleA2BBaruOverview(w http.ResponseWriter, r *http.Request) {
	bound, user, sessionValue, ok := s.requireAccess(w, r, "a2b-baru-overview")
	if !ok {
		return
	}
	data := A2BBaruOverviewPageData{ShellPageData: bound.shellData(user, sessionValue, "a2b-baru-overview")}
	query := r.URL.Query()
	ranges := map[string]service.A2BBaruRange{}
	for _, prefix := range []string{"fm", "ratio", "fk"} {
		ranges[prefix] = a2bBaruRange(query, prefix)
	}
	ranges["fm"] = service.A2BBaruRange{From: ranges["fm"].From, To: ranges["fm"].To}
	overview, err := bound.a2bBaru.Overview(r.Context(), service.A2BBaruOverviewFilter{
		Orders: ranges["fm"], Ratios: ranges["ratio"], Fills: ranges["fk"],
	})
	// The fuel keluar table is paged. Filtering another table keeps its page
	// and size; filtering it keeps the size and starts again from page one.
	size, page := pagerSize(query.Get("fk_size")), pagerPage(query.Get("fk_page"))
	paging := url.Values{}
	if size != defaultPageSize {
		paging.Set("fk_size", strconv.Itoa(size))
	}
	sizeOnly := url.Values{}
	for key, values := range paging {
		sizeOnly[key] = values
	}
	if page > 1 {
		paging.Set("fk_page", strconv.Itoa(page))
	}
	data.OrderFilter = tableFilter("fm", "fuel-masuk", false, ranges, paging)
	data.RatioFilter = tableFilter("ratio", "ratio", true, ranges, paging)
	data.FillFilter = tableFilter("fk", "fuel-keluar", true, ranges, sizeOnly)
	register := make([]model.UnitA2B, 0, len(overview.UnitList))
	for _, unit := range overview.UnitList {
		register = append(register, model.UnitA2B{IDUnit: unit.IDUnit, NamaUnit: unit.NamaUnit})
	}
	data.RatioFilter.Picker = unitPickerFor("ratio_unit", register, ranges["ratio"].Units)
	data.FillFilter.Picker = unitPickerFor("fk_unit", register, ranges["fk"].Units)
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
		data.Months = append(data.Months, shortMonthLabel(month))
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
			// A month the unit logged no hours in reads "-", like any other
			// figure that has nothing to show.
			cell := "-"
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
	allRows, total := fillRows(overview.Fills, true)
	data.Fills.Total = total
	data.Fills.Pager, data.Fills.Rows = paginate(allRows, page, size, ranges)
	const noMatch = "Tidak ada data untuk filter ini."
	data.Fills.Empty = "Belum ada fuel keluar tercatat."
	if data.FillFilter.Active {
		data.Fills.Empty = noMatch
	}
	if data.OrderFilter.Active {
		data.Orders.Empty = noMatch
	}
	data.RatioEmpty = "Belum ada fuel keluar tercatat."
	if data.RatioFilter.Active {
		data.RatioEmpty = noMatch
	}
	bound.render(w, "a2b_baru_overview", data, http.StatusOK)
}

// a2bBaruRange reads one table's filter. A date that is not one is dropped,
// and a range given backwards is turned round.
func a2bBaruRange(query url.Values, prefix string) service.A2BBaruRange {
	date := func(field string) string {
		value := strings.TrimSpace(query.Get(prefix + "_" + field))
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return ""
		}
		return value
	}
	result := service.A2BBaruRange{From: date("from"), To: date("to")}
	for _, unit := range query[prefix+"_unit"] {
		if unit = strings.TrimSpace(unit); unit != "" {
			result.Units = append(result.Units, unit)
		}
	}
	if result.From != "" && result.To != "" && result.From > result.To {
		result.From, result.To = result.To, result.From
	}
	return result
}

// tableFilter lays one table's filter out, carrying every other table's so
// that filtering one leaves the rest as they were.
func tableFilter(prefix, anchor string, hasUnit bool, ranges map[string]service.A2BBaruRange, extra url.Values) A2BBaruTableFilter {
	own := ranges[prefix]
	filter := A2BBaruTableFilter{Prefix: prefix, Anchor: anchor, From: own.From, To: own.To, HasUnit: hasUnit,
		Active: own.From != "" || own.To != "" || len(own.Units) > 0}
	others := url.Values{}
	for _, other := range []string{"fm", "ratio", "fk"} {
		if other == prefix {
			continue
		}
		theirs := ranges[other]
		for field, value := range map[string]string{"from": theirs.From, "to": theirs.To} {
			if value != "" {
				others.Add(other+"_"+field, value)
			}
		}
		for _, unit := range theirs.Units {
			others.Add(other+"_unit", unit)
		}
	}
	for name, values := range extra {
		for _, value := range values {
			others.Add(name, value)
		}
	}
	names := make([]string, 0, len(others))
	for name := range others {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, value := range others[name] {
			filter.Keep = append(filter.Keep, A2BBaruKeep{Name: name, Value: value})
		}
	}
	filter.ResetURL = "/a2b-baru/overview"
	if encoded := others.Encode(); encoded != "" {
		filter.ResetURL += "?" + encoded
	}
	filter.ResetURL += "#" + anchor
	return filter
}

// ---- Paging ----

// pageSizes are the row counts a reader may page the fuel keluar list by.
var pageSizes = []int{10, 25, 50, 100}

const defaultPageSize = 10

func pagerSize(value string) int {
	size, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return defaultPageSize
	}
	for _, allowed := range pageSizes {
		if size == allowed {
			return size
		}
	}
	return defaultPageSize
}

func pagerPage(value string) int {
	page, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// A2BBaruPager is the paging bar under the fuel keluar list.
type A2BBaruPager struct {
	Page, Pages, Size int
	FirstRow, LastRow int
	Rows              int
	PrevURL, NextURL  string
	Links             []A2BBaruPageLink
	Sizes             []A2BBaruPageSize
}

// A2BBaruPageLink is one page number, or a gap between them.
type A2BBaruPageLink struct {
	Label   string
	URL     string
	Current bool
	Gap     bool
}

// A2BBaruPageSize is one choice of rows a page, as a link that starts again
// from the first page.
type A2BBaruPageSize struct {
	Size     int
	URL      string
	Selected bool
}

// paginate cuts one page out of the rows and lays out the bar that moves
// between pages. A page past the end shows the last one. Every address it
// builds keeps the page's filters.
func paginate(rows []A2BBaruFillRow, page, size int, ranges map[string]service.A2BBaruRange) (*A2BBaruPager, []A2BBaruFillRow) {
	pages := (len(rows) + size - 1) / size
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * size
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	pager := &A2BBaruPager{Page: page, Pages: pages, Size: size, Rows: len(rows), FirstRow: start + 1, LastRow: end}
	if len(rows) == 0 {
		pager.FirstRow = 0
	}

	filters := url.Values{}
	for _, prefix := range []string{"fm", "ratio", "fk"} {
		own := ranges[prefix]
		if own.From != "" {
			filters.Set(prefix+"_from", own.From)
		}
		if own.To != "" {
			filters.Set(prefix+"_to", own.To)
		}
		for _, unit := range own.Units {
			filters.Add(prefix+"_unit", unit)
		}
	}
	address := func(target, rows int) string {
		query := url.Values{}
		for key, values := range filters {
			query[key] = values
		}
		if rows != defaultPageSize {
			query.Set("fk_size", strconv.Itoa(rows))
		}
		if target > 1 {
			query.Set("fk_page", strconv.Itoa(target))
		}
		path := "/a2b-baru/overview"
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		return path + "#fuel-keluar"
	}
	link := func(target int) string { return address(target, size) }
	if page > 1 {
		pager.PrevURL = link(page - 1)
	}
	if page < pages {
		pager.NextURL = link(page + 1)
	}
	// The first and last pages, and two either side of this one, with a gap
	// where pages are skipped.
	last := 0
	for number := 1; number <= pages; number++ {
		if number != 1 && number != pages && (number < page-2 || number > page+2) {
			continue
		}
		if last != 0 && number > last+1 {
			pager.Links = append(pager.Links, A2BBaruPageLink{Label: "…", Gap: true})
		}
		pager.Links = append(pager.Links, A2BBaruPageLink{Label: strconv.Itoa(number), URL: link(number), Current: number == page})
		last = number
	}
	for _, allowed := range pageSizes {
		pager.Sizes = append(pager.Sizes, A2BBaruPageSize{Size: allowed, URL: address(1, allowed), Selected: allowed == size})
	}
	return pager, rows[start:end]
}

// ---- Formatting ----

var shortMonths = []string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

// shortMonthLabel writes "2026-08" as "Agu 2026", narrow enough for a column
// of figures.
func shortMonthLabel(month string) string {
	date, err := time.Parse("2006-01", month)
	if err != nil {
		return month
	}
	return fmt.Sprintf("%s %d", shortMonths[date.Month()-1], date.Year())
}

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
