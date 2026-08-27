package export

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"opp-management/internal/service"
)

func sampleMonthlyAbsensi() *service.MonthlyAbsensi {
	marks := make([]string, 31)
	marks[2], marks[3], marks[4], marks[5], marks[6] = "✓", "✓", "✓", "✓", "✓"
	return &service.MonthlyAbsensi{
		Month:   "2026-08",
		Jabatan: "",
		Days:    31,
		Rows: []service.MonthlyAbsensiRow{
			{
				No: 1, Nama: "Budi Hartono", Jabatan: "Surveyor",
				Hari: marks, M1: 5, Hadir: 5, TidakAbsen: 16, Persentase: 23.81,
			},
		},
	}
}

func sampleAbsensiMeta() Meta {
	return Meta{
		Company:   "PT Orecon Putra Perkasa",
		Title:     "Rekap Absensi Bulanan",
		Period:    "Agustus 2026",
		Generated: time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC),
		Signatory: Signatory{Name: "Budi Hartono", Title: "HRD", Place: "Balikpapan"},
	}
}

// The column layout: No, Nama, Jabatan, one column per day of the month, then
// the ten total columns.
const absensiTotalColumns = 10

func TestAbsensiTableCoversEveryDayAndTotal(t *testing.T) {
	report := sampleMonthlyAbsensi()
	table := AbsensiTable(report)

	wantColumns := 3 + report.Days + absensiTotalColumns
	if len(table.Columns) != wantColumns {
		t.Fatalf("columns = %d, want %d", len(table.Columns), wantColumns)
	}
	if table.Columns[0].Header != "No" || table.Columns[1].Header != "Nama" || table.Columns[2].Header != "Jabatan" {
		t.Fatalf("leading headers = %q/%q/%q", table.Columns[0].Header, table.Columns[1].Header, table.Columns[2].Header)
	}
	if table.Columns[3].Header != "1" || table.Columns[len(table.Columns)-absensiTotalColumns-1].Header != "31" {
		t.Fatalf("day headers = %q..%q, want 1..31",
			table.Columns[3].Header, table.Columns[len(table.Columns)-absensiTotalColumns-1].Header)
	}
	tails := []string{"Total M1", "Total M2", "Total M3", "Total M4", "Total Kehadiran",
		"Total Sakit", "Total Izin", "Total Cuti", "Total Tidak absen", "Presentase"}
	for i, want := range tails {
		index := len(table.Columns) - absensiTotalColumns + i
		if table.Columns[index].Header != want {
			t.Fatalf("total column %d = %q, want %q", i, table.Columns[index].Header, want)
		}
	}
	// The filter is only on the identity columns, not on every day of the month.
	if table.FilterColumns != 3 {
		t.Fatalf("filter columns = %d, want 3", table.FilterColumns)
	}

	if len(table.Rows) != 1 || len(table.Rows[0]) != len(table.Columns) {
		t.Fatalf("rows = %d x %d, want 1 x %d", len(table.Rows), len(table.Rows[0]), len(table.Columns))
	}
	row := table.Values[0]
	if row[0] != 1 || row[1] != "Budi Hartono" || row[2] != "Surveyor" {
		t.Fatalf("row identity = %v", row[:3])
	}
	// The first two days (Saturday and Sunday) are blank; the check lands on
	// the third, which is where the sample puts it.
	if row[3] != "" || row[5] != "✓" {
		t.Fatalf("row marks wrong: day1=%v day3=%v", row[3], row[5])
	}
	// The percentage is a live formula, not the static number.
	if _, ok := row[len(row)-1].(Formula); !ok {
		t.Fatalf("presentase = %T, want a Formula", row[len(row)-1])
	}
	if got := table.Rows[0][len(table.Rows[0])-1]; got != "23.8%" {
		t.Fatalf("presentase row string = %q, want 23.8%%", got)
	}
}

