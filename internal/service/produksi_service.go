package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// Seed options. The form offers whatever the Produksi sheet already contains
// plus these, so a fresh sheet still has something to pick from - Kategori in
// particular is empty across every imported row.
var (
	ProjectOptions  = []string{"PCPM"}
	SupplierOptions = []string{"HPP"}
	QuaryOptions    = []string{"HS"}
	KategoriOptions = []string{"Replace", "Timbunan", "Akses"}
	LayerOptions    = []string{"L1", "L2", "L3", "L4", "L5"}
)

// ProduksiOptions is what each picker offers. The fields are suggestions, not
// a closed set: an operator may type a value that is not listed yet.
type ProduksiOptions struct {
	Project  []string
	Supplier []string
	Quary    []string
	Kategori []string
	Layer    []string
	Lokasi   []string
}

// optionsCacheTTL keeps the form from re-reading thousands of rows on every
// render. New values only appear as fast as they are typed, so a minute of
// staleness costs nothing.
const optionsCacheTTL = time.Minute

// Options lists the distinct values already used in the sheet, merged with the
// seed lists above.
func (s *ProduksiService) Options(ctx context.Context) (ProduksiOptions, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.options, nil
	}

	rows, err := s.store.ListProduksi(ctx)
	if err != nil {
		return ProduksiOptions{}, fmt.Errorf("read produksi options: %w", err)
	}
	options := ProduksiOptions{
		Project:  distinctValues(ProjectOptions, rows, func(r model.Produksi) string { return r.Project }),
		Supplier: distinctValues(SupplierOptions, rows, func(r model.Produksi) string { return r.Supplier }),
		Quary:    distinctValues(QuaryOptions, rows, func(r model.Produksi) string { return r.Quary }),
		Kategori: distinctValues(KategoriOptions, rows, func(r model.Produksi) string { return r.Kategori }),
		Layer:    distinctValues(LayerOptions, rows, func(r model.Produksi) string { return r.Layer }),
		Lokasi:   distinctValues(nil, rows, func(r model.Produksi) string { return r.Lokasi }),
	}
	s.options = options
	s.optionsAt = s.now()
	return options, nil
}

// distinctValues merges the seeds with whatever the sheet holds, keeping the
// first spelling seen for any value that differs only in case.
func distinctValues[T any](seed []string, rows []T, pick func(T) string) []string {
	seen := make(map[string]string)
	var values []string
	add := func(value string) {
		value = strings.Join(strings.Fields(value), " ")
		if value == "" {
			return
		}
		key := strings.ToUpper(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = value
		values = append(values, value)
	}
	for _, value := range seed {
		add(value)
	}
	for _, row := range rows {
		add(pick(row))
	}
	sort.Strings(values)
	return values
}

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

	optionsMu sync.Mutex
	options   ProduksiOptions
	optionsAt time.Time
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

// RowsBetween returns the production rows inside an inclusive date range,
// oldest first. Either bound may be empty, meaning no bound on that side.
func (s *ProduksiService) RowsBetween(ctx context.Context, from, to string) ([]model.Produksi, string, string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from != "" {
		if _, err := time.Parse("2006-01-02", from); err != nil {
			return nil, "", "", fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
		}
	}
	if to != "" {
		if _, err := time.Parse("2006-01-02", to); err != nil {
			return nil, "", "", fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
	}
	// A reversed range would quietly export nothing, which reads as "no data".
	if from != "" && to != "" && from > to {
		from, to = to, from
	}

	all, err := s.store.ListProduksi(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("read produksi: %w", err)
	}
	rows := make([]model.Produksi, 0, len(all))
	for _, row := range all {
		if from != "" && row.Tanggal < from {
			continue
		}
		if to != "" && row.Tanggal > to {
			continue
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tanggal != rows[j].Tanggal {
			return rows[i].Tanggal < rows[j].Tanggal
		}
		return rows[i].ProduksiID < rows[j].ProduksiID
	})
	return rows, from, to, nil
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
	// The pickers accept new values, so the job here is to require one and to
	// settle on a single spelling - not to reject anything unfamiliar.
	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	project, err := adoptOption("Project", input.Project, options.Project)
	if err != nil {
		return nil, err
	}
	supplier, err := adoptOption("Supplier", input.Supplier, options.Supplier)
	if err != nil {
		return nil, err
	}
	quary, err := adoptOption("Quary", input.Quary, options.Quary)
	if err != nil {
		return nil, err
	}
	kategori, err := adoptOption("Kategori", input.Kategori, options.Kategori)
	if err != nil {
		return nil, err
	}
	layer, err := adoptOption("Layer", input.Layer, options.Layer)
	if err != nil {
		return nil, err
	}
	lokasi, err := adoptOption("Lokasi", input.Lokasi, options.Lokasi)
	if err != nil {
		return nil, err
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
	// A value typed just now has to be offered - and adopted - by the very next
	// submission. Without this the cache hides it for a minute and the same
	// value gets stored twice under two spellings.
	s.invalidateOptions()
	return produksi, nil
}

func (s *ProduksiService) invalidateOptions() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
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

// adoptOption normalises whitespace and, when the value matches something the
// sheet already holds apart from case, adopts that spelling. Without it
// "replace" and "Replace" become two categories and every per-category report
// splits in half.
func adoptOption(label, value string, options []string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return "", fmt.Errorf("%w: %s wajib diisi", ErrValidation, label)
	}
	for _, option := range options {
		if strings.EqualFold(option, value) {
			return option, nil
		}
	}
	return value, nil
}

// round2 and round4 keep the stored figures at the precision the form displays,
// so the sheet never disagrees with what the operator saw.
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

func round4(value float64) float64 {
	return math.Round(value*10_000) / 10_000
}
