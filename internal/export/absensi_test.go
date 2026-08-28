package export

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
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

// The PDF has to fit forty-odd columns on one landscape page, whatever the
// month's length, so its widths must never exceed the usable width.
func TestAbsensiPDFFitsTheLandscapePage(t *testing.T) {
	for _, days := range []int{28, 30, 31} {
		report := &service.MonthlyAbsensi{Month: "2026-08", Days: days}
		if total := absensiPDFTable(report).totalWidth(); total > pageWidth-2*pageMargin {
			t.Fatalf("%d days: PDF columns total %.1fmm, wider than the %.1fmm usable page",
				days, total, pageWidth-2*pageMargin)
		}
	}
}

// The attendance matrix prints compact and unsigned: it is a data sheet, not a
// signed document, and the check for hadir prints as "v" because the core PDF
// fonts cannot draw the check mark.
func TestAbsensiPDFIsCompactLandscapeAndUnsigned(t *testing.T) {
	report := sampleMonthlyAbsensi()
	payload, err := AbsensiPDF(report, sampleAbsensiMeta())
	if err != nil {
		t.Fatalf("render pdf: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte("%PDF-")) {
		t.Fatal("the output is not a PDF")
	}
	if !strings.Contains(string(payload), "841.89 595.28") {
		t.Fatal("the page is not landscape A4")
	}

	text := pdfText(t, payload)
	if !strings.Contains(text, "Rekap Absensi Bulanan") {
		t.Fatal("the letterhead did not print")
	}
	if !strings.Contains(text, "(v)Tj") {
		t.Fatal("the present-day check did not print as v")
	}
	if !strings.Contains(text, "(23.8%)Tj") {
		t.Fatal("the percentage cell did not print")
	}
	if strings.Contains(text, "Tertanda,") {
		t.Fatal("an unsigned data sheet must not carry a signature block")
	}

	// The same layout, rendered signed, does carry the signature - proving the
	// unsigned PDF is unsigned by choice, not because the renderer lost it.
	signed, err := RenderPDF(absensiPDFTable(report), sampleAbsensiMeta())
	if err != nil {
		t.Fatalf("render signed pdf: %v", err)
	}
	if !strings.Contains(pdfText(t, signed), "Tertanda,") {
		t.Fatal("the signed render lost its signature block")
	}
}

func TestPDFCheckmarkPrintsAsV(t *testing.T) {
	if got := tr("✓"); got != "v" {
		t.Fatalf("tr(✓) = %q, want v", got)
	}
}

// pdfText decompresses the PDF content streams so a test can read what was
// actually printed. The text itself is zlib-compressed, so searching the raw
// file for a word would miss it.
func pdfText(t *testing.T, payload []byte) string {
	t.Helper()
	var out strings.Builder
	for _, stream := range pdfStreams(payload) {
		reader, err := zlib.NewReader(bytes.NewReader(stream))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			continue
		}
		out.Write(data)
	}
	return out.String()
}

func pdfStreams(payload []byte) [][]byte {
	var streams [][]byte
	pos := 0
	for {
		i := bytes.Index(payload[pos:], []byte("stream"))
		if i < 0 {
			break
		}
		i += pos
		after := i + len("stream")
		for after < len(payload) && (payload[after] == '\r' || payload[after] == '\n') {
			after++
		}
		end := bytes.Index(payload[after:], []byte("endstream"))
		if end < 0 {
			break
		}
		streams = append(streams, payload[after:after+end])
		pos = after + end
	}
	return streams
}
