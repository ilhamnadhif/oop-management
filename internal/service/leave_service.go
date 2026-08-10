package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// LeaveInput is the part of a leave request that its owner may supply. Identity,
// status, duration and audit fields are deliberately absent: the service derives
// those values instead of trusting a form post.
type LeaveInput struct {
	JenisLeave     string
	TanggalMulai   string
	TanggalSelesai string
	Alasan         string
	BuktiPendukung string
}

type LeaveAttachmentAction string

const (
	LeaveAttachmentKeep    LeaveAttachmentAction = "KEEP"
	LeaveAttachmentReplace LeaveAttachmentAction = "REPLACE"
	LeaveAttachmentRemove  LeaveAttachmentAction = "REMOVE"
)

type LeaveDecision string

const (
	LeaveDecisionApprove LeaveDecision = "APPROVE"
	LeaveDecisionReject  LeaveDecision = "REJECT"
)

const LeaveTextMaxLength = 1000

// LeaveFilters are applied to the approval queue. The date range matches any
// request that overlaps it, rather than only requests created during it.
type LeaveFilters struct {
	Q          string
	Status     string
	JenisLeave string
	From       string
	To         string
}

type LeavePersonalSummary struct {
	Year         int
	ApprovedDays int
	PendingCount int
	TodayStatus  string
	TodayLeave   *model.Leave
}

type HRDatePoint struct {
	Tanggal    string
	Label      string
	Hadir      int
	TidakHadir int
	Cuti       int
}

type HRJabatanShare struct {
	Jabatan string
	Total   int
	Percent float64
}

// HROverview contains only already-aggregated values. In particular it never
// carries attendance photos or leave attachments into the dashboard response.
type HROverview struct {
	From                string
	To                  string
	LastUpdated         string
	TotalKaryawan       int
	HadirHariAkhir      int
	TidakHadirHariAkhir int
	CutiHariAkhir       int
	LemburJam           float64
	Series              []HRDatePoint
	JabatanShares       []HRJabatanShare
	KaryawanBaru        []model.User
	PengajuanTerbaru    []model.Leave
}

// LeaveService owns every lifecycle transition. The mutex makes the read-check-
// write sequence and sequential ID allocation atomic within an application
// process, which is also what prevents two simultaneous approvals from winning.
type LeaveService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex
}

func NewLeaveService(store repository.Store, location *time.Location, now NowFunc) *LeaveService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &LeaveService{store: store, location: location, now: now}
}

