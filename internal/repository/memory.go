package repository

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
)

// TestRepository is used only by automated tests.
// The application runtime always uses Google Sheets.
type TestRepository struct {
	mu         sync.RWMutex
	users      []*model.User
	activities []*model.LoginActivity
	attendance []*model.Attendance
	unitDT     []*model.UnitDT
	produksi   []*model.Produksi
	plans      []*model.ProduksiPlan
	unitA2B    []*model.UnitA2B
	nota       []*model.Nota
	leaves     []*model.Leave
	fuelMasuk  []*model.FuelMasuk
	fuelKeluar []*model.FuelKeluar
}

func NewTestRepository() *TestRepository {
	return &TestRepository{}
}

func (r *TestRepository) EnsureSchema(context.Context) error { return nil }

func (r *TestRepository) UnitDTExists(_ context.Context, nopol string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nopol = strings.ToUpper(strings.TrimSpace(nopol))
	for _, unit := range r.unitDT {
		if strings.ToUpper(strings.TrimSpace(unit.Nopol)) == nopol {
			return true, nil
		}
	}
	return false, nil
}

func (r *TestRepository) MaxUnitDTSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, unit := range r.unitDT {
		trimmed := strings.TrimSpace(unit.UnitID)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateUnitDT(_ context.Context, unit *model.UnitDT) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *unit
	r.unitDT = append(r.unitDT, &stored)
	return nil
}

func (r *TestRepository) ListUnitDT(_ context.Context) ([]model.UnitDT, error) {
	return r.UnitDTList(), nil
}

func (r *TestRepository) MaxProduksiSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, produksi := range r.produksi {
		trimmed := strings.TrimSpace(produksi.ProduksiID)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateProduksi(_ context.Context, produksi *model.Produksi) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *produksi
	r.produksi = append(r.produksi, &stored)
	return nil
}

func (r *TestRepository) CreateProduksiBatch(_ context.Context, rows []*model.Produksi) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, produksi := range rows {
		stored := *produksi
		r.produksi = append(r.produksi, &stored)
	}
	return nil
}

func (r *TestRepository) ListProduksi(_ context.Context) ([]model.Produksi, error) {
	return r.ProduksiList(), nil
}

