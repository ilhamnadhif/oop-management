package handler

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

//go:embed templates/*.html static/css/* static/js/* static/img/* static/fonts/* static/vendor/leaflet/leaflet.js static/vendor/leaflet/leaflet.css static/vendor/leaflet/images/*
var assetFiles embed.FS

type Server struct {
	auth           *service.AuthService
	attendance     *service.AttendanceService
	unitDT         *service.UnitDTService
	produksi       *service.ProduksiService
	overview       *service.OverviewService
	sessions       *session.Manager
	location       *time.Location
	now            service.NowFunc
	maxUploadBytes int64
	maxPhotoChars  int
	templates      *template.Template
}

type AuthPageData struct {
	Title          string
	ActiveTab      string
	Error          string
	Success        string
	Identifier     string
	Register       RegisterFormData
	JabatanOptions []string
}

type RegisterFormData struct {
	TanggalGabung string
	NamaLengkap   string
	NRP           string
	Jabatan       string
	Email         string
	Status        string
}

// ShellPageData is what every signed-in page needs to draw the sidebar,
// breadcrumb and logout form.
type ShellPageData struct {
	Title      string
	User       *model.User
	Today      string
	ClockNow   string
	CSRFToken  string
	NavItems   []NavItem
	ActiveNav  string
	PageTitle  string
	Breadcrumb string
}

type UnitDTFormData struct {
	Nopol      string
	Panjang    string
	Lebar      string
	Tinggi     string
	Driver     string
	Keterangan string
}

type UnitDTPageData struct {
	ShellPageData
	Form              UnitDTFormData
	KeteranganOptions []string
	NextUnitID        string
	Error             string
	Success           string
}

type ProduksiFormData struct {
	Tanggal  string
	Project  string
	Supplier string
	Quary    string
	Kategori string
	Lokasi   string
	Layer    string
	Nopol    string
	TT       string
}

type OverviewPageData struct {
	ShellPageData
	Overview     *service.Overview
	From         string
	To           string
	VolumeChart  *Chart
	RitaseChart  *Chart
	UnitChart    *Chart
	CompareChart *Chart
	Error        string
}

type ProduksiPageData struct {
	ShellPageData
	Form            ProduksiFormData
	Units           []model.UnitDT
	ProjectOptions  []string
	SupplierOptions []string
	QuaryOptions    []string
	KategoriOptions []string
	LayerOptions    []string
	Error           string
	Success         string
}

type DashboardPageData struct {
	ShellPageData
	Attendance    *model.Attendance
	ClockInTime   string
	ClockOutTime  string
	TimezoneLabel string
	HasClockIn    bool
	HasClockOut   bool
}

