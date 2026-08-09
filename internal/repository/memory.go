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
	unitA2B    []*model.UnitA2B
	nota       []*model.Nota
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
	return r.NotaList(), nil
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
			return cloneUser(user), nil
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
			return cloneUser(user), nil
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
