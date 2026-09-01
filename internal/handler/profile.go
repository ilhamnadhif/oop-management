package handler

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

// ProfilePageData backs both renderings of the same form: the dialog carried by
// every page, and the standalone page a browser without JavaScript lands on.
type ProfilePageData struct {
	ShellPageData
	Error   string
	Success string
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleProfilePage(w, r)
	case http.MethodPost:
		s.handleProfileSave(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProfilePage(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	// The profile page belongs to no project, but its sidebar does: it is drawn
	// with the menu of whatever project the session is working in.
	s, sessionValue, ok = s.bindProject(w, r, user, sessionValue)
	if !ok {
		return
	}
	success := ""
	if r.URL.Query().Get("tersimpan") == "1" {
		success = "Data pribadi tersimpan."
	}
	s.renderProfile(w, user, sessionValue, "", success, http.StatusOK)
}

func (s *Server) handleProfileSave(w http.ResponseWriter, r *http.Request) {
	// requireUser answers GET only, so the session is loaded here the way every
	// other form post does it.
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
	s, sessionValue, ok = s.bindProject(w, r, user, sessionValue)
	if !ok {
		return
	}

	maxBody := s.maxUploadBytes + 96*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderProfile(w, user, sessionValue, "Form tidak valid atau foto terlalu besar", "", http.StatusUnprocessableEntity)
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

	raw, err := s.readOptionalUpload(r, "foto_profil")
	if err != nil {
		s.renderProfile(w, user, sessionValue, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}
	input := service.ProfileInput{
		NamaLengkap:  r.FormValue("nama_lengkap"),
		NoTelp:       r.FormValue("no_telp"),
		TanggalLahir: r.FormValue("tanggal_lahir"),
		Foto:         raw,
		HapusFoto:    r.FormValue("hapus_foto") == "1",
	}

	updated, err := s.auth.UpdateProfile(r.Context(), user.UserID, input)
	if err != nil {
		message := "Data pribadi tidak dapat disimpan"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrInvalidPhoto):
			message = "Foto profil tidak dapat diproses"
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		case errors.Is(err, repository.ErrNotFound):
			message = "Akun tidak ditemukan"
			status = http.StatusNotFound
		default:
			log.Printf("update profile: %v", err)
			message = "Terjadi kesalahan saat menyimpan data pribadi"
			status = http.StatusInternalServerError
		}
		// The form is re-rendered from what was typed, so a rejected save does
		// not silently discard the other fields.
		typed := *user
		typed.NamaLengkap = strings.TrimSpace(input.NamaLengkap)
		typed.NoTelp = strings.TrimSpace(input.NoTelp)
		typed.TanggalLahir = strings.TrimSpace(input.TanggalLahir)
		s.renderProfile(w, &typed, sessionValue, message, "", status)
		return
	}

	_ = updated
	redirect(w, r, "/profile?tersimpan=1")
}

func (s *Server) renderProfile(w http.ResponseWriter, user *model.User, sessionValue session.Session, errMessage, success string, status int) {
	data := ProfilePageData{
		ShellPageData: s.shellData(user, sessionValue, "dashboard"),
		Error:         errMessage,
		Success:       success,
	}
	// The page is reached from the account menu rather than the sidebar, so it
	// names itself instead of borrowing the dashboard's heading.
	data.Title = "Profil"
	data.PageTitle = "Profil"
	data.Breadcrumb = "Profil"
	data.Section = ""
	data.Lede = "Data pribadi Anda. NRP dan jabatan dikelola HR dan tidak dapat diubah di sini."
	s.render(w, "profile_page", data, status)
}

// handleProfilePhoto serves an avatar. The picture lives in a column no other
// read touches, so it is fetched here rather than inlined into every page.
//
// Without user_id it serves the caller's own photo. With one it serves a
// colleague's, which is why it asks for the same permission as the page that
// lists those colleagues by name: anyone who may not see that page may not
// enumerate faces through this either.
func (s *Server) handleProfilePhoto(w http.ResponseWriter, r *http.Request) {
	user, _, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	wanted := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if wanted == "" {
		wanted = user.UserID
	}
	if wanted != user.UserID && !CanAccess(s.accessRules(r.Context()), user.Jabatan, "hr-overview") {
		http.NotFound(w, r)
		return
	}
	dataURL, err := s.auth.ProfilePhoto(r.Context(), wanted)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read profile photo: %v", err)
		http.Error(w, "Gagal memuat foto profil", http.StatusInternalServerError)
		return
	}
	if dataURL == "" {
		http.NotFound(w, r)
		return
	}
	payload, err := photo.DecodeDataURL(dataURL)
	if err != nil {
		log.Printf("decode profile photo %s: %v", wanted, err)
		http.Error(w, "Foto profil rusak", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	// Cached against the account's updated_at, which the page puts in the URL,
	// so a new picture is a new URL and an unchanged one is not refetched.
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write(payload)
}

// readOptionalUpload returns the raw bytes of an uploaded file, or nil when the
// field was left empty. It stops short of encoding, because how small a picture
// has to end up depends on what it is for.
func (s *Server) readOptionalUpload(r *http.Request, field string) ([]byte, error) {
	file, _, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("gagal membaca foto")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, s.maxUploadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("gagal membaca foto")
	}
	if int64(len(raw)) > s.maxUploadBytes {
		return nil, fmt.Errorf("ukuran foto maksimal %d MB", s.maxUploadBytes/(1024*1024))
	}
	return raw, nil
}
