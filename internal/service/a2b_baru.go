package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// A2BBaruService keeps the A2B Baru books: the machine register, the purchase
// orders the tank is filled from, and every dispense. It reads and writes the
// three A2B Baru sheets only; the older A2B module is not touched.
type A2BBaruService struct {
	mu       sync.Mutex
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
	Tanggal   string
	IDUnit    string
	HMAwalFM  string
	HMAkhirFM string
	HMSaatIsi string
	JamKerja  string
	Operator  string
	// Foto is the closing flow meter reading, already turned into a data URL.
	Foto string
}

// CreateFuelKeluar files one dispense the way the older fuel keluar form does:
// the litres are the distance between the two flow meter readings, the unit's
// name comes from the register, the machine's hour meter is held against its
// fills either side, and a photo of the closing reading is required. The
// second return is a caution about the hour meter; the row is saved either way.
func (s *A2BBaruService) CreateFuelKeluar(ctx context.Context, user *model.User, input A2BBaruFuelKeluarInput) (*model.A2BBaruFuelKeluar, string, error) {
	fill := model.A2BBaruFuelKeluar{CreatedBy: userName(user), CreatedAt: s.now().In(s.location)}
	tanggal, err := s.parseDate(input.Tanggal)
	if err != nil {
		return nil, "", err
	}
	fill.Tanggal = tanggal

	id := strings.TrimSpace(input.IDUnit)
	if id == "" {
		return nil, "", fmt.Errorf("%w: ID unit wajib dipilih", ErrValidation)
	}
	units, err := s.store.ListA2BBaruUnits(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list unit a2b baru: %w", err)
	}
	for _, unit := range units {
		if strings.EqualFold(unit.IDUnit, id) {
			fill.IDUnit, fill.NamaUnit = unit.IDUnit, unit.NamaUnit
		}
	}
	if fill.IDUnit == "" {
		return nil, "", fmt.Errorf("%w: ID unit %s belum terdaftar", ErrValidation, id)
	}

	awal, err := parseNonNegative("HM awal flow meter", input.HMAwalFM)
	if err != nil {
		return nil, "", err
	}
	akhir, err := parseNonNegative("HM akhir flow meter", input.HMAkhirFM)
	if err != nil {
		return nil, "", err
	}
	if akhir <= awal {
		return nil, "", fmt.Errorf("%w: HM akhir flow meter harus lebih besar dari HM awal", ErrValidation)
	}
	fill.HMAwalFM, fill.HMAkhirFM = &awal, &akhir
	fill.FuelLiter = round2(akhir - awal)

	fills, err := s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	var warning string
	if fill.HMSaatIsi, err = optionalHourMeter(input.HMSaatIsi); err != nil {
		return nil, "", err
	}
	if fill.HMSaatIsi != nil {
		var readings []meterReading
		for _, other := range fills {
			if other.HMSaatIsi != nil && strings.EqualFold(other.IDUnit, fill.IDUnit) {
				readings = append(readings, meterReading{tanggal: other.Tanggal, meter: *other.HMSaatIsi})
			}
		}
		before, after := nearestReadings(readings, fill.Tanggal)
		if warning, err = judgeHourMeter(fill.IDUnit, fill.Tanggal, *fill.HMSaatIsi, before, after); err != nil {
			return nil, "", err
		}
	}
	if fill.JamKerja, err = optionalNonNegative("Jam kerja", input.JamKerja); err != nil {
		return nil, "", err
	}

	var operators []string
	for _, other := range fills {
		operators = append(operators, other.Operator)
	}
	if fill.Operator, err = adoptOption("Operator", input.Operator, operators); err != nil {
		return nil, "", err
	}

	foto := strings.TrimSpace(input.Foto)
	if foto == "" {
		return nil, "", fmt.Errorf("%w: foto akhir flow meter wajib dilampirkan", ErrValidation)
	}
	if err := photo.ValidateDataURL(foto); err != nil {
		return nil, "", fmt.Errorf("%w: foto akhir flow meter tidak valid", ErrInvalidPhoto)
	}
	fill.Foto = foto

	// Numbered and written under one lock, so two people saving at once do
	// not both take the same number.
	s.mu.Lock()
	defer s.mu.Unlock()
	fills, err = s.store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list fuel keluar a2b baru: %w", err)
	}
	fill.FuelOutID = nextDailyID("FUELOUT-", fill.CreatedAt, len(fills), func(i int) string { return fills[i].FuelOutID })
	if err := s.store.AppendA2BBaruFuelKeluar(ctx, []model.A2BBaruFuelKeluar{fill}); err != nil {
		return nil, "", fmt.Errorf("create fuel keluar a2b baru: %w", err)
	}
	fill.HasFoto, fill.Foto = true, ""
	return &fill, warning, nil
}