func (r *TestRepository) MaxProduksiPlanSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, plan := range r.plans {
		trimmed := strings.TrimSpace(plan.PlanID)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateProduksiPlan(_ context.Context, plan *model.ProduksiPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *plan
	r.plans = append(r.plans, &stored)
	return nil
}

func (r *TestRepository) ListProduksiPlan(_ context.Context) ([]model.ProduksiPlan, error) {
	return r.ProduksiPlanList(), nil
}

// ProduksiPlanList exposes stored plans to tests.
func (r *TestRepository) ProduksiPlanList() []model.ProduksiPlan {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plans := make([]model.ProduksiPlan, 0, len(r.plans))
	for _, plan := range r.plans {
		plans = append(plans, *plan)
	}
	return plans
}

func (r *TestRepository) UnitA2BExists(_ context.Context, idUnit string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	idUnit = strings.ToUpper(strings.TrimSpace(idUnit))
	for _, unit := range r.unitA2B {
		if strings.ToUpper(strings.TrimSpace(unit.IDUnit)) == idUnit {
			return true, nil
		}
	}
	return false, nil
}

func (r *TestRepository) MaxUnitA2BNumber(_ context.Context) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, unit := range r.unitA2B {
		if unit.NoUrut > highest {
			highest = unit.NoUrut
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateUnitA2B(_ context.Context, unit *model.UnitA2B) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *unit
	r.unitA2B = append(r.unitA2B, &stored)
	return nil
}

func (r *TestRepository) ListUnitA2B(_ context.Context) ([]model.UnitA2B, error) {
	return r.UnitA2BList(), nil
}

func (r *TestRepository) MaxNotaSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, nota := range r.nota {
		if !strings.HasPrefix(nota.NotaID, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(nota.NotaID, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateNota(_ context.Context, nota *model.Nota) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored := *nota
	stored.Items = append([]model.NotaItem(nil), nota.Items...)
	r.nota = append(r.nota, &stored)
	return nil
}

func (r *TestRepository) ListNota(_ context.Context) ([]model.Nota, error) {
	// The sheet leaves the photo columns unread here; the in-memory store has
	// to do the same, or a test would pass on data production never returns.
	notas := r.NotaList()
	for i := range notas {
		notas[i].FotoKwitansi = ""
		notas[i].BuktiTransfer = ""
		notas[i].BuktiBayar = ""
	}
	return notas, nil
}

// ListNotaWithAttachments reads the receipt photo and nothing else: the sheet
// stops at that column, so returning the payment proofs here would let a test
// pass on data production never returns.
func (r *TestRepository) ListNotaWithAttachments(_ context.Context) ([]model.Nota, error) {
	notas := r.NotaList()
	for i := range notas {
		notas[i].BuktiTransfer = ""
		notas[i].BuktiBayar = ""
	}
	return notas, nil
}

func (r *TestRepository) ListNotaItems(_ context.Context) ([]model.NotaItem, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]model.NotaItem, 0)
	for _, nota := range r.nota {
		items = append(items, nota.Items...)
	}
	return items, nil
}

func (r *TestRepository) FindNotaRow(_ context.Context, notaID string) (*model.Nota, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := strings.ToUpper(strings.TrimSpace(notaID))
	for i, nota := range r.nota {
		if strings.ToUpper(strings.TrimSpace(nota.NotaID)) != wanted {
			continue
		}
		stored := *nota
		stored.Items = append([]model.NotaItem(nil), nota.Items...)
		// Row 1 is the header in the sheet, so the first record sits on row 2.
		return &stored, i + 2, nil
	}
	return nil, 0, ErrNotFound
}

func (r *TestRepository) SettleNota(_ context.Context, rowNumber int, nota *model.Nota) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.nota) {
		return fmt.Errorf("invalid nota row number %d", rowNumber)
	}
	stored := r.nota[index]
	stored.StatusPembayaran = nota.StatusPembayaran
	stored.UpdatedAt = nota.UpdatedAt
	stored.BuktiBayar = nota.BuktiBayar
	stored.DibayarPada = nota.DibayarPada
	stored.DirekonsiliasiOleh = nota.DirekonsiliasiOleh
	stored.DirekonsiliasiOlehID = nota.DirekonsiliasiOlehID
	return nil
}

// UserList exposes registered users to tests.
func (r *TestRepository) UserList() []model.User {
	r.mu.RLock()
	defer r.mu.RUnlock()
	users := make([]model.User, 0, len(r.users))
	for _, user := range r.users {
		users = append(users, *user)
	}
	return users
}

func (r *TestRepository) ListUsers(context.Context) ([]model.User, error) {
	users := r.UserList()
	for i := range users {
		users[i].FotoProfil = ""
	}
	return users, nil
}

// NotaList exposes stored notes to tests, without their line items.
func (r *TestRepository) NotaList() []model.Nota {
	r.mu.RLock()
	defer r.mu.RUnlock()
	notas := make([]model.Nota, 0, len(r.nota))
	for _, nota := range r.nota {
		stored := *nota
		stored.Items = append([]model.NotaItem(nil), nota.Items...)
		notas = append(notas, stored)
	}
	return notas
}

// UnitA2BList exposes stored A2B units to tests.
func (r *TestRepository) UnitA2BList() []model.UnitA2B {
	r.mu.RLock()
	defer r.mu.RUnlock()
	units := make([]model.UnitA2B, 0, len(r.unitA2B))
	for _, unit := range r.unitA2B {
		units = append(units, *unit)
	}
	return units
}

// ProduksiList exposes stored production rows to tests.
func (r *TestRepository) ProduksiList() []model.Produksi {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.Produksi, 0, len(r.produksi))
	for _, produksi := range r.produksi {
		rows = append(rows, *produksi)
	}
	return rows
}

// UnitDTList exposes stored units to tests.
func (r *TestRepository) UnitDTList() []model.UnitDT {
	r.mu.RLock()
	defer r.mu.RUnlock()
	units := make([]model.UnitDT, 0, len(r.unitDT))
	for _, unit := range r.unitDT {
		units = append(units, *unit)
	}
	return units
}

