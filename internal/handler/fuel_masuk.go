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

type FuelMasukFormData struct {
	TanggalInput     string
	Vendor           string
	Driver           string
	Nopol            string
	JumlahLiter      string
	Keterangan       string
	LiterTidakSesuai string
}

// FuelPhotoLink is one evidence photo as the page links to it.
type FuelPhotoLink struct {
	Label string
	URL   string
}

type FuelMasukView struct {
	model.FuelMasuk
	TanggalLabel   string
	ProcessedLabel string
	StatusClass    string
	JumlahLabel    string
	SelisihLabel   string
	TidakSesuai    bool
	Photos         []FuelPhotoLink
}

type FuelMasukPageData struct {
	ShellPageData
	Form              FuelMasukFormData
	KeteranganOptions []string
	PhotoKinds        []service.FuelPhotoKind
	Options           service.FuelMasukOptions
	NextFuelID        string
	Rows              []FuelMasukView
	Error             string
	Success           string
}

func (s *Server) handleFuelMasuk(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleFuelMasukPage(w, r)
	case http.MethodPost:
		s.handleFuelMasukCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleFuelMasukPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-fuel-masuk")
	if !ok {
		return
	}
	s.renderFuelMasuk(w, r, user, sessionValue, FuelMasukFormData{}, "", "", http.StatusOK)
}

func (s *Server) handleFuelMasukCreate(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "a2b-fuel-masuk")
	if !okProject {
		return
	}

	// Four photos travel with one delivery, so the body allowance is four times
	// a single upload plus room for the fields.
	maxBody := int64(len(service.FuelPhotoKinds))*s.maxUploadBytes + 96*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderFuelMasuk(w, r, user, sessionValue, FuelMasukFormData{}, "Form tidak valid atau foto terlalu besar", "", http.StatusUnprocessableEntity)
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

	form := FuelMasukFormData{
		TanggalInput:     strings.TrimSpace(r.FormValue("tanggal_input")),
		Vendor:           strings.TrimSpace(r.FormValue("vendor")),
		Driver:           strings.TrimSpace(r.FormValue("driver")),
		Nopol:            strings.TrimSpace(r.FormValue("nopol")),
		JumlahLiter:      strings.TrimSpace(r.FormValue("jumlah_liter")),
		Keterangan:       strings.TrimSpace(r.FormValue("keterangan")),
		LiterTidakSesuai: strings.TrimSpace(r.FormValue("liter_tidak_sesuai")),
	}
	input := service.FuelMasukInput{
		TanggalInput:     form.TanggalInput,
		Vendor:           form.Vendor,
		Driver:           form.Driver,
		Nopol:            form.Nopol,
		JumlahLiter:      form.JumlahLiter,
		Keterangan:       form.Keterangan,
		LiterTidakSesuai: form.LiterTidakSesuai,
	}
	for _, kind := range service.FuelPhotoKinds {
		value, err := s.readOptionalPhoto(r, kind.Field)
		if err != nil {
			s.renderFuelMasuk(w, r, user, sessionValue, form, kind.Label+": "+err.Error(), "", http.StatusUnprocessableEntity)
			return
		}
		input.Photos[kind.Index] = value
	}

	fuel, err := s.fuelMasuk.Create(r.Context(), user, input)
	if err != nil {
		message := "Data fuel masuk tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrInvalidPhoto):
			message = strings.TrimPrefix(err.Error(), "invalid photo: ")
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create fuel masuk: %v", err)
			message = "Terjadi kesalahan saat menyimpan fuel masuk"
			status = http.StatusInternalServerError
		}
		s.renderFuelMasuk(w, r, user, sessionValue, form, message, "", status)
		return
	}
	s.renderFuelMasuk(w, r, user, sessionValue, FuelMasukFormData{}, "",
		fmt.Sprintf("Fuel masuk %s tersimpan.", fuel.FuelID), http.StatusOK)
}

