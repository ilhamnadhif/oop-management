package handler

import (
	"bytes"
	"context"
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

	"opp-management/internal/export"
	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/receipt"
	"opp-management/internal/service"
	"opp-management/internal/session"
	"opp-management/internal/tally"
)

//go:embed templates/*.html static/css/* static/js/* static/img/* static/fonts/* static/vendor/leaflet/leaflet.js static/vendor/leaflet/leaflet.css static/vendor/leaflet/images/*
var assetFiles embed.FS

const (
	aiScanRateLimit       = 5
	aiScanRateWindow      = time.Minute
	aiScanRateMaxUsers    = 4096
	aiScanConcurrentLimit = 3
)

type aiScanRateEntry struct {
	windowStart time.Time
	count       int
}

// Server has two halves. The fields above the line belong to the deployment and
// are the same for everybody; the ones below belong to one project and are
// empty until a request says which project it is working in.
//
// They are nil on purpose. A handler that forgets to bind a project panics on
// its first service call rather than quietly serving another project's books,
// which is the failure worth being loud about.
type Server struct {
	auth     *service.AuthService
	projects *service.ProjectService
	bundles  *projectCache
	// provision writes the sheets into a spreadsheet a new project names. Nil
	// in the tests, where there is no spreadsheet to prepare.
	provision      service.Provisioner
	defaults       Branding
	sessions       *session.Manager
	location       *time.Location
	now            service.NowFunc
	maxUploadBytes int64
	maxPhotoChars  int
	receiptScanner receipt.Scanner
	tallyScanner   tally.Scanner
	// scanTimeout is how long the sheet reader is allowed to take. The handler
	// needs it too: the server's write deadline is shorter, and the page has to
	// tell the browser how long to wait.
	scanTimeout time.Duration
	// scan is held by pointer because a Server is copied per request to bind a
	// project, and the AI budget is the deployment's, not one project's. A
	// copied mutex would guard a copy of nothing.
	scan      *scanGate
	templates *template.Template

	// Bound per request by forProject. Nil until then.
	project model.Project
	// reachable is what the project switcher offers this person, in sheet
	// order. One entry means there is nothing to switch between and the
	// control is not drawn.
	reachable    []model.Project
	attendance   *service.AttendanceService
	unitDT       *service.UnitDTService
	produksi     *service.ProduksiService
	overview     *service.OverviewService
	unitA2B      *service.UnitA2BService
	nota         *service.NotaService
	leave        *service.LeaveService
	unitOverview *service.UnitOverviewService
	fuelMasuk    *service.FuelMasukService
	fuelKeluar   *service.FuelKeluarService
	hourMeter    *service.HourMeterService
	company      string
	signatory    export.Signatory
}

// forProject returns a copy of the server bound to one project's services. The
// copy is cheap - the struct is pointers and a few strings - and it means a
// request never has to thread a project through every call it makes.
func (s *Server) forProject(ctx context.Context, project model.Project, reachable []model.Project) (*Server, error) {
	services, err := s.bundles.get(ctx, project)
	if err != nil {
		return nil, err
	}
	bound := *s
	bound.project = project
	bound.reachable = reachable
	bound.attendance = services.Attendance
	bound.unitDT = services.UnitDT
	bound.produksi = services.Produksi
	bound.overview = services.Overview
	bound.unitA2B = services.UnitA2B
	bound.nota = services.Nota
	bound.leave = services.Leave
	bound.unitOverview = services.UnitOverview
	bound.fuelMasuk = services.FuelMasuk
	bound.fuelKeluar = services.FuelKeluar
	bound.hourMeter = services.HourMeter
	bound.company = services.Company
	bound.signatory = services.Signatory
	return &bound, nil
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
	Title string
	User  *model.User
	Today string
	// TodayShort is the same date in the form a phone header has room for.
	TodayShort string
	ClockNow   string
	CSRFToken  string
	NavItems   []NavItem
	ActiveNav  string
	// Project is the project this page is showing, and Projects is what the
	// switcher offers. Projects holds one entry when there is nothing to switch
	// between, and the control is left out.
	Project    string
	Projects   []string
	PageTitle  string
	Breadcrumb string
	// Section names the group the page sits in; empty for an ungrouped page.
	Section string
	// Lede is the sentence under the title in the page header.
	Lede string
	// UserInitial is the single letter shown in the account avatar.
	UserInitial string
	// The footer names the company and the current year, which the templates
	// have no way to work out on their own.
	Company string
	Year    int
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
	DriverOptions     []string
	NextUnitID        string
	Error             string
	Success           string
}

type UnitA2BFormData struct {
	Tanggal     string
	IDUnit      string
	NamaUnit    string
	MerekType   string
	FuelStorage string
	FRUnit      string
	Lokasi      string
	HMAwal      string
}

type UnitA2BPageData struct {
	ShellPageData
	Form       UnitA2BFormData
	NextNumber int
	Options    service.UnitA2BOptions
	Error      string
	Success    string
}

type ProduksiFormData struct {
	Tanggal  string
	Supplier string
	Quary    string
	Kategori string
	Lokasi   string
	Layer    string
	Nopol    string
	TT       string
}

type ExportPageData struct {
	ShellPageData
	From string
	To   string
	Rows int
	// Metode and MetodeOptions drive an optional second filter. A report with
	// no method to filter by leaves the options empty and the control is not
	// drawn, so one template still serves every date-filtered export.
	Metode        string
	MetodeOptions []service.NotaMetode
	// BasePath is where the filter posts back to and where the downloads hang
	// off, so one template serves every date-filtered export.
	BasePath string
	Note     string
	Error    string
	Ready    bool
	Company  string
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
	LokasiChart  *LokasiPlanChart
	Error        string
}

type ProduksiPageData struct {
	ShellPageData
	Form    ProduksiFormData
	Units   []model.UnitDT
	Options service.ProduksiOptions
	Error   string
	Success string
	// ScanEnabled drives the photo panel. Without a key the panel still renders,
	// disabled and saying so, rather than vanishing and leaving the operator
	// wondering where the feature went.
	ScanEnabled bool
	// ScanTimeoutMS is how long the browser waits before giving up on a scan. It
	// comes from the same setting the reader is given, so the two cannot drift
	// into the browser abandoning a scan the server is still running.
	ScanTimeoutMS int64
	// Today dates the confirmation dialog. It is kept apart from Form.Tanggal so
	// a date typed into the entry form, and bounced back by a validation error,
	// does not become the default for a sheet that has nothing to do with it.
	Today string
}

type DashboardPageData struct {
	ShellPageData
	Schedule      service.Schedule
	Attendance    *model.Attendance
	ClockInTime   string
	ClockOutTime  string
	TimezoneLabel string
	HasClockIn    bool
	HasClockOut   bool
}

// Branding is what appears on exported reports.
type Branding struct {
	Company   string
	Signatory export.Signatory
}

// Deps is what a server is built from. It is a struct rather than a parameter
// list because the list had reached eighteen arguments, and half of them were
// the same type.
type Deps struct {
	// Auth and Projects answer from the master spreadsheet, which holds the
	// accounts and the list of projects. Both are the same for every project.
	Auth     *service.AuthService
	Projects *service.ProjectService
	// Services builds one project's own services, the first time that project
	// is opened.
	Services ServiceFactory
	// Provision prepares the spreadsheet a newly added project names, so a file
	// that cannot be written to is reported on the screen that named it rather
	// than the first time somebody tries to open the project.
	Provision service.Provisioner
	Sessions  *session.Manager
	Location  *time.Location
	Now       service.NowFunc
	// Branding is the deployment default, used by the pages that belong to no
	// project - login, and the project settings screen itself.
	Branding       Branding
	MaxUploadBytes int64
	MaxPhotoChars  int
}

