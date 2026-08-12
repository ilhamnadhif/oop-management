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
	FindUserRow(ctx context.Context, userID string) (*model.User, int, error)
	UpdateUserProfile(ctx context.Context, rowNumber int, user *model.User, updatePhoto bool) error
	ReadUserPhoto(ctx context.Context, rowNumber int) (string, error)
	AppendActivity(ctx context.Context, activity *model.LoginActivity) error
	UnitDTExists(ctx context.Context, nopol string) (bool, error)
	MaxUnitDTSequence(ctx context.Context, prefix string) (int, error)
	CreateUnitDT(ctx context.Context, unit *model.UnitDT) error
	ListUnitDT(ctx context.Context) ([]model.UnitDT, error)
	MaxProduksiSequence(ctx context.Context, prefix string) (int, error)
	CreateProduksi(ctx context.Context, produksi *model.Produksi) error
	CreateProduksiBatch(ctx context.Context, rows []*model.Produksi) error
	ListProduksi(ctx context.Context) ([]model.Produksi, error)
	MaxProduksiPlanSequence(ctx context.Context, prefix string) (int, error)
	CreateProduksiPlan(ctx context.Context, plan *model.ProduksiPlan) error
	ListProduksiPlan(ctx context.Context) ([]model.ProduksiPlan, error)
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
	MaxFuelMasukSequence(ctx context.Context, prefix string) (int, error)
	CreateFuelMasuk(ctx context.Context, fuel *model.FuelMasuk) error
	// ListFuelMasuk reads every column except the four photos. Those sit in the
	// middle of the row because the sheet's column order was fixed before this
	// app existed, so the listing is stitched from the ranges either side of
	// them rather than dragging four base64 images per row into every page.
	ListFuelMasuk(ctx context.Context) ([]model.FuelMasuk, error)
	FindFuelMasukRow(ctx context.Context, fuelID string) (*model.FuelMasuk, int, error)
	// ReadFuelMasukPhoto fetches one photo. photoIndex is 0-3 in the order the
	// sheet holds them: truck, tank before, flowmeter, tank after.
	ReadFuelMasukPhoto(ctx context.Context, rowNumber, photoIndex int) (string, error)
	MaxFuelKeluarSequence(ctx context.Context, prefix string) (int, error)
	CreateFuelKeluar(ctx context.Context, fuel *model.FuelKeluar) error
	// ListFuelKeluar reads every column except the flowmeter photo, which is a
	// base64 image sitting between the delivery columns and the audit trail.
	ListFuelKeluar(ctx context.Context) ([]model.FuelKeluar, error)
	FindFuelKeluarRow(ctx context.Context, fuelOutID string) (*model.FuelKeluar, int, error)
	ReadFuelKeluarPhoto(ctx context.Context, rowNumber int) (string, error)
	MaxHourMeterSequence(ctx context.Context, prefix string) (int, error)
	CreateHourMeter(ctx context.Context, reading *model.HourMeter) error
	ListHourMeter(ctx context.Context) ([]model.HourMeter, error)
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
