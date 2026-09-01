package handler

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
	"opp-management/internal/service"
	"opp-management/internal/session"
)

type LeaveFormData struct {
	LeaveID        string
	JenisLeave     string
	TanggalMulai   string
	TanggalSelesai string
	Alasan         string
	HasAttachment  bool
	Editing        bool
}

type LeaveView struct {
	model.Leave
	TanggalMulaiLabel   string
	TanggalSelesaiLabel string
	CreatedLabel        string
	ProcessedLabel      string
	StatusClass         string
	Selected            bool
}

type LeaveRequestPageData struct {
	ShellPageData
	Form    LeaveFormData
	Types   []string
	Rows    []LeaveView
	Summary *service.LeavePersonalSummary
	Error   string
	Success string
}

type HRChartPoint struct {
	X           float64
	HadirY      float64
	TidakHadirY float64
	CutiY       float64
	Hadir       int
	TidakHadir  int
	Cuti        int
	Label       string
	ShowLabel   bool
}

type HRChartGridline struct {
	Y     float64
	Label int
}

type HRSeriesChart struct {
	Points           []HRChartPoint
	Gridlines        []HRChartGridline
	HadirPoints      string
	TidakHadirPoints string
	CutiPoints       string
}

type HRRoleSlice struct {
	Jabatan   string
	Total     int
	Percent   float64
	Dash      string
	Offset    string
	ClassName string
}

type HROverviewPageData struct {
	ShellPageData
	Overview *service.HROverview
	From     string
	To       string
	Chart    *HRSeriesChart
	Roles    []HRRoleSlice
	NewUsers []HRUserView
	Recent   []LeaveView
	Error    string
}

type HRUserView struct {
	model.User
	JoinLabel string
}

type LeaveApprovalPageData struct {
	ShellPageData
	Filters       service.LeaveFilters
	StatusFilter  string
	Rows          []LeaveView
	Types         []string
	Statuses      []string
	SelectedLeave string
	Error         string
	Success       string
}

var leaveTypes = []string{
	model.LeaveJenisCutiTahunan,
	model.LeaveJenisCutiSakit,
	model.LeaveJenisIzin,
}

var leaveStatuses = []string{
	model.LeaveStatusMenunggu,
	model.LeaveStatusDisetujui,
	model.LeaveStatusDitolak,
	model.LeaveStatusDibatalkan,
}

func (s *Server) handleLeaveRequest(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleLeaveRequestPage(w, r)
	case http.MethodPost:
		s.handleLeaveRequestPost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLeaveRequestPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "leave-request")
	if !ok {
		return
	}
	form := LeaveFormData{
		TanggalMulai:   s.leave.Today(),
		TanggalSelesai: s.leave.Today(),
	}
	editID := strings.TrimSpace(r.URL.Query().Get("edit"))
	if editID != "" {
		rows, err := s.leave.OwnRequests(r.Context(), user.UserID)
		if err != nil {
			log.Printf("load leave edit request: %v", err)
			s.renderLeaveRequest(w, r, user, sessionValue, form, "Gagal memuat pengajuan leave", "", http.StatusInternalServerError)
			return
		}
		found := false
		for _, row := range rows {
			if !strings.EqualFold(row.LeaveID, editID) {
				continue
			}
			if row.Status != model.LeaveStatusMenunggu {
				s.renderLeaveRequest(w, r, user, sessionValue, form, "Hanya pengajuan yang masih menunggu yang dapat diedit", "", http.StatusConflict)
				return
			}
			form = leaveFormFromModel(row)
			found = true
			break
		}
		if !found {
			s.renderLeaveRequest(w, r, user, sessionValue, form, "Pengajuan leave tidak ditemukan", "", http.StatusNotFound)
			return
		}
	}
	s.renderLeaveRequest(w, r, user, sessionValue, form, "", "", http.StatusOK)
}

