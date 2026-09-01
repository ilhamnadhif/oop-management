package export

import (
	"bytes"
	"testing"
	"time"

	"opp-management/internal/model"
)

func hourMeterFixtures() []model.HourMeter {
	return []model.HourMeter{
		{
			HMID: "HM-2026-0001", Tanggal: "2026-08-07", Shift: "Shift 1",
			IDUnit: "EXC-01", NamaUnit: "Excavator PC200",
			HMAwal: 1200, HMAkhir: 1208, TotalHM: 8,
			PA: 80, UA: 80,
		},
		{
			HMID: "HM-2026-0002", Tanggal: "2026-08-08", Shift: "Shift 2",
			IDUnit: "EXC-01", NamaUnit: "Excavator PC200",
			HMAwal: 1208, HMAkhir: 1214, TotalHM: 6,
			PA: 75, UA: 62.5,
		},
	}
}

// The hour meter columns must line up with the letterhead rule, the same way
// every other report's do.
func TestHourMeterColumnWidthsFitLandscapeA4(t *testing.T) {
	usable := pageWidth - 2*pageMargin
	total := 0.0
	for _, column := range hourMeterColumns {
		total += column.Width
	}
	if total > usable {
		t.Fatalf("hour meter columns total %.1fmm, wider than the %.1fmm usable page", total, usable)
	}
	if total < usable-6 {
		t.Fatalf("hour meter columns total %.1fmm, leaving %.1fmm of the page empty", total, usable-total)
	}
}

// The readings stay numbers so the spreadsheet can sort and sum them.
func TestHourMeterValuesStayNumeric(t *testing.T) {
	table := HourMeterTable(hourMeterFixtures())
	if _, ok := table.Values[0][2].(float64); !ok {
		t.Fatalf("hm awal exported as %T", table.Values[0][2])
	}
	if _, ok := table.Values[0][5].(float64); !ok {
		t.Fatalf("pa exported as %T", table.Values[0][5])
	}
}

// The report carries the columns the site records: the reading itself and the
// two availability figures, in that order.
func TestHourMeterColumnsNameTheReading(t *testing.T) {
	headers := make([]string, 0, len(hourMeterColumns))
	for _, column := range hourMeterColumns {
		headers = append(headers, column.Header)
	}
	want := []string{"No", "Tanggal", "HM Awal", "HM Akhir", "Total HM", "PA", "UA"}
	for i := range want {
		if headers[i] != want[i] {
			t.Fatalf("column %d = %q, want %q", i, headers[i], want[i])
		}
	}
}

func TestHourMeterRendersBothFormats(t *testing.T) {
	meta := Meta{
		Company: "PT Orecon Putra Perkasa", Title: "Input HM",
		Period: "Agustus 2026", Generated: time.Date(2026, 8, 9, 9, 0, 0, 0, time.UTC),
		Signatory: Signatory{Title: "Direktur"},
	}
	pdf, err := HourMeterPDF(hourMeterFixtures(), meta)
	if err != nil {
		t.Fatalf("render hour meter pdf: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatal("hour meter pdf is not a pdf")
	}
	xlsx, err := HourMeterXLSX(hourMeterFixtures(), meta)
	if err != nil {
		t.Fatalf("render hour meter xlsx: %v", err)
	}
	if !bytes.HasPrefix(xlsx, []byte("PK")) {
		t.Fatal("hour meter xlsx is not a zip")
	}
}

// An empty report still has to print as a document, not as an error.
func TestHourMeterSurvivesNoRows(t *testing.T) {
	if _, err := HourMeterPDF(nil, unitMeta("Input HM")); err != nil {
		t.Fatalf("empty hour meter pdf: %v", err)
	}
	if _, err := HourMeterXLSX(nil, unitMeta("Input HM")); err != nil {
		t.Fatalf("empty hour meter xlsx: %v", err)
	}
}