func NewServer(auth *service.AuthService, attendance *service.AttendanceService, unitDT *service.UnitDTService, produksi *service.ProduksiService, overview *service.OverviewService, sessions *session.Manager, location *time.Location, now service.NowFunc, maxUploadBytes int64, maxPhotoChars int) (*Server, error) {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	if maxUploadBytes <= 0 {
		maxUploadBytes = photo.MaxInputBytes
	}
	if maxPhotoChars <= 0 {
		maxPhotoChars = photo.MaxOutputChars
	}
	// The chart template positions gridline labels relative to the plot edges,
	// which needs arithmetic the template language does not provide.
	templates, err := template.New("pages").Funcs(template.FuncMap{
		"add": func(a, b float64) float64 { return a + b },
		"sub": func(a, b float64) float64 { return a - b },
		"div": func(a, b float64) float64 { return a / b },
	}).ParseFS(assetFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{
		auth:           auth,
		attendance:     attendance,
		unitDT:         unitDT,
		produksi:       produksi,
		overview:       overview,
		sessions:       sessions,
		location:       location,
		now:            now,
		maxUploadBytes: maxUploadBytes,
		maxPhotoChars:  maxPhotoChars,
		templates:      templates,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/produksi", s.handleProduksi)
	mux.HandleFunc("/produksi/overview", s.handleProduksiOverview)
	mux.HandleFunc("/unit-dt", s.handleUnitDT)
	mux.HandleFunc("/absensi/clock-in", s.handleClockIn)
	mux.HandleFunc("/absensi/clock-out", s.handleClockOut)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/static/", staticHandler())
	return securityHeaders(mux)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.currentSession(r); ok {
		redirect(w, r, "/dashboard")
		return
	}
	redirect(w, r, "/login")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		success := ""
		if r.URL.Query().Get("registered") == "1" {
			success = "Registrasi berhasil. Silakan masuk."
		}
		s.render(w, "login", AuthPageData{Title: "Masuk", ActiveTab: "login", Success: success}, http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	identifier := strings.TrimSpace(r.FormValue("identifier"))
	user, err := s.auth.Authenticate(r.Context(), identifier, r.FormValue("password"), requestMeta(r))
	if err != nil {
		message := "NRP/Email atau password salah"
		if !errors.Is(err, service.ErrInvalidCredentials) && !errors.Is(err, service.ErrInactiveUser) {
			log.Printf("login error: %v", err)
			message = "Terjadi kesalahan saat memproses login"
		}
		s.render(w, "login", AuthPageData{Title: "Masuk", ActiveTab: "login", Error: message, Identifier: identifier}, http.StatusUnauthorized)
		return
	}
	if _, err := s.sessions.Create(w, user.UserID, s.now().In(s.location)); err != nil {
		log.Printf("create session: %v", err)
		s.render(w, "login", AuthPageData{Title: "Masuk", ActiveTab: "login", Error: "Terjadi kesalahan saat membuat sesi"}, http.StatusInternalServerError)
		return
	}
	redirect(w, r, "/dashboard")
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.render(w, "register", AuthPageData{
			Title:          "Daftar Akun",
			ActiveTab:      "register",
			Register:       RegisterFormData{Status: model.StatusAktif},
			JabatanOptions: service.JabatanOptions,
		}, http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	form := RegisterFormData{
		TanggalGabung: strings.TrimSpace(r.FormValue("tanggal_gabung")),
		NamaLengkap:   strings.TrimSpace(r.FormValue("nama_lengkap")),
		NRP:           strings.TrimSpace(r.FormValue("nrp")),
		Jabatan:       strings.TrimSpace(r.FormValue("jabatan")),
		Email:         strings.TrimSpace(r.FormValue("email")),
		Status:        strings.TrimSpace(r.FormValue("status_pengguna")),
	}
	_, err := s.auth.Register(r.Context(), service.RegisterInput{
		TanggalGabung: form.TanggalGabung,
		NamaLengkap:   form.NamaLengkap,
		NRP:           form.NRP,
		Jabatan:       form.Jabatan,
		Email:         form.Email,
		Password:      r.FormValue("password"),
		Status:        form.Status,
	})
	if err != nil {
		message := "Data registrasi tidak valid"
		status := http.StatusUnprocessableEntity
		if errors.Is(err, service.ErrDuplicateUser) {
			message = "NRP atau email sudah digunakan"
			status = http.StatusConflict
		} else if !errors.Is(err, service.ErrValidation) {
			log.Printf("register error: %v", err)
			message = "Terjadi kesalahan saat menyimpan akun"
			status = http.StatusInternalServerError
		}
		s.render(w, "register", AuthPageData{
			Title:          "Daftar Akun",
			ActiveTab:      "register",
			Error:          message,
			Register:       form,
			JabatanOptions: service.JabatanOptions,
		}, status)
		return
	}
	redirect(w, r, "/login?registered=1")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err == nil {
		if err := s.auth.RecordLogout(r.Context(), user, requestMeta(r)); err != nil {
			log.Printf("record logout: %v", err)
		}
	}
	s.sessions.Delete(r, w)
	redirect(w, r, "/login")
}

// requireUser resolves the signed-in, active user for a page request. It writes
// the redirect itself and reports false when the caller must stop.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) (*model.User, session.Session, bool) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return nil, session.Session{}, false
	}
	sessionValue, ok := s.currentSession(r)
	if !ok {
		redirect(w, r, "/login")
		return nil, session.Session{}, false
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		s.sessions.Delete(r, w)
		redirect(w, r, "/login")
		return nil, session.Session{}, false
	}
	return user, sessionValue, true
}

func (s *Server) shellData(user *model.User, sessionValue session.Session, navKey string) ShellPageData {
	now := s.now().In(s.location)
	item, _ := navItemByKey(navKey)
	return ShellPageData{
		Title:      item.Label,
		User:       user,
		Today:      formatIndonesianDate(now),
		ClockNow:   now.Format("15:04"),
		CSRFToken:  sessionValue.CSRFToken,
		NavItems:   navItemsFor(user.Jabatan),
		ActiveNav:  navKey,
		PageTitle:  item.Label,
		Breadcrumb: item.Label,
	}
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	attendance, err := s.attendance.Today(r.Context(), user.UserID)
	if err != nil {
		log.Printf("load dashboard attendance: %v", err)
		http.Error(w, "Gagal memuat data absensi", http.StatusInternalServerError)
		return
	}
	now := s.now().In(s.location)
	clockInTime := emptyClock
	if attendance != nil && !attendance.ClockInAt.IsZero() {
		clockInTime = attendance.ClockInAt.In(s.location).Format("15:04")
	}
	clockOutTime := emptyClock
	if attendance != nil && attendance.ClockOutAt != nil && !attendance.ClockOutAt.IsZero() {
		clockOutTime = attendance.ClockOutAt.In(s.location).Format("15:04")
	}
	s.render(w, "dashboard", DashboardPageData{
		ShellPageData: s.shellData(user, sessionValue, "absensi"),
		Attendance:    attendance,
		ClockInTime:   clockInTime,
		ClockOutTime:  clockOutTime,
		TimezoneLabel: now.Format("MST"),
		HasClockIn:    attendance != nil,
		HasClockOut:   attendance != nil && attendance.ClockOutAt != nil,
	}, http.StatusOK)
}

func (s *Server) handleProduksi(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleProduksiCreate(w, r)
		return
	}
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{
		Tanggal: s.produksi.Today(),
	}, "", "", http.StatusOK)
}

