// Package export renders report files from sheet data.
//
// Two formats, because they answer different needs: XLSX keeps every column and
// stays workable in a spreadsheet, while PDF is the fixed, signable document -
// landscape, because the production table has twenty columns and portrait
// cannot hold them legibly.
package export

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Signatory is the block printed at the end of a PDF. Every field may be empty:
// an unnamed signature line is printed rather than a guessed name, because a
// wrong name on a signed document is worse than a blank one.
type Signatory struct {
	Name  string
	Title string
	Place string
}

// Meta is the letterhead: what the report is, which period it covers and who
// signs it.
type Meta struct {
	Company   string
	Title     string
	Period    string
	Generated time.Time
	Logo      []byte
	Signatory Signatory
}

// Column describes one column of a report.
type Column struct {
	Header  string
	Width   float64 // millimetres, PDF only
	Numeric bool
	// Decimals is how many the PDF prints; the XLSX keeps the raw number and
	// carries the format instead, so the value stays usable in a formula.
	Decimals int
	// Money prints the PDF cell the way rupiah is written here: thousands
	// separated by dots. Six unbroken digits are hard to read at 6pt, and a
	// misread figure on an expense report is an argument about money.
	Money bool
	// Percent prints the spreadsheet cell as a percentage (23.8 rather than
	// 0.238), which is how a rate reads when someone looks at the file. The
	// value is still stored as the raw fraction, so it stays usable in a
	// spreadsheet formula.
	Percent bool
}

// Formula is a spreadsheet cell that is computed rather than stored. The PDF
// prints the row's formatted string; the XLSX keeps the expression so the
// number stays live when the sheet underneath it changes.
type Formula struct {
	Expression string
}

func (m Meta) periodLabel() string {
	if strings.TrimSpace(m.Period) == "" {
		return "Seluruh periode"
	}
	return m.Period
}

// SnapshotLabel names the moment a register was taken. A register has no
// period the way a production report does; what matters is when it was read.
func SnapshotLabel(at time.Time) string {
	return "Data per " + formatIndonesianDate(at)
}

// signedOn renders "9 Agustus 2026", prefixed with a place when one is set.
func (m Meta) signedOn() string {
	date := formatIndonesianDate(m.Generated)
	if place := strings.TrimSpace(m.Signatory.Place); place != "" {
		return place + ", " + date
	}
	return date
}

var indonesianMonths = [...]string{
	"Januari", "Februari", "Maret", "April", "Mei", "Juni",
	"Juli", "Agustus", "September", "Oktober", "November", "Desember",
}

func formatIndonesianDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d %s %d", value.Day(), indonesianMonths[int(value.Month())-1], value.Year())
}

// FormatFloat trims trailing zeros so a whole number does not read as 7.5000.
// FormatMoney writes 110000 as "110.000". Whole rupiah only: the decimals are
// always zero in practice and would cost width the columns do not have.
func FormatMoney(value float64) string {
	whole := int64(math.Round(value))
	sign := ""
	if whole < 0 {
		sign, whole = "-", -whole
	}
	digits := strconv.FormatInt(whole, 10)
	var grouped strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			grouped.WriteByte('.')
		}
		grouped.WriteRune(digit)
	}
	return sign + grouped.String()
}

// FormatCell renders a numeric cell the way its column asks for.
func FormatCell(value float64, column Column) string {
	if column.Money {
		return FormatMoney(value)
	}
	return FormatFloat(value, column.Decimals)
}

func FormatFloat(value float64, decimals int) string {
	text := fmt.Sprintf("%.*f", decimals, value)
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	return text
}