func (s *Server) handleLeaveRequestPost(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "leave-request")
	if !okProject {
		return
	}

	contentType := strings.ToLower(r.Header.Get("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		maxBody := s.maxUploadBytes + 96*1024
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		if err := r.ParseMultipartForm(maxBody); err != nil {
			s.renderLeaveRequest(w, r, user, sessionValue, LeaveFormData{}, "Form tidak valid atau foto terlalu besar", "", http.StatusUnprocessableEntity)
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
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 96*1024)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Form tidak valid", http.StatusBadRequest)
			return
		}
		if !s.sessions.ValidCSRF(r, sessionValue) {
			http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
			return
		}
	}

	operation := strings.ToLower(strings.TrimSpace(r.FormValue("operation")))
	if operation == "cancel" {
		leave, err := s.leave.Cancel(r.Context(), user, r.FormValue("leave_id"))
		if err != nil {
			s.renderLeaveOperationError(w, r, user, sessionValue, LeaveFormData{}, err)
			return
		}
		s.renderLeaveRequest(w, r, user, sessionValue,
			LeaveFormData{TanggalMulai: s.leave.Today(), TanggalSelesai: s.leave.Today()},
			"", "Pengajuan "+leave.LeaveID+" berhasil dibatalkan.", http.StatusOK)
		return
	}

	form := LeaveFormData{
		LeaveID:        strings.TrimSpace(r.FormValue("leave_id")),
		JenisLeave:     strings.TrimSpace(r.FormValue("jenis_leave")),
		TanggalMulai:   strings.TrimSpace(r.FormValue("tanggal_mulai")),
		TanggalSelesai: strings.TrimSpace(r.FormValue("tanggal_selesai")),
		Alasan:         strings.TrimSpace(r.FormValue("alasan")),
		Editing:        operation == "update",
	}
	attachment, err := s.readOptionalPhoto(r, "bukti_pendukung")
	if err != nil {
		s.renderLeaveRequest(w, r, user, sessionValue, form, err.Error(), "", http.StatusUnprocessableEntity)
		return
	}
	input := service.LeaveInput{
		JenisLeave:     form.JenisLeave,
		TanggalMulai:   form.TanggalMulai,
		TanggalSelesai: form.TanggalSelesai,
		Alasan:         form.Alasan,
		BuktiPendukung: attachment,
	}
	var saved *model.Leave
	if operation == "update" {
		action := service.LeaveAttachmentKeep
		switch {
		case attachment != "":
			action = service.LeaveAttachmentReplace
		case r.FormValue("hapus_bukti") != "":
			action = service.LeaveAttachmentRemove
		}
		saved, err = s.leave.Update(r.Context(), user, form.LeaveID, input, attachment, action)
	} else if operation == "create" || operation == "" {
		saved, err = s.leave.Create(r.Context(), user, input)
	} else {
		err = fmt.Errorf("%w: aksi pengajuan tidak dikenal", service.ErrValidation)
	}
	if err != nil {
		s.renderLeaveOperationError(w, r, user, sessionValue, form, err)
		return
	}

	verb := "diajukan"
	if operation == "update" {
		verb = "diperbarui"
	}
	s.renderLeaveRequest(w, r, user, sessionValue,
		LeaveFormData{TanggalMulai: s.leave.Today(), TanggalSelesai: s.leave.Today()},
		"", fmt.Sprintf("Pengajuan %s berhasil %s.", saved.LeaveID, verb), http.StatusOK)
}

func (s *Server) renderLeaveOperationError(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form LeaveFormData, err error) {
	message := "Pengajuan leave tidak dapat diproses"
	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, service.ErrForbidden):
		message = "Anda tidak berhak mengubah pengajuan ini"
		status = http.StatusForbidden
	case errors.Is(err, service.ErrConflict):
		message = "Pengajuan ini sudah diproses dan tidak dapat diubah"
		status = http.StatusConflict
		// A stale edit tab must not keep presenting a final request as an
		// editable MENUNGGU form. Return to a clean create state; the refreshed
		// history below shows the authoritative final status.
		form = LeaveFormData{TanggalMulai: s.leave.Today(), TanggalSelesai: s.leave.Today()}
	case errors.Is(err, repository.ErrNotFound):
		message = "Pengajuan leave tidak ditemukan"
		status = http.StatusNotFound
	case errors.Is(err, service.ErrInvalidPhoto):
		message = "Bukti pendukung tidak valid"
	case errors.Is(err, service.ErrValidation):
		message = strings.TrimPrefix(err.Error(), "validation error: ")
	default:
		log.Printf("leave request operation: %v", err)
		message = "Terjadi kesalahan saat memproses pengajuan leave"
		status = http.StatusInternalServerError
	}
	s.renderLeaveRequest(w, r, user, sessionValue, form, message, "", status)
}