func NewServer(deps Deps) (*Server, error) {
	auth, sessions := deps.Auth, deps.Sessions
	location, now := deps.Location, deps.Now
	maxUploadBytes, maxPhotoChars := deps.MaxUploadBytes, deps.MaxPhotoChars
	branding := deps.Branding
	if strings.TrimSpace(branding.Company) == "" {
		branding.Company = "PT Orecon Putra Perkasa"
	}
	if deps.Services == nil {
		return nil, fmt.Errorf("server needs a service factory")
	}
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
		// Table rows are numbered from one; the range index starts at zero.
		"add1": func(index int) int { return index + 1 },
		// Money is written grouped everywhere it appears, so the templates do
		// not each reinvent the formatting.
		"rupiah": formatRupiah,
		// The avatar letter, for the lists that show people the shell header
		// does not. Walking runes is not something a template can do.
		"inisial": firstLetter,
	}).ParseFS(assetFiles, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{
		auth:           auth,
		projects:       deps.Projects,
		bundles:        newProjectCache(deps.Services),
		provision:      deps.Provision,
		defaults:       branding,
		sessions:       sessions,
		location:       location,
		now:            now,
		maxUploadBytes: maxUploadBytes,
		maxPhotoChars:  maxPhotoChars,
		company:        branding.Company,
		signatory:      branding.Signatory,
		scan: &scanGate{
			rates: make(map[string]aiScanRateEntry),
			slots: make(chan struct{}, aiScanConcurrentLimit),
		},
		templates: templates,
	}, nil
}

// WithTallyScanner enables the optional AI production tally scanner. Like the
// receipt scanner it is optional: without a key the page still renders and says
// the scan is not configured, and the form is filled in by hand.
func (s *Server) WithTallyScanner(scanner tally.Scanner, timeout time.Duration) *Server {
	s.tallyScanner = scanner
	s.scanTimeout = timeout
	return s
}

// WithReceiptScanner enables the optional AI receipt scanner. Keeping this as
// an option means Nota input and every existing server fixture keep working
// when no MiMo API key has been configured yet.
func (s *Server) WithReceiptScanner(scanner receipt.Scanner) *Server {
	s.receiptScanner = scanner
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRoot)
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/register", s.handleRegister)
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/dashboard", s.handleDashboard)
	mux.HandleFunc("/absensi", s.handleAbsensi)
	mux.HandleFunc("/leave/request", s.handleLeaveRequest)
	mux.HandleFunc("/leave/attachment", s.handleLeaveAttachment)
	mux.HandleFunc("/project/switch", s.handleProjectSwitch)
	mux.HandleFunc("/project/settings", s.handleProjectSettings)
	mux.HandleFunc("/profile", s.handleProfile)
	mux.HandleFunc("/profile/photo", s.handleProfilePhoto)
	mux.HandleFunc("/hr/overview", s.handleHROverview)
	mux.HandleFunc("/hr/approval-leave", s.handleLeaveApproval)
	mux.HandleFunc("/hr/export", s.handleAbsensiExportPage)
	mux.HandleFunc("/hr/export/download", s.handleAbsensiExportDownload)
	mux.HandleFunc("/produksi", s.handleProduksi)
	mux.HandleFunc("/produksi/scan", s.handleProduksiScan)
	mux.HandleFunc("/produksi/scan/commit", s.handleProduksiScanCommit)
	mux.HandleFunc("/produksi/plan", s.handleProduksiPlan)
	mux.HandleFunc("/produksi/overview", s.handleProduksiOverview)
	mux.HandleFunc("/produksi/export", s.handleProduksiExport)
	mux.HandleFunc("/produksi/export/download", s.handleProduksiDownload)
	mux.HandleFunc("/unit/overview", s.handleUnitOverview)
	mux.HandleFunc("/a2b/overview", s.handleA2BOverview)
	mux.HandleFunc("/a2b/hm", s.handleA2BHourMeter)
	mux.HandleFunc("/a2b/fuel-keluar", s.handleFuelKeluar)
	mux.HandleFunc("/a2b/fuel-keluar/foto", s.handleFuelKeluarPhoto)
	mux.HandleFunc("/a2b/fuel-masuk", s.handleFuelMasuk)
	mux.HandleFunc("/a2b/fuel-masuk/foto", s.handleFuelMasukPhoto)
	mux.HandleFunc("/unit/export", s.handleUnitExport)
	mux.HandleFunc("/unit/export/download", s.handleUnitDownload)
	mux.HandleFunc("/a2b/export", s.handleA2BExport)
	mux.HandleFunc("/a2b/export/download", s.handleA2BDownload)
	mux.HandleFunc("/unit-dt", s.handleUnitDT)
	mux.HandleFunc("/unit-a2b", s.handleUnitA2B)
	mux.HandleFunc("/nota", s.handleNota)
	mux.HandleFunc("/nota/scan-receipt", s.handleNotaReceiptScan)
	mux.HandleFunc("/nota/overview", s.handleNotaOverview)
	mux.HandleFunc("/nota/rekonsiliasi", s.handleRekonsiliasi)
	mux.HandleFunc("/nota/export", s.handleNotaExport)
	mux.HandleFunc("/nota/export/download", s.handleNotaDownload)
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

// requireAccess is requireUser plus the authorisation check. Hiding a menu is a
// courtesy; this is what actually keeps a page out of reach, since a URL can be
// typed, bookmarked or shared.
// The first return is the server bound to the project this request works in.
// Handlers reassign their receiver to it, so every service call after this line
// reaches that project's spreadsheet and no other.
func (s *Server) requireAccess(w http.ResponseWriter, r *http.Request, navKey string) (*Server, *model.User, session.Session, bool) {
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return nil, nil, session.Session{}, false
	}
	bound, sessionValue, ok := s.bindProject(w, r, user, sessionValue)
	if !ok {
		return nil, nil, session.Session{}, false
	}
	if !CanReach(user.Jabatan, bound.project, navKey) {
		bound.renderForbidden(w, user, sessionValue)
		return nil, nil, session.Session{}, false
	}
	return bound, user, sessionValue, true
}

// bindProject settles which project this request is working in and returns the
// server bound to it. The settled project is written back to the session, so the
// choice survives the next request without being asked again.
func (s *Server) bindProject(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session) (*Server, session.Session, bool) {
	reachable, err := s.projects.Reachable(r.Context(), user)
	if err != nil {
		log.Printf("read projects: %v", err)
		s.renderProjectProblem(w, user, sessionValue, "Daftar project gagal dimuat.")
		return nil, sessionValue, false
	}
	if len(reachable) == 0 {
		s.renderProjectProblem(w, user, sessionValue,
			"Akun ini belum ditugaskan ke project mana pun. Hubungi Management.")
		return nil, sessionValue, false
	}

	project := reachable[0]
	for _, candidate := range reachable {
		if strings.EqualFold(strings.TrimSpace(candidate.Nama), strings.TrimSpace(sessionValue.Project)) {
			project = candidate
			break
		}
	}
	if !strings.EqualFold(sessionValue.Project, project.Nama) {
		if updated, ok := s.sessions.SetProject(r, project.Nama); ok {
			sessionValue = updated
		}
	}

	bound, err := s.forProject(r.Context(), project, reachable)
	if err != nil {
		log.Printf("bind project %s: %v", project.Nama, err)
		s.renderProjectProblem(w, user, sessionValue,
			fmt.Sprintf("Project %s tidak bisa dibuka saat ini.", project.Nama))
		return nil, sessionValue, false
	}
	return bound, sessionValue, true
}

