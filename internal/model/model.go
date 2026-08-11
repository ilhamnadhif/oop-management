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

	// Personal details the employee maintains. Both may be empty: they were
	// added after people already had accounts, and neither is needed to log in.
	NoTelp       string
	TanggalLahir string

	// PunyaFoto says whether a profile photo exists without carrying it.
	// FotoProfil is a base64 data URL tens of thousands of characters long, so
	// it is read one row at a time and never as part of a listing.
	PunyaFoto  bool
	FotoProfil string
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

// ProduksiPlan is the volume planned for one location. It is a standing target
// rather than a daily one: the date records when the plan was set, and the
// overview compares production in a chosen range against the whole plan.
type ProduksiPlan struct {
	PlanID      string
	Tanggal     string
	Project     string
	Supplier    string
	Lokasi      string
	Volume      float64
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

// A delivery is either the quantity written on the delivery note or it is not.
// The two words are stored as the vendor writes them on the sheet, in lower
// case, so the column reads the same whether a person or this app filled it in.
const (
	FuelKeteranganSesuai      = "sesuai"
	FuelKeteranganTidakSesuai = "tidak sesuai"

	FuelStatusMenunggu  = "MENUNGGU"
	FuelStatusDisetujui = "DISETUJUI"
	FuelStatusDitolak   = "DITOLAK"
)

// FuelMasuk is one fuel delivery from a vendor into the site tank. The four
// photos are the whole point of recording it: a delivery note says how many
// litres were sent, and only the flowmeter and the tank either side of the
// discharge say how many arrived. They are base64 images tens of thousands of
// characters long, so no listing carries them.
type FuelMasuk struct {
	FuelID string
	// TanggalInput is when the delivery was recorded. It is entered rather than
	// stamped, because a delivery that arrives after hours is written up later.
	TanggalInput     time.Time
	Vendor           string
	Driver           string
	Nopol            string
	JumlahLiter      float64
	Keterangan       string
	LiterTidakSesuai float64
	StatusApproval   string

	FotoTruckDepan    string
	FotoTangkiSebelum string
	FotoFlowmeter     string
	FotoTangkiSetelah string

	// Filled in when the delivery is approved or rejected, after the row exists.
	CatatanApproval    string
	DiprosesOleh       string
	DiprosesOlehUserID string
	DiprosesPada       *time.Time

	CreatedBy   string
	CreatedByID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// FuelKeluar is one dispense of fuel from the site tank into a machine. The
// flowmeter on the pump is a running total, so the litres are the distance
// between the two readings rather than a figure anyone types.
type FuelKeluar struct {
	FuelOutID string
	Tanggal   string
	// IDUnit and NamaUnit are a snapshot of the A2B register at the time of
	// dispensing: a machine later renamed must not silently rewrite history.
	IDUnit           string
	NamaUnit         string
	HMAwalFlowMeter  float64
	HMAkhirFlowMeter float64
	Liter            float64
	// HMAlatBerat is the machine's own hour meter when it was filled. It is
	// optional, and nil is a reading nobody took rather than a reading of zero.
	HMAlatBerat        *float64
	Operator           string
	FotoAkhirFlowMeter string

	CreatedBy   string
	CreatedByID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
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