func (s *Server) handleProduksiOverview(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}

	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	data := OverviewPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-overview"),
		From:          from,
		To:            to,
	}

	overview, err := s.overview.Build(r.Context(), from, to)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build overview: %v", err)
			data.Error = "Gagal memuat data produksi"
		}
		s.render(w, "produksi_overview", data, http.StatusOK)
		return
	}

	data.Overview = overview
	// The filter inputs echo whatever the aggregation settled on, so a reversed
	// range shows the corrected order rather than what was typed.
	data.From = overview.From
	data.To = overview.To
	data.VolumeChart = BuildLineChart(seriesLabels(overview.Series), seriesVolumes(overview.Series), 0)
	data.RitaseChart = BuildStackedChart(seriesLabels(overview.Series), seriesKecil(overview.Series), seriesBesar(overview.Series))
	data.UnitChart = BuildValueChart(seriesLabels(overview.Series), seriesUnits(overview.Series), 0)
	data.CompareChart = BuildGroupedChart(seriesLabels(overview.Series), seriesVolumes(overview.Series), seriesOPP(overview.Series))
	s.render(w, "produksi_overview", data, http.StatusOK)
}

func seriesLabels(points []service.DatePoint) []string {
	labels := make([]string, len(points))
	for i, point := range points {
		labels[i] = point.Label
	}
	return labels
}

func seriesVolumes(points []service.DatePoint) []float64 {
	values := make([]float64, len(points))
	for i, point := range points {
		values[i] = point.Volume
	}
	return values
}

func seriesOPP(points []service.DatePoint) []float64 {
	values := make([]float64, len(points))
	for i, point := range points {
		values[i] = point.VolumeOPP
	}
	return values
}

func seriesUnits(points []service.DatePoint) []float64 {
	values := make([]float64, len(points))
	for i, point := range points {
		values[i] = float64(point.Units)
	}
	return values
}

func seriesKecil(points []service.DatePoint) []int {
	values := make([]int, len(points))
	for i, point := range points {
		values[i] = point.Kecil
	}
	return values
}

func seriesBesar(points []service.DatePoint) []int {
	values := make([]int, len(points))
	for i, point := range points {
		values[i] = point.Besar
	}
	return values
}

