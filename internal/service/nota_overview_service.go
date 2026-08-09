package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// NotaPoint is one bucket on the spending charts: a day when a range is
// filtered, a month otherwise.
type NotaPoint struct {
	Tanggal   string
	Label     string
	Total     float64
	CA        float64
	Reimburse float64
	Jumlah    int
}

// NotaShare is one line of the category breakdown.
type NotaShare struct {
	Label   string
	Total   float64
	Jumlah  int
	Percent float64
}

// NotaPICRank is one line of the biggest-spender table.
type NotaPICRank struct {
	PIC      string
	Jumlah   int
	Total    float64
	RataTiap float64
}

// NotaOverview is everything the nota dashboard draws.
type NotaOverview struct {
	From    string
	To      string
	Monthly bool

	TotalPengeluaran float64
	JumlahNota       int
	RataRata         float64
	Outstanding      float64
	OutstandingCount int
	Dibayar          float64
	DibayarCount     int

	Series         []NotaPoint
	KategoriShares []NotaShare
	TopPIC         []NotaPICRank

	// Rupiah runs to millions, which no chart axis can label legibly. The
	// series is divided by Scale and the unit is printed beside the heading.
	Scale float64
	Unit  string

	LastUpdated string
	RowsTotal   int
}

// BuildOverview aggregates the notes between two dates. Either bound may be
// empty, which means no bound on that side.
func (s *NotaService) BuildOverview(ctx context.Context, from, to string) (*NotaOverview, error) {
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

	rows, err := s.store.ListNota(ctx)
	if err != nil {
		return nil, fmt.Errorf("read nota: %w", err)
	}

	// With no range chosen the charts would plot every day since the records
	// began, which is unreadable. Group by month until someone narrows it.
	monthly := from == "" && to == ""

	overview := &NotaOverview{
		From:        from,
		To:          to,
		Monthly:     monthly,
		RowsTotal:   len(rows),
		Scale:       1,
		LastUpdated: s.now().In(s.location).Format("02 Jan 2006 15:04"),
	}

	perBucket := map[string]*NotaPoint{}
	perKategori := map[string]*NotaShare{}
	perPIC := map[string]*NotaPICRank{}

	for _, nota := range rows {
		if from != "" && nota.Tanggal < from {
			continue
		}
		if to != "" && nota.Tanggal > to {
			continue
		}

		overview.TotalPengeluaran += nota.Total
		overview.JumlahNota++
		if nota.StatusPembayaran == model.NotaStatusBelumDibayar {
			overview.Outstanding += nota.Total
			overview.OutstandingCount++
		} else {
			overview.Dibayar += nota.Total
			overview.DibayarCount++
		}

		bucket := nota.Tanggal
		if monthly {
			bucket = monthKey(nota.Tanggal)
		}
		point, ok := perBucket[bucket]
		if !ok {
			point = &NotaPoint{Tanggal: bucket, Label: bucketLabel(bucket, monthly)}
			perBucket[bucket] = point
		}
		point.Total += nota.Total
		point.Jumlah++
		if nota.MetodePembayaran == model.NotaMetodeCA {
			point.CA += nota.Total
		} else {
			point.Reimburse += nota.Total
		}

		label := strings.TrimSpace(nota.Kategori)
		if sub := strings.TrimSpace(nota.SubKategori); sub != "" {
			label += " — " + sub
		}
		if label == "" {
			label = "Tanpa kategori"
		}
		share, ok := perKategori[label]
		if !ok {
			share = &NotaShare{Label: label}
			perKategori[label] = share
		}
		share.Total += nota.Total
		share.Jumlah++

		pic := strings.TrimSpace(nota.PIC)
		if pic == "" {
			pic = "Tanpa PIC"
		}
		rank, ok := perPIC[pic]
		if !ok {
			rank = &NotaPICRank{PIC: pic}
			perPIC[pic] = rank
		}
		rank.Total += nota.Total
		rank.Jumlah++
	}

	overview.Series = make([]NotaPoint, 0, len(perBucket))
	for _, point := range perBucket {
		point.Total = round2(point.Total)
		point.CA = round2(point.CA)
		point.Reimburse = round2(point.Reimburse)
		overview.Series = append(overview.Series, *point)
	}
	sort.Slice(overview.Series, func(i, j int) bool {
		return overview.Series[i].Tanggal < overview.Series[j].Tanggal
	})
	if monthly {
		overview.Series = fillNotaMonths(overview.Series, s.now().In(s.location))
	}

	for _, share := range perKategori {
		share.Total = round2(share.Total)
		if overview.TotalPengeluaran > 0 {
			share.Percent = round2(share.Total / overview.TotalPengeluaran * 100)
		}
		overview.KategoriShares = append(overview.KategoriShares, *share)
	}
	sort.Slice(overview.KategoriShares, func(i, j int) bool {
		if overview.KategoriShares[i].Total != overview.KategoriShares[j].Total {
			return overview.KategoriShares[i].Total > overview.KategoriShares[j].Total
		}
		return overview.KategoriShares[i].Label < overview.KategoriShares[j].Label
	})

	ranks := make([]NotaPICRank, 0, len(perPIC))
	for _, rank := range perPIC {
		rank.Total = round2(rank.Total)
		if rank.Jumlah > 0 {
			rank.RataTiap = round2(rank.Total / float64(rank.Jumlah))
		}
		ranks = append(ranks, *rank)
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].Total != ranks[j].Total {
			return ranks[i].Total > ranks[j].Total
		}
		return ranks[i].PIC < ranks[j].PIC
	})
	if len(ranks) > 5 {
		ranks = ranks[:5]
	}
	overview.TopPIC = ranks

	if overview.JumlahNota > 0 {
		overview.RataRata = round2(overview.TotalPengeluaran / float64(overview.JumlahNota))
	}
	overview.TotalPengeluaran = round2(overview.TotalPengeluaran)
	overview.Outstanding = round2(overview.Outstanding)
	overview.Dibayar = round2(overview.Dibayar)
	overview.Scale, overview.Unit = chartScale(overview.Series)
	return overview, nil
}

