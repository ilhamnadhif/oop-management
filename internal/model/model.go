package model

import "time"

const (
	StatusAktif      = "AKTIF"
	StatusTidakAktif = "TIDAK_AKTIF"

	ActivityLogin   = "LOGIN"
	ActivityLogout  = "LOGOUT"
	ActivitySuccess = "SUCCESS"
	ActivityFailed  = "FAILED"

	AttendanceBelumClockOut = "BELUM_CLOCK_OUT"
	AttendanceSelesai       = "SELESAI"
)

type User struct {
	UserID         string
	TanggalGabung  string
	NamaLengkap    string
	NRP            string
	Jabatan        string
	Email          string
	PasswordHash   string
	StatusPengguna string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastLoginAt    *time.Time
}

type LoginActivity struct {
	ActivityID   string
	UserID       string
	NRP          string
	Email        string
	ActivityType string
	ActivityTime time.Time
	Status       string
	IPAddress    string
	UserAgent    string
	Message      string
}

// UnitDT is one dump truck in the fleet register. Panjang, Lebar and Tinggi
// are metres.
type UnitDT struct {
	UnitID      string
	Nopol       string
	Panjang     float64
	Lebar       float64
	Tinggi      float64
	Driver      string
	Keterangan  string
	Foto        string
	CreatedBy   string
	CreatedByID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Attendance struct {
	AbsensiID        string
	UserID           string
	NRP              string
	NamaLengkap      string
	Jabatan          string
	TanggalAbsensi   string
	ClockInAt        time.Time
	ClockInLat       float64
	ClockInLng       float64
	ClockInAccuracy  *float64
	ClockInPhoto     string
	ClockInIP        string
	ClockOutAt       *time.Time
	ClockOutLat      *float64
	ClockOutLng      *float64
	ClockOutAccuracy *float64
	ClockOutPhoto    string
	ClockOutIP       string
	StatusAbsensi    string
	DurasiMenit      *int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
