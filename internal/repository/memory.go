package repository

import (
	"context"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
)

// MemoryRepository is intended for automated tests and local UI smoke tests.
// Google Sheets remains the default production backend.
type MemoryRepository struct {
	mu         sync.RWMutex
	users      []*model.User
	activities []*model.LoginActivity
	attendance []*model.Attendance
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{}
}

func (r *MemoryRepository) EnsureSchema(context.Context) error { return nil }

func (r *MemoryRepository) FindUserByID(_ context.Context, userID string) (*model.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, user := range r.users {
		if user.UserID == userID {
			return cloneUser(user), nil
		}
	}
	return nil, ErrNotFound
}

func (r *MemoryRepository) FindUserByIdentifier(_ context.Context, identifier string) (*model.User, error) {
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

func (r *MemoryRepository) UserExists(_ context.Context, nrp, email string) (bool, error) {
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

func (r *MemoryRepository) CreateUser(_ context.Context, user *model.User) error {
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

func (r *MemoryRepository) UpdateLastLogin(_ context.Context, userID string, at time.Time) error {
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

func (r *MemoryRepository) AppendActivity(_ context.Context, activity *model.LoginActivity) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities = append(r.activities, cloneActivity(activity))
	return nil
}

func (r *MemoryRepository) FindAttendanceByUserDate(_ context.Context, userID, date string) (*model.Attendance, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for index, attendance := range r.attendance {
		if attendance.UserID == userID && attendance.TanggalAbsensi == date {
			return cloneAttendance(attendance), index + 2, nil
		}
	}
	return nil, 0, nil
}

func (r *MemoryRepository) CreateAttendance(_ context.Context, attendance *model.Attendance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.attendance = append(r.attendance, cloneAttendance(attendance))
	return nil
}

func (r *MemoryRepository) UpdateAttendance(_ context.Context, rowNumber int, attendance *model.Attendance) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := rowNumber - 2
	if index < 0 || index >= len(r.attendance) {
		return ErrNotFound
	}
	r.attendance[index] = cloneAttendance(attendance)
	return nil
}

func (r *MemoryRepository) Activities() []*model.LoginActivity {
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