// chartScale picks the unit the axis is labelled in. Plotting rupiah directly
// puts eight digits on every gridline, which is unreadable at chart size.
func chartScale(series []NotaPoint) (float64, string) {
	peak := 0.0
	for _, point := range series {
		if point.Total > peak {
			peak = point.Total
		}
	}
	switch {
	case peak >= 100_000_000:
		return 1_000_000, "juta rupiah"
	case peak >= 100_000:
		return 1_000, "ribu rupiah"
	default:
		return 1, "rupiah"
	}
}

// Scaled reports a figure in the unit the charts are drawn in.
func (o *NotaOverview) Scaled(value float64) float64 {
	if o.Scale <= 0 {
		return value
	}
	return round2(value / o.Scale)
}

// fillNotaMonths adds the months nobody filed a nota in, so a quiet month reads
// as zero spending rather than disappearing from the axis.
func fillNotaMonths(series []NotaPoint, now time.Time) []NotaPoint {
	if len(series) == 0 {
		return series
	}
	first, err := time.Parse("2006-01", series[0].Tanggal)
	if err != nil {
		return series
	}
	last := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if recorded, err := time.Parse("2006-01", series[len(series)-1].Tanggal); err == nil && recorded.After(last) {
		last = recorded
	}

	existing := make(map[string]NotaPoint, len(series))
	for _, point := range series {
		existing[point.Tanggal] = point
	}
	filled := make([]NotaPoint, 0, len(series))
	for cursor := first; !cursor.After(last); cursor = cursor.AddDate(0, 1, 0) {
		key := cursor.Format("2006-01")
		if point, ok := existing[key]; ok {
			filled = append(filled, point)
			continue
		}
		filled = append(filled, NotaPoint{Tanggal: key, Label: cursor.Format("Jan 2006")})
	}
	return filled
}