// allowedIn is requireAccess for the handlers that cannot use it: they parse a
// form before they know who is asking, so they load the user themselves and
// arrive here with a server that has no project bound yet.
//
// Like requireAccess it returns the bound server, and callers reassign their
// receiver to it.
func (s *Server) allowedIn(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, navKey string) (*Server, session.Session, bool) {
	bound, sessionValue, ok := s.bindProject(w, r, user, sessionValue)
	if !ok {
		return nil, sessionValue, false
	}
	if !CanReach(user.Jabatan, bound.project, navKey) {
		bound.renderForbidden(w, user, sessionValue)
		return nil, sessionValue, false
	}
	return bound, sessionValue, true
}

// renderProjectProblem says why there is nothing to show. It draws the shell
// with no project bound, which leaves the sidebar holding only the pages that
// belong to nobody in particular.
func (s *Server) renderProjectProblem(w http.ResponseWriter, user *model.User, sessionValue session.Session, message string) {
	data := s.shellData(user, sessionValue, "beranda")
	data.PageTitle = "Project tidak tersedia"
	data.Breadcrumb = "Project tidak tersedia"
	data.Section = ""
	data.Lede = message
	s.render(w, "forbidden", ForbiddenPageData{ShellPageData: data}, http.StatusForbidden)
}

func (s *Server) renderForbidden(w http.ResponseWriter, user *model.User, sessionValue session.Session) {
	// The shell is drawn with the menu this person does have, so the page they
	// cannot open still leaves them somewhere to go.
	data := s.shellData(user, sessionValue, "beranda")
	data.PageTitle = "Akses ditolak"
	data.Breadcrumb = "Akses ditolak"
	data.Section = ""
	data.Lede = "Halaman ini tidak terbuka untuk jabatan Anda."
	s.render(w, "forbidden", ForbiddenPageData{ShellPageData: data, Jabatan: user.Jabatan}, http.StatusForbidden)
}

type ForbiddenPageData struct {
	ShellPageData
	Jabatan string
}

func (s *Server) shellData(user *model.User, sessionValue session.Session, navKey string) ShellPageData {
	now := s.now().In(s.location)
	item, parent, _ := navItemByKey(navKey)
	return ShellPageData{
		Title:       item.Label,
		User:        user,
		Today:       formatIndonesianDate(now),
		TodayShort:  formatShortIndonesianDate(now),
		ClockNow:    now.Format("15:04"),
		CSRFToken:   sessionValue.CSRFToken,
		NavItems:    navItemsFor(user.Jabatan, s.project),
		Project:     s.project.Nama,
		Projects:    projectNames(s.reachable),
		ActiveNav:   navKey,
		PageTitle:   item.Label,
		Breadcrumb:  item.Label,
		Lede:        item.Lede,
		Section:     parent.Label,
		UserInitial: firstLetter(user.NamaLengkap),
		Company:     s.company,
		Year:        now.Year(),
	}
}

// firstLetter returns the avatar letter. It walks runes rather than bytes so a
// non-ASCII name does not produce a broken half-character.
func firstLetter(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return "?"
}

type BerandaPageData struct {
	ShellPageData
	Summary      *service.AttendanceSummary
	LeaveSummary *service.LeavePersonalSummary
	LeaveError   string
	// JamChart plots the hours worked per day; a gap in it is a day nobody
	// clocked in, which is as much a reading as a tall bar.
	JamChart *Chart
	Error    string
}

// handleDashboard shows the signed-in person their own attendance. It answers
// "how am I doing", which is why it never reads anyone else's rows.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "beranda")
	if !ok {
		return
	}
	data := BerandaPageData{ShellPageData: s.shellData(user, sessionValue, "beranda")}

	summary, err := s.attendance.Summary(r.Context(), user.UserID)
	if err != nil {
		log.Printf("build attendance summary: %v", err)
		data.Error = "Gagal memuat riwayat kehadiran"
		s.render(w, "beranda", data, http.StatusOK)
		return
	}

	labels := make([]string, 0, len(summary.Series))
	hours := make([]float64, 0, len(summary.Series))
	for _, day := range summary.Series {
		labels = append(labels, day.Label)
		hours = append(hours, day.Jam)
	}
	data.Summary = summary
	data.JamChart = BuildValueChart(labels, hours, 1)
	leaveSummary, leaveErr := s.leave.PersonalSummary(r.Context(), user.UserID)
	if leaveErr != nil {
		log.Printf("build personal leave summary: %v", leaveErr)
		data.LeaveError = "Ringkasan cuti dan izin belum dapat dimuat."
	} else {
		if leaveSummary.TodayStatus == "" {
			leaveSummary.TodayStatus = "Tidak ada leave"
		}
		data.LeaveSummary = leaveSummary
	}
	s.render(w, "beranda", data, http.StatusOK)
}

func (s *Server) handleAbsensi(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "absensi")
	if !ok {
		return
	}
	attendance, err := s.attendance.Today(r.Context(), user.UserID)
	if err != nil {
		log.Printf("load attendance: %v", err)
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
	s.render(w, "absensi", DashboardPageData{
		ShellPageData: s.shellData(user, sessionValue, "absensi"),
		Schedule:      s.attendance.Schedule(),
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
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-input")
	if !ok {
		return
	}
	s.renderProduksi(w, r, user, sessionValue, ProduksiFormData{
		Tanggal: s.produksi.Today(),
	}, "", "", http.StatusOK)
}

func (s *Server) handleProduksiExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-export")
	if !ok {
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))

	data := ExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-export"),
		From:          from,
		To:            to,
		BasePath:      "/produksi/export",
		Note: fmt.Sprintf("Kop laporan memakai logo dan nama %s. PDF dicetak mendatar karena "+
			"tabelnya 20 kolom, dan halaman terakhir memuat blok tanda tangan.", s.company),
		Ready:   true,
		Company: s.company,
	}
	rows, appliedFrom, appliedTo, err := s.produksi.RowsBetween(r.Context(), from, to)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("count produksi for export: %v", err)
			data.Error = "Gagal memuat data produksi"
		}
		s.render(w, "export_page", data, http.StatusOK)
		return
	}
	data.Rows = len(rows)
	data.From = appliedFrom
	data.To = appliedTo
	s.render(w, "export_page", data, http.StatusOK)
}

