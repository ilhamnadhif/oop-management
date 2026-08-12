package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
	"opp-management/internal/repository"
)

// FuelInputLayout is what the datetime-local control in the form submits.
const FuelInputLayout = "2006-01-02T15:04"

// FuelPhotoKind names one of the four required photos. The slug is what the
// photo URL carries, and the index is the order the sheet holds them in.
type FuelPhotoKind struct {
	Slug  string
	Label string
	Field string
	Index int
}

// FuelPhotoKinds is the whole evidence set, in the order the form asks for it:
// the truck that arrived, the tank before discharge, the flowmeter reading, and
// the tank after. Any one of them missing leaves the delivery unverifiable.
var FuelPhotoKinds = []FuelPhotoKind{
	{Slug: "truck-depan", Label: "Foto truck tampak depan", Field: "foto_truck_depan", Index: 0},
	{Slug: "tangki-sebelum", Label: "Foto tangki sebelum bongkar", Field: "foto_tangki_sebelum", Index: 1},
	{Slug: "flowmeter", Label: "Foto flowmeter", Field: "foto_flowmeter", Index: 2},
	{Slug: "tangki-setelah", Label: "Foto tangki setelah bongkar", Field: "foto_tangki_setelah", Index: 3},
}

func fuelPhotoKindBySlug(slug string) (FuelPhotoKind, bool) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, kind := range FuelPhotoKinds {
		if kind.Slug == slug {
			return kind, true
		}
	}
	return FuelPhotoKind{}, false
}

// FuelKeteranganOptions is the closed set the form renders and the service
// enforces, so a direct POST cannot store a third word.
var FuelKeteranganOptions = []string{model.FuelKeteranganSesuai, model.FuelKeteranganTidakSesuai}

// FuelMasukInput is the part of a delivery the person recording it supplies.
// The transaction number, the status and the audit trail are absent: the
// service derives those rather than trusting a form post.
type FuelMasukInput struct {
	TanggalInput     string
	Vendor           string
	Driver           string
	Nopol            string
	JumlahLiter      string
	Keterangan       string
	LiterTidakSesuai string
	// Photos are indexed by FuelPhotoKind.Index.
	Photos [4]string
}

// FuelMasukFilters narrow the delivery list. The date range is matched against
// the recorded input time.
type FuelMasukFilters struct {
	Q    string
	From string
	To   string
}

// FuelMasukOptions are the suggestions the form offers. Both are open lists: a
// vendor or a driver seen for the first time must still be typeable.
type FuelMasukOptions struct {
	Vendors []string
	Drivers []string
}

// FuelMasukService owns the delivery lifecycle. The mutex makes sequential ID
// allocation atomic within a process.
type FuelMasukService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	mu       sync.Mutex

	optionsMu sync.Mutex
	options   FuelMasukOptions
	optionsAt time.Time
}

func NewFuelMasukService(store repository.Store, location *time.Location, now NowFunc) *FuelMasukService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &FuelMasukService{store: store, location: location, now: now}
}

// NowInput is the value the form's datetime field starts on.
func (s *FuelMasukService) NowInput() string {
	return s.now().In(s.location).Format(FuelInputLayout)
}

// NextFuelID reports the number the next saved delivery would receive. The form
// shows it as a preview; Create assigns the authoritative value under the lock.
func (s *FuelMasukService) NextFuelID(ctx context.Context) (string, error) {
	now := s.now().In(s.location)
	prefix := fuelIDPrefix(now)
	highest, err := s.store.MaxFuelMasukSequence(ctx, prefix)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%04d", prefix, highest+1), nil
}

// fuelIDPrefix numbers deliveries within a day, so the transaction number says
// when it was recorded without anyone having to look the row up.
func fuelIDPrefix(now time.Time) string {
	return "FUEL-" + now.Format("20060102") + "-"
}

