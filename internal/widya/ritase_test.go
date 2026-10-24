package widya

import (
	"context"
	"errors"
	"testing"
	"time"
)

var wib = time.FixedZone("WIB", 7*60*60)

func at(day string, clock string) time.Time {
	stamp, err := time.ParseInLocation("2006-01-02 15:04", day+" "+clock, wib)
	if err != nil {
		panic(err)
	}
	return stamp
}

// One day's loads, dated by the full scan in site time: the totals, the trucks
// classed by what they carry, the vendors that hauled, and the busy hour.
func TestSummarizeReadsOneDay(t *testing.T) {
	day := "2026-10-18"
	tickets := []Ticket{
		{Vehicle: "B 1 A", Supplier: "HPP", Volume: 27, ScanFullAt: at(day, "08:06")},
		{Vehicle: "B 1 A", Supplier: "HPP", Volume: 29, ScanFullAt: at(day, "09:10")},
		{Vehicle: "B 2 A", Supplier: "HPP", Volume: 16, ScanFullAt: at(day, "09:20")},
		{Vehicle: "B 3 A", Supplier: "MNP", Volume: 12, ScanFullAt: at(day, "09:59")},
		{Vehicle: "B 3 A", Supplier: "MNP", Volume: 13, ScanFullAt: at(day, "12:30")},
		{Vehicle: "B 2 A", Supplier: "HPP", Volume: 18, ScanFullAt: at(day, "16:36")},
		// Scanned full the next morning: it belongs to that day.
		{Vehicle: "B 4 A", Supplier: "Gosyen", Volume: 30, ScanFullAt: at("2026-10-19", "00:10")},
	}
	summary := Summarize(tickets, day, day, wib)

	if summary.TotalRitase != 6 || summary.TotalVolume != 115 || summary.AvgVolume != 19.17 {
		t.Fatalf("totals = %d rit, %v m³, avg %v", summary.TotalRitase, summary.TotalVolume, summary.AvgVolume)
	}
	// B 1 A averages 28 (DTT), B 2 A 17 (DTE), B 3 A 12.5 (DTK).
	if summary.UnitAktif != 3 || summary.DTT != 1 || summary.DTE != 1 || summary.DTK != 1 {
		t.Fatalf("units = %+v", summary)
	}
	if summary.FirstScan != "08:06" || summary.LastScan != "16:36" {
		t.Fatalf("hours = %s - %s", summary.FirstScan, summary.LastScan)
	}
	if summary.PeakHour != 9 || summary.PeakRitase != 3 || summary.RestRitase != 1 {
		t.Fatalf("peak %d (%d rit), rest %d rit", summary.PeakHour, summary.PeakRitase, summary.RestRitase)
	}
	if len(summary.Vendors) != 2 || summary.Vendors[0].Name != "HPP" || summary.Vendors[0].Ritase != 4 ||
		summary.Vendors[1].Name != "MNP" {
		t.Fatalf("vendors = %+v", summary.Vendors)
	}
}

// The class boundaries: under 14 is DTK, 14 to 20 is DTE, over 20 is DTT.
func TestSummarizeClassesTrucksAtTheBoundaries(t *testing.T) {
	day := "2026-10-18"
	summary := Summarize([]Ticket{
		{Vehicle: "A", Volume: 13.99, ScanFullAt: at(day, "08:00")},
		{Vehicle: "B", Volume: 14, ScanFullAt: at(day, "08:00")},
		{Vehicle: "C", Volume: 20, ScanFullAt: at(day, "08:00")},
		{Vehicle: "D", Volume: 20.01, ScanFullAt: at(day, "08:00")},
	}, day, day, wib)
	if summary.DTK != 1 || summary.DTE != 2 || summary.DTT != 1 {
		t.Fatalf("classes = DTK %d, DTE %d, DTT %d", summary.DTK, summary.DTE, summary.DTT)
	}
}

