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

	LeaveStatusMenunggu   = "MENUNGGU"
	LeaveStatusDisetujui  = "DISETUJUI"
	LeaveStatusDitolak    = "DITOLAK"
	LeaveStatusDibatalkan = "DIBATALKAN"

	LeaveJenisCutiTahunan = "Cuti Tahunan"
	LeaveJenisCutiSakit   = "Cuti Sakit"
	LeaveJenisIzin        = "Izin"
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

// UnitA2B is one registered A2B unit. FuelStorage is litres, FRUnit is litres
// per hour, and HMAwal is the hour meter at registration.
type UnitA2B struct {
	NoUrut      int
	TanggalIn   string
	IDUnit      string
	NamaUnit    string
	MerekType   string
	FuelStorage float64
	FRUnit      float64
	Lokasi      string
	HMAwal      float64
	Foto        string
	CreatedBy   string
	CreatedByID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Produksi is one hauling record. Every dimension is metres and every volume
// is cubic metres.
type Produksi struct {
	ProduksiID  string
	Tanggal     string
	Project     string
	Supplier    string
	Quary       string
	Kategori    string
	Lokasi      string
	Layer       string
	UnitID      string
	Nopol       string
	Driver      string
	JenisDT     string
	Panjang     float64
	Lebar       float64
	Tinggi      float64
	TT          float64
	TF          float64
	Volume      float64
	VolumeOPP   float64
	Deviasi     float64
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

// Leave is one employee leave request. Requester fields are snapshots so the
// audit trail keeps the name, NRP and position that were in effect when the
// request was submitted. BuktiPendukung is kept out of list reads because it
// can contain a large base64 data URL.
type Leave struct {
	LeaveID            string
	UserID             string
	NRP                string
	NamaLengkap        string
	Jabatan            string
	JenisLeave         string
	TanggalMulai       string
	TanggalSelesai     string
	JumlahHari         int
	Alasan             string
	Status             string
	CatatanApproval    string
	DiprosesOleh       string
	DiprosesOlehUserID string
	DiprosesPada       *time.Time
	DibatalkanPada     *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
	HasBuktiPendukung  bool
	BuktiPendukung     string
}

// Payment methods and the payment status each one implies. A cash advance is
// money already handed out, so the nota records it as settled; a reimbursement
// is money the company still owes the person who paid.
const (
	NotaMetodeCA        = "CA"
	NotaMetodeReimburse = "REIMBURSE"

	NotaStatusSudahDibayar = "SUDAH DIBAYAR"
	NotaStatusBelumDibayar = "BELUM DIBAYAR"
)

// Nota is one expense note. Total is the sum of its items, stored alongside the
// header so a reader of the sheet does not have to add the detail rows up.
type Nota struct {
	NotaID            string
	Tanggal           string
	PIC               string
	MetodePembayaran  string
	StatusPembayaran  string
	PenerimaReimburse string
	Kategori          string
	SubKategori       string
	JenisPerjalanan   string
	Total             float64
	FotoKwitansi      string
	BuktiTransfer     string
	CreatedBy         string
	CreatedByID       string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	Items             []NotaItem

	// Set when finance settles a reimbursement: the proof of payment, when it
	// was paid and who recorded it. A status that changed with nobody's name
	// against it is not something an audit can follow up.
	BuktiBayar           string
	DibayarPada          *time.Time
	DirekonsiliasiOleh   string
	DirekonsiliasiOlehID string
}

// NotaItem is one line of a nota. It lives in its own sheet: keeping the lines
// beside the header would repeat the attachments, which are base64 images tens
// of thousands of characters long, once per line.
type NotaItem struct {
	NotaID     string
	Baris      int
	NamaProduk string
	Satuan     string
	Volume     float64
	Harga      float64
	Subtotal   float64
}
