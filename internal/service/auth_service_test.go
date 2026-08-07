package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

func TestRegisterAndAuthenticate(t *testing.T) {
	store := repository.NewMemoryRepository()
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.FixedZone("WIB", 7*60*60))
	auth := NewAuthService(store, now.Location(), func() time.Time { return now })

	user, err := auth.Register(context.Background(), RegisterInput{
		TanggalGabung: "2026-08-07",
		NamaLengkap:   "Budi Santoso",
		NRP:           "123456",
		Jabatan:       "Staff Operasional",
		Email:         "BUDI@example.com",
		Password:      "rahasia123",
		Status:        model.StatusAktif,
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.PasswordHash == "rahasia123" || !strings.HasPrefix(user.PasswordHash, "$2") {
		t.Fatalf("password was not bcrypt hashed: %q", user.PasswordHash)
	}

	loggedIn, err := auth.Authenticate(context.Background(), "123456", "rahasia123", ActivityMeta{IPAddress: "127.0.0.1"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if loggedIn.UserID != user.UserID || loggedIn.LastLoginAt == nil {
		t.Fatalf("unexpected authenticated user: %+v", loggedIn)
	}
	activities := store.Activities()
	if len(activities) != 1 || activities[0].ActivityType != model.ActivityLogin || activities[0].Status != model.ActivitySuccess {
		t.Fatalf("unexpected login activity: %+v", activities)
	}
}

func TestRegisterRejectsDuplicateAndInvalidInput(t *testing.T) {
	store := repository.NewMemoryRepository()
	auth := NewAuthService(store, time.Local, time.Now)
	input := RegisterInput{
		TanggalGabung: "2026-08-07",
		NamaLengkap:   "Budi Santoso",
		NRP:           "123456",
		Jabatan:       "Staff",
		Email:         "budi@example.com",
		Password:      "rahasia123",
	}
	if _, err := auth.Register(context.Background(), input); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if _, err := auth.Register(context.Background(), input); !errors.Is(err, ErrDuplicateUser) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
	input.Password = "short"
	if _, err := auth.Register(context.Background(), input); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
}

func TestInactiveAndWrongPasswordAreLoggedAsFailed(t *testing.T) {
	store := repository.NewMemoryRepository()
	now := time.Date(2026, 8, 7, 8, 0, 0, 0, time.Local)
	auth := NewAuthService(store, time.Local, func() time.Time { return now })
	_, err := auth.Register(context.Background(), RegisterInput{
		TanggalGabung: "2026-08-07",
		NamaLengkap:   "Inactive User",
		NRP:           "999999",
		Jabatan:       "Staff",
		Email:         "inactive@example.com",
		Password:      "rahasia123",
		Status:        model.StatusTidakAktif,
	})
	if err != nil {
		t.Fatalf("register inactive user: %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), "999999", "rahasia123", ActivityMeta{}); !errors.Is(err, ErrInactiveUser) {
		t.Fatalf("expected inactive error, got %v", err)
	}
	if _, err := auth.Authenticate(context.Background(), "999999", "wrongpass", ActivityMeta{}); !errors.Is(err, ErrInactiveUser) {
		t.Fatalf("expected inactive error on second attempt, got %v", err)
	}
	activities := store.Activities()
	if len(activities) != 2 {
		t.Fatalf("expected two failed activities, got %d", len(activities))
	}
	for _, activity := range activities {
		if activity.Status != model.ActivityFailed || activity.ActivityType != model.ActivityLogin {
			t.Fatalf("unexpected failed activity: %+v", activity)
		}
	}
}
