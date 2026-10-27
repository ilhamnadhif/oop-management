package widya

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ticket is one load across the scanner: a truck scanned full, and the volume
// it carried. It is all the summary needs of a ticket, which on the wire runs
// to several kilobytes of scan file paths.
type Ticket struct {
	Number     string
	Vehicle    string
	Supplier   string
	Material   string
	Volume     float64
	ScanFullAt time.Time
	// UpdatedAt is when the scanner last changed the ticket: its full scan, or
	// a correction. The archive reads forward from the newest one it has seen.
	UpdatedAt time.Time
}

// ticketPageSize is how many tickets are asked for at once. A page is a few
// megabytes; the scanner answers one in about two seconds.
const ticketPageSize = 500

// ticketPageLimit stops a runaway range from paging forever: 20 pages is ten
// thousand loads, several months of this site.
const ticketPageLimit = 20

// ticketWorkers is how many pages are read at once after the first. A month is
// ten pages; read one after another they outlast a page request.
const ticketWorkers = 4

type ticketPage struct {
	Data []struct {
		Number       string  `json:"number"`
		VehicleVrm   string  `json:"vehicleVrm"`
		SupplierName string  `json:"supplierName"`
		MaterialName string  `json:"materialName"`
		Volume       float64 `json:"scanLoadVolume"`
		ScanFullAt   string  `json:"scanFullAt"`
		UpdatedAt    string  `json:"updatedAt"`
	} `json:"data"`
	Meta struct {
		TotalPages int `json:"totalPages"`
	} `json:"meta"`
}

// Tickets reads the tickets created between from and to. The scanner filters
// on when a ticket was created, which sits a minute or two before the full
// scan the summary is dated by; the caller pads the range and buckets again.
func (c *Client) Tickets(ctx context.Context, from, to time.Time) ([]Ticket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.ticketsSigned(ctx, ticketFilter{
		field: "createdAt",
		value: "$btw:" + from.Format(time.RFC3339) + "," + to.Format(time.RFC3339),
	})
}

// TicketsUpdatedSince reads the tickets the scanner created or changed at or
// after since. Read every few seconds it is a handful of tickets, where a
// whole day is hundreds at several kilobytes each.
func (c *Client) TicketsUpdatedSince(ctx context.Context, since time.Time) ([]Ticket, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ticketsSigned(ctx, ticketFilter{field: "updatedAt", value: "$gte:" + since.UTC().Format(time.RFC3339Nano)})
}

// ticketFilter is the one condition a ticket read pages through, and the
// column it is sorted by so the pages do not shift while they are read.
type ticketFilter struct {
	field string
	value string
}

func (c *Client) ticketsSigned(ctx context.Context, filter ticketFilter) ([]Ticket, error) {
	tickets, err := c.tickets(ctx, filter)
	if errors.Is(err, errUnauthorized) {
		c.token, c.companyID = "", ""
		tickets, err = c.tickets(ctx, filter)
	}
	return tickets, err
}

func (c *Client) tickets(ctx context.Context, filter ticketFilter) ([]Ticket, error) {
	if c.token == "" {
		if err := c.signIn(ctx); err != nil {
			return nil, err
		}
	}
	first, err := c.ticketPage(ctx, filter, 1)
	if err != nil {
		return nil, err
	}
	pages := []*ticketPage{first}
	total := first.Meta.TotalPages
	if total > ticketPageLimit {
		total = ticketPageLimit
	}
	if total > 1 {
		rest := make([]*ticketPage, total-1)
		errs := make([]error, total-1)
		next := make(chan int)
		var wait sync.WaitGroup
		for worker := 0; worker < ticketWorkers; worker++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				for page := range next {
					rest[page-2], errs[page-2] = c.ticketPage(ctx, filter, page)
				}
			}()
		}
		for page := 2; page <= total; page++ {
			next <- page
		}
		close(next)
		wait.Wait()
		for _, err := range errs {
			if err != nil {
				return nil, err
			}
		}
		pages = append(pages, rest...)
	}

	var tickets []Ticket
	for _, page := range pages {
		for _, row := range page.Data {
			scanned, err := time.Parse(time.RFC3339, row.ScanFullAt)
			if err != nil {
				// A ticket that was never scanned full carried nothing yet.
				continue
			}
			updated, _ := time.Parse(time.RFC3339, row.UpdatedAt)
			tickets = append(tickets, Ticket{
				UpdatedAt:  updated,
				Number:     strings.TrimSpace(row.Number),
				Vehicle:    normalizeVrm(row.VehicleVrm),
				Supplier:   strings.TrimSpace(row.SupplierName),
				Material:   strings.TrimSpace(row.MaterialName),
				Volume:     row.Volume,
				ScanFullAt: scanned,
			})
		}
	}
	return tickets, nil
}

