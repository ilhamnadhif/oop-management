package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// A2BBaruService keeps the A2B Baru books: the machine register, the purchase
// orders the tank is filled from, and every dispense. It reads and writes the
// three A2B Baru sheets only; the older A2B module is not touched.
type A2BBaruService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
}

func NewA2BBaruService(store repository.Store, location *time.Location, now NowFunc) *A2BBaruService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &A2BBaruService{store: store, location: location, now: now}
}

func (s *A2BBaruService) Today() string {
	return s.now().In(s.location).Format("2006-01-02")
}

// Units lists the register by id.
func (s *A2BBaruService) Units(ctx context.Context) ([]model.A2BBaruUnit, error) {
	units, err := s.store.ListA2BBaruUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unit a2b baru: %w", err)
	}
	sort.SliceStable(units, func(i, j int) bool {
		return strings.ToLower(units[i].IDUnit) < strings.ToLower(units[j].IDUnit)
	})
	return units, nil
}

type A2BBaruUnitInput struct {
	IDUnit   string
	NamaUnit string
}

func (s *A2BBaruService) CreateUnit(ctx context.Context, user *model.User, input A2BBaruUnitInput) (*model.A2BBaruUnit, error) {
	id := strings.Join(strings.Fields(input.IDUnit), " ")
	name := strings.Join(strings.Fields(input.NamaUnit), " ")
	if id == "" {
		return nil, fmt.Errorf("%w: ID unit wajib diisi", ErrValidation)
	}
	if name == "" {
		return nil, fmt.Errorf("%w: nama unit wajib diisi", ErrValidation)
	}
	units, err := s.store.ListA2BBaruUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unit a2b baru: %w", err)
	}
	for _, unit := range units {
		if strings.EqualFold(unit.IDUnit, id) {
			return nil, fmt.Errorf("%w: ID unit %s sudah terdaftar", ErrValidation, unit.IDUnit)
		}
	}
	unit := model.A2BBaruUnit{IDUnit: id, NamaUnit: name, CreatedBy: userName(user), CreatedAt: s.now().In(s.location)}
	if err := s.store.AppendA2BBaruUnits(ctx, []model.A2BBaruUnit{unit}); err != nil {
		return nil, fmt.Errorf("create unit a2b baru: %w", err)
	}
	return &unit, nil
}

// FuelMasuk lists the orders oldest first, the way the workbook reads.
func (s *A2BBaruService) FuelMasuk(ctx context.Context) ([]model.A2BBaruFuelMasuk, error) {
	orders, err := s.store.ListA2BBaruFuelMasuk(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fuel masuk a2b baru: %w", err)
	}
	sort.SliceStable(orders, func(i, j int) bool { return orders[i].Tanggal < orders[j].Tanggal })
	return orders, nil
}

// NextPhase offers the phase after the highest "PO-n" given so far.
func (s *A2BBaruService) NextPhase(ctx context.Context) (string, error) {
	orders, err := s.store.ListA2BBaruFuelMasuk(ctx)
	if err != nil {
		return "", fmt.Errorf("list fuel masuk a2b baru: %w", err)
	}
	highest := 0
	for _, order := range orders {
		phase := strings.ToUpper(strings.TrimSpace(order.Phase))
		if number, err := strconv.Atoi(strings.TrimPrefix(phase, "PO-")); err == nil && number > highest {
			highest = number
		}
	}
	return fmt.Sprintf("PO-%d", highest+1), nil
}

type A2BBaruFuelMasukInput struct {
	Phase           string
	Tanggal         string
	NoPO            string
	POVolume        string
	POHargaSatuan   string
	SiteVolume      string
	SiteHargaSatuan string
}

