package handler

import (
	"fmt"
	"time"
)

// emptyClock is what a clock card shows before that action is recorded.
const emptyClock = "--:--"

// Go's time package has no localisation, so the Indonesian day and month names
// are spelled out here.
var indonesianDays = map[time.Weekday]string{
	time.Sunday:    "Minggu",
	time.Monday:    "Senin",
	time.Tuesday:   "Selasa",
	time.Wednesday: "Rabu",
	time.Thursday:  "Kamis",
	time.Friday:    "Jumat",
	time.Saturday:  "Sabtu",
}

var indonesianMonths = map[time.Month]string{
	time.January:   "Januari",
	time.February:  "Februari",
	time.March:     "Maret",
	time.April:     "April",
	time.May:       "Mei",
	time.June:      "Juni",
	time.July:      "Juli",
	time.August:    "Agustus",
	time.September: "September",
	time.October:   "Oktober",
	time.November:  "November",
	time.December:  "Desember",
}

// formatIndonesianDate renders "Jumat, 7 Agustus 2026".
func formatIndonesianDate(value time.Time) string {
	return fmt.Sprintf("%s, %d %s %d",
		indonesianDays[value.Weekday()], value.Day(), indonesianMonths[value.Month()], value.Year())
}
