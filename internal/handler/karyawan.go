package handler

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// KaryawanPageData is the HR screen for adding people to this project.
type KaryawanPageData struct {
	ShellPageData
	Form KaryawanFormData
	// JabatanOptions is what this person may create. HR is offered everything
	// but Management; offering it and refusing the post afterwards would be a
	// dropdown that lies.
	JabatanOptions []string
	// DefaultPassword is printed on the page because HR has to say it out loud
	// when handing the account over.
	DefaultPassword string
	Members         []ProjectMember
	// SemuaProyekMark is what the Assigned column holds for an account that
	// reaches every project, so the table can word it rather than print "*".
	SemuaProyekMark string
	Error           string
	Success         string
}

type KaryawanFormData struct {
	TanggalGabung string
	NamaLengkap   string
	NRP           string
	Jabatan       string
	Email         string
	Status        string
}

func (s *Server) handleKaryawan(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleKaryawanCreate(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-karyawan")
	if !ok {
		return
	}
	s.renderKaryawan(w, r, user, sessionValue,
		KaryawanFormData{TanggalGabung: s.today(), Status: model.StatusAktif},
		"", "", http.StatusOK)
}

func (s *Server) handleKaryawanCreate(w http.ResponseWriter, r *http.Request) {
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
		http.Error(w, "Form tidak valid", http.StatusUnprocessableEntity)
		return
	}
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "hr-karyawan")
	if !okProject {
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	form := KaryawanFormData{
		TanggalGabung: strings.TrimSpace(r.FormValue("tanggal_gabung")),
		NamaLengkap:   strings.TrimSpace(r.FormValue("nama_lengkap")),
		NRP:           strings.TrimSpace(r.FormValue("nrp")),
		Jabatan:       strings.TrimSpace(r.FormValue("jabatan")),
		Email:         strings.TrimSpace(r.FormValue("email")),
		Status:        strings.TrimSpace(r.FormValue("status_pengguna")),
	}

	created, err := s.auth.AddEmployee(r.Context(), service.RegisterInput{
		TanggalGabung: form.TanggalGabung,
		NamaLengkap:   form.NamaLengkap,
		NRP:           form.NRP,
		Jabatan:       form.Jabatan,
		Email:         form.Email,
		Status:        form.Status,
		// The account belongs to the project this request is working in. HR of
		// one site cannot add somebody to another.
		Project: s.project.Nama,
	}, canCreateManagement(user))
	if err != nil {
		message := "Karyawan tidak bisa ditambahkan"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrDuplicateUser):
			message = "NRP atau email itu sudah terdaftar"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("add employee: %v", err)
			message = "Terjadi kesalahan saat menambah karyawan"
			status = http.StatusInternalServerError
		}
		s.renderKaryawan(w, r, user, sessionValue, form, message, "", status)
		return
	}

	s.renderKaryawan(w, r, user, sessionValue,
		KaryawanFormData{TanggalGabung: s.today(), Status: model.StatusAktif},
		"",
		created.NamaLengkap+" ditambahkan ke project "+s.project.Nama+
			". Beritahu password awalnya; aplikasi akan meminta orangnya menggantinya saat login pertama.",
		http.StatusOK)
}

func (s *Server) renderKaryawan(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form KaryawanFormData, errMessage, success string, status int) {
	if form.TanggalGabung == "" {
		form.TanggalGabung = s.today()
	}
	if form.Status == "" {
		form.Status = model.StatusAktif
	}

	users, err := s.projects.AllUsers(r.Context())
	if err != nil {
		log.Printf("list users: %v", err)
		if errMessage == "" {
			errMessage = "Daftar karyawan gagal dimuat"
		}
	}
	projects, err := s.projects.List(r.Context())
	if err != nil {
		log.Printf("list projects: %v", err)
	}

	s.render(w, "karyawan", KaryawanPageData{
		ShellPageData:   s.shellData(user, sessionValue, "hr-karyawan"),
		Form:            form,
		JabatanOptions:  s.jabatanOptionsFor(r.Context(), user),
		DefaultPassword: service.DefaultPassword,
		Members:         membersOf(users, s.project.Nama, firstProjectName(projects)),
		SemuaProyekMark: model.ProjectSemua,
		Error:           errMessage,
		Success:         success,
	}, status)
}

// canCreateManagement reports whether this person may mint an account that
// reaches every project. Only somebody who already does may.
func canCreateManagement(user *model.User) bool {
	return user != nil && strings.EqualFold(strings.TrimSpace(user.Jabatan), model.JabatanManagement)
}

// jabatanOptions is every position this project has: the built-in ones plus
// whatever the project made for itself. A position another project made is not
// offered, because it is not a position here.
func (s *Server) jabatanOptions(ctx context.Context) []string {
	options, err := s.auth.JabatanOptionsFor(ctx, s.project.Nama)
	if err != nil {
		// Losing the project's own positions costs the dropdown those entries,
		// not the page: the built-in ones are what every site has anyway.
		log.Printf("read jabatan options: %v", err)
		return service.JabatanOptions
	}
	return options
}

// jabatanOptionsFor is what this person may choose from. Management is left out
// for everybody else, so the dropdown offers exactly what the server will
// accept.
func (s *Server) jabatanOptionsFor(ctx context.Context, user *model.User) []string {
	all := s.jabatanOptions(ctx)
	if canCreateManagement(user) {
		return all
	}
	options := make([]string, 0, len(all))
	for _, jabatan := range all {
		if strings.EqualFold(jabatan, model.JabatanManagement) {
			continue
		}
		options = append(options, jabatan)
	}
	return options
}

// today is the date a form preselects.
func (s *Server) today() string {
	return s.now().In(s.location).Format("2006-01-02")
}