func (s *A2BBaruService) CreateFuelMasuk(ctx context.Context, user *model.User, input A2BBaruFuelMasukInput) (*model.A2BBaruFuelMasuk, error) {
	order := model.A2BBaruFuelMasuk{
		Phase: strings.TrimSpace(input.Phase), NoPO: strings.TrimSpace(input.NoPO),
		CreatedBy: userName(user), CreatedAt: s.now().In(s.location),
	}
	if order.Phase == "" {
		return nil, fmt.Errorf("%w: phase wajib diisi", ErrValidation)
	}
	tanggal, err := s.parseDate(input.Tanggal)
	if err != nil {
		return nil, err
	}
	order.Tanggal = tanggal
	if order.NoPO == "" {
		return nil, fmt.Errorf("%w: No. PO wajib diisi", ErrValidation)
	}
	if order.POVolume, err = parsePositive("Volume PO", input.POVolume); err != nil {
		return nil, err
	}
	if order.POHargaSatuan, err = parsePositive("Harga satuan PO", input.POHargaSatuan); err != nil {
		return nil, err
	}
	// The order may be written before the truck arrives, so what reached the
	// site may be left for later as zero.
	if order.SiteVolume, err = optionalNonNegative("Volume diterima site", input.SiteVolume); err != nil {
		return nil, err
	}
	if order.SiteHargaSatuan, err = optionalNonNegative("Harga satuan diterima site", input.SiteHargaSatuan); err != nil {
		return nil, err
	}
	orders, err := s.store.ListA2BBaruFuelMasuk(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fuel masuk a2b baru: %w", err)
	}
	order.FuelInID = nextDailyID("FUELIN-", order.CreatedAt, len(orders), func(i int) string { return orders[i].FuelInID })
	if err := s.store.AppendA2BBaruFuelMasuk(ctx, []model.A2BBaruFuelMasuk{order}); err != nil {
		return nil, fmt.Errorf("create fuel masuk a2b baru: %w", err)
	}
	return &order, nil
}

// FuelKeluar lists the dispenses oldest first.
func (s *A2BBaruService) FuelKeluar(ctx context.Context) ([]model.A2BBaruFuelKeluar, error) {
	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	sort.SliceStable(fills, func(i, j int) bool { return fills[i].Tanggal < fills[j].Tanggal })
	return fills, nil
}

// NextFuelOutID is the number the next dispense saved today would get.
func (s *A2BBaruService) NextFuelOutID(ctx context.Context) (string, error) {
	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return "", fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	return nextDailyID("FUELOUT-", s.now().In(s.location), len(fills), func(i int) string { return fills[i].FuelOutID }), nil
}

// LastFlowMeter is the highest closing flow meter reading, which is where the
// next dispense starts. Zero when nothing has been read.
func (s *A2BBaruService) LastFlowMeter(ctx context.Context) (float64, error) {
	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return 0, fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	last := 0.0
	for _, fill := range fills {
		if fill.HMAkhirFM != nil && *fill.HMAkhirFM > last {
			last = *fill.HMAkhirFM
		}
	}
	return last, nil
}

// Operators lists the names given before, for the form to suggest.
func (s *A2BBaruService) Operators(ctx context.Context) ([]string, error) {
	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	seen := make(map[string]bool)
	var names []string
	for _, fill := range fills {
		name := strings.TrimSpace(fill.Operator)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	return names, nil
}

type A2BBaruFuelKeluarInput struct {
	Tanggal    string
	IDUnit     string
	HMAwalA2B  string
	HMAkhirA2B string
	HMAwalFM   string
	HMAkhirFM  string
	FuelLiter  string
	HMSaatIsi  string
	JamKerja   string
	Operator   string
}

func (s *A2BBaruService) CreateFuelKeluar(ctx context.Context, user *model.User, input A2BBaruFuelKeluarInput) (*model.A2BBaruFuelKeluar, error) {
	fill := model.A2BBaruFuelKeluar{
		Operator:  strings.Join(strings.Fields(input.Operator), " "),
		CreatedBy: userName(user), CreatedAt: s.now().In(s.location),
	}
	tanggal, err := s.parseDate(input.Tanggal)
	if err != nil {
		return nil, err
	}
	fill.Tanggal = tanggal

	id := strings.TrimSpace(input.IDUnit)
	if id == "" {
		return nil, fmt.Errorf("%w: ID unit wajib dipilih", ErrValidation)
	}
	units, err := s.store.ListA2BBaruUnits(ctx)
	if err != nil {
		return nil, fmt.Errorf("list unit a2b baru: %w", err)
	}
	for _, unit := range units {
		if strings.EqualFold(unit.IDUnit, id) {
			fill.IDUnit, fill.NamaUnit = unit.IDUnit, unit.NamaUnit
		}
	}
	if fill.IDUnit == "" {
		return nil, fmt.Errorf("%w: ID unit %s belum terdaftar", ErrValidation, id)
	}

	readings := []struct {
		label string
		value string
		into  **float64
	}{
		{"HM awal A2B", input.HMAwalA2B, &fill.HMAwalA2B},
		{"HM akhir A2B", input.HMAkhirA2B, &fill.HMAkhirA2B},
		{"HM awal flow meter", input.HMAwalFM, &fill.HMAwalFM},
		{"HM akhir flow meter", input.HMAkhirFM, &fill.HMAkhirFM},
		{"HM A2B saat isi", input.HMSaatIsi, &fill.HMSaatIsi},
	}
	for _, reading := range readings {
		if strings.TrimSpace(reading.value) == "" {
			continue
		}
		value, err := parseNumber(reading.label, reading.value)
		if err != nil {
			return nil, err
		}
		if value < 0 {
			return nil, fmt.Errorf("%w: %s tidak boleh negatif", ErrValidation, reading.label)
		}
		*reading.into = &value
	}
	if fill.HMAwalA2B != nil && fill.HMAkhirA2B != nil && *fill.HMAkhirA2B < *fill.HMAwalA2B {
		return nil, fmt.Errorf("%w: HM akhir A2B lebih kecil dari HM awal", ErrValidation)
	}
	if fill.HMAwalFM != nil && fill.HMAkhirFM != nil && *fill.HMAkhirFM < *fill.HMAwalFM {
		return nil, fmt.Errorf("%w: HM akhir flow meter lebih kecil dari HM awal", ErrValidation)
	}
	if fill.FuelLiter, err = parsePositive("Fuel (L)", input.FuelLiter); err != nil {
		return nil, err
	}
	if fill.JamKerja, err = optionalNonNegative("Jam kerja", input.JamKerja); err != nil {
		return nil, err
	}
	if fill.Operator == "" {
		return nil, fmt.Errorf("%w: operator wajib diisi", ErrValidation)
	}

	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return nil, fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	fill.FuelOutID = nextDailyID("FUELOUT-", fill.CreatedAt, len(fills), func(i int) string { return fills[i].FuelOutID })
	if err := s.store.AppendA2BBaruFuelKeluar(ctx, []model.A2BBaruFuelKeluar{fill}); err != nil {
		return nil, fmt.Errorf("create fuel keluar a2b baru: %w", err)
	}
	return &fill, nil
}

func (s *A2BBaruService) parseDate(value string) (string, error) {
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(value), s.location)
	if err != nil {
		return "", fmt.Errorf("%w: tanggal wajib valid", ErrValidation)
	}
	return parsed.Format("2006-01-02"), nil
}

