package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/repository"
)

// DatePoint is one bucket on a chart: a day when a range is filtered, a month
// otherwise.
type DatePoint struct {
	Tanggal   string
	Label     string
	Volume    float64
	VolumeOPP float64
	Kecil     int
	Besar     int
	Units     int
}

// UnitRank is one line of the most-productive table.
type UnitRank struct {
	Nopol           string
	Driver          string
	Ritase          int
	Volume          float64
	VolumePerRitase float64
}

// LokasiShare is one slice of the per-location breakdown.
type LokasiShare struct {
	Lokasi  string
	Volume  float64
	Percent float64
}

// Overview is everything the dashboard draws.
type Overview struct {
	From         string
	To           string
	TotalVolume  float64
	TotalOPP     float64
	TotalRitase  int
	ActiveUnits  int
	Series       []DatePoint
	Monthly      bool
	TopUnits     []UnitRank
	LokasiShares []LokasiShare
	HasLokasi    bool
	LastUpdated  string
	RowsTotal    int
}

// overviewCacheTTL keeps a dashboard reload from re-reading thousands of rows.
// Production is entered by hand through the day, so a minute of staleness costs
// nothing and the page stays quick.
const overviewCacheTTL = time.Minute

type OverviewService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc

	mu       sync.Mutex
	cached   []model.Produksi
	cachedAt time.Time
}

func NewOverviewService(store repository.Store, location *time.Location, now NowFunc) *OverviewService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &OverviewService{store: store, location: location, now: now}
}

func (s *OverviewService) rows(ctx context.Context) ([]model.Produksi, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && s.now().Sub(s.cachedAt) < overviewCacheTTL {
		return s.cached, nil
	}
	rows, err := s.store.ListProduksi(ctx)
	if err != nil {
		return nil, fmt.Errorf("read produksi: %w", err)
	}
	s.cached = rows
	s.cachedAt = s.now()
	return rows, nil
}