// handleProduksiDownload streams the report itself.
func (s *Server) handleProduksiDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "produksi-export")
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "xlsx" && format != "pdf" {
		http.Error(w, "format tidak dikenal", http.StatusBadRequest)
		return
	}

	rows, from, to, err := s.produksi.RowsBetween(r.Context(),
		r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read produksi for export: %v", err)
		http.Error(w, "Gagal memuat data produksi", http.StatusInternalServerError)
		return
	}

	meta := s.exportMeta("Laporan Produksi", from, to)
	var payload []byte
	var contentType, extension string
	if format == "xlsx" {
		payload, err = export.ProduksiXLSX(rows, meta)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
		extension = "xlsx"
	} else {
		payload, err = export.ProduksiPDF(rows, meta)
		contentType = "application/pdf"
		extension = "pdf"
	}
	if err != nil {
		log.Printf("build produksi %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}

	filename := fmt.Sprintf("laporan-produksi-%s.%s", exportPeriodSlug(from, to), extension)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	// A report is a snapshot of a moving sheet; a cached copy would quietly go
	// stale behind the person downloading it.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

type NotaOverviewPageData struct {
	ShellPageData
	Overview *service.NotaOverview
	From     string
	To       string
	// The money figures are printed grouped, so the template does not have to
	// know how rupiah is written.
	TotalRupiah       string
	OutstandingRupiah string
	DibayarRupiah     string
	RataRupiah        string
	SpendChart        *Chart
	MethodChart       *Chart
	CountChart        *Chart
	Error             string
}

func (s *Server) handleNotaOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "nota-overview")
	if !ok {
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	data := NotaOverviewPageData{
		ShellPageData: s.shellData(user, sessionValue, "nota-overview"),
		From:          from,
		To:            to,
	}

	overview, err := s.nota.BuildOverview(r.Context(), from, to)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build nota overview: %v", err)
			data.Error = "Gagal memuat data nota"
		}
		s.render(w, "nota_overview", data, http.StatusOK)
		return
	}

	labels := make([]string, 0, len(overview.Series))
	spend := make([]float64, 0, len(overview.Series))
	advances := make([]float64, 0, len(overview.Series))
	reimbursements := make([]float64, 0, len(overview.Series))
	counts := make([]float64, 0, len(overview.Series))
	for _, point := range overview.Series {
		labels = append(labels, point.Label)
		spend = append(spend, overview.Scaled(point.Total))
		advances = append(advances, overview.Scaled(point.CA))
		reimbursements = append(reimbursements, overview.Scaled(point.Reimburse))
		counts = append(counts, float64(point.Jumlah))
	}

	data.Overview = overview
	data.From = overview.From
	data.To = overview.To
	data.TotalRupiah = formatRupiah(overview.TotalPengeluaran)
	data.OutstandingRupiah = formatRupiah(overview.Outstanding)
	data.DibayarRupiah = formatRupiah(overview.Dibayar)
	data.RataRupiah = formatRupiah(overview.RataRata)
	data.SpendChart = BuildLineChart(labels, spend, 2)
	data.MethodChart = BuildGroupedChart(labels, advances, reimbursements,
		GroupedSeries{Name: "real", Label: "CA", Decimals: 2},
		GroupedSeries{Name: "opp", Label: "Reimburse", Decimals: 2})
	data.CountChart = BuildValueChart(labels, counts, 0)
	s.render(w, "nota_overview", data, http.StatusOK)
}

// RekonsiliasiRow is one outstanding reimbursement as the table shows it.
type RekonsiliasiRow struct {
	model.Nota
	TotalRupiah string
	// Selected renders this row's dialog already open, which is how the page
	// behaves for a browser that never ran the script.
	Selected bool
}

type RekonsiliasiPageData struct {
	ShellPageData
	Query   string
	Rows    []RekonsiliasiRow
	Total   string
	Error   string
	Success string
}

func (s *Server) handleRekonsiliasi(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleRekonsiliasiSettle(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "nota-rekonsiliasi")
	if !ok {
		return
	}
	s.renderRekonsiliasi(w, r, user, sessionValue,
		strings.TrimSpace(r.URL.Query().Get("q")),
		strings.TrimSpace(r.URL.Query().Get("nota")), "", "", http.StatusOK)
}

func (s *Server) handleRekonsiliasiSettle(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "nota-rekonsiliasi")
	if !okProject {
		return
	}

	maxBody := s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderRekonsiliasi(w, r, user, sessionValue, "", "", "Form tidak valid atau file terlalu besar", "", http.StatusUnprocessableEntity)
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

	query := strings.TrimSpace(r.FormValue("q"))
	notaID := strings.TrimSpace(r.FormValue("nota_id"))
	bukti, err := s.readOptionalPhoto(r, "bukti_bayar")
	if err != nil {
		// The dialog reopens on the nota that failed, so the message sits next
		// to the upload it is about.
		s.renderRekonsiliasi(w, r, user, sessionValue, query, notaID, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}

	nota, err := s.nota.Settle(r.Context(), user, notaID, bukti)
	if err != nil {
		message := "Rekonsiliasi gagal"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrNotaNotFound):
			message = "Nota tidak ditemukan"
			status = http.StatusNotFound
		case errors.Is(err, service.ErrNotaAlreadyPaid):
			message = "Nota ini sudah ditandai dibayar"
			status = http.StatusConflict
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Bukti bayar tidak valid"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("settle nota: %v", err)
			message = "Terjadi kesalahan saat menyimpan rekonsiliasi"
			status = http.StatusInternalServerError
		}
		s.renderRekonsiliasi(w, r, user, sessionValue, query, notaID, message, "", status)
		return
	}

	s.renderRekonsiliasi(w, r, user, sessionValue, query, "", "",
		fmt.Sprintf("Nota %s ditandai %s sebesar Rp %s.",
			nota.NotaID, strings.ToLower(nota.StatusPembayaran), formatRupiah(nota.Total)),
		http.StatusOK)
}

func (s *Server) renderRekonsiliasi(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, query, openNotaID, errMessage, success string, status int) {
	data := RekonsiliasiPageData{
		ShellPageData: s.shellData(user, sessionValue, "nota-rekonsiliasi"),
		Query:         query,
		Error:         errMessage,
		Success:       success,
	}
	openNotaID = strings.ToUpper(strings.TrimSpace(openNotaID))
	rows, err := s.nota.Outstanding(r.Context(), query)
	if err != nil {
		log.Printf("read outstanding nota: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat data nota"
		}
		s.render(w, "rekonsiliasi", data, status)
		return
	}
	outstanding := 0.0
	for _, nota := range rows {
		data.Rows = append(data.Rows, RekonsiliasiRow{
			Nota:        nota,
			TotalRupiah: formatRupiah(nota.Total),
			Selected:    openNotaID != "" && strings.EqualFold(nota.NotaID, openNotaID),
		})
		outstanding += nota.Total
	}
	data.Total = formatRupiah(outstanding)
	s.render(w, "rekonsiliasi", data, status)
}

func (s *Server) handleNotaExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "nota-export")
	if !ok {
		return
	}
	filter := service.NotaFilter{
		From:   strings.TrimSpace(r.URL.Query().Get("from")),
		To:     strings.TrimSpace(r.URL.Query().Get("to")),
		Metode: strings.TrimSpace(r.URL.Query().Get("metode")),
	}

	data := ExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "nota-export"),
		From:          filter.From,
		To:            filter.To,
		Metode:        filter.Metode,
		MetodeOptions: service.NotaMetodeOptions,
		BasePath:      "/nota/export",
		Note: fmt.Sprintf("Kop laporan memakai logo dan nama %s. Satu baris mewakili satu item "+
			"nota, sehingga rinciannya tetap terlihat, dan halaman terakhir memuat blok tanda tangan.", s.company),
		Ready:   true,
		Company: s.company,
	}
	rows, applied, err := s.nota.RowsBetween(r.Context(), filter)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("count nota for export: %v", err)
			data.Error = "Gagal memuat data nota"
		}
		s.render(w, "export_page", data, http.StatusOK)
		return
	}
	// The report has one row per item, so the count on the page has to be the
	// number of rows the file will hold, not the number of notes.
	data.Rows = countNotaItems(rows)
	data.From = applied.From
	data.To = applied.To
	data.Metode = applied.Metode
	s.render(w, "export_page", data, http.StatusOK)
}

func countNotaItems(rows []model.Nota) int {
	total := 0
	for _, nota := range rows {
		total += len(nota.Items)
	}
	return total
}

