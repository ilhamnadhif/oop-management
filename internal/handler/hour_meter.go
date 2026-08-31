package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

type HourMeterFormData struct {
	Tanggal   string
	Shift     string
	IDUnit    string
	Operator  string
	HMAwal    string
	HMAkhir   string
	FuelLiter string
	Standby   []service.HourMeterStandbyInput
	Breakdown []service.HourMeterBreakdownInput
	Remark    string
}

type HourMeterView struct {
	model.HourMeter
	TanggalLabel   string
	AwalLabel      string
	AkhirLabel     string
	TotalLabel     string
	FuelLabel      string
	StandbyLabel   string
	BreakdownLabel string
	PALabel        string
	BDLabel        string
	UALabel        string
	PAClass        string
	BDClass        string
	UAClass        string
}

type HourMeterPageData struct {
	ShellPageData
	Form               HourMeterFormData
	Units              []service.HourMeterUnitPick
	Options            service.HourMeterOptions
	StandbyVariables   []model.StandbyVariable
	BreakdownVariables []string
	NextHMID           string
	WorkMinutes        int
	IdleMinutes        float64
	PATarget           float64
	BDTarget           float64
	UATarget           float64
	RemarkMaxLength    int
	Rows               []HourMeterView
	Error              string
	Success            string
}

func (s *Server) handleA2BHourMeter(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleHourMeterPage(w, r)
	case http.MethodPost:
		s.handleHourMeterCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHourMeterPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-hm")
	if !ok {
		return
	}
	s.renderHourMeter(w, r, user, sessionValue, HourMeterFormData{}, "", "", http.StatusOK)
}

func (s *Server) handleHourMeterCreate(w http.ResponseWriter, r *http.Request) {
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return
	}
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "a2b-hm")
	if !okProject {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	form := HourMeterFormData{
		Tanggal:   strings.TrimSpace(r.FormValue("tanggal")),
		Shift:     strings.TrimSpace(r.FormValue("shift")),
		IDUnit:    strings.TrimSpace(r.FormValue("id_unit")),
		Operator:  strings.TrimSpace(r.FormValue("operator")),
		HMAwal:    strings.TrimSpace(r.FormValue("hm_awal")),
		HMAkhir:   strings.TrimSpace(r.FormValue("hm_akhir")),
		FuelLiter: strings.TrimSpace(r.FormValue("fuel_liter")),
		Standby:   standbyFromForm(r),
		Breakdown: breakdownFromForm(r),
		Remark:    strings.TrimSpace(r.FormValue("remark")),
	}
	reading, err := s.hourMeter.Create(r.Context(), user, service.HourMeterInput{
		Tanggal:   form.Tanggal,
		Shift:     form.Shift,
		IDUnit:    form.IDUnit,
		Operator:  form.Operator,
		HMAwal:    form.HMAwal,
		HMAkhir:   form.HMAkhir,
		FuelLiter: form.FuelLiter,
		Standby:   form.Standby,
		Breakdown: form.Breakdown,
		Remark:    form.Remark,
	})
	if err != nil {
		message := "Data hour meter tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create hour meter: %v", err)
			message = "Terjadi kesalahan saat menyimpan hour meter"
			status = http.StatusInternalServerError
		}
		s.renderHourMeter(w, r, user, sessionValue, form, message, "", status)
		return
	}
	s.renderHourMeter(w, r, user, sessionValue, HourMeterFormData{}, "",
		fmt.Sprintf("Hour meter %s tersimpan: %s jam kerja, %s menit standby, %s menit breakdown untuk %s.",
			reading.HMID, formatLiter(reading.TotalHM), formatLiter(reading.TotalStandby),
			formatLiter(reading.TotalBreakdown), reading.NamaUnit), http.StatusOK)
}