// nextDailyID numbers records within the day they were written:
// PREFIX-YYYYMMDD-0001 and up.
func nextDailyID(prefix string, at time.Time, count int, idAt func(int) string) string {
	day := prefix + at.Format("20060102") + "-"
	highest := 0
	for i := 0; i < count; i++ {
		id := idAt(i)
		if !strings.HasPrefix(id, day) {
			continue
		}
		if number, err := strconv.Atoi(strings.TrimPrefix(id, day)); err == nil && number > highest {
			highest = number
		}
	}
	return fmt.Sprintf("%s%04d", day, highest+1)
}

func optionalNonNegative(label, value string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := parseNumber(label, value)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%w: %s tidak boleh negatif", ErrValidation, label)
	}
	return parsed, nil
}

func userName(user *model.User) string {
	if user == nil {
		return ""
	}
	return user.NamaLengkap
}

// A2BBaruOrderTotals adds the purchase orders up, the way the head of the
// workbook's fuel masuk table does.
type A2BBaruOrderTotals struct {
	POVolume, POJumlah     float64
	SiteVolume, SiteJumlah float64
	DevVolume, DevJumlah   float64
}

// HargaSatuan is the average price paid for each side, and for the
// deviation what the missing litres were worth each. Zero has no price.
func (t A2BBaruOrderTotals) POHarga() float64   { return perUnit(t.POJumlah, t.POVolume) }
func (t A2BBaruOrderTotals) SiteHarga() float64 { return perUnit(t.SiteJumlah, t.SiteVolume) }
func (t A2BBaruOrderTotals) DevHarga() float64  { return perUnit(t.DevJumlah, t.DevVolume) }

func perUnit(amount, volume float64) float64 {
	if volume == 0 {
		return 0
	}
	return amount / volume
}

// A2BBaruUnitRatio is one machine's consumption: every litre it took, the
// litres taken over hours worked, and those over the hours, overall and month
// by month.
type A2BBaruUnitRatio struct {
	IDUnit     string
	NamaUnit   string
	FuelKeluar float64
	// FuelRatio is the fuel of the dispenses that had hours to set it
	// against; a fill with no hours is counted out, not divided by zero.
	FuelRatio float64
	JamKerja  float64
	// Ratio is litres per hour, false when the machine logged no hours.
	Ratio    float64
	HasRatio bool
	// Monthly holds one entry per overview month, in the same order.
	Monthly []A2BBaruMonthRatio
}