// Options lists the vendors and drivers already seen, so the same vendor does
// not end up stored under three spellings.
func (s *FuelMasukService) Options(ctx context.Context) (FuelMasukOptions, error) {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	if s.optionsAt.After(time.Time{}) && s.now().Sub(s.optionsAt) < optionsCacheTTL {
		return s.options, nil
	}
	rows, err := s.store.ListFuelMasuk(ctx)
	if err != nil {
		return FuelMasukOptions{}, fmt.Errorf("read fuel masuk options: %w", err)
	}
	s.options = FuelMasukOptions{
		Vendors: distinctValues(nil, rows, func(f model.FuelMasuk) string { return f.Vendor }),
		Drivers: distinctValues(nil, rows, func(f model.FuelMasuk) string { return f.Driver }),
	}
	s.optionsAt = s.now()
	return s.options, nil
}

func (s *FuelMasukService) invalidateOptions() {
	s.optionsMu.Lock()
	defer s.optionsMu.Unlock()
	s.optionsAt = time.Time{}
}

func (s *FuelMasukService) Create(ctx context.Context, user *model.User, input FuelMasukInput) (*model.FuelMasuk, error) {
	if err := validateUser(user); err != nil {
		return nil, err
	}

	tanggalInput, err := s.parseInputTime(input.TanggalInput)
	if err != nil {
		return nil, err
	}
	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	vendor, err := adoptOption("Vendor", input.Vendor, options.Vendors)
	if err != nil {
		return nil, err
	}
	driver, err := adoptOption("Nama driver", input.Driver, options.Drivers)
	if err != nil {
		return nil, err
	}
	nopol, err := NormalizeNopol(input.Nopol)
	if err != nil {
		return nil, err
	}
	jumlah, err := parseDimension("Jumlah fuel masuk", input.JumlahLiter)
	if err != nil {
		return nil, err
	}
	keterangan, err := canonicalFuelKeterangan(input.Keterangan)
	if err != nil {
		return nil, err
	}
	selisih, err := parseFuelSelisih(keterangan, input.LiterTidakSesuai, jumlah)
	if err != nil {
		return nil, err
	}
	// Every photo is required. A delivery recorded without the flowmeter or the
	// tank either side of it cannot be checked later, and there is no screen to
	// go back and add one.
	for _, kind := range FuelPhotoKinds {
		value := strings.TrimSpace(input.Photos[kind.Index])
		if value == "" {
			return nil, fmt.Errorf("%w: %s wajib dilampirkan", ErrValidation, strings.ToLower(kind.Label))
		}
		if err := photo.ValidateDataURL(value); err != nil {
			return nil, fmt.Errorf("%w: %s tidak valid", ErrInvalidPhoto, strings.ToLower(kind.Label))
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	prefix := fuelIDPrefix(now)
	sequence, err := s.store.MaxFuelMasukSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read fuel masuk sequence: %w", err)
	}
	fuel := &model.FuelMasuk{
		FuelID:           fmt.Sprintf("%s%04d", prefix, sequence+1),
		TanggalInput:     tanggalInput,
		Vendor:           vendor,
		Driver:           driver,
		Nopol:            nopol,
		JumlahLiter:      round2(jumlah),
		Keterangan:       keterangan,
		LiterTidakSesuai: round2(selisih),
		// Nobody signs a delivery off any more: the four photos are the check,
		// and a status that only ever had one value is not a decision.
		StatusApproval:    model.FuelStatusDisetujui,
		FotoTruckDepan:    input.Photos[0],
		FotoTangkiSebelum: input.Photos[1],
		FotoFlowmeter:     input.Photos[2],
		FotoTangkiSetelah: input.Photos[3],
		CreatedBy:         strings.TrimSpace(user.NamaLengkap),
		CreatedByID:       user.UserID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.CreateFuelMasuk(ctx, fuel); err != nil {
		return nil, fmt.Errorf("create fuel masuk: %w", err)
	}
	// A vendor or driver typed just now must be offered to the next delivery, or
	// the same name gets stored again under a different spelling.
	s.invalidateOptions()
	return fuel, nil
}

// List returns the deliveries newest first, which is the order the input page
// wants: the row someone just saved is the one they check.
func (s *FuelMasukService) List(ctx context.Context, filters FuelMasukFilters) ([]model.FuelMasuk, error) {
	rows, err := s.filteredRows(ctx, filters)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].TanggalInput.Equal(rows[j].TanggalInput) {
			return rows[i].TanggalInput.After(rows[j].TanggalInput)
		}
		return rows[i].FuelID > rows[j].FuelID
	})
	return rows, nil
}