func (s *Server) renderHourMeter(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form HourMeterFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.hourMeter.Today()
	}
	// The list is drawn from the submitted rows, so a rejected form comes back
	// with the standby lines the operator already typed. One empty row is the
	// starting point and the template someone adds more from.
	if len(form.Standby) == 0 {
		form.Standby = []service.HourMeterStandbyInput{{}}
	}
	if len(form.Breakdown) == 0 {
		form.Breakdown = []service.HourMeterBreakdownInput{{}}
	}
	data := HourMeterPageData{
		ShellPageData:      s.shellData(user, sessionValue, "a2b-hm"),
		Form:               form,
		StandbyVariables:   service.StandbyVariables,
		BreakdownVariables: service.BreakdownVariables,
		WorkMinutes:        s.hourMeter.WorkMinutes(),
		PATarget:           paTarget,
		BDTarget:           bdTarget,
		UATarget:           uaTarget,
		RemarkMaxLength:    service.HourMeterRemarkMaxLength,
		Error:              errMessage,
		Success:            success,
	}
	units, err := s.hourMeter.UnitPicks(r.Context())
	if err != nil {
		log.Printf("load hour meter units: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat daftar unit A2B"
		}
	} else {
		data.Units = units
	}
	options, err := s.hourMeter.Options(r.Context())
	if err != nil {
		// Losing the suggestions costs autocomplete, not the form.
		log.Printf("load hour meter options: %v", err)
	} else {
		data.Options = options
	}
	// What the two sections have to add up to, worked out from whatever was
	// typed. The page redoes this as the readings change; this is what a browser
	// without JavaScript sees.
	data.IdleMinutes = float64(data.WorkMinutes)
	if hours, err := strconv.ParseFloat(strings.ReplaceAll(form.HMAwal, ",", "."), 64); err == nil {
		if akhir, err := strconv.ParseFloat(strings.ReplaceAll(form.HMAkhir, ",", "."), 64); err == nil && akhir >= hours {
			data.IdleMinutes = s.hourMeter.IdleMinutesFor(akhir - hours)
		}
	}
	nextID, err := s.hourMeter.NextHMID(r.Context())
	if err != nil {
		log.Printf("preview hour meter id: %v", err)
	} else {
		data.NextHMID = nextID
	}
	rows, err := s.hourMeter.List(r.Context())
	if err != nil {
		log.Printf("list hour meter: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat riwayat hour meter"
		}
	} else {
		data.Rows = hourMeterViews(rows)
	}
	s.render(w, "hour_meter", data, status)
}

// standbyFromForm reads the repeated standby inputs. The two arrays are
// submitted in parallel, so the shortest one decides how many lines arrived: a
// browser that sent a partial row must not shift minutes onto another reason.
func standbyFromForm(r *http.Request) []service.HourMeterStandbyInput {
	variables := r.Form["standby_variable"]
	menit := r.Form["standby_menit"]
	count := len(variables)
	if len(menit) < count {
		count = len(menit)
	}
	rows := make([]service.HourMeterStandbyInput, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, service.HourMeterStandbyInput{
			Variable: strings.TrimSpace(variables[i]),
			Menit:    strings.TrimSpace(menit[i]),
		})
	}
	return rows
}

// breakdownFromForm reads the repeated breakdown inputs, on the same terms as
// the standby ones: the shortest array decides how many lines arrived.
func breakdownFromForm(r *http.Request) []service.HourMeterBreakdownInput {
	variables := r.Form["breakdown_variable"]
	menit := r.Form["breakdown_menit"]
	count := len(variables)
	if len(menit) < count {
		count = len(menit)
	}
	rows := make([]service.HourMeterBreakdownInput, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, service.HourMeterBreakdownInput{
			Variable: strings.TrimSpace(variables[i]),
			Menit:    strings.TrimSpace(menit[i]),
		})
	}
	return rows
}

// The targets a shift is read against. Availability and its mirror are the
// same rule stated twice, which is how the site states them.
const (
	paTarget = 80.0
	bdTarget = 20.0
	uaTarget = 80.0
)

// figureClass colours a figure by whether it met its target. Green is the only
// state worth spotting from across the room; everything else reads as short.
func figureClass(met bool) string {
	if met {
		return "good"
	}
	return "short"
}

func hourMeterViews(rows []model.HourMeter) []HourMeterView {
	views := make([]HourMeterView, 0, len(rows))
	for _, row := range rows {
		views = append(views, HourMeterView{
			HourMeter:      row,
			TanggalLabel:   dateOnlyLabel(row.Tanggal),
			AwalLabel:      formatLiter(row.HMAwal),
			AkhirLabel:     formatLiter(row.HMAkhir),
			TotalLabel:     formatLiter(row.TotalHM),
			FuelLabel:      formatLiter(row.FuelLiter),
			StandbyLabel:   formatLiter(row.TotalStandby),
			BreakdownLabel: formatLiter(row.TotalBreakdown),
			PALabel:        formatLiter(row.PA),
			BDLabel:        formatLiter(row.BDPersen),
			UALabel:        formatLiter(row.UA),
			PAClass:        figureClass(row.PA >= paTarget),
			BDClass:        figureClass(row.BDPersen <= bdTarget),
			UAClass:        figureClass(row.UA >= uaTarget),
		})
	}
	return views
}