type A2BBaruMonthRatio struct {
	Ratio    float64
	HasRatio bool
}

// A2BBaruOverview is the page the module opens on.
type A2BBaruOverview struct {
	Units        int
	FuelDiterima float64
	FuelKeluar   float64
	// Stock is what should be in the tank: everything received less everything
	// dispensed.
	Stock  float64
	Orders []model.A2BBaruFuelMasuk
	Totals A2BBaruOrderTotals
	// Months are "2006-01" keys, oldest first, for the ratio columns.
	Months []string
	Ratios []A2BBaruUnitRatio
	Fills  []model.A2BBaruFuelKeluar
}

func (s *A2BBaruService) Overview(ctx context.Context) (A2BBaruOverview, error) {
	var view A2BBaruOverview
	units, err := s.Units(ctx)
	if err != nil {
		return view, err
	}
	orders, err := s.FuelMasuk(ctx)
	if err != nil {
		return view, err
	}
	fills, err := s.FuelKeluar(ctx)
	if err != nil {
		return view, err
	}
	view.Units, view.Orders, view.Fills = len(units), orders, fills

	for _, order := range orders {
		view.Totals.POVolume += order.POVolume
		view.Totals.POJumlah += order.POJumlah()
		view.Totals.SiteVolume += order.SiteVolume
		view.Totals.SiteJumlah += order.SiteJumlah()
	}
	view.Totals.DevVolume = view.Totals.POVolume - view.Totals.SiteVolume
	view.Totals.DevJumlah = view.Totals.POJumlah - view.Totals.SiteJumlah
	view.FuelDiterima = view.Totals.SiteVolume

	names := make(map[string]string, len(units))
	for _, unit := range units {
		names[strings.ToLower(unit.IDUnit)] = unit.NamaUnit
	}
	type sums struct{ fuel, jam float64 }
	type unitSums struct {
		id, name          string
		keluar, fuel, jam float64
		months            map[string]*sums
	}
	byUnit := make(map[string]*unitSums)
	monthSeen := make(map[string]bool)
	for _, fill := range fills {
		view.FuelKeluar += fill.FuelLiter
		// A dispense the workbook names no unit for counts towards the
		// fuel out, but has no machine to set a ratio against.
		if strings.TrimSpace(fill.IDUnit) == "" {
			continue
		}
		key := strings.ToLower(fill.IDUnit)
		entry := byUnit[key]
		if entry == nil {
			name := names[key]
			if name == "" {
				name = fill.NamaUnit
			}
			entry = &unitSums{id: fill.IDUnit, name: name, months: make(map[string]*sums)}
			byUnit[key] = entry
		}
		entry.keluar += fill.FuelLiter
		if fill.JamKerja <= 0 {
			continue
		}
		entry.fuel += fill.FuelLiter
		entry.jam += fill.JamKerja
		if len(fill.Tanggal) >= 7 {
			month := fill.Tanggal[:7]
			monthSeen[month] = true
			if entry.months[month] == nil {
				entry.months[month] = &sums{}
			}
			entry.months[month].fuel += fill.FuelLiter
			entry.months[month].jam += fill.JamKerja
		}
	}
	view.Stock = view.FuelDiterima - view.FuelKeluar

	for month := range monthSeen {
		view.Months = append(view.Months, month)
	}
	sort.Strings(view.Months)
	for _, entry := range byUnit {
		ratio := A2BBaruUnitRatio{IDUnit: entry.id, NamaUnit: entry.name,
			FuelKeluar: entry.keluar, FuelRatio: entry.fuel, JamKerja: entry.jam}
		if entry.jam > 0 {
			ratio.Ratio, ratio.HasRatio = entry.fuel/entry.jam, true
		}
		for _, month := range view.Months {
			cell := A2BBaruMonthRatio{}
			if m := entry.months[month]; m != nil && m.jam > 0 {
				cell = A2BBaruMonthRatio{Ratio: m.fuel / m.jam, HasRatio: true}
			}
			ratio.Monthly = append(ratio.Monthly, cell)
		}
		view.Ratios = append(view.Ratios, ratio)
	}
	sort.Slice(view.Ratios, func(i, j int) bool {
		return strings.ToLower(view.Ratios[i].IDUnit) < strings.ToLower(view.Ratios[j].IDUnit)
	})
	return view, nil
}
