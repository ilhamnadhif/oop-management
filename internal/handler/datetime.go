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

// formatShortIndonesianDate renders "7 Agu 2026". The full date does not fit
// a phone header beside the menu button and the account avatar, and a date cut
// off mid-month reads as a rendering fault.
func formatShortIndonesianDate(value time.Time) string {
	month := indonesianMonths[value.Month()]
	if len(month) > 3 {
		month = month[:3]
	}
	return fmt.Sprintf("%d %s %d", value.Day(), month, value.Year())
}

// formatIndonesianDate renders "Jumat, 7 Agustus 2026".
func formatIndonesianDate(value time.Time) string {
	return fmt.Sprintf("%s, %d %s %d",
		indonesianDays[value.Weekday()], value.Day(), indonesianMonths[value.Month()], value.Year())
}