func (r *TestRepository) FindUserByID(_ context.Context, userID string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.users {
		if user.UserID == userID {
			return readUser(user), nil
		}
	}
	return nil, ErrNotFound
}

func (r *TestRepository) FindUserByIdentifier(_ context.Context, identifier string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	identifier = strings.ToLower(strings.TrimSpace(identifier))
	for _, user := range r.users {
		if strings.ToLower(user.NRP) == identifier || strings.ToLower(user.Email) == identifier {
			return readUser(user), nil
		}
	}
	return nil, ErrNotFound
}

func (r *TestRepository) UserExists(_ context.Context, nrp, email string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	nrp = strings.ToLower(strings.TrimSpace(nrp))
	email = strings.ToLower(strings.TrimSpace(email))
	for _, user := range r.users {
		if strings.ToLower(user.NRP) == nrp || strings.ToLower(user.Email) == email {
			return true, nil
		}
	}
	return false, nil
}

func (r *TestRepository) CreateUser(_ context.Context, user *model.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.users {
		if strings.EqualFold(existing.NRP, user.NRP) || strings.EqualFold(existing.Email, user.Email) {
			return ErrNotFound
		}
	}
	r.users = append(r.users, cloneUser(user))
	return nil
}

func (r *TestRepository) UpdateLastLogin(_ context.Context, userID string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, user := range r.users {
		if user.UserID == userID {
			user.LastLoginAt = timePtr(at)
			user.UpdatedAt = at
			return nil
		}
	}
	return ErrNotFound
}

// FindUserRow mirrors the sheet: the returned user carries no photo, because
// the read that finds it stops before that column.
func (r *TestRepository) FindUserRow(_ context.Context, userID string) (*model.User, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i, user := range r.users {
		if user.UserID != userID {
			continue
		}
		// Row 1 is the header in the sheet, so the first record sits on row 2.
		return readUser(user), i + 2, nil
	}
	return nil, 0, ErrNotFound
}

func (r *TestRepository) UpdateUserProfile(_ context.Context, rowNumber int, user *model.User, updatePhoto bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.users) {
		return fmt.Errorf("invalid user row number %d", rowNumber)
	}
	stored := r.users[index]
	stored.NamaLengkap = user.NamaLengkap
	stored.NoTelp = user.NoTelp
	stored.TanggalLahir = user.TanggalLahir
	stored.UpdatedAt = user.UpdatedAt
	// A save that carries no new image leaves the stored one alone, the way a
	// write of only the profile columns does on the sheet.
	if updatePhoto {
		stored.PunyaFoto = user.PunyaFoto
		stored.FotoProfil = user.FotoProfil
	}
	return nil
}

func (r *TestRepository) ReadUserPhoto(_ context.Context, rowNumber int) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.users) {
		return "", fmt.Errorf("invalid user row number %d", rowNumber)
	}
	return r.users[index].FotoProfil, nil
}

func (r *TestRepository) AppendActivity(_ context.Context, activity *model.LoginActivity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities = append(r.activities, cloneActivity(activity))
	return nil
}

func (r *TestRepository) FindAttendanceByUserDate(_ context.Context, userID, date string) (*model.Attendance, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for index, attendance := range r.attendance {
		if attendance.UserID == userID && attendance.TanggalAbsensi == date {
			return cloneAttendance(attendance), index + 2, nil
		}
	}
	return nil, 0, nil
}

func (r *TestRepository) ListAttendanceByUser(_ context.Context, userID string) ([]model.Attendance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.Attendance, 0, len(r.attendance))
	for _, attendance := range r.attendance {
		if attendance.UserID == userID {
			rows = append(rows, *attendance)
		}
	}
	return rows, nil
}

func (r *TestRepository) ListAttendanceBetween(_ context.Context, from, to string) ([]model.Attendance, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	rows := make([]model.Attendance, 0, len(r.attendance))
	for _, attendance := range r.attendance {
		date := strings.TrimSpace(attendance.TanggalAbsensi)
		if date == "" || (from != "" && date < from) || (to != "" && date > to) {
			continue
		}
		stored := cloneAttendance(attendance)
		// Match the production repository: aggregate reads never carry photos.
		stored.ClockInPhoto = ""
		stored.ClockOutPhoto = ""
		rows = append(rows, *stored)
	}
	return rows, nil
}