func (s *Server) handleProduksiCreate(w http.ResponseWriter, r *http.Request) {
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
	if err := r.ParseForm(); err != nil {
		s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{Tanggal: s.produksi.Today()}, "Form tidak valid", "", http.StatusUnprocessableEntity)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	form := ProduksiFormData{
		Tanggal:  strings.TrimSpace(r.FormValue("tanggal")),
		Project:  strings.TrimSpace(r.FormValue("project")),
		Supplier: strings.TrimSpace(r.FormValue("supplier")),
		Quary:    strings.TrimSpace(r.FormValue("quary")),
		Kategori: strings.TrimSpace(r.FormValue("kategori")),
		Lokasi:   strings.TrimSpace(r.FormValue("lokasi")),
		Layer:    strings.TrimSpace(r.FormValue("layer")),
		Nopol:    strings.TrimSpace(r.FormValue("nopol")),
		TT:       strings.TrimSpace(r.FormValue("tt")),
	}

	produksi, err := s.produksi.Create(r.Context(), user, service.ProduksiInput{
		Tanggal:  form.Tanggal,
		Project:  form.Project,
		Supplier: form.Supplier,
		Quary:    form.Quary,
		Kategori: form.Kategori,
		Lokasi:   form.Lokasi,
		Layer:    form.Layer,
		Nopol:    form.Nopol,
		TT:       form.TT,
	})
	if err != nil {
		message := "Data produksi tidak valid"
		status := http.StatusUnprocessableEntity
		if errors.Is(err, service.ErrValidation) {
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("create produksi: %v", err)
			message = "Terjadi kesalahan saat menyimpan produksi"
			status = http.StatusInternalServerError
		}
		s.renderProduksi(w, r, user, sessionValue, form, message, "", status)
		return
	}

	s.renderProduksi(w, r, user, sessionValue,
		ProduksiFormData{Tanggal: s.produksi.Today()},
		"",
		fmt.Sprintf("%s tersimpan. Volume %.4f m³, OPP %.0f m³, deviasi %.4f m³.",
			produksi.ProduksiID, produksi.Volume, produksi.VolumeOPP, produksi.Deviasi),
		http.StatusOK)
}

func (s *Server) renderProduksi(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form ProduksiFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.produksi.Today()
	}
	units, err := s.produksi.Units(r.Context())
	if err != nil {
		// Without the register the nopol picker is empty, but the page itself
		// still renders and says why.
		log.Printf("list unit dt: %v", err)
		if errMessage == "" {
			errMessage = "Daftar unit gagal dimuat"
		}
	}
	s.render(w, "produksi", ProduksiPageData{
		ShellPageData:   s.shellData(user, sessionValue, "produksi"),
		Form:            form,
		Units:           units,
		ProjectOptions:  service.ProjectOptions,
		SupplierOptions: service.SupplierOptions,
		QuaryOptions:    service.QuaryOptions,
		KategoriOptions: service.KategoriOptions,
		LayerOptions:    service.LayerOptions,
		Error:           errMessage,
		Success:         success,
	}, status)
}

func (s *Server) handleUnitDT(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleUnitDTCreate(w, r)
		return
	}
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	s.render(w, "unit_dt", UnitDTPageData{
		ShellPageData:     s.shellData(user, sessionValue, "unit-dt"),
		Form:              UnitDTFormData{Keterangan: service.DefaultKeterangan},
		KeteranganOptions: service.KeteranganOptions,
		NextUnitID:        s.nextUnitID(r),
	}, http.StatusOK)
}

// nextUnitID is a preview only. A failure here must not block the form, so the
// field simply renders empty and the server still assigns the real ID on save.
func (s *Server) nextUnitID(r *http.Request) string {
	nextID, err := s.unitDT.NextUnitID(r.Context())
	if err != nil {
		log.Printf("preview unit id: %v", err)
		return ""
	}
	return nextID
}