func (s *Server) handleNotaDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "nota-export")
	if !ok {
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "xlsx" && format != "pdf" {
		http.Error(w, "format tidak dikenal", http.StatusBadRequest)
		return
	}

	rows, applied, err := s.nota.ExportRowsBetween(r.Context(), service.NotaFilter{
		From:   r.URL.Query().Get("from"),
		To:     r.URL.Query().Get("to"),
		Metode: r.URL.Query().Get("metode"),
	})
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			http.Error(w, strings.TrimPrefix(err.Error(), "validation error: "), http.StatusUnprocessableEntity)
			return
		}
		log.Printf("read nota for export: %v", err)
		http.Error(w, "Gagal memuat data nota", http.StatusInternalServerError)
		return
	}

	// A report covering one payment method has to say so on its own letterhead:
	// filtered figures read as the complete set otherwise.
	title := "Laporan Nota"
	if applied.Metode != "" {
		title += " - " + service.NotaMetodeLabel(applied.Metode)
	}
	meta := s.exportMeta(title, applied.From, applied.To)
	var payload []byte
	contentType := "application/pdf"
	if format == "xlsx" {
		payload, err = export.NotaXLSX(rows, meta)
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	} else {
		payload, err = export.NotaPDF(rows, meta)
	}
	if err != nil {
		log.Printf("build nota %s: %v", format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}

	// The method goes in the name too, so two downloads of the same period do
	// not overwrite each other in the downloads folder.
	filename := fmt.Sprintf("laporan-nota-%s%s.%s",
		exportMetodeSlug(applied.Metode), exportPeriodSlug(applied.From, applied.To), format)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	// A report is a snapshot of a moving sheet; a cached copy would quietly go
	// stale behind the person downloading it.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

func (s *Server) exportMeta(title, from, to string) export.Meta {
	logo, err := assetFiles.ReadFile("static/img/opp-logo.png")
	if err != nil {
		// A missing logo costs the letterhead its mark, not the report.
		log.Printf("read logo for export: %v", err)
	}
	return export.Meta{
		Company:   s.company,
		Title:     title,
		Period:    exportPeriodLabel(from, to),
		Generated: s.now().In(s.location),
		Logo:      logo,
		Signatory: s.signatory,
	}
}

func exportPeriodLabel(from, to string) string {
	switch {
	case from != "" && to != "":
		return from + " s/d " + to
	case from != "":
		return "Sejak " + from
	case to != "":
		return "Sampai " + to
	default:
		return ""
	}
}

// exportMetodeSlug is the method as a filename fragment, empty when the report
// covers every method.
func exportMetodeSlug(metode string) string {
	if metode = strings.TrimSpace(metode); metode == "" {
		return ""
	}
	return strings.ToLower(metode) + "-"
}

func exportPeriodSlug(from, to string) string {
	switch {
	case from != "" && to != "":
		return from + "_" + to
	case from != "":
		return "sejak-" + from
	case to != "":
		return "sampai-" + to
	default:
		return "semua"
	}
}

type UnitOverviewPageData struct {
	ShellPageData
	Overview *service.UnitOverview
	Error    string
}

func (s *Server) handleUnitOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "unit-overview")
	if !ok {
		return
	}
	data := UnitOverviewPageData{ShellPageData: s.shellData(user, sessionValue, "unit-overview")}

	overview, err := s.unitOverview.Build(r.Context())
	if err != nil {
		log.Printf("build unit overview: %v", err)
		data.Error = "Gagal memuat data unit"
		s.render(w, "unit_overview", data, http.StatusOK)
		return
	}

	data.Overview = overview
	s.render(w, "unit_overview", data, http.StatusOK)
}

// A2BOverviewPageData drives the machine dashboard: what the fleet did over a
// range, rather than what the register holds.
type A2BOverviewPageData struct {
	ShellPageData
	Overview    *service.A2BOverview
	From        string
	To          string
	UnitChart   *Chart
	FuelChart   *Chart
	DelaySlices []DelaySlice
	Error       string
}

// DelaySlice is one wedge of the delay donut, ready to draw.
type DelaySlice struct {
	service.A2BDelayShare
	Dash      string
	Offset    string
	ClassName string
}

func (s *Server) handleA2BOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-overview")
	if !ok {
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" && to == "" {
		from, to = s.unitOverview.DefaultA2BRange()
	}
	data := A2BOverviewPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-overview"),
		From:          from,
		To:            to,
	}

	overview, err := s.unitOverview.BuildA2B(r.Context(), from, to, s.hourMeter.WorkMinutes())
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build a2b overview: %v", err)
			data.Error = "Gagal memuat data alat berat"
		}
		s.render(w, "a2b_overview", data, http.StatusOK)
		return
	}
	data.Overview = overview
	data.From = overview.From
	data.To = overview.To
	data.DelaySlices = buildDelaySlices(overview.TopDelay)

	labels := make([]string, 0, len(overview.Series))
	active := make([]float64, 0, len(overview.Series))
	broken := make([]float64, 0, len(overview.Series))
	masuk := make([]float64, 0, len(overview.Series))
	keluar := make([]float64, 0, len(overview.Series))
	for _, point := range overview.Series {
		labels = append(labels, point.Label)
		active = append(active, float64(point.UnitActive))
		broken = append(broken, float64(point.UnitBreakdown))
		masuk = append(masuk, point.FuelMasuk)
		keluar = append(keluar, point.FuelKeluar)
	}
	// Whole machines only; half a bulldozer is not a reading.
	data.UnitChart = BuildGroupedChart(labels, active, broken,
		GroupedSeries{Name: "aktif", Label: "Unit active"},
		GroupedSeries{Name: "rusak", Label: "Unit breakdown"})
	data.FuelChart = BuildGroupedChart(labels, masuk, keluar,
		GroupedSeries{Name: "masuk", Label: "Fuel masuk", Decimals: 2},
		GroupedSeries{Name: "keluar", Label: "Fuel keluar", Decimals: 2})
	s.render(w, "a2b_overview", data, http.StatusOK)
}

// buildDelaySlices lays the reasons round the donut in the order they were
// ranked, so the same reason keeps the same colour as long as it keeps its
// place.
func buildDelaySlices(shares []service.A2BDelayShare) []DelaySlice {
	slices := make([]DelaySlice, 0, len(shares))
	offset := 0.0
	for index, share := range shares {
		percent := share.Percent
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		slices = append(slices, DelaySlice{
			A2BDelayShare: share,
			Dash:          fmt.Sprintf("%.2f %.2f", percent, 100-percent),
			Offset:        fmt.Sprintf("%.2f", -offset),
			ClassName:     fmt.Sprintf("role-slice-%d", index%8),
		})
		offset += percent
	}
	return slices
}

// RegisterExportPageData drives one register's download page. Each register has
// its own page under its own menu: they describe different machines and are
// downloaded by different people.
type RegisterExportPageData struct {
	ShellPageData
	Register string
	Rows     int
	BasePath string
	Note     string
	Error    string
	Company  string
}

func (s *Server) handleUnitExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "unit-export")
	if !ok {
		return
	}
	data := RegisterExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "unit-export"),
		Company:       s.company,
		BasePath:      "/unit/export",
		Register:      "Unit DT",
		Note:          "Daftar dump truck lengkap dengan ukuran bak dan drivernya.",
	}
	units, err := s.produksi.Units(r.Context())
	if err != nil {
		log.Printf("count unit dt for export: %v", err)
		data.Error = "Gagal memuat data unit"
	}
	data.Rows = len(units)
	s.render(w, "register_export", data, http.StatusOK)
}

func (s *Server) handleA2BExport(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	data := RegisterExportPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-export"),
		Company:       s.company,
		BasePath:      "/a2b/export",
		Register:      "Unit A2B",
		Note:          "Daftar alat berat lengkap dengan kapasitas tangki, konsumsi per jam, dan lokasinya.",
	}
	units, err := s.unitA2B.List(r.Context())
	if err != nil {
		log.Printf("count unit a2b for export: %v", err)
		data.Error = "Gagal memuat data unit"
	}
	data.Rows = len(units)
	s.render(w, "register_export", data, http.StatusOK)
}