func (r *TestRepository) CreateAttendance(_ context.Context, attendance *model.Attendance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attendance = append(r.attendance, cloneAttendance(attendance))
	return nil
}

func (r *TestRepository) UpdateAttendance(_ context.Context, rowNumber int, attendance *model.Attendance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.attendance) {
		return ErrNotFound
	}
	r.attendance[index] = cloneAttendance(attendance)
	return nil
}

func (r *TestRepository) MaxLeaveSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, leave := range r.leaves {
		if !strings.HasPrefix(strings.TrimSpace(leave.LeaveID), prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(leave.LeaveID), prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateLeave(_ context.Context, leave *model.Leave) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.leaves = append(r.leaves, cloneLeave(leave))
	return nil
}

func (r *TestRepository) ListLeave(context.Context) ([]model.Leave, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.Leave, 0, len(r.leaves))
	for _, leave := range r.leaves {
		stored := cloneLeave(leave)
		stored.BuktiPendukung = ""
		rows = append(rows, *stored)
	}
	return rows, nil
}

func (r *TestRepository) FindLeaveRow(_ context.Context, leaveID string) (*model.Leave, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := strings.ToUpper(strings.TrimSpace(leaveID))
	for index, leave := range r.leaves {
		if strings.ToUpper(strings.TrimSpace(leave.LeaveID)) != wanted {
			continue
		}
		stored := cloneLeave(leave)
		stored.BuktiPendukung = ""
		return stored, index + 2, nil
	}
	return nil, 0, ErrNotFound
}

func (r *TestRepository) UpdateLeaveRequest(_ context.Context, rowNumber int, leave *model.Leave, updateAttachment bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, err := r.leaveAtRow(rowNumber)
	if err != nil {
		return err
	}
	stored.JenisLeave = leave.JenisLeave
	stored.TanggalMulai = leave.TanggalMulai
	stored.TanggalSelesai = leave.TanggalSelesai
	stored.JumlahHari = leave.JumlahHari
	stored.Alasan = leave.Alasan
	stored.UpdatedAt = leave.UpdatedAt
	if updateAttachment {
		stored.HasBuktiPendukung = leave.HasBuktiPendukung
		stored.BuktiPendukung = leave.BuktiPendukung
	}
	return nil
}

func (r *TestRepository) CancelLeave(_ context.Context, rowNumber int, leave *model.Leave) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, err := r.leaveAtRow(rowNumber)
	if err != nil {
		return err
	}
	stored.Status = leave.Status
	stored.DibatalkanPada = cloneTime(leave.DibatalkanPada)
	stored.UpdatedAt = leave.UpdatedAt
	return nil
}

func (r *TestRepository) UpdateLeaveDecision(_ context.Context, rowNumber int, leave *model.Leave) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, err := r.leaveAtRow(rowNumber)
	if err != nil {
		return err
	}
	stored.Status = leave.Status
	stored.CatatanApproval = leave.CatatanApproval
	stored.DiprosesOleh = leave.DiprosesOleh
	stored.DiprosesOlehUserID = leave.DiprosesOlehUserID
	stored.DiprosesPada = cloneTime(leave.DiprosesPada)
	stored.UpdatedAt = leave.UpdatedAt
	return nil
}

func (r *TestRepository) ReadLeaveAttachment(_ context.Context, rowNumber int) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, err := r.leaveAtRow(rowNumber)
	if err != nil {
		return "", err
	}
	return stored.BuktiPendukung, nil
}

// LeaveList exposes the complete stored rows, including attachments, to tests.
func (r *TestRepository) LeaveList() []model.Leave {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.Leave, 0, len(r.leaves))
	for _, leave := range r.leaves {
		rows = append(rows, *cloneLeave(leave))
	}
	return rows
}

// leaveAtRow expects the caller to hold either r.mu's read or write lock.
func (r *TestRepository) leaveAtRow(rowNumber int) (*model.Leave, error) {
	index := rowNumber - 2
	if index < 0 || index >= len(r.leaves) {
		return nil, ErrNotFound
	}
	return r.leaves[index], nil
}