func (s *Server) handleUnitDTCreate(w http.ResponseWriter, r *http.Request) {
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

	// Bound the body before parsing, then check CSRF from the parsed form.
	// ValidCSRF refuses to read a form value out of a multipart request
	// precisely because it cannot know the body was limited first.
	maxBody := s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderUnitDTError(w, r, user, sessionValue, UnitDTFormData{}, "Form tidak valid atau file terlalu besar", http.StatusUnprocessableEntity)
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

	form := UnitDTFormData{
		Nopol:      strings.TrimSpace(r.FormValue("nopol")),
		Panjang:    strings.TrimSpace(r.FormValue("panjang")),
		Lebar:      strings.TrimSpace(r.FormValue("lebar")),
		Tinggi:     strings.TrimSpace(r.FormValue("tinggi")),
		Driver:     strings.TrimSpace(r.FormValue("driver")),
		Keterangan: strings.TrimSpace(r.FormValue("keterangan")),
	}

	photoValue, err := s.readOptionalPhoto(r, "foto_unit")
	if err != nil {
		s.renderUnitDTError(w, r, user, sessionValue, form, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	unit, err := s.unitDT.Create(r.Context(), user, service.UnitDTInput{
		Nopol:      form.Nopol,
		Panjang:    form.Panjang,
		Lebar:      form.Lebar,
		Tinggi:     form.Tinggi,
		Driver:     form.Driver,
		Keterangan: form.Keterangan,
		Foto:       photoValue,
	})
	if err != nil {
		message := "Data unit tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrDuplicateUnitDT):
			message = "Nopol sudah terdaftar"
			status = http.StatusConflict
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Foto unit tidak valid"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create unit dt: %v", err)
			message = "Terjadi kesalahan saat menyimpan unit"
			status = http.StatusInternalServerError
		}
		s.renderUnitDTError(w, r, user, sessionValue, form, message, status)
		return
	}

	s.render(w, "unit_dt", UnitDTPageData{
		ShellPageData:     s.shellData(user, sessionValue, "unit-dt"),
		Form:              UnitDTFormData{Keterangan: service.DefaultKeterangan},
		KeteranganOptions: service.KeteranganOptions,
		NextUnitID:        s.nextUnitID(r),
		Success:           "Unit " + unit.UnitID + " (" + unit.Nopol + ") berhasil disimpan.",
	}, http.StatusOK)
}

func (s *Server) renderUnitDTError(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form UnitDTFormData, message string, status int) {
	if form.Keterangan == "" {
		form.Keterangan = service.DefaultKeterangan
	}
	s.render(w, "unit_dt", UnitDTPageData{
		ShellPageData:     s.shellData(user, sessionValue, "unit-dt"),
		Form:              form,
		KeteranganOptions: service.KeteranganOptions,
		NextUnitID:        s.nextUnitID(r),
		Error:             message,
	}, status)
}

// readOptionalPhoto normalises an uploaded image to the same compressed data
// URL the attendance photos use. An absent file yields an empty value; only a
// file that is present but unreadable is an error.
func (s *Server) readOptionalPhoto(r *http.Request, field string) (string, error) {
	file, _, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("gagal membaca foto")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, s.maxUploadBytes+1))
	if err != nil {
		return "", fmt.Errorf("gagal membaca foto")
	}
	if int64(len(raw)) > s.maxUploadBytes {
		return "", fmt.Errorf("ukuran foto maksimal %d MB", s.maxUploadBytes/(1024*1024))
	}
	value, err := photo.Normalize(raw, s.maxPhotoChars)
	if err != nil {
		return "", fmt.Errorf("format foto tidak didukung")
	}
	return value, nil
}

func (s *Server) handleClockIn(w http.ResponseWriter, r *http.Request) {
	s.handleAttendanceAction(w, r, false)
}

func (s *Server) handleClockOut(w http.ResponseWriter, r *http.Request) {
	s.handleAttendanceAction(w, r, true)
}

func (s *Server) handleAttendanceAction(w http.ResponseWriter, r *http.Request, clockOut bool) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}
	sessionValue, ok := s.currentSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "sesi tidak valid"})
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "CSRF token tidak valid"})
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "user tidak ditemukan"})
		return
	}

	input, err := s.parseAttendanceInput(w, r)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	var attendance *model.Attendance
	if clockOut {
		attendance, err = s.attendance.ClockOut(r.Context(), user, input)
	} else {
		attendance, err = s.attendance.ClockIn(r.Context(), user, input)
	}
	if err != nil {
		status, message := attendanceError(err, clockOut)
		if status >= http.StatusInternalServerError {
			log.Printf("attendance action error: %v", err)
		}
		writeJSON(w, status, map[string]interface{}{"ok": false, "error": message})
		return
	}
	message := "Clock in berhasil"
	if clockOut {
		message = "Clock out berhasil"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"message":        message,
		"status_absensi": attendance.StatusAbsensi,
		"durasi_menit":   attendance.DurasiMenit,
	})
}