// handleUnitDownload streams the dump truck register.
func (s *Server) handleUnitDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "unit-export")
	if !ok {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	units, err := s.produksi.Units(r.Context())
	if err != nil {
		log.Printf("read unit dt for export: %v", err)
		http.Error(w, "Gagal memuat data unit", http.StatusInternalServerError)
		return
	}
	meta := s.exportMeta("Daftar Unit DT", "", "")
	meta.Period = export.SnapshotLabel(meta.Generated)

	var payload []byte
	if format == "xlsx" {
		payload, err = export.UnitDTXLSX(units, meta)
	} else {
		payload, err = export.UnitDTPDF(units, meta)
	}
	s.writeRegister(w, "unit-dt", format, payload, err)
}

// handleA2BDownload streams the machine register. It is a file of its own: the
// two registers describe different machines with different columns, and merging
// them would leave half of every row empty.
func (s *Server) handleA2BDownload(w http.ResponseWriter, r *http.Request) {
	s, _, _, ok := s.requireAccess(w, r, "a2b-export")
	if !ok {
		return
	}
	format, ok := downloadFormat(w, r)
	if !ok {
		return
	}
	units, err := s.unitA2B.List(r.Context())
	if err != nil {
		log.Printf("read unit a2b for export: %v", err)
		http.Error(w, "Gagal memuat data unit", http.StatusInternalServerError)
		return
	}
	meta := s.exportMeta("Daftar Unit A2B", "", "")
	meta.Period = export.SnapshotLabel(meta.Generated)

	var payload []byte
	if format == "xlsx" {
		payload, err = export.UnitA2BXLSX(units, meta)
	} else {
		payload, err = export.UnitA2BPDF(units, meta)
	}
	s.writeRegister(w, "unit-a2b", format, payload, err)
}

func downloadFormat(w http.ResponseWriter, r *http.Request) (string, bool) {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "xlsx" && format != "pdf" {
		http.Error(w, "format tidak dikenal", http.StatusBadRequest)
		return "", false
	}
	return format, true
}

func (s *Server) writeRegister(w http.ResponseWriter, slug, format string, payload []byte, err error) {
	if err != nil {
		log.Printf("build %s %s: %v", slug, format, err)
		http.Error(w, "Gagal membuat berkas", http.StatusInternalServerError)
		return
	}
	contentType := "application/pdf"
	if format == "xlsx" {
		contentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("daftar-%s.%s", slug, format)))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	// A register is a snapshot of a moving sheet; a cached copy would quietly go
	// stale behind the person downloading it.
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(payload)
}

func (s *Server) handleUnitA2B(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleUnitA2BCreate(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "a2b-unit")
	if !ok {
		return
	}
	s.renderUnitA2B(w, r, user, sessionValue, UnitA2BFormData{Tanggal: s.unitA2B.Today()}, "", "", http.StatusOK)
}

func (s *Server) handleUnitA2BCreate(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "a2b-unit")
	if !okProject {
		return
	}

	// Bound the body before parsing, then check CSRF from the parsed form, the
	// same order the Unit DT form uses.
	maxBody := s.maxUploadBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderUnitA2B(w, r, user, sessionValue, UnitA2BFormData{}, "Form tidak valid atau file terlalu besar", "", http.StatusUnprocessableEntity)
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

	form := UnitA2BFormData{
		Tanggal:     strings.TrimSpace(r.FormValue("tanggal")),
		IDUnit:      strings.TrimSpace(r.FormValue("id_unit")),
		NamaUnit:    strings.TrimSpace(r.FormValue("nama_unit")),
		MerekType:   strings.TrimSpace(r.FormValue("merek_type")),
		FuelStorage: strings.TrimSpace(r.FormValue("fuel_storage")),
		FRUnit:      strings.TrimSpace(r.FormValue("fr_unit")),
		Lokasi:      strings.TrimSpace(r.FormValue("lokasi")),
		HMAwal:      strings.TrimSpace(r.FormValue("hm_awal")),
	}

	photoValue, err := s.readOptionalPhoto(r, "foto_unit")
	if err != nil {
		s.renderUnitA2B(w, r, user, sessionValue, form, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}

	unit, err := s.unitA2B.Create(r.Context(), user, service.UnitA2BInput{
		Tanggal:     form.Tanggal,
		IDUnit:      form.IDUnit,
		NamaUnit:    form.NamaUnit,
		MerekType:   form.MerekType,
		FuelStorage: form.FuelStorage,
		FRUnit:      form.FRUnit,
		Lokasi:      form.Lokasi,
		HMAwal:      form.HMAwal,
		Foto:        photoValue,
	})
	if err != nil {
		message := "Data unit tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrDuplicateUnitA2B):
			message = "ID unit sudah terdaftar"
			status = http.StatusConflict
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Foto unit tidak valid"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create unit a2b: %v", err)
			message = "Terjadi kesalahan saat menyimpan unit"
			status = http.StatusInternalServerError
		}
		s.renderUnitA2B(w, r, user, sessionValue, form, message, "", status)
		return
	}

	s.renderUnitA2B(w, r, user, sessionValue,
		UnitA2BFormData{Tanggal: s.unitA2B.Today()},
		"",
		fmt.Sprintf("Unit %s tersimpan sebagai nomor urut %d.", unit.IDUnit, unit.NoUrut),
		http.StatusOK)
}

func (s *Server) renderUnitA2B(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form UnitA2BFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.unitA2B.Today()
	}
	// The running number is a preview only. A failure here must not block the
	// form, since Create assigns the authoritative number anyway.
	next, err := s.unitA2B.NextNumber(r.Context())
	if err != nil {
		log.Printf("preview unit a2b number: %v", err)
	}
	options, err := s.unitA2B.Options(r.Context())
	if err != nil {
		// The pickers fall back to free typing, which still works.
		log.Printf("load unit a2b options: %v", err)
	}
	s.render(w, "unit_a2b", UnitA2BPageData{
		ShellPageData: s.shellData(user, sessionValue, "a2b-unit"),
		Form:          form,
		NextNumber:    next,
		Options:       options,
		Error:         errMessage,
		Success:       success,
	}, status)
}

func (s *Server) handleProduksiOverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-overview")
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
	data.LokasiChart = buildLokasiPlanChart(overview.LokasiShares, overview.TotalVolume, overview.TotalRencana)
	data.VolumeChart = BuildLineChart(seriesLabels(overview.Series), seriesVolumes(overview.Series), 0)
	// Side by side rather than stacked: stacked, the DT Besar count was a sliver
	// on top of the DT Kecil bar and the badge carried only the total, so the
	// two numbers the chart is named after could not be read off it.
	data.RitaseChart = BuildGroupedChart(seriesLabels(overview.Series),
		countsAsFloats(seriesKecil(overview.Series)), countsAsFloats(seriesBesar(overview.Series)),
		GroupedSeries{Name: "kecil", Label: "DT Kecil"},
		GroupedSeries{Name: "besar", Label: "DT Besar"})
	data.UnitChart = BuildValueChart(seriesLabels(overview.Series), seriesUnits(overview.Series), 0)
	data.CompareChart = BuildGroupedChart(seriesLabels(overview.Series),
		seriesVolumes(overview.Series), seriesOPP(overview.Series),
		GroupedSeries{Name: "real", Label: "Volume Real", Decimals: 2},
		GroupedSeries{Name: "opp", Label: "Volume OPP", Decimals: 2})
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