func (s *Server) renderLeaveRequest(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, form LeaveFormData, errMessage, success string, status int) {
	if form.TanggalMulai == "" {
		form.TanggalMulai = s.leave.Today()
	}
	if form.TanggalSelesai == "" {
		form.TanggalSelesai = form.TanggalMulai
	}
	data := LeaveRequestPageData{
		ShellPageData: s.shellData(user, sessionValue, "leave-request"),
		Form:          form,
		Types:         leaveTypes,
		Error:         errMessage,
		Success:       success,
	}
	rows, err := s.leave.OwnRequests(r.Context(), user.UserID)
	if err != nil {
		log.Printf("list own leave requests: %v", err)
		if data.Error == "" {
			data.Error = "Gagal memuat riwayat pengajuan leave"
		}
	} else {
		data.Rows = leaveViews(rows, "")
		if form.Editing {
			for _, row := range rows {
				if strings.EqualFold(row.LeaveID, form.LeaveID) {
					data.Form.HasAttachment = row.HasBuktiPendukung
					break
				}
			}
		}
	}
	data.Summary, err = s.leave.PersonalSummary(r.Context(), user.UserID)
	if err != nil {
		log.Printf("build personal leave summary: %v", err)
	} else if data.Summary != nil && data.Summary.TodayStatus == "" {
		data.Summary.TodayStatus = "Tidak ada leave"
	}
	s.render(w, "leave_request", data, status)
}

func (s *Server) handleLeaveAttachment(w http.ResponseWriter, r *http.Request) {
	user, sessionValue, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	// The attachment lives in the project's own spreadsheet, so the project is
	// settled before it is looked for. A file is served here rather than a
	// page, so a project problem is a plain not-found.
	bound, _, ok := s.bindProject(w, r, user, sessionValue)
	if !ok {
		return
	}
	s = bound
	leaveID := strings.TrimSpace(r.URL.Query().Get("leave_id"))
	canHR := CanAccess(s.accessRules(r.Context()), user.Jabatan, "hr-approval-leave")
	dataURL, err := s.leave.Attachment(r.Context(), user, leaveID, canHR)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) || errors.Is(err, repository.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		log.Printf("read leave attachment: %v", err)
		http.Error(w, "Gagal memuat bukti pendukung", http.StatusInternalServerError)
		return
	}
	payload, err := photo.DecodeDataURL(dataURL)
	if err != nil {
		log.Printf("decode leave attachment %s: %v", leaveID, err)
		http.Error(w, "Bukti pendukung rusak", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", "bukti-"+leaveID+".jpg"))
	w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(payload)
}

func (s *Server) handleHROverview(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-overview")
	if !ok {
		return
	}
	from := strings.TrimSpace(r.URL.Query().Get("from"))
	to := strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" && to == "" {
		from, to = s.leave.DefaultOverviewRange()
	}
	data := HROverviewPageData{
		ShellPageData: s.shellData(user, sessionValue, "hr-overview"),
		From:          from,
		To:            to,
	}
	overview, err := s.leave.BuildHROverview(r.Context(), from, to)
	if err != nil {
		if errors.Is(err, service.ErrValidation) {
			data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
		} else {
			log.Printf("build HR overview: %v", err)
			data.Error = "Gagal memuat overview HR"
		}
		s.render(w, "hr_overview", data, http.StatusOK)
		return
	}
	data.Overview = overview
	data.From = overview.From
	data.To = overview.To
	data.Chart = buildHRSeriesChart(overview.Series)
	data.Roles = buildHRRoleSlices(overview.JabatanShares)
	for _, employee := range overview.KaryawanBaru {
		data.NewUsers = append(data.NewUsers, HRUserView{User: employee, JoinLabel: dateOnlyLabel(employee.TanggalGabung)})
	}
	data.Recent = leaveViews(overview.PengajuanTerbaru, "")
	s.render(w, "hr_overview", data, http.StatusOK)
}

