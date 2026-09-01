package model

import (
	"strings"
	"time"
)

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

	// Project is the one project this account works in. Management is the
	// exception and carries ProjectSemua, because the position is defined by
	// reaching everything rather than by belonging somewhere.
	//
	// An account written before projects existed has this empty. It is read as
	// the first project rather than as "none": the alternative locks out
	// everybody who already had an account.
	Project string
}

// ProjectSemua is what a user row carries instead of a project name when the
// account reaches every project.
const ProjectSemua = "*"

// JabatanManagement is the position defined by what it may reach rather than by
// what it does. It reaches every project whatever its row says, so an account
// created before the column existed - or one somebody assigned to a single site
// by mistake - still gets there.
const JabatanManagement = "Management"

// ReachesEveryProject reports whether this account is not tied to one project.
func (u User) ReachesEveryProject() bool {
	if strings.TrimSpace(u.Project) == ProjectSemua {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(u.Jabatan), JabatanManagement)
}

// JabatanAccess is one position's menu rights as HR configures them. MenuAktif
// lists the top-level menus the position may open, replacing the built-in
// menuAccess rule for that position when a row exists.
type JabatanAccess struct {
	Jabatan   string
	MenuAktif []string
}

// Project is one site the app is keeping books for. Each has a spreadsheet of
// its own, so no filter stands between one project's rows and another's: they
// are never in the same file to begin with.
//
// SpreadsheetID may name the same spreadsheet the users live in. That is how
// the first project carries on unchanged: the file it has always written to
// becomes both the master and its own project store.
type Project struct {
	ProjectID     string
	Nama          string
	SpreadsheetID string
	// MenuAktif lists the top-level menu keys this project has. Empty means
	// every menu, which is what an unconfigured project gets.
	MenuAktif []string
	Status    string
	Settings  ProjectSettings
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Aktif reports whether the project may be opened at all.
func (p Project) Aktif() bool {
	return strings.EqualFold(strings.TrimSpace(p.Status), StatusAktif)
}

// HasMenu reports whether a top-level menu is switched on here. A project that
// lists no menus has them all: a row added with the column left blank should
// open, not lock its own operators out.
func (p Project) HasMenu(key string) bool {
	if len(p.MenuAktif) == 0 {
		return true
	}
	for _, menu := range p.MenuAktif {
		if strings.EqualFold(strings.TrimSpace(menu), strings.TrimSpace(key)) {
			return true
		}
	}
	return false
}

// ProjectSettings are the figures that used to come from the environment, now
// answered per project. Every field may be empty or zero, and an empty field
// means "use the deployment default": a project that has never been configured
// must behave exactly as the app did before it had settings at all.
type ProjectSettings struct {
	// The working day attendance is judged against.
	WorkStart            string
	WorkEnd              string
	LateToleranceMinutes int
	// One shift's worth of minutes for an A2B machine, which hour meter
	// readings are measured against.
	A2BWorkMinutes int
	// What the letterhead and the signature block of every export say.
	Company        string
	SignatoryName  string
	SignatoryTitle string
	SignatoryPlace string
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

// ProduksiScan is one tally sheet read by the scanner. It is a log rather than
// a document: what it exists for is to recognise a photo that has already been
// turned into production rows, so the same file cannot be filed twice.
//
// Sidik is the SHA-256 of the decoded image bytes, not of the data URL, so
// rewrapping the same picture does not change its fingerprint. Foto is the
// sheet itself, kept for when a stored figure is disputed.
type ProduksiScan struct {
	ScanID       string
	Sidik        string
	BarisMasuk   int
	BarisDitolak int
	DibuatOleh   string
	DibuatOlehID string
	CreatedAt    time.Time
	Foto         string
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

// HourMeter is one shift's hour meter reading for one machine. The three HM
// figures are hours, which is what the meter on the machine counts; standby and
// breakdown are minutes, which is how the timesheet records them.
type HourMeter struct {
	HMID    string
	Tanggal string
	Shift   string
	// IDUnit and NamaUnit are a snapshot of the A2B register at the time of
	// reading, so a machine later renamed does not rewrite history.
	IDUnit    string
	NamaUnit  string
	Operator  string
	HMAwal    float64
	HMAkhir   float64
	TotalHM   float64
	FuelLiter float64

	// Standby is the shift broken down into the reasons the machine was not
	// working, holding only the reasons actually given. Each has a column of its
	// own on the sheet, left empty when it did not happen, and TotalStandby is
	// their sum.
	TotalStandby float64
	Standby      []HourMeterStandby

	// Breakdown is the same shape for time the machine was not merely idle but
	// unable to work.
	TotalBreakdown float64
	Breakdown      []HourMeterBreakdown

	// The shift's three figures, stored beside the reading so a reader of the
	// sheet does not have to recompute them. PA is how much of the shift the
	// machine was fit to work, BDPersen the share lost to breakdown, and UA how
	// much of the time it was fit for it actually worked.
	PA       float64
	BDPersen float64
	UA       float64

	// Remark is whatever the shift needs saying about it that the figures do
	// not carry.
	Remark string

	CreatedBy   string
	CreatedByID string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// StandbyVariable is one reason a machine stood still. The code is the site's
// own, and the two travel together: the code is what the paper timesheet is
// filed under, the name is what an operator recognises.
type StandbyVariable struct {
	Kode string
	Nama string
}

// StandbyVariables is the closed set, in the order the timesheet lists them.
// The spellings are the site's own and are stored as written. Each one has a
// column of its own on the sheet, which is why a reason may be given only once
// per reading.
var StandbyVariables = []StandbyVariable{
	{Kode: "D01", Nama: "P2H"},
	{Kode: "D02", Nama: "ISI BBM"},
	{Kode: "D03", Nama: "PEMERIKSAAN UNIT"},
	{Kode: "D04", Nama: "TRAVELING"},
	{Kode: "D05", Nama: "TUNGGU ALAT"},
	{Kode: "D06", Nama: "TUNGGU PENGUKURAN SURVEY"},
	{Kode: "D07", Nama: "TUNGGU BLASTING"},
	{Kode: "D08", Nama: "CUCI UNIT"},
	{Kode: "D09", Nama: "ISTIRAHAT"},
	{Kode: "D10", Nama: "TUNGGU PEMERIKSAAN SAFTY"},
	{Kode: "D11", Nama: "STANDBY PERMINTAAN"},
	{Kode: "D12", Nama: "TUNGGU OPERATOR"},
	{Kode: "D13", Nama: "CHANGE SHIFT"},
	{Kode: "D14", Nama: "DEBU"},
	{Kode: "D15", Nama: "SHOLAT"},
	{Kode: "D16", Nama: "PIT STOP"},
	{Kode: "D17", Nama: "TIDAK ADA THIMESHET"},
	{Kode: "I15", Nama: "HUJAN"},
	{Kode: "I16", Nama: "FORCE MAJEURE"},
	{Kode: "I17", Nama: "LICIN"},
	{Kode: "I18", Nama: "DEMO"},
	{Kode: "I19", Nama: "COSTUMER PROBLEM"},
	{Kode: "I20", Nama: "KABUT"},
}

// StandbyColumn is the sheet header for one reason, "d01_p2h" and so on. It is
// derived rather than written out so a reason cannot end up with a column whose
// name disagrees with its code.
func (v StandbyVariable) StandbyColumn() string {
	name := strings.ToLower(strings.Join(strings.Fields(v.Nama), "_"))
	return strings.ToLower(v.Kode) + "_" + name
}

// BreakdownVariables is the closed set of breakdown reasons. Unlike standby
// these carry no timesheet code, so the name is the whole identifier. Each has
// a column of its own on the sheet for the same reason standby does.
var BreakdownVariables = []string{"SCM", "USM", "TRM", "ICM", "NO OPR"}

// BreakdownColumn is the sheet header for one breakdown reason, "bd_scm" and so
// on. The prefix keeps them apart from every other column in the row.
func BreakdownColumn(variable string) string {
	return "bd_" + strings.ToLower(strings.Join(strings.Fields(variable), "_"))
}

// HourMeterStandby is one standby reason and the minutes spent on it.
type HourMeterStandby struct {
	Variable string
	Menit    float64
}

// HourMeterBreakdown is one breakdown reason and the minutes lost to it.
type HourMeterBreakdown struct {
	Variable string
	Menit    float64
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
