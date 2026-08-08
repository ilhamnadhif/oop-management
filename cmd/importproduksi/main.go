// Command importproduksi loads the historical production backlog from
// Book1.xlsx (converted to produksi.tsv) into the Produksi sheet.
//
// It writes the rows as recorded rather than recomputing them. These are past
// figures that were already reported; recalculating them from today's Unit DT
// dimensions would silently restate history. Kategori and Lokasi stay empty
// because the source has neither, and Layer is set to L1 throughout.
//
// Nothing is written without -apply, and the Produksi sheet must be empty
// unless -force is passed, so a second run cannot quietly duplicate 3870 rows.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/config"
	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/service"
)

// defaultSourceFile is read from disk, never embedded: it holds every haul this
// company recorded, and this repository is public.
const defaultSourceFile = "cmd/importproduksi/produksi.tsv"

// seedLayer is what every imported row gets: the source has no layer at all.
const seedLayer = "L1"

const importedBy = "Import Book1.xlsx"

// indonesianMonths maps the month names the source workbook uses.
var indonesianMonths = map[string]time.Month{
	"januari": time.January, "februari": time.February, "maret": time.March,
	"april": time.April, "mei": time.May, "juni": time.June,
	"juli": time.July, "agustus": time.August, "september": time.September,
	"oktober": time.October, "november": time.November, "desember": time.December,
}

type importRow struct {
	line     int
	tanggal  string
	project  string
	supplier string
	quary    string
	nopol    string
	panjang  float64
	lebar    float64
	tinggi   float64
	tt       float64
	tf       float64
	volume   float64
	opp      float64
	jenisDT  string
}

func main() {
	apply := flag.Bool("apply", false, "write the rows to the sheet; without it nothing is written")
	force := flag.Bool("force", false, "allow importing even when the Produksi sheet already has rows")
	path := flag.String("file", defaultSourceFile, "TSV converted from the source workbook")
	flag.Parse()

	rows, err := readRows(*path)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("%d rows read from %s", len(rows), *path)

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	sheetService, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.GoogleCredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		log.Fatal(err)
	}
	store := repository.NewGoogleSheetsRepository(sheetService, cfg.GoogleSpreadsheetID, cfg.Timezone)
	if err := store.EnsureSchema(ctx); err != nil {
		log.Fatal(err)
	}

	units, err := store.ListUnitDT(ctx)
	if err != nil {
		log.Fatal(err)
	}
	byNopol := make(map[string]model.UnitDT, len(units))
	for _, unit := range units {
		byNopol[strings.ToUpper(strings.TrimSpace(unit.Nopol))] = unit
	}
	log.Printf("%d units in the register", len(byNopol))

	now := time.Now().In(cfg.Timezone)
	highest, err := store.MaxProduksiSequence(ctx, fmt.Sprintf("PRD-%04d-", now.Year()))
	if err != nil {
		log.Fatal(err)
	}
	if highest > 0 && !*force {
		log.Fatalf("Produksi already holds rows up to PRD-%04d-%04d; re-run with -force only if you mean to add more", now.Year(), highest)
	}

	prepared := make([]*model.Produksi, 0, len(rows))
	var unknown, invalid int
	for _, row := range rows {
		nopol, err := service.NormalizeNopol(row.nopol)
		if err != nil {
			log.Printf("line %d %-13s INVALID nopol: %v", row.line, row.nopol, err)
			invalid++
			continue
		}
		unit, ok := byNopol[nopol]
		if !ok {
			log.Printf("line %d %-13s not in Unit DT, skipped", row.line, nopol)
			unknown++
			continue
		}
		tanggal, err := parseIndonesianDate(row.tanggal)
		if err != nil {
			log.Printf("line %d %-13s %v", row.line, nopol, err)
			invalid++
			continue
		}

		highest++
		prepared = append(prepared, &model.Produksi{
			ProduksiID: fmt.Sprintf("PRD-%04d-%04d", now.Year(), highest),
			Tanggal:    tanggal,
			Project:    row.project,
			Supplier:   row.supplier,
			Quary:      row.quary,
			Kategori:   "",
			Lokasi:     "",
			Layer:      seedLayer,
			UnitID:     unit.UnitID,
			Nopol:      nopol,
			Driver:     unit.Driver,
			JenisDT:    row.jenisDT,
			Panjang:    row.panjang,
			Lebar:      row.lebar,
			Tinggi:     row.tinggi,
			TT:         row.tt,
			TF:         row.tf,
			Volume:     row.volume,
			VolumeOPP:  row.opp,
			Deviasi:    round4(row.volume - row.opp),
			CreatedBy:  importedBy,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	log.Printf("ready: %d rows, skipped %d unknown plates, %d invalid", len(prepared), unknown, invalid)
	if len(prepared) > 0 {
		first, last := prepared[0], prepared[len(prepared)-1]
		log.Printf("id range %s .. %s, dates %s .. %s", first.ProduksiID, last.ProduksiID, first.Tanggal, last.Tanggal)
		log.Printf("total volume %.2f m3", totalVolume(prepared))
	}
	if !*apply {
		log.Print("nothing was written; re-run with -apply to write to the sheet")
		return
	}
	if err := store.CreateProduksiBatch(ctx, prepared); err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d rows to the Produksi sheet", len(prepared))
}

func totalVolume(rows []*model.Produksi) float64 {
	var total float64
	for _, row := range rows {
		total += row.Volume
	}
	return total
}

func round4(value float64) float64 { return math.Round(value*10_000) / 10_000 }

// parseIndonesianDate turns "09 Juni 2026" into "2026-06-09".
func parseIndonesianDate(value string) (string, error) {
	fields := strings.Fields(value)
	if len(fields) != 3 {
		return "", fmt.Errorf("unrecognised date %q", value)
	}
	day, err := strconv.Atoi(fields[0])
	if err != nil {
		return "", fmt.Errorf("unrecognised day in %q", value)
	}
	month, ok := indonesianMonths[strings.ToLower(fields[1])]
	if !ok {
		return "", fmt.Errorf("unrecognised month in %q", value)
	}
	year, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", fmt.Errorf("unrecognised year in %q", value)
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, int(month), day), nil
}

func readRows(path string) ([]importRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rows []importRow
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), "\r")
		if lineNumber == 1 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 13 {
			return nil, fmt.Errorf("line %d: expected 13 columns, got %d", lineNumber, len(fields))
		}
		numbers := make([]float64, 8)
		for i, at := range []int{5, 6, 7, 8, 9, 10, 11} {
			value, err := strconv.ParseFloat(strings.TrimSpace(fields[at]), 64)
			if err != nil {
				return nil, fmt.Errorf("line %d: column %d is not a number: %q", lineNumber, at+1, fields[at])
			}
			numbers[i] = value
		}
		rows = append(rows, importRow{
			line:     lineNumber,
			tanggal:  strings.TrimSpace(fields[0]),
			project:  strings.TrimSpace(fields[1]),
			supplier: strings.TrimSpace(fields[2]),
			quary:    strings.TrimSpace(fields[3]),
			nopol:    strings.TrimSpace(fields[4]),
			panjang:  numbers[0],
			lebar:    numbers[1],
			tinggi:   numbers[2],
			tt:       numbers[3],
			tf:       numbers[4],
			volume:   numbers[5],
			opp:      numbers[6],
			jenisDT:  strings.TrimSpace(fields[12]),
		})
	}
	return rows, scanner.Err()
}