func (s *Server) handleLeaveApproval(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleLeaveApprovalPage(w, r)
	case http.MethodPost:
		s.handleLeaveApprovalPost(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLeaveApprovalPage(w http.ResponseWriter, r *http.Request) {
	s, user, sessionValue, ok := s.requireAccess(w, r, "hr-approval-leave")
	if !ok {
		return
	}
	filters, statusFilter := approvalFiltersFromRequest(r)
	s.renderLeaveApproval(w, r, user, sessionValue, filters, statusFilter,
		strings.TrimSpace(r.URL.Query().Get("leave")), "", "", http.StatusOK)
}

func (s *Server) handleLeaveApprovalPost(w http.ResponseWriter, r *http.Request) {
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
	s, sessionValue, okProject := s.allowedIn(w, r, user, sessionValue, "hr-approval-leave")
	if !okProject {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 96*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form tidak valid", http.StatusBadRequest)
		return
	}
	if !s.sessions.ValidCSRF(r, sessionValue) {
		http.Error(w, "CSRF token tidak valid", http.StatusForbidden)
		return
	}
	filters, statusFilter := approvalFiltersFromRequest(r)
	leaveID := strings.TrimSpace(r.FormValue("leave_id"))
	decision := service.LeaveDecision(strings.ToUpper(strings.TrimSpace(r.FormValue("decision"))))
	if decision == "APPROVE" {
		decision = service.LeaveDecisionApprove
	} else if decision == "REJECT" {
		decision = service.LeaveDecisionReject
	}
	leave, err := s.leave.Decide(r.Context(), user, leaveID, decision, r.FormValue("catatan_approval"))
	if err != nil {
		message := "Keputusan leave tidak dapat disimpan"
		status := http.StatusUnprocessableEntity
		switch {
		case errors.Is(err, service.ErrConflict):
			message = "Pengajuan ini sudah diproses"
			status = http.StatusConflict
		case errors.Is(err, repository.ErrNotFound):
			message = "Pengajuan leave tidak ditemukan"
			status = http.StatusNotFound
		case errors.Is(err, service.ErrValidation):
			message = strings.TrimPrefix(err.Error(), "validation error: ")
		default:
			log.Printf("decide leave: %v", err)
			status = http.StatusInternalServerError
		}
		s.renderLeaveApproval(w, r, user, sessionValue, filters, statusFilter, leaveID, message, "", status)
		return
	}
	s.renderLeaveApproval(w, r, user, sessionValue, filters, statusFilter, "", "",
		fmt.Sprintf("Pengajuan %s berhasil ditandai %s.", leave.LeaveID, strings.ToLower(leave.Status)), http.StatusOK)
}

func approvalFiltersFromRequest(r *http.Request) (service.LeaveFilters, string) {
	status := strings.TrimSpace(r.FormValue("status"))
	if status == "" && r.URL.Query().Get("status") == "" {
		status = model.LeaveStatusMenunggu
	}
	filterStatus := status
	if strings.EqualFold(status, "SEMUA") {
		filterStatus = ""
	}
	return service.LeaveFilters{
		Q:          strings.TrimSpace(r.FormValue("q")),
		Status:     filterStatus,
		JenisLeave: strings.TrimSpace(r.FormValue("jenis")),
		From:       strings.TrimSpace(r.FormValue("from")),
		To:         strings.TrimSpace(r.FormValue("to")),
	}, status
}

func (s *Server) renderLeaveApproval(w http.ResponseWriter, r *http.Request, user *model.User, sessionValue session.Session, filters service.LeaveFilters, statusFilter, selected, errMessage, success string, status int) {
	data := LeaveApprovalPageData{
		ShellPageData: s.shellData(user, sessionValue, "hr-approval-leave"),
		Filters:       filters,
		StatusFilter:  statusFilter,
		Types:         leaveTypes,
		Statuses:      leaveStatuses,
		SelectedLeave: strings.ToUpper(strings.TrimSpace(selected)),
		Error:         errMessage,
		Success:       success,
	}
	rows, err := s.leave.ApprovalRows(r.Context(), filters)
	if err != nil {
		log.Printf("list leave approvals: %v", err)
		if data.Error == "" {
			if errors.Is(err, service.ErrValidation) {
				data.Error = strings.TrimPrefix(err.Error(), "validation error: ")
			} else {
				data.Error = "Gagal memuat pengajuan leave"
			}
		}
	} else {
		data.Rows = leaveViews(rows, data.SelectedLeave)
	}
	s.render(w, "leave_approval", data, status)
}

func leaveFormFromModel(row model.Leave) LeaveFormData {
	return LeaveFormData{
		LeaveID:        row.LeaveID,
		JenisLeave:     row.JenisLeave,
		TanggalMulai:   row.TanggalMulai,
		TanggalSelesai: row.TanggalSelesai,
		Alasan:         row.Alasan,
		HasAttachment:  row.HasBuktiPendukung,
		Editing:        true,
	}
}

func leaveViews(rows []model.Leave, selected string) []LeaveView {
	views := make([]LeaveView, 0, len(rows))
	for _, row := range rows {
		view := LeaveView{
			Leave:               row,
			TanggalMulaiLabel:   dateOnlyLabel(row.TanggalMulai),
			TanggalSelesaiLabel: dateOnlyLabel(row.TanggalSelesai),
			CreatedLabel:        row.CreatedAt.Format("02 Jan 2006 15:04"),
			StatusClass:         leaveStatusClass(row.Status),
			Selected:            selected != "" && strings.EqualFold(selected, row.LeaveID),
		}
		if row.DiprosesPada != nil {
			view.ProcessedLabel = row.DiprosesPada.Format("02 Jan 2006 15:04")
		}
		views = append(views, view)
	}
	return views
}

func leaveStatusClass(status string) string {
	switch status {
	case model.LeaveStatusDisetujui:
		return "approved"
	case model.LeaveStatusDitolak:
		return "rejected"
	case model.LeaveStatusDibatalkan:
		return "cancelled"
	default:
		return "pending"
	}
}

func dateOnlyLabel(value string) string {
	date, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return value
	}
	return date.Format("02 Jan 2006")
}

func buildHRSeriesChart(series []service.HRDatePoint) *HRSeriesChart {
	chart := &HRSeriesChart{}
	if len(series) == 0 {
		return chart
	}
	maxValue := 1
	for _, point := range series {
		if point.Hadir > maxValue {
			maxValue = point.Hadir
		}
		if point.TidakHadir > maxValue {
			maxValue = point.TidakHadir
		}
		if point.Cuti > maxValue {
			maxValue = point.Cuti
		}
	}
	plotLeft, plotRight := 48.0, 772.0
	plotTop, plotBottom := 25.0, 245.0
	step := 0.0
	if len(series) > 1 {
		step = (plotRight - plotLeft) / float64(len(series)-1)
	}
	yFor := func(value int) float64 {
		return plotBottom - (float64(value)/float64(maxValue))*(plotBottom-plotTop)
	}
	var hadir, absent, leave strings.Builder
	labelEvery := 1
	if len(series) > 10 {
		labelEvery = int(mathCeil(float64(len(series)) / 8))
	}
	for index, source := range series {
		x := plotLeft + float64(index)*step
		if len(series) == 1 {
			x = (plotLeft + plotRight) / 2
		}
		point := HRChartPoint{
			X:           x,
			HadirY:      yFor(source.Hadir),
			TidakHadirY: yFor(source.TidakHadir),
			CutiY:       yFor(source.Cuti),
			Hadir:       source.Hadir,
			TidakHadir:  source.TidakHadir,
			Cuti:        source.Cuti,
			Label:       source.Label,
			ShowLabel:   index%labelEvery == 0 || index == len(series)-1,
		}
		chart.Points = append(chart.Points, point)
		appendSVGPoint(&hadir, x, point.HadirY)
		appendSVGPoint(&absent, x, point.TidakHadirY)
		appendSVGPoint(&leave, x, point.CutiY)
	}
	chart.HadirPoints = strings.TrimSpace(hadir.String())
	chart.TidakHadirPoints = strings.TrimSpace(absent.String())
	chart.CutiPoints = strings.TrimSpace(leave.String())
	for index := 0; index <= 4; index++ {
		value := int(mathCeil(float64(maxValue) * float64(4-index) / 4))
		y := plotTop + (plotBottom-plotTop)*float64(index)/4
		chart.Gridlines = append(chart.Gridlines, HRChartGridline{Y: y, Label: value})
	}
	return chart
}

func appendSVGPoint(builder *strings.Builder, x, y float64) {
	fmt.Fprintf(builder, "%.1f,%.1f ", x, y)
}

func mathCeil(value float64) float64 {
	integer := int(value)
	if float64(integer) == value {
		return value
	}
	return float64(integer + 1)
}

func buildHRRoleSlices(shares []service.HRJabatanShare) []HRRoleSlice {
	// Stable class assignment keeps the same jabatan the same colour as long as
	// the service's descending order is unchanged.
	sort.SliceStable(shares, func(i, j int) bool { return shares[i].Total > shares[j].Total })
	offset := 0.0
	result := make([]HRRoleSlice, 0, len(shares))
	for index, share := range shares {
		percent := share.Percent
		if percent < 0 {
			percent = 0
		}
		if percent > 100 {
			percent = 100
		}
		result = append(result, HRRoleSlice{
			Jabatan:   share.Jabatan,
			Total:     share.Total,
			Percent:   percent,
			Dash:      fmt.Sprintf("%.2f %.2f", percent, 100-percent),
			Offset:    fmt.Sprintf("%.2f", -offset),
			ClassName: fmt.Sprintf("role-slice-%d", index%8),
		})
		offset += percent
	}
	return result
}