func TestAbsensiXLSXCarriesTheMatrix(t *testing.T) {
	payload, err := AbsensiXLSX(sampleMonthlyAbsensi(), sampleAbsensiMeta())
	if err != nil {
		t.Fatalf("render xlsx: %v", err)
	}

	file, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("reopen xlsx: %v", err)
	}
	defer file.Close()

	if sheets := file.GetSheetList(); len(sheets) != 1 || sheets[0] != "Absensi" {
		t.Fatalf("sheets = %v, want only Absensi", sheets)
	}

	company, _ := file.GetCellValue("Absensi", "A1")
	if company != "PT Orecon Putra Perkasa" {
		t.Fatalf("letterhead = %q", company)
	}
	// Header row 5 names the first day column and the last total column.
	firstDay, _ := file.GetCellValue("Absensi", "D5")
	if firstDay != "1" {
		t.Fatalf("first day header = %q, want 1", firstDay)
	}
	lastTotal, _ := file.GetCellValue("Absensi", "AR5")
	if lastTotal != "Presentase" {
		t.Fatalf("last total header = %q, want Presentase", lastTotal)
	}
	check, _ := file.GetCellValue("Absensi", "F6")
	if check != "✓" {
		t.Fatalf("day cell = %q, want the hadir check", check)
	}
	// A count reads as a whole number, not "5.00".
	if total, _ := file.GetCellValue("Absensi", "AI6"); total != "5" {
		t.Fatalf("Total M1 = %q, want a whole 5 without decimals", total)
	}
	// The percentage is stored as a live formula: hadir plus every leave over
	// the days that were owed, read off the totals columns.
	formula, err := file.GetCellFormula("Absensi", "AR6")
	if err != nil {
		t.Fatalf("read presentase formula: %v", err)
	}
	if formula != "IF(AM6+AN6+AO6+AP6+AQ6=0,0,(AM6+AN6+AO6+AP6)/(AM6+AN6+AO6+AP6+AQ6))" {
		t.Fatalf("presentase formula = %q", formula)
	}
	// The autofilter stays on the identity columns only.
	sheetXML := absensiSheetXML(t, payload)
	if !strings.Contains(sheetXML, `autoFilter ref="$A$5:$C$6"`) {
		t.Fatalf("autofilter does not stop at the identity columns: %s", sheetXML)
	}
}

func absensiSheetXML(t *testing.T, payload []byte) string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(payload), int64(len(payload)))
	if err != nil {
		t.Fatalf("open xlsx zip: %v", err)
	}
	for _, file := range reader.File {
		if !strings.HasPrefix(file.Name, "xl/worksheets/") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatalf("open sheet xml: %v", err)
		}
		defer rc.Close()
		data, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read sheet xml: %v", err)
		}
		return string(data)
	}
	t.Fatal("no worksheet part found")
	return ""
}

func TestAbsensiXLSXHandlesNoRows(t *testing.T) {
	report := &service.MonthlyAbsensi{Month: "2026-08", Days: 31}
	payload, err := AbsensiXLSX(report, sampleAbsensiMeta())
	if err != nil {
		t.Fatalf("render empty xlsx: %v", err)
	}
	file, err := excelize.OpenReader(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("reopen empty xlsx: %v", err)
	}
	defer file.Close()
	if header, _ := file.GetCellValue("Absensi", "D5"); header != "1" {
		t.Fatalf("an empty report lost its day columns: %q", header)
	}
}

// The month controls how wide the sheet is, so a 30-day month must not print a
// phantom 31st column.
func TestAbsensiTableFollowsTheMonthLength(t *testing.T) {
	report := &service.MonthlyAbsensi{Month: "2026-06", Days: 30}
	table := AbsensiTable(report)
	if len(table.Columns) != 3+30+absensiTotalColumns {
		t.Fatalf("columns = %d, want %d for a 30 day month", len(table.Columns), 3+30+absensiTotalColumns)
	}
	if table.Columns[len(table.Columns)-absensiTotalColumns-1].Header != "30" {
		t.Fatalf("last day header = %q, want 30",
			table.Columns[len(table.Columns)-absensiTotalColumns-1].Header)
	}
}
