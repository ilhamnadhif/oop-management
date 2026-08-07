package repository

import (
	"context"
	"errors"
	"time"

	"opp-management/internal/model"
)

var ErrNotFound = errors.New("not found")

type Store interface {
	EnsureSchema(ctx context.Context) error
	FindUserByID(ctx context.Context, userID string) (*model.User, error)
	FindUserByIdentifier(ctx context.Context, identifier string) (*model.User, error)
	UserExists(ctx context.Context, nrp, email string) (bool, error)
	CreateUser(ctx context.Context, user *model.User) error
	UpdateLastLogin(ctx context.Context, userID string, at time.Time) error
	AppendActivity(ctx context.Context, activity *model.LoginActivity) error
	UnitDTExists(ctx context.Context, nopol string) (bool, error)
	MaxUnitDTSequence(ctx context.Context, prefix string) (int, error)
	CreateUnitDT(ctx context.Context, unit *model.UnitDT) error
	ListUnitDT(ctx context.Context) ([]model.UnitDT, error)
	MaxProduksiSequence(ctx context.Context, prefix string) (int, error)
	CreateProduksi(ctx context.Context, produksi *model.Produksi) error
	FindAttendanceByUserDate(ctx context.Context, userID, date string) (*model.Attendance, int, error)
	CreateAttendance(ctx context.Context, attendance *model.Attendance) error
	UpdateAttendance(ctx context.Context, rowNumber int, attendance *model.Attendance) error
}
