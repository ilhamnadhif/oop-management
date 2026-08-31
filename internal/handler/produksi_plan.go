package handler

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"opp-management/internal/model"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

type ProduksiPlanFormData struct {
	Tanggal  string
	Supplier string
	Lokasi   string
	Volume   string
}

// ProduksiPlanRow is one stored plan, with the volume already written the way
// the page prints it.
type ProduksiPlanRow struct {
	model.ProduksiPlan
	VolumeLabel string
}

type ProduksiPlanPageData struct {
	ShellPageData
	Form    ProduksiPlanFormData
	Options service.ProduksiOptions
	Rows    []ProduksiPlanRow
	Total   string
	Error   string
	Success string
}

func (s *Server) handleProduksiPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleProduksiPlanCreate(w, r)
		return
	}
	s, user, sessionValue, ok := s.requireAccess(w, r, "produksi-plan")
	if !ok {
		return
	}
	s.renderProduksiPlan(w, r, user, sessionValue, ProduksiPlanFormData{
		Tanggal: s.produksi.Today(),
	}, "", "", http.StatusOK)
}

func (s *Server) handleProduksiPlanCreate(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "produksi-plan")
	if !okProject {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 32*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}

	form := ProduksiPlanFormData{
		Tanggal: strings.TrimSpace(r.FormValue("tanggal")),

		Supplier: strings.TrimSpace(r.FormValue("supplier")),
		Lokasi:   strings.TrimSpace(r.FormValue("lokasi")),
		Volume:   strings.TrimSpace(r.FormValue("volume")),
	}
	plan, err := s.produksi.CreatePlan(r.Context(), user, service.ProduksiPlanInput{
		Tanggal:  form.Tanggal,
		Project:  s.project.Nama,
		Supplier: form.Supplier,
		Lokasi:   form.Lokasi,
		Volume:   form.Volume,
	})
	if err != nil {
		message := "Rencana tidak dapat disimpan"
		status := http.StatusUnprocessableEntity
		if errors.Is(err, service.ErrValidation) {
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("create produksi plan: %v", err)
			message = "Terjadi kesalahan saat menyimpan rencana"
			status = http.StatusInternalServerError
		}
		// The typed values come back with the error, so a rejected save does
		// not make someone retype the whole row.
		s.renderProduksiPlan(w, r, user, sessionValue, form, message, "", status)
		return
	}

	s.renderProduksiPlan(w, r, user, sessionValue,
		ProduksiPlanFormData{Tanggal: plan.Tanggal, Supplier: plan.Supplier},
		"", "Rencana "+plan.PlanID+" tersimpan untuk "+plan.Lokasi+".", http.StatusOK)
}

// formatVolume writes a planned volume the way the rest of the app writes
// figures: thousands grouped, no decimals, because a plan is set in whole cubic
// metres and "15.000" is read at a glance where "15000.00" is not.
func formatVolume(value float64) string { return formatRupiah(value) }

func (s *Server) renderProduksiPlan(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form ProduksiPlanFormData, errMessage, success string, status int) {
	if form.Tanggal == "" {
		form.Tanggal = s.produksi.Today()
	}
	data := ProduksiPlanPageData{
		ShellPageData: s.shellData(user, sessionValue, "produksi-plan"),
		Form:          form,
		Error:         errMessage,
		Success:       success,
	}
	options, err := s.produksi.Options(r.Context())
	if err != nil {
		// The pickers fall back to typing freely, which still works, so this
		// must not take the form down.
		log.Printf("load produksi options: %v", err)
	}
	data.Options = options

	plans, err := s.produksi.Plans(r.Context())
	if err != nil {
		log.Printf("list produksi plan: %v", err)
		if data.Error == "" {
			data.Error = "Daftar rencana gagal dimuat"
		}
		s.render(w, "produksi_plan", data, status)
		return
	}
	total := 0.0
	for _, plan := range plans {
		data.Rows = append(data.Rows, ProduksiPlanRow{
			ProduksiPlan: plan,
			VolumeLabel:  formatVolume(plan.Volume),
		})
		total += plan.Volume
	}
	data.Total = formatVolume(total)
	s.render(w, "produksi_plan", data, status)
}
