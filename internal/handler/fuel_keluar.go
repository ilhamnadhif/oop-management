package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

type FuelKeluarFormData struct {
	Tanggal          string
	IDUnit           string
	HMAwalFlowMeter  string
	HMAkhirFlowMeter string
	HMAlatBerat      string
	Operator         string
}

type FuelKeluarView struct {
	model.FuelKeluar
	TanggalLabel     string
	AwalLabel        string
	AkhirLabel       string
	LiterLabel       string
	HMAlatBeratLabel string
	PhotoURL         string
}

type FuelKeluarPageData struct {
	ShellPageData
	Form            FuelKeluarFormData
	Units           []service.FuelUnitOption
	OperatorOptions []string
	NextFuelOutID   string
	Rows            []FuelKeluarView
	Error           string
	Success         string
}

func (s *Server) handleFuelKeluar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleFuelKeluarPage(w, r)
	case http.MethodPost:
		s.handleFuelKeluarCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFuelKeluarPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-fuel-keluar")
	if !ok {
		return
	}
	s.renderFuelKeluar(w, r, user, sessionValue, FuelKeluarFormData{}, "", "", http.StatusOK)
}

func (s *Server) handleFuelKeluarCreate(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "a2b-fuel-keluar")
	if !okProject {
		return
	}

	maxBody := s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderFuelKeluar(w, r, user, sessionValue, FuelKeluarFormData{}, "Form tidak valid atau foto terlalu besar", "", http.StatusUnprocessableEntity)
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	if !s.sessions.ValidCSRFToken(r.FormValue("csrf_token"), sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	form := FuelKeluarFormData{
		Tanggal:          strings.TrimSpace(r.FormValue("tanggal")),
		IDUnit:           strings.TrimSpace(r.FormValue("id_unit")),
		HMAwalFlowMeter:  strings.TrimSpace(r.FormValue("hm_awal")),
		HMAkhirFlowMeter: strings.TrimSpace(r.FormValue("hm_akhir")),
		HMAlatBerat:      strings.TrimSpace(r.FormValue("hm_alat_berat")),
		Operator:         strings.TrimSpace(r.FormValue("operator")),
	}
	fotoValue, err := s.readOptionalPhoto(r, "foto_flow_meter")
	if err != nil {
		s.renderFuelKeluar(w, r, user, sessionValue, form, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}

	fuel, err := s.fuelKeluar.Create(r.Context(), user, service.FuelKeluarInput{
		Tanggal:          form.Tanggal,
		IDUnit:           form.IDUnit,
		HMAwalFlowMeter:  form.HMAwalFlowMeter,
		HMAkhirFlowMeter: form.HMAkhirFlowMeter,
		HMAlatBerat:      form.HMAlatBerat,
		Operator:         form.Operator,
		Foto:             fotoValue,
	})
	if err != nil {
		message := "Data fuel keluar tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Foto akhir flow meter tidak valid"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create fuel keluar: %v", err)
			message = "Terjadi kesalahan saat menyimpan fuel keluar"
			status = http.StatusInternalServerError
		}
		s.renderFuelKeluar(w, r, user, sessionValue, form, message, "", status)
		return
	}
	s.renderFuelKeluar(w, r, user, sessionValue, FuelKeluarFormData{}, "",
		fmt.Sprintf("Fuel keluar %s tersimpan: %s liter untuk %s.",
			fuel.FuelOutID, formatLiter(fuel.Liter), fuel.NamaUnit), http.StatusOK)
}

func (s *Server) renderFuelKeluar(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form FuelKeluarFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.fuelKeluar.Today()
	}
	data := FuelKeluarPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-fuel-keluar"),
		Form:          form,
		Error:         errMessage,
		Success:       success,
	}
	units, err := s.fuelKeluar.UnitOptions(r.Context())
	if err != nil {
		log.Printf("load fuel keluar units: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat daftar unit A2B"
		}
	} else {
		data.Units = units
	}
	operators, err := s.fuelKeluar.Operators(r.Context())
	if err != nil {
		// Losing the suggestions costs autocomplete, not the form.
		log.Printf("load fuel keluar operators: %v", err)
	} else {
		data.OperatorOptions = operators
	}
	nextID, err := s.fuelKeluar.NextFuelOutID(r.Context())
	if err != nil {
		log.Printf("preview fuel keluar id: %v", err)
	} else {
		data.NextFuelOutID = nextID
	}
	// The pump's totaliser carries on from where it stopped, so the opening
	// reading is offered rather than asked for. It stays editable: the number on
	// the pump is the authority, not the last row.
	if data.Form.HMAwalFlowMeter == "" {
		last, err := s.fuelKeluar.LastFlowMeter(r.Context())
		if err != nil {
			log.Printf("read last flow meter: %v", err)
		} else if last > 0 {
			data.Form.HMAwalFlowMeter = formatLiter(last)
		}
	}
	rows, err := s.fuelKeluar.List(r.Context())
	if err != nil {
		log.Printf("list fuel keluar: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat riwayat fuel keluar"
		}
	} else {
		data.Rows = fuelKeluarViews(rows)
	}
	s.render(w, "fuel_keluar", data, status)
}

func (s *Server) handleFuelKeluarPhoto(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "a2b-fuel-keluar")
	if !ok {
		return
	}
	fuelOutID := strings.TrimSpace(r.URL.Query().Get("fuel_id"))
	dataURL, err := s.fuelKeluar.Photo(r.Context(), fuelOutID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrValidation) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read fuel keluar photo: %v", err)
		http.Error(w, "Gagal memuat foto flow meter", http.StatusInternalServerError)
		return
	}
	payload, err := photo.DecodeDataURL(dataURL)
	if err != nil {
		log.Printf("decode fuel keluar photo %s: %v", fuelOutID, err)
		http.Error(w, "Foto flow meter rusak", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fuelOutID+"-flowmeter.jpg"))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(payload)
}

func fuelKeluarViews(rows []model.FuelKeluar) []FuelKeluarView {
	views := make([]FuelKeluarView, 0, len(rows))
	for _, row := range rows {
		view := FuelKeluarView{
			FuelKeluar:   row,
			TanggalLabel: dateOnlyLabel(row.Tanggal),
			AwalLabel:    formatLiter(row.HMAwalFlowMeter),
			AkhirLabel:   formatLiter(row.HMAkhirFlowMeter),
			LiterLabel:   formatLiter(row.Liter),
			PhotoURL:     "/a2b/fuel-keluar/foto?fuel_id=" + url.QueryEscape(row.FuelOutID),
		}
		if row.HMAlatBerat != nil {
			view.HMAlatBeratLabel = formatLiter(*row.HMAlatBerat)
		}
		views = append(views, view)
	}
	return views
}