func (s *FuelMasukService) filteredRows(ctx context.Context, filters FuelMasukFilters) ([]model.FuelMasuk, error) {
	filters, err := normalizeFuelFilters(filters)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListFuelMasuk(ctx)
	if err != nil {
		return nil, fmt.Errorf("read fuel masuk: %w", err)
	}
	result := make([]model.FuelMasuk, 0, len(rows))
	for _, row := range rows {
		date := row.TanggalInput.In(s.location).Format("2006-01-02")
		if filters.From != "" && date < filters.From {
			continue
		}
		if filters.To != "" && date > filters.To {
			continue
		}
		if filters.Q != "" && !fuelMatchesQuery(row, filters.Q) {
			continue
		}
		result = append(result, row)
	}
	return result, nil
}

// Photo reads one stored photo. The delivery row is found first so a caller can
// ask by transaction number without knowing where the row sits.
func (s *FuelMasukService) Photo(ctx context.Context, fuelID, slug string) (string, error) {
	fuelID = strings.TrimSpace(fuelID)
	if fuelID == "" {
		return "", fmt.Errorf("%w: nomor transaksi wajib diisi", ErrValidation)
	}
	kind, ok := fuelPhotoKindBySlug(slug)
	if !ok {
		return "", repository.ErrNotFound
	}
	_, rowNumber, err := s.store.FindFuelMasukRow(ctx, fuelID)
	if err != nil {
		return "", fmt.Errorf("find fuel masuk: %w", err)
	}
	value, err := s.store.ReadFuelMasukPhoto(ctx, rowNumber, kind.Index)
	if err != nil {
		return "", fmt.Errorf("read fuel masuk photo: %w", err)
	}
	if strings.TrimSpace(value) == "" {
		return "", repository.ErrNotFound
	}
	if err := photo.ValidateDataURL(value); err != nil {
		return "", fmt.Errorf("stored fuel masuk photo: %w", ErrInvalidPhoto)
	}
	return value, nil
}

func (s *FuelMasukService) parseInputTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, fmt.Errorf("%w: tanggal & waktu input wajib diisi", ErrValidation)
	}
	for _, layout := range []string{FuelInputLayout, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, s.location); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("%w: tanggal & waktu input tidak valid", ErrValidation)
}

func canonicalFuelKeterangan(value string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	for _, option := range FuelKeteranganOptions {
		if strings.EqualFold(option, value) {
			return option, nil
		}
	}
	return "", fmt.Errorf("%w: keterangan harus %s atau %s", ErrValidation,
		model.FuelKeteranganSesuai, model.FuelKeteranganTidakSesuai)
}

// parseFuelSelisih reads the shortfall only when the delivery is marked as not
// matching. A delivery marked "sesuai" stores a zero whatever the form sent, so
// the two columns can never contradict each other.
func parseFuelSelisih(keterangan, value string, jumlah float64) (float64, error) {
	if keterangan == model.FuelKeteranganSesuai {
		return 0, nil
	}
	selisih, err := parseDimension("Liter tidak sesuai", value)
	if err != nil {
		return 0, err
	}
	if selisih > jumlah {
		return 0, fmt.Errorf("%w: liter tidak sesuai tidak boleh melebihi jumlah fuel masuk", ErrValidation)
	}
	return selisih, nil
}

func normalizeFuelFilters(filters FuelMasukFilters) (FuelMasukFilters, error) {
	filters.Q = strings.ToLower(strings.TrimSpace(filters.Q))
	filters.From = strings.TrimSpace(filters.From)
	filters.To = strings.TrimSpace(filters.To)
	if filters.From != "" {
		if _, err := time.Parse("2006-01-02", filters.From); err != nil {
			return FuelMasukFilters{}, fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
		}
	}
	if filters.To != "" {
		if _, err := time.Parse("2006-01-02", filters.To); err != nil {
			return FuelMasukFilters{}, fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
	}
	if filters.From != "" && filters.To != "" && filters.From > filters.To {
		filters.From, filters.To = filters.To, filters.From
	}
	return filters, nil
}

func fuelMatchesQuery(row model.FuelMasuk, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		row.FuelID, row.Vendor, row.Driver, row.Nopol, row.Keterangan, row.CreatedBy,
	}, " "))
	return strings.Contains(haystack, query)
}