func (r *TestRepository) MaxFuelMasukSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, fuel := range r.fuelMasuk {
		trimmed := strings.TrimSpace(fuel.FuelID)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateFuelMasuk(_ context.Context, fuel *model.FuelMasuk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fuelMasuk = append(r.fuelMasuk, cloneFuelMasuk(fuel))
	return nil
}

// ListFuelMasuk drops the photos, as the Sheets listing does, so a handler that
// accidentally relies on them fails in tests rather than in production.
func (r *TestRepository) ListFuelMasuk(context.Context) ([]model.FuelMasuk, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.FuelMasuk, 0, len(r.fuelMasuk))
	for _, fuel := range r.fuelMasuk {
		rows = append(rows, *withoutFuelPhotos(cloneFuelMasuk(fuel)))
	}
	return rows, nil
}

func (r *TestRepository) FindFuelMasukRow(_ context.Context, fuelID string) (*model.FuelMasuk, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := strings.ToUpper(strings.TrimSpace(fuelID))
	for index, fuel := range r.fuelMasuk {
		if strings.ToUpper(strings.TrimSpace(fuel.FuelID)) != wanted {
			continue
		}
		return withoutFuelPhotos(cloneFuelMasuk(fuel)), index + 2, nil
	}
	return nil, 0, ErrNotFound
}

func (r *TestRepository) UpdateFuelMasukDecision(_ context.Context, rowNumber int, fuel *model.FuelMasuk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	stored, err := r.fuelMasukAtRow(rowNumber)
	if err != nil {
		return err
	}
	stored.StatusApproval = fuel.StatusApproval
	stored.CatatanApproval = fuel.CatatanApproval
	stored.DiprosesOleh = fuel.DiprosesOleh
	stored.DiprosesOlehUserID = fuel.DiprosesOlehUserID
	stored.DiprosesPada = cloneTime(fuel.DiprosesPada)
	stored.UpdatedAt = fuel.UpdatedAt
	return nil
}

func (r *TestRepository) ReadFuelMasukPhoto(_ context.Context, rowNumber, photoIndex int) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	stored, err := r.fuelMasukAtRow(rowNumber)
	if err != nil {
		return "", err
	}
	switch photoIndex {
	case 0:
		return stored.FotoTruckDepan, nil
	case 1:
		return stored.FotoTangkiSebelum, nil
	case 2:
		return stored.FotoFlowmeter, nil
	case 3:
		return stored.FotoTangkiSetelah, nil
	default:
		return "", fmt.Errorf("invalid fuel photo index %d", photoIndex)
	}
}

func (r *TestRepository) fuelMasukAtRow(rowNumber int) (*model.FuelMasuk, error) {
	index := rowNumber - 2
	if index < 0 || index >= len(r.fuelMasuk) {
		return nil, ErrNotFound
	}
	return r.fuelMasuk[index], nil
}

// FuelMasukList exposes the complete stored rows, photos included, to tests.
func (r *TestRepository) FuelMasukList() []model.FuelMasuk {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.FuelMasuk, 0, len(r.fuelMasuk))
	for _, fuel := range r.fuelMasuk {
		rows = append(rows, *cloneFuelMasuk(fuel))
	}
	return rows
}

func cloneFuelMasuk(fuel *model.FuelMasuk) *model.FuelMasuk {
	copy := *fuel
	copy.DiprosesPada = cloneTime(fuel.DiprosesPada)
	return &copy
}

func withoutFuelPhotos(fuel *model.FuelMasuk) *model.FuelMasuk {
	fuel.FotoTruckDepan = ""
	fuel.FotoTangkiSebelum = ""
	fuel.FotoFlowmeter = ""
	fuel.FotoTangkiSetelah = ""
	return fuel
}

func (r *TestRepository) MaxFuelKeluarSequence(_ context.Context, prefix string) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	highest := 0
	for _, fuel := range r.fuelKeluar {
		trimmed := strings.TrimSpace(fuel.FuelOutID)
		if !strings.HasPrefix(trimmed, prefix) {
			continue
		}
		sequence, err := strconv.Atoi(strings.TrimPrefix(trimmed, prefix))
		if err == nil && sequence > highest {
			highest = sequence
		}
	}
	return highest, nil
}

