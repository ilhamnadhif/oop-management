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
	ListUsers(ctx context.Context) ([]model.User, error)
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
	CreateProduksiBatch(ctx context.Context, rows []*model.Produksi) error
	ListProduksi(ctx context.Context) ([]model.Produksi, error)
	UnitA2BExists(ctx context.Context, idUnit string) (bool, error)
	MaxUnitA2BNumber(ctx context.Context) (int, error)
	CreateUnitA2B(ctx context.Context, unit *model.UnitA2B) error
	ListUnitA2B(ctx context.Context) ([]model.UnitA2B, error)
	MaxNotaSequence(ctx context.Context, prefix string) (int, error)
	CreateNota(ctx context.Context, nota *model.Nota) error
	ListNota(ctx context.Context) ([]model.Nota, error)
	// ListNotaWithAttachments is the same list with the receipt photo read as
	// well. It is a separate call because that column is a base64 image tens of
	// thousands of characters long: every screen that only lists notes pays for
	// it otherwise, and only the export actually prints it.
	ListNotaWithAttachments(ctx context.Context) ([]model.Nota, error)
	ListNotaItems(ctx context.Context) ([]model.NotaItem, error)
	FindNotaRow(ctx context.Context, notaID string) (*model.Nota, int, error)
	SettleNota(ctx context.Context, rowNumber int, nota *model.Nota) error
	FindAttendanceByUserDate(ctx context.Context, userID, date string) (*model.Attendance, int, error)
	ListAttendanceByUser(ctx context.Context, userID string) ([]model.Attendance, error)
	ListAttendanceBetween(ctx context.Context, from, to string) ([]model.Attendance, error)
	CreateAttendance(ctx context.Context, attendance *model.Attendance) error
	UpdateAttendance(ctx context.Context, rowNumber int, attendance *model.Attendance) error
	MaxLeaveSequence(ctx context.Context, prefix string) (int, error)
	CreateLeave(ctx context.Context, leave *model.Leave) error
	ListLeave(ctx context.Context) ([]model.Leave, error)
	FindLeaveRow(ctx context.Context, leaveID string) (*model.Leave, int, error)
	UpdateLeaveRequest(ctx context.Context, rowNumber int, leave *model.Leave, updateAttachment bool) error
	CancelLeave(ctx context.Context, rowNumber int, leave *model.Leave) error
	UpdateLeaveDecision(ctx context.Context, rowNumber int, leave *model.Leave) error
	ReadLeaveAttachment(ctx context.Context, rowNumber int) (string, error)
}