func TestSummarizeAnEmptyDay(t *testing.T) {
	summary := Summarize(nil, "2026-10-18", "2026-10-18", wib)
	if summary.TotalRitase != 0 || summary.PeakHour != -1 || summary.Vendors != nil {
		t.Fatalf("an empty day = %+v", summary)
	}
}

// Over a range the hours read as times of day: the operating hours run from
// the earliest load of any day to the latest, and a truck working both days
// counts once.
func TestSummarizeReadsARange(t *testing.T) {
	tickets := []Ticket{
		{Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-01", "09:30")},
		{Vehicle: "A", Supplier: "HPP", Volume: 25, ScanFullAt: at("2026-10-02", "07:45")},
		{Vehicle: "B", Supplier: "MNP", Volume: 12, ScanFullAt: at("2026-10-02", "17:10")},
		{Vehicle: "C", Supplier: "MNP", Volume: 12, ScanFullAt: at("2026-10-03", "09:00")},
	}
	summary := Summarize(tickets, "2026-10-01", "2026-10-02", wib)
	if summary.TotalRitase != 3 || summary.UnitAktif != 2 || summary.FirstScan != "07:45" || summary.LastScan != "17:10" {
		t.Fatalf("range = %+v", summary)
	}
}

// Days that have ended are read once and kept; today is read again once per
// interval.
func TestFeedKeepsEndedDaysAndRereadsToday(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &stubSource{tickets: []Ticket{
		{Vehicle: "A", Volume: 25, ScanFullAt: at("2026-10-01", "09:00")},
		{Vehicle: "A", Volume: 25, ScanFullAt: at("2026-10-24", "09:00")},
		{Vehicle: "B", Volume: 12, ScanFullAt: at("2026-10-25", "08:00")},
	}}
	feed := NewFeed(source, 30*time.Second, func() time.Time { return now })

	summary, has, err := feed.Ritase(context.Background(), "2026-10-01", "2026-10-25", wib)
	if err != nil || !has || summary.TotalRitase != 3 {
		t.Fatalf("month = %+v, %v, %v", summary, has, err)
	}
	// One read for the ended days, one for today.
	if source.ticketCalls != 2 {
		t.Fatalf("read tickets %d times, want 2", source.ticketCalls)
	}

	// Inside the interval nothing is read; after it only today is.
	feed.Ritase(context.Background(), "2026-10-01", "2026-10-25", wib)
	now = now.Add(time.Minute)
	summary, _, _ = feed.Ritase(context.Background(), "2026-10-01", "2026-10-25", wib)
	if source.ticketCalls != 3 || summary.TotalRitase != 3 {
		t.Fatalf("read tickets %d times, want 3; ritase %d", source.ticketCalls, summary.TotalRitase)
	}

	// A range of ended days already read costs nothing.
	feed.Ritase(context.Background(), "2026-10-02", "2026-10-20", wib)
	if source.ticketCalls != 3 {
		t.Fatalf("an ended range was read again: %d reads", source.ticketCalls)
	}
}

// When today cannot be read the last reading of it stands, with the error.
func TestFeedKeepsTodayWhenTheScannerIsDown(t *testing.T) {
	now := at("2026-10-25", "10:00")
	source := &stubSource{tickets: []Ticket{{Vehicle: "B", Volume: 12, ScanFullAt: at("2026-10-25", "08:00")}}}
	feed := NewFeed(source, 30*time.Second, func() time.Time { return now })
	feed.Ritase(context.Background(), "2026-10-25", "2026-10-25", wib)

	now = now.Add(time.Minute)
	source.err = errors.New("down")
	summary, has, err := feed.Ritase(context.Background(), "2026-10-25", "2026-10-25", wib)
	if err == nil || !has || summary.TotalRitase != 1 {
		t.Fatalf("an outage lost today: %+v, %v, %v", summary, has, err)
	}
	if _, _, err := feed.Ritase(context.Background(), "25-10-2026", "2026-10-25", wib); err == nil {
		t.Fatal("a malformed date was read")
	}
}
