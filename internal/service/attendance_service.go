package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"opp-management/internal/id"
	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

type AttendanceService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex
}

type AttendanceInput struct {
	Latitude  float64
	Longitude float64
	Accuracy  *float64
	Photo     string
	IPAddress string
}

func NewAttendanceService(store repository.Store, location *time.Location, now NowFunc) *AttendanceService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &AttendanceService{store: store, location: location, now: now}
}

func (s *AttendanceService) Today(ctx context.Context, userID string) (*model.Attendance, error) {
	now := s.now().In(s.location)
	attendance, _, err := s.store.FindAttendanceByUserDate(ctx, userID, now.Format("2006-01-02"))
	return attendance, err
}

func (s *AttendanceService) ClockIn(ctx context.Context, user *model.User, input AttendanceInput) (*model.Attendance, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	if err := validateAttendanceInput(input); err != nil {
		return nil, err
	}
	if err := photo.ValidateDataURL(input.Photo); err != nil {
		return nil, ErrInvalidPhoto
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	date := now.Format("2006-01-02")
	existing, _, err := s.store.FindAttendanceByUserDate(ctx, user.UserID, date)
	if err != nil {
		return nil, fmt.Errorf("find today's attendance: %w", err)
	}
	if existing != nil {
		return nil, ErrConflict
	}

	attendanceID, err := id.New("abs")
	if err != nil {
		return nil, err
	}
	attendance := &model.Attendance{
		AbsensiID:       attendanceID,
		UserID:          user.UserID,
		NRP:             user.NRP,
		NamaLengkap:     user.NamaLengkap,
		Jabatan:         user.Jabatan,
		TanggalAbsensi:  date,
		ClockInAt:       now,
		ClockInLat:      input.Latitude,
		ClockInLng:      input.Longitude,
		ClockInAccuracy: input.Accuracy,
		ClockInPhoto:    input.Photo,
		ClockInIP:       input.IPAddress,
		StatusAbsensi:   model.AttendanceBelumClockOut,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.store.CreateAttendance(ctx, attendance); err != nil {
		return nil, fmt.Errorf("create clock in: %w", err)
	}
	return attendance, nil
}

func (s *AttendanceService) ClockOut(ctx context.Context, user *model.User, input AttendanceInput) (*model.Attendance, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}
	if err := validateAttendanceInput(input); err != nil {
		return nil, err
	}
	if err := photo.ValidateDataURL(input.Photo); err != nil {
		return nil, ErrInvalidPhoto
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	date := now.Format("2006-01-02")
	attendance, rowNumber, err := s.store.FindAttendanceByUserDate(ctx, user.UserID, date)
	if err != nil {
		return nil, fmt.Errorf("find today's attendance: %w", err)
	}
	if attendance == nil {
		return nil, ErrNoClockIn
	}
	if attendance.ClockOutAt != nil || attendance.StatusAbsensi == model.AttendanceSelesai {
		return nil, ErrAlreadyClockedOut
	}
	if now.Before(attendance.ClockInAt) {
		return nil, fmt.Errorf("%w: clock out lebih awal dari clock in", ErrValidation)
	}
	duration := int(now.Sub(attendance.ClockInAt).Minutes())
	attendance.ClockOutAt = timePtr(now)
	attendance.ClockOutLat = floatPtr(input.Latitude)
	attendance.ClockOutLng = floatPtr(input.Longitude)
	attendance.ClockOutAccuracy = input.Accuracy
	attendance.ClockOutPhoto = input.Photo
	attendance.ClockOutIP = input.IPAddress
	attendance.StatusAbsensi = model.AttendanceSelesai
	attendance.DurasiMenit = intPtr(duration)
	attendance.UpdatedAt = now
	if err := s.store.UpdateAttendance(ctx, rowNumber, attendance); err != nil {
		return nil, fmt.Errorf("update clock out: %w", err)
	}
	return attendance, nil
}

func validateUser(user *model.User) error {
	if user == nil {
		return fmt.Errorf("%w: user tidak ditemukan", ErrValidation)
	}
	if user.StatusPengguna != model.StatusAktif {
		return ErrInactiveUser
	}
	return nil
}

func validateAttendanceInput(input AttendanceInput) error {
	if math.IsNaN(input.Latitude) || math.IsInf(input.Latitude, 0) || input.Latitude < -90 || input.Latitude > 90 {
		return ErrInvalidLocation
	}
	if math.IsNaN(input.Longitude) || math.IsInf(input.Longitude, 0) || input.Longitude < -180 || input.Longitude > 180 {
		return ErrInvalidLocation
	}
	if input.Accuracy != nil && (math.IsNaN(*input.Accuracy) || math.IsInf(*input.Accuracy, 0) || *input.Accuracy < 0) {
		return ErrInvalidLocation
	}
	if strings.TrimSpace(input.Photo) == "" {
		return ErrInvalidPhoto
	}
	return nil
}

func timePtr(value time.Time) *time.Time { return &value }

func floatPtr(value float64) *float64 { return &value }

func intPtr(value int) *int { return &value }
