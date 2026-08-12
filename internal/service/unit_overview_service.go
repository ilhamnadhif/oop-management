package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/repository"
)

// UnitShare is one slice of a register: how many units carry a value and what
// portion of the fleet that is.
type UnitShare struct {
	Label   string
	Jumlah  int
	Percent float64
}

// UnitOverview is what the dump truck register holds. What the machine fleet
// did over a range is A2BOverview, on its own page.
type UnitOverview struct {
	TotalDT int

	JenisShares []UnitShare
	// TanpaDriver is the number of trucks with nobody assigned. A truck without
	// a driver cannot be dispatched, so it is worth naming rather than burying.
	TanpaDriver int
	Drivers     int

	LastUpdated string
}

// driverPlaceholders are the values the register uses when nobody has been
// assigned yet. They are not names and must not be counted as drivers.
var driverPlaceholders = map[string]bool{
	"":            true,
	"BELUM DIISI": true,
	"-":           true,
	"N/A":         true,
}

type UnitOverviewService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
}

func NewUnitOverviewService(store repository.Store, location *time.Location, now NowFunc) *UnitOverviewService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &UnitOverviewService{store: store, location: location, now: now}
}

func (s *UnitOverviewService) Build(ctx context.Context) (*UnitOverview, error) {
	trucks, err := s.store.ListUnitDT(ctx)
	if err != nil {
		return nil, fmt.Errorf("read unit dt: %w", err)
	}

	overview := &UnitOverview{
		TotalDT:     len(trucks),
		LastUpdated: s.now().In(s.location).Format("02 Jan 2006 15:04"),
	}

	perJenis := map[string]int{}
	drivers := map[string]bool{}
	for _, truck := range trucks {
		jenis := strings.ToUpper(strings.TrimSpace(truck.Keterangan))
		if jenis == "" {
			jenis = "TANPA KETERANGAN"
		}
		perJenis[jenis]++

		driver := strings.ToUpper(strings.TrimSpace(truck.Driver))
		if driverPlaceholders[driver] {
			overview.TanpaDriver++
			continue
		}
		drivers[driver] = true
	}
	overview.Drivers = len(drivers)
	overview.JenisShares = sharesOf(perJenis, len(trucks))

	return overview, nil
}

// sharesOf turns a tally into portions of the whole, biggest first.
func sharesOf(tally map[string]int, total int) []UnitShare {
	shares := make([]UnitShare, 0, len(tally))
	for label, count := range tally {
		share := UnitShare{Label: label, Jumlah: count}
		if total > 0 {
			share.Percent = round2(float64(count) / float64(total) * 100)
		}
		shares = append(shares, share)
	}
	sort.Slice(shares, func(i, j int) bool {
		if shares[i].Jumlah != shares[j].Jumlah {
			return shares[i].Jumlah > shares[j].Jumlah
		}
		return shares[i].Label < shares[j].Label
	})
	return shares
}