func (c *Client) ticketPage(ctx context.Context, filter ticketFilter, page int) (*ticketPage, error) {
	query := url.Values{}
	query.Set("filter.companyId", c.companyID)
	query.Set("filter."+filter.field, filter.value)
	query.Set("limit", strconv.Itoa(ticketPageSize))
	query.Set("page", strconv.Itoa(page))
	// Sorted, so the pages do not shift under a ticket cut while they are read.
	query.Set("sortBy", filter.field+":ASC")
	var response ticketPage
	if err := c.getQuery(ctx, "/api/v1/tickets", query, &response); err != nil {
		return nil, fmt.Errorf("read tickets page %d: %w", page, err)
	}
	return &response, nil
}

// normalizeVrm folds the spacing a plate was typed with, so "\t E 9374 HB" and
// "E 9374 HB" count as the one truck.
func normalizeVrm(vrm string) string {
	return strings.ToUpper(strings.Join(strings.Fields(vrm), " "))
}

// The dump truck classes, by what one load carries. The scanner records no
// vessel size worth reading, so a truck is classed by the volume it hauled.
const (
	dtkBelow = 14.0 // DTK carries less than 14 m³.
	dttAbove = 20.0 // DTT carries more than 20 m³; DTE is between.
)

// Rest 1 is the fixed midday break the summary reports on.
const (
	rest1Start = 12
	rest1End   = 13
)

// VendorRitase is one vendor's loads over the range.
type VendorRitase struct {
	Name   string
	Ritase int
}

// ArmadaRekap is what one group of trucks hauled: how many trucks, how many
// loads, and how much.
type ArmadaRekap struct {
	Units  int
	Ritase int
	Volume float64
}

// VendorRekap is one vendor's hauling, in total and by truck class.
type VendorRekap struct {
	Name  string
	Total ArmadaRekap
	DTK   ArmadaRekap
	DTE   ArmadaRekap
	DTT   ArmadaRekap
}

// RitaseSummary is the hauling over a range of days, as the site reports it.
// Over more than one day the hours are read as times of day: the operating
// hours run from the earliest load of any day to the latest, and the peak is
// the hour of the day that carried the most across the range.
type RitaseSummary struct {
	From        string
	To          string
	TotalRitase int
	TotalVolume float64
	UnitAktif   int
	DTK         int
	DTE         int
	DTT         int
	Vendors     []VendorRitase
	AvgVolume   float64
	// FirstScan and LastScan bound the operating hours, as HH:MM.
	FirstScan string
	LastScan  string
	// PeakHour is the hour with the most loads, as its starting hour; -1 when
	// nothing was hauled.
	PeakHour   int
	PeakRitase int
	RestStart  int
	RestEnd    int
	RestRitase int
	// Rekap breaks the loads down by vendor and truck class, in the order of
	// Vendors. A truck is classed once, by its average load over the whole
	// range, and counted under every vendor it hauled for; RekapTotal counts
	// each truck once.
	Rekap      []VendorRekap
	RekapTotal ArmadaRekap
}