// countsAsFloats feeds whole counts to a chart that plots decimals.
func countsAsFloats(values []int) []float64 {
	out := make([]float64, len(values))
	for i, value := range values {
		out[i] = float64(value)
	}
	return out
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "produksi-input")
	if !okProject {
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
		Supplier: strings.TrimSpace(r.FormValue("supplier")),
		Quary:    strings.TrimSpace(r.FormValue("quary")),
		Kategori: strings.TrimSpace(r.FormValue("kategori")),
		Lokasi:   strings.TrimSpace(r.FormValue("lokasi")),
		Layer:    strings.TrimSpace(r.FormValue("layer")),
		Nopol:    strings.TrimSpace(r.FormValue("nopol")),
		TT:       strings.TrimSpace(r.FormValue("tt")),
	}

	produksi, err := s.produksi.Create(r.Context(), user, service.ProduksiInput{
		Tanggal: form.Tanggal,
		// Stamped from the project this request is working in, not read off the
		// form: the row is going into that project's spreadsheet either way.
		Project:  s.project.Nama,
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
	options, err := s.produksi.Options(r.Context())
	if err != nil {
		// The pickers fall back to typing freely, which still works, so this
		// must not take the form down.
		log.Printf("load produksi options: %v", err)
	}
	s.render(w, "produksi", ProduksiPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-input"),
		Form:          form,
		Units:         units,
		Options:       options,
		Error:         errMessage,
		Success:       success,
		ScanEnabled:   s.tallyScanner != nil,
		ScanTimeoutMS: s.scanBudget().Milliseconds(),
		Today:         s.produksi.Today(),
	}, status)
}

func (s *Server) handleUnitDT(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleUnitDTCreate(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "unit-dt")
	if !ok {
		return
	}
	s.render(w, "unit_dt", UnitDTPageData{
		ShellPageData:     s.shellData(user, sessionValue, "unit-dt"),
		Form:              UnitDTFormData{Keterangan: service.DefaultKeterangan},
		KeteranganOptions: service.KeteranganOptions,
		DriverOptions:     s.driverOptions(r),
		NextUnitID:        s.nextUnitID(r),
	}, http.StatusOK)
}

// driverOptions backs the driver picker. A failure only costs the suggestions,
// so the form still renders and the field can be typed into.
func (s *Server) driverOptions(r *http.Request) []string {
	drivers, err := s.unitDT.Drivers(r.Context())
	if err != nil {
		log.Printf("load unit dt drivers: %v", err)
		return nil
	}
	return drivers
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "unit-dt")
	if !okProject {
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
		DriverOptions:     s.driverOptions(r),
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
		DriverOptions:     s.driverOptions(r),
		NextUnitID:        s.nextUnitID(r),
		Error:             message,
	}, status)
}

// readOptionalPhoto normalises an uploaded image to the same compressed data
// URL the attendance photos use. An absent file yields an empty value; only a
// file that is present but unreadable is an error.
// NotaFormData is the form as typed, so a rejected submission comes back
// filled in rather than blank.
type NotaFormData struct {
	Tanggal           string
	PIC               string
	Metode            string
	PenerimaReimburse string
	Kategori          string
	SubKategori       string
	JenisPerjalanan   string
	Items             []service.NotaItemInput
}

// Status is what the badge shows while typing. The stored status is decided by
// the service; this only mirrors it so the form does not lie about it.
func (f NotaFormData) Status() string { return service.StatusFor(f.Metode) }

func (f NotaFormData) IsCA() bool { return f.Metode == model.NotaMetodeCA }

type NotaPageData struct {
	ShellPageData
	Form               NotaFormData
	NextID             string
	Options            service.NotaOptions
	MetodeOptions      []service.NotaMetode
	KategoriOptions    []service.NotaKategori
	JenisOptions       []string
	PerjalananDinas    string
	ReceiptScanEnabled bool
	Error              string
	Success            string
}

func (s *Server) handleNota(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleNotaCreate(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "nota-input")
	if !ok {
		return
	}
	s.renderNota(w, r, user, sessionValue, NotaFormData{Tanggal: s.nota.Today()}, "", "", http.StatusOK)
}

// handleNotaReceiptScan extracts purchase lines from one receipt image. It is
// deliberately transient: the result only prefills the editable Nota form and
// never reaches the repository until the person reviews and submits that form.
func (s *Server) handleNotaReceiptScan(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}

	sessionValue, ok := s.currentSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "Sesi tidak valid. Silakan masuk kembali."})
		return
	}
	user, err := s.auth.LoadUser(r.Context(), sessionValue.UserID)
	if err != nil || user.StatusPengguna != model.StatusAktif {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"ok": false, "error": "Sesi tidak valid. Silakan masuk kembali."})
		return
	}
	if !CanAccess(user.Jabatan, "nota-input") {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "Jabatan Anda tidak berhak mengakses input Nota."})
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "CSRF token tidak valid"})
		return
	}
	if s.receiptScanner == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{"ok": false, "error": "Scan struk belum dikonfigurasi. Anda tetap dapat mengisi item secara manual."})
		return
	}
	if allowed, retryAfter := s.allowAIScan(sessionValue.UserID); !allowed {
		writeScanLimit(w, retryAfter, "Batas scan struk tercapai. Silakan coba lagi sebentar.")
		return
	}
	select {
	case s.scan.slots <- struct{}{}:
		defer func() { <-s.scan.slots }()
	default:
		writeScanLimit(w, time.Second, "Layanan scan struk sedang memproses permintaan lain. Silakan coba lagi.")
		return
	}

	maxReceiptBytes := min(s.maxUploadBytes, photo.MaxInputBytes)
	maxBody := maxReceiptBytes + 64*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Foto struk tidak valid atau terlalu besar."})
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	files := r.MultipartForm.File["receipt"]
	fileCount := 0
	for _, headers := range r.MultipartForm.File {
		fileCount += len(headers)
	}
	if len(files) != 1 || fileCount != 1 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Pilih tepat satu foto struk."})
		return
	}
	file, err := files[0].Open()
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Foto struk tidak dapat dibaca."})
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(file, maxReceiptBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Foto struk tidak dapat dibaca."})
		return
	}
	if len(raw) == 0 || int64(len(raw)) > maxReceiptBytes {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": fmt.Sprintf("Ukuran foto struk maksimal %d MB.", maxReceiptBytes/(1024*1024))})
		return
	}
	imageDataURL, err := photo.RawDataURL(raw)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{"ok": false, "error": "Gunakan foto struk berformat JPEG, PNG, atau WebP yang valid."})
		return
	}

	result, err := s.receiptScanner.Scan(r.Context(), imageDataURL)
	if err != nil {
		status, message := receiptScanError(err)
		// Scanner errors are intentionally classified; never log an image,
		// provider response, prompt, or credential here.
		log.Printf("receipt scan failed (status=%d, kind=%T)", status, err)
		writeJSON(w, status, map[string]interface{}{"ok": false, "error": message})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":            true,
		"items":         result.Items,
		"total_terbaca": result.TotalTerbaca,
		"warnings":      result.Warnings,
	})
}

// allowAIScan applies a small, per-user fixed window before any receipt
// bytes are decoded or sent to the provider. The state map is capped so even a
// long-running server cannot accumulate an unbounded number of user keys.
func (s *Server) allowAIScan(userID string) (bool, time.Duration) {
	now := time.Now()
	if s.now != nil {
		now = s.now()
	}

	s.scan.mu.Lock()
	defer s.scan.mu.Unlock()
	if s.scan.rates == nil {
		s.scan.rates = make(map[string]aiScanRateEntry)
	}

	entry, exists := s.scan.rates[userID]
	if exists && (now.Before(entry.windowStart) || !now.Before(entry.windowStart.Add(aiScanRateWindow))) {
		entry = aiScanRateEntry{windowStart: now}
	}
	if !exists {
		s.makeScanRateRoom(now)
		entry = aiScanRateEntry{windowStart: now}
	}
	if entry.count >= aiScanRateLimit {
		retryAfter := entry.windowStart.Add(aiScanRateWindow).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		return false, retryAfter
	}
	entry.count++
	s.scan.rates[userID] = entry
	return true, 0
}