// Build aggregates the sheet between the two dates. Either bound may be empty,
// which means "no bound on that side".
func (s *OverviewService) Build(ctx context.Context, from, to string) (*Overview, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if from != "" {
		if _, err := time.Parse("2006-01-02", from); err != nil {
			return nil, fmt.Errorf("%w: tanggal awal tidak valid", ErrValidation)
		}
	}
	if to != "" {
		if _, err := time.Parse("2006-01-02", to); err != nil {
			return nil, fmt.Errorf("%w: tanggal akhir tidak valid", ErrValidation)
		}
	}
	// A reversed range would silently return nothing, which reads as "no data"
	// rather than "you typed the dates the wrong way round".
	if from != "" && to != "" && from > to {
		from, to = to, from
	}

	rows, err := s.rows(ctx)
	if err != nil {
		return nil, err
	}

	// With no range chosen the charts would otherwise plot every single day
	// since the records began, which is unreadable. Group by month instead and
	// only drop to daily once someone narrows the range.
	monthly := from == "" && to == ""

	overview := &Overview{
		From:        from,
		To:          to,
		Monthly:     monthly,
		RowsTotal:   len(rows),
		LastUpdated: s.now().In(s.location).Format("02 Jan 2006 15:04"),
	}

	perDate := map[string]*DatePoint{}
	unitsPerDate := map[string]map[string]bool{}
	perUnit := map[string]*UnitRank{}
	perLokasi := map[string]float64{}
	activeUnits := map[string]bool{}

	for _, row := range rows {
		if from != "" && row.Tanggal < from {
			continue
		}
		if to != "" && row.Tanggal > to {
			continue
		}

		overview.TotalVolume += row.Volume
		overview.TotalOPP += row.VolumeOPP
		overview.TotalRitase++

		nopol := strings.ToUpper(strings.TrimSpace(row.Nopol))
		if nopol != "" {
			activeUnits[nopol] = true
			rank, ok := perUnit[nopol]
			if !ok {
				rank = &UnitRank{Nopol: nopol, Driver: row.Driver}
				perUnit[nopol] = rank
			}
			rank.Ritase++
			rank.Volume += row.Volume
		}

		bucket := row.Tanggal
		if monthly {
			bucket = monthKey(row.Tanggal)
		}
		point, ok := perDate[bucket]
		if !ok {
			point = &DatePoint{Tanggal: bucket, Label: bucketLabel(bucket, monthly)}
			perDate[bucket] = point
			unitsPerDate[bucket] = map[string]bool{}
		}
		point.Volume += row.Volume
		point.VolumeOPP += row.VolumeOPP
		if strings.EqualFold(strings.TrimSpace(row.JenisDT), "DT BESAR") {
			point.Besar++
		} else {
			point.Kecil++
		}
		if nopol != "" {
			unitsPerDate[bucket][nopol] = true
		}

		lokasi := strings.TrimSpace(row.Lokasi)
		if lokasi == "" {
			lokasi = "Lainnya"
		} else {
			overview.HasLokasi = true
		}
		perLokasi[lokasi] += row.Volume
	}

	overview.ActiveUnits = len(activeUnits)

	overview.Series = make([]DatePoint, 0, len(perDate))
	for bucket, point := range perDate {
		point.Units = len(unitsPerDate[bucket])
		point.Volume = round2(point.Volume)
		point.VolumeOPP = round2(point.VolumeOPP)
		overview.Series = append(overview.Series, *point)
	}
	sort.Slice(overview.Series, func(i, j int) bool {
		return overview.Series[i].Tanggal < overview.Series[j].Tanggal
	})
	if monthly {
		overview.Series = fillMonths(overview.Series, s.now().In(s.location))
	}

	ranks := make([]UnitRank, 0, len(perUnit))
	for _, rank := range perUnit {
		rank.Volume = round2(rank.Volume)
		if rank.Ritase > 0 {
			rank.VolumePerRitase = round2(rank.Volume / float64(rank.Ritase))
		}
		ranks = append(ranks, *rank)
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].Volume != ranks[j].Volume {
			return ranks[i].Volume > ranks[j].Volume
		}
		return ranks[i].Nopol < ranks[j].Nopol
	})
	if len(ranks) > 5 {
		ranks = ranks[:5]
	}
	overview.TopUnits = ranks

	for lokasi, volume := range perLokasi {
		share := LokasiShare{Lokasi: lokasi, Volume: round2(volume)}
		if overview.TotalVolume > 0 {
			share.Percent = round2(volume / overview.TotalVolume * 100)
		}
		overview.LokasiShares = append(overview.LokasiShares, share)
	}
	sort.Slice(overview.LokasiShares, func(i, j int) bool {
		return overview.LokasiShares[i].Volume > overview.LokasiShares[j].Volume
	})

	overview.TotalVolume = round2(overview.TotalVolume)
	overview.TotalOPP = round2(overview.TotalOPP)
	return overview, nil
}

// monthKey reduces 2026-06-09 to 2026-06.
func monthKey(tanggal string) string {
	if len(tanggal) < 7 {
		return tanggal
	}
	return tanggal[:7]
}

func bucketLabel(bucket string, monthly bool) string {
	if monthly {
		parsed, err := time.Parse("2006-01", bucket)
		if err != nil {
			return bucket
		}
		return parsed.Format("Jan 2006")
	}
	parsed, err := time.Parse("2006-01-02", bucket)
	if err != nil {
		return bucket
	}
	return parsed.Format("02/01")
}

// fillMonths inserts the months that recorded nothing, up to the current one.
// Without them a quiet month simply vanishes and the chart implies production
// ran continuously.
func fillMonths(series []DatePoint, now time.Time) []DatePoint {
	if len(series) == 0 {
		return series
	}
	first, err := time.Parse("2006-01", series[0].Tanggal)
	if err != nil {
		return series
	}
	last := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if lastRecorded, err := time.Parse("2006-01", series[len(series)-1].Tanggal); err == nil && lastRecorded.After(last) {
		last = lastRecorded
	}

	existing := make(map[string]DatePoint, len(series))
	for _, point := range series {
		existing[point.Tanggal] = point
	}
	filled := make([]DatePoint, 0, len(series))
	for cursor := first; !cursor.After(last); cursor = cursor.AddDate(0, 1, 0) {
		key := cursor.Format("2006-01")
		if point, ok := existing[key]; ok {
			filled = append(filled, point)
			continue
		}
		filled = append(filled, DatePoint{Tanggal: key, Label: cursor.Format("Jan 2006")})
	}
	return filled
}