// Summarize reads the tickets scanned full from one day to another, both
// included, in the site's own time.
func Summarize(tickets []Ticket, from, to string, location *time.Location) RitaseSummary {
	summary := RitaseSummary{From: from, To: to, PeakHour: -1, RestStart: rest1Start, RestEnd: rest1End}
	perHour := make(map[int]int)
	perVehicle := make(map[string][]float64)
	perVendor := make(map[string]int)
	// vendorLoads is each vendor's loads per truck, for the rekap.
	vendorLoads := make(map[string]map[string]*ArmadaRekap)
	first, last := "", ""
	for _, ticket := range tickets {
		scanned := ticket.ScanFullAt.In(location)
		day := scanned.Format("2006-01-02")
		if day < from || day > to {
			continue
		}
		summary.TotalRitase++
		summary.TotalVolume += ticket.Volume
		perHour[scanned.Hour()]++
		perVehicle[ticket.Vehicle] = append(perVehicle[ticket.Vehicle], ticket.Volume)
		vendor := ticket.Supplier
		if vendor == "" {
			vendor = "Tanpa vendor"
		}
		perVendor[vendor]++
		if vendorLoads[vendor] == nil {
			vendorLoads[vendor] = make(map[string]*ArmadaRekap)
		}
		truck := vendorLoads[vendor][ticket.Vehicle]
		if truck == nil {
			truck = &ArmadaRekap{Units: 1}
			vendorLoads[vendor][ticket.Vehicle] = truck
		}
		truck.Ritase++
		truck.Volume += ticket.Volume
		clock := scanned.Format("15:04")
		if first == "" || clock < first {
			first = clock
		}
		if clock > last {
			last = clock
		}
	}
	if summary.TotalRitase == 0 {
		return summary
	}

	summary.TotalVolume = round2(summary.TotalVolume)
	summary.AvgVolume = round2(summary.TotalVolume / float64(summary.TotalRitase))
	summary.FirstScan, summary.LastScan = first, last

	summary.UnitAktif = len(perVehicle)
	class := make(map[string]string, len(perVehicle))
	for vehicle, loads := range perVehicle {
		total := 0.0
		for _, volume := range loads {
			total += volume
		}
		class[vehicle] = classify(total / float64(len(loads)))
		switch class[vehicle] {
		case "DTK":
			summary.DTK++
		case "DTT":
			summary.DTT++
		default:
			summary.DTE++
		}
	}

	for hour := 0; hour < 24; hour++ {
		if perHour[hour] > summary.PeakRitase {
			summary.PeakHour, summary.PeakRitase = hour, perHour[hour]
		}
	}
	for hour := rest1Start; hour < rest1End; hour++ {
		summary.RestRitase += perHour[hour]
	}

	for name, count := range perVendor {
		summary.Vendors = append(summary.Vendors, VendorRitase{Name: name, Ritase: count})
	}
	// Busiest first; a tie reads alphabetically so the order does not jump
	// between refreshes.
	sort.Slice(summary.Vendors, func(i, j int) bool {
		if summary.Vendors[i].Ritase != summary.Vendors[j].Ritase {
			return summary.Vendors[i].Ritase > summary.Vendors[j].Ritase
		}
		return strings.ToLower(summary.Vendors[i].Name) < strings.ToLower(summary.Vendors[j].Name)
	})

	for _, vendor := range summary.Vendors {
		rekap := VendorRekap{Name: vendor.Name}
		for vehicle, truck := range vendorLoads[vendor.Name] {
			var group *ArmadaRekap
			switch class[vehicle] {
			case "DTK":
				group = &rekap.DTK
			case "DTT":
				group = &rekap.DTT
			default:
				group = &rekap.DTE
			}
			for _, into := range []*ArmadaRekap{group, &rekap.Total} {
				into.Units++
				into.Ritase += truck.Ritase
				into.Volume += truck.Volume
			}
		}
		for _, group := range []*ArmadaRekap{&rekap.Total, &rekap.DTK, &rekap.DTE, &rekap.DTT} {
			group.Volume = round2(group.Volume)
		}
		summary.Rekap = append(summary.Rekap, rekap)
	}
	summary.RekapTotal = ArmadaRekap{Units: summary.UnitAktif, Ritase: summary.TotalRitase, Volume: summary.TotalVolume}
	return summary
}

func round2(value float64) float64 {
	if value < 0 {
		return -round2(-value)
	}
	return float64(int64(value*100+0.5)) / 100
}