// makeScanRateRoom is only called for a new user. It first removes expired
// windows, then evicts the oldest remaining entry if the hard cap is full.
func (s *Server) makeScanRateRoom(now time.Time) {
	if len(s.scan.rates) < aiScanRateMaxUsers {
		return
	}
	for userID, entry := range s.scan.rates {
		if now.Before(entry.windowStart) || !now.Before(entry.windowStart.Add(aiScanRateWindow)) {
			delete(s.scan.rates, userID)
		}
	}
	if len(s.scan.rates) < aiScanRateMaxUsers {
		return
	}

	oldestUserID := ""
	var oldestWindow time.Time
	oldestFound := false
	for userID, entry := range s.scan.rates {
		if !oldestFound || entry.windowStart.Before(oldestWindow) {
			oldestUserID = userID
			oldestWindow = entry.windowStart
			oldestFound = true
		}
	}
	if oldestFound {
		delete(s.scan.rates, oldestUserID)
	}
}

func writeScanLimit(w http.ResponseWriter, retryAfter time.Duration, message string) {
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{"ok": false, "error": message})
}

func receiptScanError(err error) (int, string) {
	switch {
	case errors.Is(err, receipt.ErrNoItems):
		return http.StatusUnprocessableEntity, "Tidak ada produk yang dapat dibaca dari foto ini. Coba foto yang lebih jelas atau isi manual."
	case errors.Is(err, receipt.ErrTimeout):
		return http.StatusGatewayTimeout, "Scan struk terlalu lama. Silakan coba lagi."
	case errors.Is(err, receipt.ErrRateLimited), errors.Is(err, receipt.ErrUnavailable):
		return http.StatusServiceUnavailable, "Layanan scan struk sedang sibuk. Silakan coba lagi sebentar lagi."
	case errors.Is(err, receipt.ErrInvalidResponse):
		return http.StatusBadGateway, "Hasil scan belum dapat dibaca dengan aman. Silakan coba lagi atau isi manual."
	default:
		return http.StatusBadGateway, "Scan struk gagal. Silakan coba lagi atau isi manual."
	}
}

func (s *Server) handleNotaCreate(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "nota-input")
	if !okProject {
		return
	}

	// A nota carries two attachments, so the body allowance is twice the
	// single-photo one before the form fields are counted.
	maxBody := 2*s.maxUploadBytes + 128*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderNota(w, r, user, sessionValue, NotaFormData{}, "Form tidak valid atau file terlalu besar", "", http.StatusUnprocessableEntity)
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

	form := NotaFormData{
		Tanggal:           strings.TrimSpace(r.FormValue("tanggal")),
		PIC:               strings.TrimSpace(r.FormValue("pic")),
		Metode:            strings.ToUpper(strings.TrimSpace(r.FormValue("metode"))),
		PenerimaReimburse: strings.TrimSpace(r.FormValue("penerima_reimburse")),
		Kategori:          strings.TrimSpace(r.FormValue("kategori")),
		SubKategori:       strings.TrimSpace(r.FormValue("sub_kategori")),
		JenisPerjalanan:   strings.TrimSpace(r.FormValue("jenis_perjalanan")),
		Items:             notaItemsFromForm(r),
	}

	kwitansi, err := s.readOptionalPhoto(r, "foto_kwitansi")
	if err != nil {
		s.renderNota(w, r, user, sessionValue, form, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}
	transfer, err := s.readOptionalPhoto(r, "bukti_transfer")
	if err != nil {
		s.renderNota(w, r, user, sessionValue, form, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}

	nota, err := s.nota.Create(r.Context(), user, service.NotaInput{
		Tanggal:           form.Tanggal,
		PIC:               form.PIC,
		Metode:            form.Metode,
		PenerimaReimburse: form.PenerimaReimburse,
		Kategori:          form.Kategori,
		SubKategori:       form.SubKategori,
		JenisPerjalanan:   form.JenisPerjalanan,
		Items:             form.Items,
		FotoKwitansi:      kwitansi,
		BuktiTransfer:     transfer,
	})
	if err != nil {
		message := "Data nota tidak valid"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Lampiran dokumen tidak valid"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("create nota: %v", err)
			message = "Terjadi kesalahan saat menyimpan nota"
			status = http.StatusInternalServerError
		}
		s.renderNota(w, r, user, sessionValue, form, message, "", status)
		return
	}

	s.renderNota(w, r, user, sessionValue,
		NotaFormData{Tanggal: s.nota.Today()},
		"",
		fmt.Sprintf("Nota %s tersimpan dengan total Rp %s.", nota.NotaID, formatRupiah(nota.Total)),
		http.StatusOK)
}

// notaItemsFromForm reads the repeated line inputs. The four arrays are
// submitted in parallel, so the shortest one decides how many lines arrived —
// a browser that sent a partial row must not shift prices onto other products.
func notaItemsFromForm(r *http.Request) []service.NotaItemInput {
	nama := r.Form["item_nama"]
	satuan := r.Form["item_satuan"]
	volume := r.Form["item_volume"]
	harga := r.Form["item_harga"]
	count := len(nama)
	for _, values := range [][]string{satuan, volume, harga} {
		if len(values) < count {
			count = len(values)
		}
	}
	items := make([]service.NotaItemInput, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, service.NotaItemInput{
			NamaProduk: strings.TrimSpace(nama[i]),
			Satuan:     strings.TrimSpace(satuan[i]),
			Volume:     strings.TrimSpace(volume[i]),
			Harga:      strings.TrimSpace(harga[i]),
		})
	}
	return items
}

func (s *Server) renderNota(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form NotaFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.nota.Today()
	}
	if len(form.Items) == 0 {
		form.Items = []service.NotaItemInput{{}}
	}
	// The identifier is a preview only; Create assigns the authoritative one,
	// so a failure here must not keep the form off the screen.
	nextID, err := s.nota.NextID(r.Context())
	if err != nil {
		log.Printf("preview nota id: %v", err)
	}
	options, err := s.nota.Options(r.Context())
	if err != nil {
		// The picker falls back to free typing, which still works.
		log.Printf("load nota options: %v", err)
	}
	s.render(w, "nota", NotaPageData{
		ShellPageData:      s.shellData(user, sessionValue, "nota-input"),
		Form:               form,
		NextID:             nextID,
		Options:            options,
		MetodeOptions:      service.NotaMetodeOptions,
		KategoriOptions:    service.NotaKategoriOptions,
		JenisOptions:       service.NotaJenisPerjalananOptions,
		PerjalananDinas:    service.NotaSubPerjalananDinas,
		ReceiptScanEnabled: s.receiptScanner != nil,
		Error:              errMessage,
		Success:            success,
	}, status)
}

// formatRupiah groups thousands with dots, the way money is written here. The
// report and the confirmation message have to agree on the figure, so both go
// through the same formatter.
func formatRupiah(value float64) string { return export.FormatMoney(value) }

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
	if !CanAccess(user.Jabatan, "absensi") {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{"ok": false, "error": "jabatan Anda tidak berhak mengakses absensi"})
		return
	}
	// Attendance is written into the project's own spreadsheet, so the project
	// has to be settled before the clock is punched. This endpoint answers in
	// JSON, so it cannot use the binder that renders a page.
	s, err = s.forProjectJSON(w, r, user, sessionValue)
	if err != nil {
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
