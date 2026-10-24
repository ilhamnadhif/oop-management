package widya

import (
	"math"
	"sort"
	"strings"
	"time"
)

// LoadDetail is one load: when it was scanned full, to the second, and what
// it carried.
type LoadDetail struct {
	Clock  string
	Volume float64
}

// UnitDetail is one truck's day: its loads in the order they were scanned
// full, and the time between them.
type UnitDetail struct {
	Vehicle string
	Class   string
	Ritase  int
	Volume  float64
	// Times are the full scans as HH:MM, first to last.
	Times []string
	// RestLoads marks which of Times fall in the midday break.
	RestLoads []bool
	// IntervalMinutes is the average gap between loads; -1 with one load.
	IntervalMinutes int
	// Loads are the loads themselves, in the order of Times.
	Loads []LoadDetail
}

// VendorDetail is one vendor's trucks on the day.
type VendorDetail struct {
	Name      string
	Units     []UnitDetail
	Ritase    int
	Volume    float64
	FirstScan string
	LastScan  string
}

// UnitDetailReport is one day, vendor by vendor and truck by truck.
type UnitDetailReport struct {
	Day       string
	Vendors   []VendorDetail
	Units     int
	Ritase    int
	Volume    float64
	FirstScan string
	LastScan  string
	MaxRitase int
	RestStart int
	RestEnd   int
}

// DetailUnits lays one day's loads out by vendor and truck. A truck is classed
// by its average load that day, the same rule the summary uses; a truck that
// hauled for two vendors is listed under each, with the loads it carried for
// that vendor.
func DetailUnits(tickets []Ticket, day string, location *time.Location) UnitDetailReport {
	report := UnitDetailReport{Day: day, RestStart: rest1Start, RestEnd: rest1End}
	type load struct {
		at     time.Time
		volume float64
	}
	type truckDay struct {
		loads  []load
		volume float64
	}
	perVendor := make(map[string]map[string]*truckDay)
	perVehicle := make(map[string][]float64)
	for _, ticket := range tickets {
		scanned := ticket.ScanFullAt.In(location)
		if scanned.Format("2006-01-02") != day {
			continue
		}
		vendor := ticket.Supplier
		if vendor == "" {
			vendor = "Tanpa vendor"
		}
		if perVendor[vendor] == nil {
			perVendor[vendor] = make(map[string]*truckDay)
		}
		truck := perVendor[vendor][ticket.Vehicle]
		if truck == nil {
			truck = &truckDay{}
			perVendor[vendor][ticket.Vehicle] = truck
		}
		truck.loads = append(truck.loads, load{at: scanned, volume: ticket.Volume})
		truck.volume += ticket.Volume
		perVehicle[ticket.Vehicle] = append(perVehicle[ticket.Vehicle], ticket.Volume)
		report.Ritase++
		report.Volume += ticket.Volume
	}
	report.Units = len(perVehicle)
	report.Volume = round2(report.Volume)

	class := make(map[string]string, len(perVehicle))
	for vehicle, loads := range perVehicle {
		total := 0.0
		for _, volume := range loads {
			total += volume
		}
		class[vehicle] = classify(total / float64(len(loads)))
	}

	for name, trucks := range perVendor {
		vendor := VendorDetail{Name: name}
		var first, last time.Time
		for vehicle, truck := range trucks {
			sort.Slice(truck.loads, func(i, j int) bool { return truck.loads[i].at.Before(truck.loads[j].at) })
			times := make([]time.Time, len(truck.loads))
			for i, each := range truck.loads {
				times[i] = each.at
			}
			unit := UnitDetail{
				Vehicle:         vehicle,
				Class:           class[vehicle],
				Ritase:          len(times),
				Volume:          round2(truck.volume),
				IntervalMinutes: -1,
			}
			for i, scanned := range times {
				unit.Loads = append(unit.Loads, LoadDetail{Clock: scanned.Format("15:04:05"), Volume: round2(truck.loads[i].volume)})
				unit.Times = append(unit.Times, scanned.Format("15:04"))
				unit.RestLoads = append(unit.RestLoads, scanned.Hour() >= rest1Start && scanned.Hour() < rest1End)
			}
			if unit.Ritase > 1 {
				span := times[len(times)-1].Sub(times[0]).Minutes()
				unit.IntervalMinutes = int(math.Round(span / float64(unit.Ritase-1)))
			}
			if first.IsZero() || times[0].Before(first) {
				first = times[0]
			}
			if lastLoad := times[len(times)-1]; lastLoad.After(last) {
				last = lastLoad
			}
			if unit.Ritase > report.MaxRitase {
				report.MaxRitase = unit.Ritase
			}
			vendor.Ritase += unit.Ritase
			vendor.Volume += truck.volume
			vendor.Units = append(vendor.Units, unit)
		}
		vendor.Volume = round2(vendor.Volume)
		vendor.FirstScan, vendor.LastScan = first.Format("15:04"), last.Format("15:04")
		// The trucks that carried the most first, the way the sheet ranks them.
		sort.Slice(vendor.Units, func(i, j int) bool {
			a, b := vendor.Units[i], vendor.Units[j]
			if a.Ritase != b.Ritase {
				return a.Ritase > b.Ritase
			}
			if a.Volume != b.Volume {
				return a.Volume > b.Volume
			}
			return a.Vehicle < b.Vehicle
		})
		report.Vendors = append(report.Vendors, vendor)
		if report.FirstScan == "" || vendor.FirstScan < report.FirstScan {
			report.FirstScan = vendor.FirstScan
		}
		if vendor.LastScan > report.LastScan {
			report.LastScan = vendor.LastScan
		}
	}
	sort.Slice(report.Vendors, func(i, j int) bool {
		if report.Vendors[i].Ritase != report.Vendors[j].Ritase {
			return report.Vendors[i].Ritase > report.Vendors[j].Ritase
		}
		return strings.ToLower(report.Vendors[i].Name) < strings.ToLower(report.Vendors[j].Name)
	})
	return report
}

// classify names a truck's class from its average load.
func classify(average float64) string {
	switch {
	case average < dtkBelow:
		return "DTK"
	case average > dttAbove:
		return "DTT"
	default:
		return "DTE"
	}
}
