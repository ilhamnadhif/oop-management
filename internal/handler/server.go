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

//go:embed templates/*.html static/css/* static/js/*
var assetFiles embed.FS

type Server struct {
	auth           *service.AuthService
	attendance     *service.AttendanceService
	sessions       *session.Manager
	location       *time.Location
	now            service.NowFunc
	maxUploadBytes int64
	maxPhotoChars  int
	templates      *template.Template
}

type AuthPageData struct {
	Title      string
	ActiveTab  string
	Error      string
	Success    string
	Identifier string
	Register   RegisterFormData
}

type RegisterFormData struct {
	TanggalGabung string
	NamaLengkap   string
	NRP           string
	Jabatan       string
	Email         string
	Status        string
}

type DashboardPageData struct {
	Title       string
	User        *model.User
	Attendance  *model.Attendance
	Today       string
	CSRFToken   string
	HasClockIn  bool
	HasClockOut bool
}

func NewServer(auth *service.AuthService, attendance *service.AttendanceService, sessions *session.Manager, location *time.Location, now service.NowFunc, maxUploadBytes int64, maxPhotoChars int) (*Server, error) {
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
	templates, err := template.New("pages").Funcs(template.FuncMap{
		"formatTime": func(value interface{}) string {
			switch typed := value.(type) {
			case time.Time:
				if typed.IsZero() {
					return "-"
				}
				return typed.In(location).Format("02 Jan 2006 15:04:05")
			case *time.Time:
				if typed == nil || typed.IsZero() {
					return "-"
				}
				return typed.In(location).Format("02 Jan 2006 15:04:05")
			default:
				return "-"
			}
		},
	}).ParseFS(assetFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{
		auth:           auth,
		attendance:     attendance,
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
	mux.HandleFunc("/absensi/clock-in", s.handleClockIn)
	mux.HandleFunc("/absensi/clock-out", s.handleClockOut)
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.Handle("/static/", http.FileServer(http.FS(assetFiles)))
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
		s.render(w, "register", AuthPageData{Title: "Daftar Akun", ActiveTab: "register", Register: RegisterFormData{Status: model.StatusAktif}}, http.StatusOK)
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
		s.render(w, "register", AuthPageData{Title: "Daftar Akun", ActiveTab: "register", Error: message, Register: form}, status)
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

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
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
	attendance, err := s.attendance.Today(r.Context(), user.UserID)
	if err != nil {
		log.Printf("load dashboard attendance: %v", err)
		http.Error(w, "Gagal memuat data absensi", http.StatusInternalServerError)
		return
	}
	s.render(w, "dashboard", DashboardPageData{
		Title:       "Dashboard Absensi",
		User:        user,
		Attendance:  attendance,
		Today:       s.now().In(s.location).Format("02 January 2006"),
		CSRFToken:   sessionValue.CSRFToken,
		HasClockIn:  attendance != nil,
		HasClockOut: attendance != nil && attendance.ClockOutAt != nil,
	}, http.StatusOK)
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

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}