func (s *Server) parseAttendanceInput(w http.ResponseWriter, r *http.Request) (service.AttendanceInput, error) {
	maxBody := s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		return service.AttendanceInput{}, fmt.Errorf("form multipart tidak valid")
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, _, err := r.FormFile("face_photo")
	if err != nil {
		return service.AttendanceInput{}, fmt.Errorf("foto wajah wajib diisi")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, s.maxUploadBytes+1))
	if err != nil {
		return service.AttendanceInput{}, fmt.Errorf("gagal membaca foto")
	}
	if int64(len(raw)) > s.maxUploadBytes {
		return service.AttendanceInput{}, fmt.Errorf("ukuran foto maksimal %d MB", s.maxUploadBytes/(1024*1024))
	}
	photoValue, err := photo.Normalize(raw, s.maxPhotoChars)
	if err != nil {
		if errors.Is(err, photo.ErrTooLarge) {
			return service.AttendanceInput{}, fmt.Errorf("foto terlalu besar setelah kompresi, silakan ambil ulang")
		}
		return service.AttendanceInput{}, fmt.Errorf("foto tidak valid")
	}

	latitude, err := parseRequiredFloat(r.FormValue("latitude"))
	if err != nil {
		return service.AttendanceInput{}, fmt.Errorf("latitude tidak valid")
	}
	longitude, err := parseRequiredFloat(r.FormValue("longitude"))
	if err != nil {
		return service.AttendanceInput{}, fmt.Errorf("longitude tidak valid")
	}
	accuracy, err := parseOptionalFloat(r.FormValue("accuracy"))
	if err != nil {
		return service.AttendanceInput{}, fmt.Errorf("accuracy lokasi tidak valid")
	}
	return service.AttendanceInput{
		Latitude:  latitude,
		Longitude: longitude,
		Accuracy:  accuracy,
		Photo:     photoValue,
		IPAddress: clientIP(r),
	}, nil
}

func (s *Server) currentSession(r *http.Request) (session.Session, bool) {
	return s.sessions.Get(r, s.now().In(s.location))
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}, status int) {
	var buffer bytes.Buffer
	if err := s.templates.ExecuteTemplate(&buffer, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Gagal menampilkan halaman", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buffer.WriteTo(w)
}

func attendanceError(err error, clockOut bool) (int, string) {
	switch {
	case errors.Is(err, service.ErrConflict):
		return http.StatusConflict, "Anda sudah melakukan clock in hari ini"
	case errors.Is(err, service.ErrNoClockIn):
		return http.StatusConflict, "Anda belum melakukan clock in hari ini"
	case errors.Is(err, service.ErrAlreadyClockedOut):
		return http.StatusConflict, "Anda sudah melakukan clock out hari ini"
	case errors.Is(err, service.ErrInactiveUser):
		return http.StatusForbidden, "User tidak aktif"
	case errors.Is(err, service.ErrInvalidLocation):
		return http.StatusUnprocessableEntity, "Lokasi tidak valid"
	case errors.Is(err, service.ErrInvalidPhoto):
		return http.StatusUnprocessableEntity, "Foto wajah tidak valid"
	case errors.Is(err, service.ErrValidation):
		if clockOut {
			return http.StatusUnprocessableEntity, "Clock out tidak valid"
		}
		return http.StatusUnprocessableEntity, "Clock in tidak valid"
	default:
		return http.StatusInternalServerError, "Terjadi kesalahan saat menyimpan absensi"
	}
}

func requestMeta(r *http.Request) service.ActivityMeta {
	return service.ActivityMeta{IPAddress: clientIP(r), UserAgent: truncate(r.UserAgent(), 1000)}
}

func clientIP(r *http.Request) string {
	address := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}

func parseRequiredFloat(value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, errors.New("invalid number")
	}
	return parsed, nil
}

func parseOptionalFloat(value string) (*float64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := parseRequiredFloat(value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

func redirect(w http.ResponseWriter, r *http.Request, location string) {
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// contentSecurityPolicy keeps every directive at 'self'. The single exception
// is img-src, which also allows the OpenStreetMap tile hosts because Leaflet
// fetches map tiles as plain <img> elements. Scripts, styles and XHR stay
// same-origin, so the tile hosts can only ever paint pixels.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"img-src 'self' data: https://*.tile.openstreetmap.org; " +
	"media-src 'self' blob:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		next.ServeHTTP(w, r)
	})
}