func (r *TestRepository) CreateFuelKeluar(_ context.Context, fuel *model.FuelKeluar) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fuelKeluar = append(r.fuelKeluar, cloneFuelKeluar(fuel))
	return nil
}

// ListFuelKeluar drops the photo, as the Sheets listing does.
func (r *TestRepository) ListFuelKeluar(context.Context) ([]model.FuelKeluar, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.FuelKeluar, 0, len(r.fuelKeluar))
	for _, fuel := range r.fuelKeluar {
		stored := cloneFuelKeluar(fuel)
		stored.FotoAkhirFlowMeter = ""
		rows = append(rows, *stored)
	}
	return rows, nil
}

func (r *TestRepository) FindFuelKeluarRow(_ context.Context, fuelOutID string) (*model.FuelKeluar, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	wanted := strings.ToUpper(strings.TrimSpace(fuelOutID))
	for index, fuel := range r.fuelKeluar {
		if strings.ToUpper(strings.TrimSpace(fuel.FuelOutID)) != wanted {
			continue
		}
		stored := cloneFuelKeluar(fuel)
		stored.FotoAkhirFlowMeter = ""
		return stored, index + 2, nil
	}
	return nil, 0, ErrNotFound
}

func (r *TestRepository) ReadFuelKeluarPhoto(_ context.Context, rowNumber int) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.fuelKeluar) {
		return "", ErrNotFound
	}
	return r.fuelKeluar[index].FotoAkhirFlowMeter, nil
}

// FuelKeluarList exposes the complete stored rows, photo included, to tests.
func (r *TestRepository) FuelKeluarList() []model.FuelKeluar {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]model.FuelKeluar, 0, len(r.fuelKeluar))
	for _, fuel := range r.fuelKeluar {
		rows = append(rows, *cloneFuelKeluar(fuel))
	}
	return rows
}

func cloneFuelKeluar(fuel *model.FuelKeluar) *model.FuelKeluar {
	copy := *fuel
	copy.HMAlatBerat = cloneFloat(fuel.HMAlatBerat)
	return &copy
}

func (r *TestRepository) Activities() []*model.LoginActivity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]*model.LoginActivity, 0, len(r.activities))
	for _, activity := range r.activities {
		result = append(result, cloneActivity(activity))
	}
	return result
}

func cloneUser(user *model.User) *model.User {
	copy := *user
	if user.LastLoginAt != nil {
		copy.LastLoginAt = timePtr(*user.LastLoginAt)
	}
	return &copy
}

// readUser is cloneUser for the lookup paths, which on the sheet stop one
// column short of the photo. Only ReadUserPhoto returns the image, and a test
// that found it elsewhere would be passing on data production never returns.
// The write paths keep using cloneUser: stripping there would discard the photo
// on the way in.
func readUser(user *model.User) *model.User {
	stored := cloneUser(user)
	stored.FotoProfil = ""
	return stored
}

func cloneActivity(activity *model.LoginActivity) *model.LoginActivity {
	copy := *activity
	return &copy
}

func cloneAttendance(attendance *model.Attendance) *model.Attendance {
	copy := *attendance
	copy.ClockInAccuracy = cloneFloat(attendance.ClockInAccuracy)
	copy.ClockOutAt = cloneTime(attendance.ClockOutAt)
	copy.ClockOutLat = cloneFloat(attendance.ClockOutLat)
	copy.ClockOutLng = cloneFloat(attendance.ClockOutLng)
	copy.ClockOutAccuracy = cloneFloat(attendance.ClockOutAccuracy)
	copy.DurasiMenit = cloneInt(attendance.DurasiMenit)
	return &copy
}

func cloneLeave(leave *model.Leave) *model.Leave {
	copy := *leave
	copy.DiprosesPada = cloneTime(leave.DiprosesPada)
	copy.DibatalkanPada = cloneTime(leave.DibatalkanPada)
	return &copy
}

func timePtr(value time.Time) *time.Time { return &value }

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	return timePtr(*value)
}

func cloneFloat(value *float64) *float64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