func (s *Server) renderFuelMasuk(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form FuelMasukFormData, errMessage, success string, status int) {
	if form.TanggalInput == "" {
		form.TanggalInput = s.fuelMasuk.NowInput()
	}
	if form.Keterangan == "" {
		form.Keterangan = model.FuelKeteranganSesuai
	}
	data := FuelMasukPageData{
		ShellPageData:     s.shellData(user, sessionValue, "a2b-fuel-masuk"),
		Form:              form,
		KeteranganOptions: service.FuelKeteranganOptions,
		PhotoKinds:        service.FuelPhotoKinds,
		Error:             errMessage,
		Success:           success,
	}
	options, err := s.fuelMasuk.Options(r.Context())
	if err != nil {
		// Losing the suggestions costs autocomplete, not the form.
		log.Printf("load fuel masuk options: %v", err)
	} else {
		data.Options = options
	}
	nextID, err := s.fuelMasuk.NextFuelID(r.Context())
	if err != nil {
		log.Printf("preview fuel masuk id: %v", err)
	} else {
		data.NextFuelID = nextID
	}
	rows, err := s.fuelMasuk.List(r.Context(), service.FuelMasukFilters{})
	if err != nil {
		log.Printf("list fuel masuk: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat riwayat fuel masuk"
		}
	} else {
		data.Rows = fuelViews(rows)
	}
	s.render(w, "fuel_masuk", data, status)
}

// handleFuelMasukPhoto serves one evidence photo. Anyone who may see the
// delivery may see the pictures it was recorded with, so the check is the same
// one that guards the input page.
func (s *Server) handleFuelMasukPhoto(w http.ResponseWriter, r *http.Request) {
	s, user, _, ok := s.requireAccess(w, r, "a2b-fuel-masuk")
	if !ok {
		return
	}
	fuelID := strings.TrimSpace(r.URL.Query().Get("fuel_id"))
	slug := strings.TrimSpace(r.URL.Query().Get("foto"))
	dataURL, err := s.fuelMasuk.Photo(r.Context(), fuelID, slug)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) || errors.Is(err, service.ErrValidation) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read fuel masuk photo for %s: %v", user.UserID, err)
		http.Error(w, "Gagal memuat foto fuel masuk", http.StatusInternalServerError)
		return
	}
	payload, err := photo.DecodeDataURL(dataURL)
	if err != nil {
		log.Printf("decode fuel masuk photo %s: %v", fuelID, err)
		http.Error(w, "Foto fuel masuk rusak", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fuelID+"-"+slug+".jpg"))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(payload)
}

func fuelViews(rows []model.FuelMasuk) []FuelMasukView {
	views := make([]FuelMasukView, 0, len(rows))
	for _, row := range rows {
		view := FuelMasukView{
			FuelMasuk:    row,
			TanggalLabel: row.TanggalInput.Format("02 Jan 2006 15:04"),
			StatusClass:  fuelStatusClass(row.StatusApproval),
			JumlahLabel:  formatLiter(row.JumlahLiter),
			SelisihLabel: formatLiter(row.LiterTidakSesuai),
			TidakSesuai:  row.Keterangan == model.FuelKeteranganTidakSesuai,
		}
		for _, kind := range service.FuelPhotoKinds {
			view.Photos = append(view.Photos, FuelPhotoLink{
				Label: kind.Label,
				URL:   "/a2b/fuel-masuk/foto?fuel_id=" + url.QueryEscape(row.FuelID) + "&foto=" + kind.Slug,
			})
		}
		if row.DiprosesPada != nil {
			view.ProcessedLabel = row.DiprosesPada.Format("02 Jan 2006 15:04")
		}
		views = append(views, view)
	}
	return views
}

func fuelStatusClass(status string) string {
	switch status {
	case model.FuelStatusDisetujui:
		return "approved"
	case model.FuelStatusDitolak:
		return "rejected"
	default:
		return "pending"
	}
}

// formatLiter prints a volume the way the sheet holds it: whole litres stay
// whole, and a fractional reading keeps its decimals.
func formatLiter(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