func (s *LeaveService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

func (s *LeaveService) DefaultOverviewRange() (string, string) {
	today := s.now().In(s.location)
	return today.AddDate(0, 0, -6).Format("2006-01-02"), today.Format("2006-01-02")
}

func (s *LeaveService) Create(ctx context.Context, user *model.User, input LeaveInput) (*model.Leave, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	normalized, days, err := normalizeLeaveInput(input, strings.TrimSpace(input.BuktiPendukung) != "")
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().In(s.location)
	prefix := "LVE-" + now.Format("20060102") + "-"
	sequence, err := s.store.MaxLeaveSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read leave sequence: %w", err)
	}
	leave := &model.Leave{
		LeaveID:           fmt.Sprintf("%s%04d", prefix, sequence+1),
		UserID:            user.UserID,
		NRP:               strings.TrimSpace(user.NRP),
		NamaLengkap:       strings.TrimSpace(user.NamaLengkap),
		Jabatan:           strings.TrimSpace(user.Jabatan),
		JenisLeave:        normalized.JenisLeave,
		TanggalMulai:      normalized.TanggalMulai,
		TanggalSelesai:    normalized.TanggalSelesai,
		JumlahHari:        days,
		Alasan:            normalized.Alasan,
		Status:            model.LeaveStatusMenunggu,
		HasBuktiPendukung: normalized.BuktiPendukung != "",
		BuktiPendukung:    normalized.BuktiPendukung,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.CreateLeave(ctx, leave); err != nil {
		return nil, fmt.Errorf("create leave: %w", err)
	}
	return leave, nil
}

func (s *LeaveService) Update(ctx context.Context, owner *model.User, leaveID string, input LeaveInput, newAttachment string, action LeaveAttachmentAction) (*model.Leave, error) {
	if err := validateUser(owner); err != nil {
		return nil, err
	}
	leaveID = strings.TrimSpace(leaveID)
	if leaveID == "" {
		return nil, fmt.Errorf("%w: ID pengajuan wajib diisi", ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	leave, rowNumber, err := s.store.FindLeaveRow(ctx, leaveID)
	if err != nil {
		return nil, fmt.Errorf("find leave: %w", err)
	}
	if leave.UserID != owner.UserID {
		return nil, ErrForbidden
	}
	if leave.Status != model.LeaveStatusMenunggu {
		return nil, fmt.Errorf("%w: hanya pengajuan menunggu yang dapat diedit", ErrConflict)
	}

	action = LeaveAttachmentAction(strings.ToUpper(strings.TrimSpace(string(action))))
	if action == "" {
		action = LeaveAttachmentKeep
	}
	updateAttachment := false
	attachment := ""
	hasAttachment := leave.HasBuktiPendukung
	switch action {
	case LeaveAttachmentKeep:
		// The list/read path intentionally omits the large image. A false update
		// flag tells the repository to preserve the existing attachment column.
	case LeaveAttachmentReplace:
		attachment = strings.TrimSpace(newAttachment)
		if attachment == "" {
			return nil, fmt.Errorf("%w: foto pengganti wajib dipilih", ErrValidation)
		}
		if err := photo.ValidateDataURL(attachment); err != nil {
			return nil, ErrInvalidPhoto
		}
		hasAttachment = true
		updateAttachment = true
	case LeaveAttachmentRemove:
		hasAttachment = false
		updateAttachment = true
	default:
		return nil, fmt.Errorf("%w: aksi lampiran tidak valid", ErrValidation)
	}

	input.BuktiPendukung = ""
	if updateAttachment {
		input.BuktiPendukung = attachment
	}
	normalized, days, err := normalizeLeaveInput(input, hasAttachment)
	if err != nil {
		return nil, err
	}
	leave.JenisLeave = normalized.JenisLeave
	leave.TanggalMulai = normalized.TanggalMulai
	leave.TanggalSelesai = normalized.TanggalSelesai
	leave.JumlahHari = days
	leave.Alasan = normalized.Alasan
	leave.HasBuktiPendukung = hasAttachment
	leave.UpdatedAt = s.now().In(s.location)
	if updateAttachment {
		leave.BuktiPendukung = attachment
	}
	if err := s.store.UpdateLeaveRequest(ctx, rowNumber, leave, updateAttachment); err != nil {
		return nil, fmt.Errorf("update leave request: %w", err)
	}
	return leave, nil
}

func (s *LeaveService) Cancel(ctx context.Context, owner *model.User, leaveID string) (*model.Leave, error) {
	if err := validateUser(owner); err != nil {
		return nil, err
	}
	leaveID = strings.TrimSpace(leaveID)
	if leaveID == "" {
		return nil, fmt.Errorf("%w: ID pengajuan wajib diisi", ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	leave, rowNumber, err := s.store.FindLeaveRow(ctx, leaveID)
	if err != nil {
		return nil, fmt.Errorf("find leave: %w", err)
	}
	if leave.UserID != owner.UserID {
		return nil, ErrForbidden
	}
	if leave.Status != model.LeaveStatusMenunggu {
		return nil, fmt.Errorf("%w: hanya pengajuan menunggu yang dapat dibatalkan", ErrConflict)
	}
	now := s.now().In(s.location)
	leave.Status = model.LeaveStatusDibatalkan
	leave.DibatalkanPada = &now
	leave.UpdatedAt = now
	if err := s.store.CancelLeave(ctx, rowNumber, leave); err != nil {
		return nil, fmt.Errorf("cancel leave: %w", err)
	}
	return leave, nil
}

func (s *LeaveService) Decide(ctx context.Context, actor *model.User, leaveID string, decision LeaveDecision, note string) (*model.Leave, error) {
	if err := validateUser(actor); err != nil {
		return nil, err
	}
	if !canManageLeave(actor) {
		return nil, ErrForbidden
	}
	leaveID = strings.TrimSpace(leaveID)
	if leaveID == "" {
		return nil, fmt.Errorf("%w: ID pengajuan wajib diisi", ErrValidation)
	}
	decision = LeaveDecision(strings.ToUpper(strings.TrimSpace(string(decision))))
	note = strings.TrimSpace(note)
	if decision != LeaveDecisionApprove && decision != LeaveDecisionReject {
		return nil, fmt.Errorf("%w: keputusan tidak valid", ErrValidation)
	}
	if decision == LeaveDecisionReject && note == "" {
		return nil, fmt.Errorf("%w: catatan wajib diisi saat menolak", ErrValidation)
	}
	if utf8.RuneCountInString(note) > LeaveTextMaxLength {
		return nil, fmt.Errorf("%w: catatan maksimal %d karakter", ErrValidation, LeaveTextMaxLength)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	leave, rowNumber, err := s.store.FindLeaveRow(ctx, leaveID)
	if err != nil {
		return nil, fmt.Errorf("find leave: %w", err)
	}
	if leave.Status != model.LeaveStatusMenunggu {
		return nil, fmt.Errorf("%w: pengajuan sudah diproses", ErrConflict)
	}
	now := s.now().In(s.location)
	if decision == LeaveDecisionApprove {
		leave.Status = model.LeaveStatusDisetujui
	} else {
		leave.Status = model.LeaveStatusDitolak
	}
	leave.CatatanApproval = note
	leave.DiprosesOleh = strings.TrimSpace(actor.NamaLengkap)
	leave.DiprosesOlehUserID = actor.UserID
	leave.DiprosesPada = &now
	leave.UpdatedAt = now
	if err := s.store.UpdateLeaveDecision(ctx, rowNumber, leave); err != nil {
		return nil, fmt.Errorf("update leave decision: %w", err)
	}
	return leave, nil
}

func (s *LeaveService) OwnRequests(ctx context.Context, userID string) ([]model.Leave, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user tidak ditemukan", ErrValidation)
	}
	rows, err := s.store.ListLeave(ctx)
	if err != nil {
		return nil, fmt.Errorf("read leave requests: %w", err)
	}
	result := make([]model.Leave, 0)
	for _, row := range rows {
		if row.UserID == userID {
			result = append(result, row)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].CreatedAt.After(result[j].CreatedAt)
		}
		return result[i].LeaveID > result[j].LeaveID
	})
	return result, nil
}

func (s *LeaveService) ApprovalRows(ctx context.Context, filters LeaveFilters) ([]model.Leave, error) {
	filters, err := normalizeLeaveFilters(filters)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListLeave(ctx)
	if err != nil {
		return nil, fmt.Errorf("read leave requests: %w", err)
	}
	result := make([]model.Leave, 0, len(rows))
	for _, row := range rows {
		if filters.Status != "" && row.Status != filters.Status {
			continue
		}
		if filters.JenisLeave != "" && row.JenisLeave != filters.JenisLeave {
			continue
		}
		if filters.From != "" && row.TanggalSelesai < filters.From {
			continue
		}
		if filters.To != "" && row.TanggalMulai > filters.To {
			continue
		}
		if filters.Q != "" && !leaveMatchesQuery(row, filters.Q) {
			continue
		}
		result = append(result, row)
	}
	sort.SliceStable(result, func(i, j int) bool {
		iPending := result[i].Status == model.LeaveStatusMenunggu
		jPending := result[j].Status == model.LeaveStatusMenunggu
		if iPending != jPending {
			return iPending
		}
		if iPending {
			if !result[i].CreatedAt.Equal(result[j].CreatedAt) {
				return result[i].CreatedAt.Before(result[j].CreatedAt)
			}
			return result[i].LeaveID < result[j].LeaveID
		}
		if !result[i].UpdatedAt.Equal(result[j].UpdatedAt) {
			return result[i].UpdatedAt.After(result[j].UpdatedAt)
		}
		return result[i].LeaveID > result[j].LeaveID
	})
	return result, nil
}

func (s *LeaveService) Attachment(ctx context.Context, requester *model.User, leaveID string, canHR bool) (string, error) {
	if err := validateUser(requester); err != nil {
		return "", err
	}
	leaveID = strings.TrimSpace(leaveID)
	if leaveID == "" {
		return "", fmt.Errorf("%w: ID pengajuan wajib diisi", ErrValidation)
	}
	leave, rowNumber, err := s.store.FindLeaveRow(ctx, leaveID)
	if err != nil {
		return "", fmt.Errorf("find leave: %w", err)
	}
	if leave.UserID != requester.UserID && !canHR {
		return "", ErrForbidden
	}
	if !leave.HasBuktiPendukung {
		return "", repository.ErrNotFound
	}
	value, err := s.store.ReadLeaveAttachment(ctx, rowNumber)
	if err != nil {
		return "", fmt.Errorf("read leave attachment: %w", err)
	}
	if err := photo.ValidateDataURL(value); err != nil {
		return "", fmt.Errorf("stored leave attachment: %w", ErrInvalidPhoto)
	}
	return value, nil
}

func (s *LeaveService) PersonalSummary(ctx context.Context, userID string) (*LeavePersonalSummary, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: user tidak ditemukan", ErrValidation)
	}
	rows, err := s.store.ListLeave(ctx)
	if err != nil {
		return nil, fmt.Errorf("read leave requests: %w", err)
	}
	now := s.now().In(s.location)
	year := now.Year()
	today := now.Format("2006-01-02")
	yearStart := fmt.Sprintf("%04d-01-01", year)
	yearEnd := fmt.Sprintf("%04d-12-31", year)
	approvedDates := make(map[string]bool)
	summary := &LeavePersonalSummary{Year: year}
	var pendingToday *model.Leave
	for i := range rows {
		row := rows[i]
		if row.UserID != userID {
			continue
		}
		if row.Status == model.LeaveStatusMenunggu {
			summary.PendingCount++
			if isWeekdayString(today) && row.TanggalMulai <= today && row.TanggalSelesai >= today {
				copy := row
				pendingToday = &copy
			}
		}
		if row.Status != model.LeaveStatusDisetujui {
			continue
		}
		start := maxDateString(row.TanggalMulai, yearStart)
		end := minDateString(row.TanggalSelesai, yearEnd)
		for _, day := range weekdayDatesInRange(start, end) {
			approvedDates[day] = true
		}
		if isWeekdayString(today) && row.TanggalMulai <= today && row.TanggalSelesai >= today {
			copy := row
			summary.TodayStatus = model.LeaveStatusDisetujui
			summary.TodayLeave = &copy
		}
	}
	summary.ApprovedDays = len(approvedDates)
	if summary.TodayLeave == nil && pendingToday != nil {
		summary.TodayStatus = model.LeaveStatusMenunggu
		summary.TodayLeave = pendingToday
	}
	return summary, nil
}

func (s *LeaveService) BuildHROverview(ctx context.Context, from, to string, schedule Schedule) (*HROverview, error) {
	from, to, err := s.normalizeOverviewRange(from, to)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	usersByID := make(map[string]model.User, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.UserID) != "" {
			usersByID[user.UserID] = user
		}
	}
	attendance, err := s.store.ListAttendanceBetween(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("read attendance: %w", err)
	}
	leaves, err := s.store.ListLeave(ctx)
	if err != nil {
		return nil, fmt.Errorf("read leave requests: %w", err)
	}

	present := make(map[string]map[string]bool)
	overtimeMinutes := 0
	for _, row := range attendance {
		if row.TanggalAbsensi < from || row.TanggalAbsensi > to || !userActiveOn(usersByID, row.UserID, row.TanggalAbsensi) {
			continue
		}
		if present[row.TanggalAbsensi] == nil {
			present[row.TanggalAbsensi] = make(map[string]bool)
		}
		present[row.TanggalAbsensi][row.UserID] = true
		if row.ClockInAt.IsZero() {
			continue
		}
		var clockOut *time.Time
		if row.ClockOutAt != nil && !row.ClockOutAt.IsZero() {
			local := row.ClockOutAt.In(s.location)
			clockOut = &local
		}
		rule := schedule.Judge(row.ClockInAt.In(s.location), clockOut)
		overtimeMinutes += rule.OvertimeMinutes
	}

	approvedLeave := make(map[string]map[string]bool)
	for _, row := range leaves {
		if row.Status != model.LeaveStatusDisetujui || row.TanggalSelesai < from || row.TanggalMulai > to {
			continue
		}
		start := maxDateString(row.TanggalMulai, from)
		end := minDateString(row.TanggalSelesai, to)
		for _, day := range weekdayDatesInRange(start, end) {
			if !userActiveOn(usersByID, row.UserID, day) {
				continue
			}
			if approvedLeave[day] == nil {
				approvedLeave[day] = make(map[string]bool)
			}
			approvedLeave[day][row.UserID] = true
		}
	}

	overview := &HROverview{
		From:        from,
		To:          to,
		LastUpdated: s.now().In(s.location).Format("02 Jan 2006 15:04"),
		LemburJam:   round2(float64(overtimeMinutes) / 60),
	}
	for _, day := range dateStringsInRange(from, to) {
		active := activeUserIDsOn(users, day)
		point := HRDatePoint{Tanggal: day, Label: hrDateLabel(day)}
		for userID := range active {
			switch {
			case present[day][userID]:
				point.Hadir++
			case approvedLeave[day][userID]:
				point.Cuti++
			default:
				point.TidakHadir++
			}
		}
		overview.Series = append(overview.Series, point)
		if day == to {
			overview.TotalKaryawan = len(active)
			overview.HadirHariAkhir = point.Hadir
			overview.TidakHadirHariAkhir = point.TidakHadir
			overview.CutiHariAkhir = point.Cuti
		}
	}

	activeAtEnd := make([]model.User, 0)
	byJabatan := make(map[string]int)
	for _, user := range users {
		if !isActiveUserOn(user, to) {
			continue
		}
		activeAtEnd = append(activeAtEnd, user)
		jabatan := strings.TrimSpace(user.Jabatan)
		if jabatan == "" {
			jabatan = "Lainnya"
		}
		byJabatan[jabatan]++
	}
	for jabatan, total := range byJabatan {
		percent := 0.0
		if overview.TotalKaryawan > 0 {
			percent = round2(float64(total) * 100 / float64(overview.TotalKaryawan))
		}
		overview.JabatanShares = append(overview.JabatanShares, HRJabatanShare{Jabatan: jabatan, Total: total, Percent: percent})
	}
	// Largest departments lead the legend; equal totals use a stable name order.
	sort.SliceStable(overview.JabatanShares, func(i, j int) bool {
		if overview.JabatanShares[i].Total != overview.JabatanShares[j].Total {
			return overview.JabatanShares[i].Total > overview.JabatanShares[j].Total
		}
		return overview.JabatanShares[i].Jabatan < overview.JabatanShares[j].Jabatan
	})

	sort.SliceStable(activeAtEnd, func(i, j int) bool {
		if activeAtEnd[i].TanggalGabung != activeAtEnd[j].TanggalGabung {
			return activeAtEnd[i].TanggalGabung > activeAtEnd[j].TanggalGabung
		}
		if !activeAtEnd[i].CreatedAt.Equal(activeAtEnd[j].CreatedAt) {
			return activeAtEnd[i].CreatedAt.After(activeAtEnd[j].CreatedAt)
		}
		return activeAtEnd[i].UserID > activeAtEnd[j].UserID
	})
	if len(activeAtEnd) > 5 {
		activeAtEnd = activeAtEnd[:5]
	}
	overview.KaryawanBaru = activeAtEnd

	sort.SliceStable(leaves, func(i, j int) bool {
		if !leaves[i].CreatedAt.Equal(leaves[j].CreatedAt) {
			return leaves[i].CreatedAt.After(leaves[j].CreatedAt)
		}
		return leaves[i].LeaveID > leaves[j].LeaveID
	})
	if len(leaves) > 5 {
		leaves = leaves[:5]
	}
	overview.PengajuanTerbaru = append([]model.Leave(nil), leaves...)
	return overview, nil
}

func normalizeLeaveInput(input LeaveInput, hasAttachment bool) (LeaveInput, int, error) {
	input.JenisLeave = strings.TrimSpace(input.JenisLeave)
	input.TanggalMulai = strings.TrimSpace(input.TanggalMulai)
	input.TanggalSelesai = strings.TrimSpace(input.TanggalSelesai)
	input.Alasan = strings.TrimSpace(input.Alasan)
	input.BuktiPendukung = strings.TrimSpace(input.BuktiPendukung)
	var validType bool
	for _, option := range []string{model.LeaveJenisCutiTahunan, model.LeaveJenisCutiSakit, model.LeaveJenisIzin} {
		if strings.EqualFold(input.JenisLeave, option) {
			input.JenisLeave = option
			validType = true
			break
		}
	}
	if !validType {
		return LeaveInput{}, 0, fmt.Errorf("%w: jenis leave tidak valid", ErrValidation)
	}
	if input.Alasan == "" {
		return LeaveInput{}, 0, fmt.Errorf("%w: alasan wajib diisi", ErrValidation)
	}
	if utf8.RuneCountInString(input.Alasan) > LeaveTextMaxLength {
		return LeaveInput{}, 0, fmt.Errorf("%w: alasan maksimal %d karakter", ErrValidation, LeaveTextMaxLength)
	}
	start, err := time.Parse("2006-01-02", input.TanggalMulai)
	if err != nil {
		return LeaveInput{}, 0, fmt.Errorf("%w: tanggal mulai tidak valid", ErrValidation)
	}
	end, err := time.Parse("2006-01-02", input.TanggalSelesai)
	if err != nil {
		return LeaveInput{}, 0, fmt.Errorf("%w: tanggal selesai tidak valid", ErrValidation)
	}
	if end.Before(start) {
		return LeaveInput{}, 0, fmt.Errorf("%w: tanggal selesai harus setelah tanggal mulai", ErrValidation)
	}
	days := countWeekdays(start, end)
	if days < 1 {
		return LeaveInput{}, 0, fmt.Errorf("%w: rentang leave harus memiliki hari kerja", ErrValidation)
	}
	if input.BuktiPendukung != "" {
		if err := photo.ValidateDataURL(input.BuktiPendukung); err != nil {
			return LeaveInput{}, 0, ErrInvalidPhoto
		}
		hasAttachment = true
	}
	if input.JenisLeave == model.LeaveJenisCutiSakit && !hasAttachment {
		return LeaveInput{}, 0, fmt.Errorf("%w: bukti pendukung wajib untuk cuti sakit", ErrValidation)
	}
	return input, days, nil
}

func normalizeLeaveFilters(filters LeaveFilters) (LeaveFilters, error) {
	filters.Q = strings.ToLower(strings.TrimSpace(filters.Q))
	filters.Status = strings.ToUpper(strings.TrimSpace(filters.Status))
	filters.JenisLeave = strings.TrimSpace(filters.JenisLeave)
	filters.From = strings.TrimSpace(filters.From)
	filters.To = strings.TrimSpace(filters.To)
	if filters.Status != "" {
		valid := false
		for _, status := range []string{model.LeaveStatusMenunggu, model.LeaveStatusDisetujui, model.LeaveStatusDitolak, model.LeaveStatusDibatalkan} {
			if filters.Status == status {
				valid = true
				break
			}
		}
		if !valid {
			return LeaveFilters{}, fmt.Errorf("%w: status leave tidak valid", ErrValidation)
		}
	}
	if filters.JenisLeave != "" {
		valid := false
		for _, option := range []string{model.LeaveJenisCutiTahunan, model.LeaveJenisCutiSakit, model.LeaveJenisIzin} {
			if strings.EqualFold(filters.JenisLeave, option) {
				filters.JenisLeave = option
				valid = true
				break
			}
		}
		if !valid {
			return LeaveFilters{}, fmt.Errorf("%w: jenis leave tidak valid", ErrValidation)
		}
	}
	if filters.From != "" {
		if _, err := time.Parse("2006-01-02", filters.From); err != nil {
			return LeaveFilters{}, fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
		}
	}
	if filters.To != "" {
		if _, err := time.Parse("2006-01-02", filters.To); err != nil {
			return LeaveFilters{}, fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
	}
	if filters.From != "" && filters.To != "" && filters.From > filters.To {
		filters.From, filters.To = filters.To, filters.From
	}
	return filters, nil
}

func (s *LeaveService) normalizeOverviewRange(from, to string) (string, string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	defaultFrom, defaultTo := s.DefaultOverviewRange()
	if from == "" && to == "" {
		return defaultFrom, defaultTo, nil
	}
	if to == "" {
		to = defaultTo
	}
	if from == "" {
		parsedTo, err := time.Parse("2006-01-02", to)
		if err != nil {
			return "", "", fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
		from = parsedTo.AddDate(0, 0, -6).Format("2006-01-02")
	}
	parsedFrom, err := time.Parse("2006-01-02", from)
	if err != nil {
		return "", "", fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
	}
	parsedTo, err := time.Parse("2006-01-02", to)
	if err != nil {
		return "", "", fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
	}
	if from > to {
		from, to = to, from
		parsedFrom, parsedTo = parsedTo, parsedFrom
	}
	if parsedTo.Sub(parsedFrom) > 365*24*time.Hour {
		return "", "", fmt.Errorf("%w: rentang overview maksimal 366 hari", ErrValidation)
	}
	return from, to, nil
}

func canManageLeave(user *model.User) bool {
	return user != nil && (strings.EqualFold(strings.TrimSpace(user.Jabatan), "HR") || strings.EqualFold(strings.TrimSpace(user.Jabatan), "Management"))
}

func leaveMatchesQuery(row model.Leave, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{row.LeaveID, row.NamaLengkap, row.NRP, row.Jabatan, row.Alasan}, " "))
	return strings.Contains(haystack, query)
}

func countWeekdays(start, end time.Time) int {
	if end.Before(start) {
		return 0
	}
	// time.Time.Sub returns a time.Duration and therefore saturates at roughly
	// 292 years. Leave dates are date-only values and Go's parser accepts a much
	// wider calendar range, so use Unix seconds to keep the inclusive day count
	// correct without looping across an attacker-controlled span.
	totalDays := int((end.Unix()-start.Unix())/secondsPerDay) + 1
	count := (totalDays / 7) * 5
	remaining := totalDays % 7
	for index := 0; index < remaining; index++ {
		day := start.AddDate(0, 0, index)
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			count++
		}
	}
	return count
}

