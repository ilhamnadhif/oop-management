package widya

import "testing"

// One day, truck by truck: loads in order, the busiest truck first, the gap
// between loads, and the loads that fell in the midday break.
func TestDetailUnitsListsEachTruck(t *testing.T) {
	day := "2026-10-18"
	report := DetailUnits([]Ticket{
		{Vehicle: "AB 8094 ZR", Supplier: "HPP", Volume: 27, ScanFullAt: at(day, "11:31")},
		{Vehicle: "AB 8094 ZR", Supplier: "HPP", Volume: 28, ScanFullAt: at(day, "08:06")},
		{Vehicle: "AB 8094 ZR", Supplier: "HPP", Volume: 28, ScanFullAt: at(day, "12:20")},
		{Vehicle: "AB 8091 ZR", Supplier: "HPP", Volume: 26.77, ScanFullAt: at(day, "08:34")},
		{Vehicle: "B 1 A", Supplier: "MNP", Volume: 12, ScanFullAt: at(day, "16:36")},
		{Vehicle: "B 9 A", Supplier: "MNP", Volume: 12, ScanFullAt: at("2026-10-19", "08:00")},
	}, day, wib)

	if report.Units != 3 || report.Ritase != 5 || report.MaxRitase != 3 ||
		report.FirstScan != "08:06" || report.LastScan != "16:36" {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Vendors) != 2 || report.Vendors[0].Name != "HPP" || report.Vendors[0].Ritase != 4 {
		t.Fatalf("vendors = %+v", report.Vendors)
	}
	busiest := report.Vendors[0].Units[0]
	if busiest.Vehicle != "AB 8094 ZR" || busiest.Class != "DTT" || busiest.Volume != 83 {
		t.Fatalf("busiest = %+v", busiest)
	}
	if got := busiest.Times; len(got) != 3 || got[0] != "08:06" || got[1] != "11:31" || got[2] != "12:20" {
		t.Fatalf("times = %v", got)
	}
	// 08:06 to 12:20 is 254 minutes over two gaps.
	if busiest.IntervalMinutes != 127 || !busiest.RestLoads[2] || busiest.RestLoads[0] {
		t.Fatalf("interval %d, rest %v", busiest.IntervalMinutes, busiest.RestLoads)
	}
	if loads := busiest.Loads; len(loads) != 3 || loads[0] != (LoadDetail{Clock: "08:06:00", Volume: 28}) || loads[1].Volume != 27 {
		t.Fatalf("loads = %+v", loads)
	}
	if single := report.Vendors[0].Units[1]; single.IntervalMinutes != -1 || single.Ritase != 1 {
		t.Fatalf("a single load = %+v", single)
	}
	if mnp := report.Vendors[1]; mnp.Units[0].Class != "DTK" || mnp.FirstScan != "16:36" {
		t.Fatalf("mnp = %+v", mnp)
	}
}