// FuelKeluarPhoto reads one dispense's photo.
func (s *A2BBaruService) FuelKeluarPhoto(ctx context.Context, fuelOutID string) (string, error) {
	fuelOutID = strings.TrimSpace(fuelOutID)
	if fuelOutID == "" {
		return "", fmt.Errorf("%w: nomor transaksi wajib diisi", ErrValidation)
	}
	foto, err := s.store.ReadA2BBaruFuelKeluarPhoto(ctx, fuelOutID)
	if err != nil {
		return "", err
	}
	if foto == "" {
		return "", repository.ErrNotFound
	}
	return foto, nil
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

// A2BBaruOverview is the page the module opens on. The figures at the top
// are the whole book; each table below is what its own filter lets through.
type A2BBaruOverview struct {
	Units        int
	UnitList     []model.A2BBaruUnit
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

// A2BBaruRange narrows one table: dates inclusive as "2006-01-02", and the
// units by id. Empty means no bound; several units are any of them.
type A2BBaruRange struct {
	From, To string
	Units    []string
}

func (r A2BBaruRange) admits(tanggal, unit string) bool {
	if r.From != "" && tanggal < r.From {
		return false
	}
	if r.To != "" && tanggal > r.To {
		return false
	}
	if len(r.Units) == 0 {
		return true
	}
	for _, wanted := range r.Units {
		if strings.EqualFold(strings.TrimSpace(unit), strings.TrimSpace(wanted)) {
			return true
		}
	}
	return false
}

// A2BBaruOverviewFilter is one range per table. The orders have no unit.
type A2BBaruOverviewFilter struct {
	Orders A2BBaruRange
	Ratios A2BBaruRange
	Fills  A2BBaruRange
}

func (s *A2BBaruService) Overview(ctx context.Context, filter A2BBaruOverviewFilter) (A2BBaruOverview, error) {
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
	view.Units, view.UnitList = len(units), units

	everything := orderTotals(orders)
	view.FuelDiterima = everything.SiteVolume
	for _, fill := range fills {
		view.FuelKeluar += fill.FuelLiter
	}
	view.Stock = view.FuelDiterima - view.FuelKeluar

	for _, order := range orders {
		if filter.Orders.admits(order.Tanggal, "") {
			view.Orders = append(view.Orders, order)
		}
	}
	view.Totals = orderTotals(view.Orders)

	var forRatio []model.A2BBaruFuelKeluar
	for _, fill := range fills {
		if filter.Ratios.admits(fill.Tanggal, fill.IDUnit) {
			forRatio = append(forRatio, fill)
		}
		if filter.Fills.admits(fill.Tanggal, fill.IDUnit) {
			view.Fills = append(view.Fills, fill)
		}
	}
	view.Months, view.Ratios = unitRatios(units, forRatio)
	return view, nil
}

// orderTotals adds purchase orders up the way the workbook's fuel masuk table
// heads them.
func orderTotals(orders []model.A2BBaruFuelMasuk) A2BBaruOrderTotals {
	var totals A2BBaruOrderTotals
	for _, order := range orders {
		totals.POVolume += order.POVolume
		totals.POJumlah += order.POJumlah()
		totals.SiteVolume += order.SiteVolume
		totals.SiteJumlah += order.SiteJumlah()
	}
	totals.DevVolume = totals.POVolume - totals.SiteVolume
	totals.DevJumlah = totals.POJumlah - totals.SiteJumlah
	return totals
}

// unitRatios is each machine's consumption over the fills given: every litre
// it took, the litres taken over hours worked, and those over the hours,
// overall and month by month. A dispense the workbook names no unit for has
// no machine to set a ratio against and is left out.
func unitRatios(units []model.A2BBaruUnit, fills []model.A2BBaruFuelKeluar) ([]string, []A2BBaruUnitRatio) {
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

	var months []string
	for month := range monthSeen {
		months = append(months, month)
	}
	sort.Strings(months)
	var ratios []A2BBaruUnitRatio
	for _, entry := range byUnit {
		ratio := A2BBaruUnitRatio{IDUnit: entry.id, NamaUnit: entry.name,
			FuelKeluar: entry.keluar, FuelRatio: entry.fuel, JamKerja: entry.jam}
		if entry.jam > 0 {
			ratio.Ratio, ratio.HasRatio = entry.fuel/entry.jam, true
		}
		for _, month := range months {
			cell := A2BBaruMonthRatio{}
			if m := entry.months[month]; m != nil && m.jam > 0 {
				cell = A2BBaruMonthRatio{Ratio: m.fuel / m.jam, HasRatio: true}
			}
			ratio.Monthly = append(ratio.Monthly, cell)
		}
		ratios = append(ratios, ratio)
	}
	sort.Slice(ratios, func(i, j int) bool {
		return strings.ToLower(ratios[i].IDUnit) < strings.ToLower(ratios[j].IDUnit)
	})
	return months, ratios
}