const secondsPerDay int64 = 24 * 60 * 60

func dateStringsInRange(from, to string) []string {
	start, startErr := time.Parse("2006-01-02", from)
	end, endErr := time.Parse("2006-01-02", to)
	if startErr != nil || endErr != nil || end.Before(start) {
		return nil
	}
	result := make([]string, 0, int(end.Sub(start).Hours()/24)+1)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		result = append(result, day.Format("2006-01-02"))
	}
	return result
}

func weekdayDatesInRange(from, to string) []string {
	all := dateStringsInRange(from, to)
	result := make([]string, 0, len(all))
	for _, day := range all {
		if isWeekdayString(day) {
			result = append(result, day)
		}
	}
	return result
}

func isWeekdayString(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Weekday() != time.Saturday && date.Weekday() != time.Sunday
}

func activeUserIDsOn(users []model.User, date string) map[string]bool {
	result := make(map[string]bool)
	for _, user := range users {
		if isActiveUserOn(user, date) {
			result[user.UserID] = true
		}
	}
	return result
}

func userActiveOn(usersByID map[string]model.User, userID, date string) bool {
	user, found := usersByID[userID]
	return found && isActiveUserOn(user, date)
}

func isActiveUserOn(user model.User, date string) bool {
	if user.StatusPengguna != model.StatusAktif || strings.TrimSpace(user.UserID) == "" {
		return false
	}
	joined := strings.TrimSpace(user.TanggalGabung)
	if joined == "" {
		return true
	}
	if _, err := time.Parse("2006-01-02", joined); err != nil {
		// Legacy rows without a parseable join date remain visible instead of
		// disappearing from every HR count.
		return true
	}
	return joined <= date
}

func minDateString(left, right string) string {
	if left < right {
		return left
	}
	return right
}

func maxDateString(left, right string) string {
	if left > right {
		return left
	}
	return right
}

var hrShortMonths = [...]string{"Jan", "Feb", "Mar", "Apr", "Mei", "Jun", "Jul", "Agu", "Sep", "Okt", "Nov", "Des"}

func hrDateLabel(value string) string {
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	return fmt.Sprintf("%02d %s", date.Day(), hrShortMonths[int(date.Month())-1])
}
