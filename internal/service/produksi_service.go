package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// Closed option lists. Each one is rendered by the form and enforced here, so a
// direct POST cannot introduce a value the reports do not expect.
var (
	ProjectOptions  = []string{"PCPM"}
	SupplierOptions = []string{"HPP"}
	QuaryOptions    = []string{"HS"}
	KategoriOptions = []string{"Replace", "Timbunan", "Akses"}
	LayerOptions    = []string{"L1", "L2", "L3", "L4", "L5"}
)

// volumeOPPByJenisDT is the nominal payload each truck class is credited with.
var volumeOPPByJenisDT = map[string]float64{
	"DT KECIL": 10,
	"DT BESAR": 28,
}

type ProduksiService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex
}

type ProduksiInput struct {
	Tanggal  string
	Project  string
	Supplier string
	Quary    string
	Kategori string
	Lokasi   string
	Layer    string
	Nopol    string
	TT       string
}

func NewProduksiService(store repository.Store, location *time.Location, now NowFunc) *ProduksiService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &ProduksiService{store: store, location: location, now: now}
}

func produksiIDPrefix(year int) string {
	return fmt.Sprintf("PRD-%04d-", year)
}

// Today is the date the form preselects.
func (s *ProduksiService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// Units backs the nopol picker on the form.
func (s *ProduksiService) Units(ctx context.Context) ([]model.UnitDT, error) {
	return s.store.ListUnitDT(ctx)
}

// volumeDivisor scales the raw P x L x TF product down to the cubic metres the
// OPP figures are expressed in. With P=375, L=190, TF=150 the product is
// 10,687,500 and the reported volume is 10.6875 m3.
const volumeDivisor = 1_000_000

// Calculate derives every figure the form previews. TF is passed through
// unconverted; only the volume is scaled.
func Calculate(panjang, lebar, tinggi, tt float64, jenisDT string) (tf, volume, volumeOPP, deviasi float64) {
	tf = tinggi + tt/2
	volume = panjang * lebar * tf / volumeDivisor
	volumeOPP = volumeOPPByJenisDT[strings.ToUpper(strings.TrimSpace(jenisDT))]
	return tf, volume, volumeOPP, volume - volumeOPP
}

func (s *ProduksiService) Create(ctx context.Context, user *model.User, input ProduksiInput) (*model.Produksi, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	tanggal := strings.TrimSpace(input.Tanggal)
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return nil, fmt.Errorf("%w: tanggal wajib valid", ErrValidation)
	}
	project, err := pickOption("Project", input.Project, ProjectOptions)
	if err != nil {
		return nil, err
	}
	supplier, err := pickOption("Supplier", input.Supplier, SupplierOptions)
	if err != nil {
		return nil, err
	}
	quary, err := pickOption("Quary", input.Quary, QuaryOptions)
	if err != nil {
		return nil, err
	}
	kategori, err := pickOption("Kategori", input.Kategori, KategoriOptions)
	if err != nil {
		return nil, err
	}
	layer, err := pickOption("Layer", input.Layer, LayerOptions)
	if err != nil {
		return nil, err
	}
	lokasi := strings.TrimSpace(input.Lokasi)
	if lokasi == "" {
		return nil, fmt.Errorf("%w: lokasi wajib diisi", ErrValidation)
	}
	// TT is the manual top-up height. Leaving it blank means a level load, so
	// an empty or zero value is legitimate.
	tt, err := parseOptionalDimension("TT", input.TT)
	if err != nil {
		return nil, err
	}

	nopol, err := NormalizeNopol(input.Nopol)
	if err != nil {
		return nil, err
	}
	// Dimensions come from the register, never from the request. A client that
	// posts its own P/L/T could otherwise invent any volume it liked.
	unit, err := s.findUnit(ctx, nopol)
	if err != nil {
		return nil, err
	}
	tf, volume, volumeOPP, deviasi := Calculate(unit.Panjang, unit.Lebar, unit.Tinggi, tt, unit.Keterangan)

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	highest, err := s.store.MaxProduksiSequence(ctx, produksiIDPrefix(now.Year()))
	if err != nil {
		return nil, fmt.Errorf("read last produksi id: %w", err)
	}

	produksi := &model.Produksi{
		ProduksiID:  fmt.Sprintf("%s%04d", produksiIDPrefix(now.Year()), highest+1),
		Tanggal:     tanggal,
		Project:     project,
		Supplier:    supplier,
		Quary:       quary,
		Kategori:    kategori,
		Lokasi:      lokasi,
		Layer:       layer,
		UnitID:      unit.UnitID,
		Nopol:       unit.Nopol,
		Driver:      unit.Driver,
		JenisDT:     unit.Keterangan,
		Panjang:     unit.Panjang,
		Lebar:       unit.Lebar,
		Tinggi:      unit.Tinggi,
		TT:          tt,
		TF:          round2(tf),
		Volume:      round4(volume),
		VolumeOPP:   volumeOPP,
		Deviasi:     round4(deviasi),
		CreatedBy:   user.NamaLengkap,
		CreatedByID: user.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateProduksi(ctx, produksi); err != nil {
		return nil, fmt.Errorf("create produksi: %w", err)
	}
	return produksi, nil
}

func (s *ProduksiService) findUnit(ctx context.Context, nopol string) (model.UnitDT, error) {
	units, err := s.store.ListUnitDT(ctx)
	if err != nil {
		return model.UnitDT{}, fmt.Errorf("read unit dt: %w", err)
	}
	for _, unit := range units {
		if strings.EqualFold(strings.TrimSpace(unit.Nopol), nopol) {
			return unit, nil
		}
	}
	return model.UnitDT{}, fmt.Errorf("%w: nopol %s belum terdaftar di Unit DT", ErrValidation, nopol)
}

func parseOptionalDimension(label, value string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	cleaned := strings.ReplaceAll(strings.TrimSpace(value), ",", ".")
	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%w: %s harus berupa angka", ErrValidation, label)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%w: %s tidak boleh minus", ErrValidation, label)
	}
	return parsed, nil
}

func pickOption(label, value string, options []string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	for _, option := range options {
		if strings.EqualFold(option, value) {
			return option, nil
		}
	}
	return "", fmt.Errorf("%w: %s wajib dipilih", ErrValidation, label)
}

// round2 and round4 keep the stored figures at the precision the form displays,
// so the sheet never disagrees with what the operator saw.
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func round4(value float64) float64 {
	return math.Round(value*10_000) / 10_000
}
