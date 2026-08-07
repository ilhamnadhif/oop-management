package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

func TestClockInOutAndDuplicateRules(t *testing.T) {
	store := repository.NewMemoryRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 8, 1, 0, 0, location)
	user := &model.User{UserID: "usr_1", NRP: "123456", NamaLengkap: "Budi", Jabatan: "Staff", StatusPengguna: model.StatusAktif}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	photoValue := testPhoto(t)
	attendance := NewAttendanceService(store, location, func() time.Time { return now })

	created, err := attendance.ClockIn(context.Background(), user, AttendanceInput{Latitude: -6.2, Longitude: 106.8, Photo: photoValue})
	if err != nil {
		t.Fatalf("clock in: %v", err)
	}
	if created.StatusAbsensi != model.AttendanceBelumClockOut {
		t.Fatalf("unexpected clock in status: %s", created.StatusAbsensi)
	}
	if _, err := attendance.ClockIn(context.Background(), user, AttendanceInput{Latitude: -6.2, Longitude: 106.8, Photo: photoValue}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected duplicate clock in conflict, got %v", err)
	}

	now = time.Date(2026, 8, 7, 17, 5, 0, 0, location)
	completed, err := attendance.ClockOut(context.Background(), user, AttendanceInput{Latitude: -6.21, Longitude: 106.81, Accuracy: floatPtr(10), Photo: photoValue})
	if err != nil {
		t.Fatalf("clock out: %v", err)
	}
	if completed.StatusAbsensi != model.AttendanceSelesai || completed.DurasiMenit == nil || *completed.DurasiMenit != 544 {
		t.Fatalf("unexpected completed attendance: %+v", completed)
	}
	if _, err := attendance.ClockOut(context.Background(), user, AttendanceInput{Latitude: -6.2, Longitude: 106.8, Photo: photoValue}); !errors.Is(err, ErrAlreadyClockedOut) {
		t.Fatalf("expected duplicate clock out conflict, got %v", err)
	}
}

func TestClockOutRequiresClockInAndValidLocation(t *testing.T) {
	store := repository.NewMemoryRepository()
	location := time.FixedZone("WIB", 7*60*60)
	now := time.Date(2026, 8, 7, 17, 0, 0, 0, location)
	user := &model.User{UserID: "usr_2", NRP: "654321", NamaLengkap: "Sari", Jabatan: "Staff", StatusPengguna: model.StatusAktif}
	if err := store.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	attendance := NewAttendanceService(store, location, func() time.Time { return now })
	photoValue := testPhoto(t)
	if _, err := attendance.ClockOut(context.Background(), user, AttendanceInput{Latitude: 91, Longitude: 0, Photo: photoValue}); !errors.Is(err, ErrInvalidLocation) {
		t.Fatalf("expected invalid location, got %v", err)
	}
	if _, err := attendance.ClockOut(context.Background(), user, AttendanceInput{Latitude: 0, Longitude: 0, Photo: photoValue}); !errors.Is(err, ErrNoClockIn) {
		t.Fatalf("expected no clock in, got %v", err)
	}
}

func testPhoto(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 120, A: 255})
		}
	}
	var source bytes.Buffer
	if err := jpeg.Encode(&source, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode test photo: %v", err)
	}
	value, err := photo.Normalize(source.Bytes(), photo.MaxOutputChars)
	if err != nil {
		t.Fatalf("normalize test photo: %v", err)
	}
	return value
}
