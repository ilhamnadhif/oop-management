// Command seedunitdt loads the initial dump truck register into the Unit DT
// sheet.
//
// It goes through the same service the web form uses, so every row is validated
// the same way, IDs continue from whatever is already in the sheet, and a plate
// that already exists is skipped rather than duplicated. Re-running it is safe.
//
// It refuses to write unless -apply is passed, so the default run only reports
// what it would do.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/config"
	"opp-management/internal/model"
	"opp-management/internal/repository"
	"opp-management/internal/service"
)

// defaultSeedFile is read from disk rather than embedded. The list carries
// plate numbers and driver names, and this repository is public, so the data
// must not travel inside the binary or the git history.
const defaultSeedFile = "cmd/seedunitdt/units.tsv"

type seedRow struct {
	line  int
	input service.UnitDTInput
}

func main() {
	apply := flag.Bool("apply", false, "write the rows to the sheet; without it nothing is written")
	path := flag.String("file", defaultSeedFile, "TSV holding the units to seed")
	flag.Parse()

	rows, err := readSeed(*path)
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

	existing, err := store.ListUnitDT(ctx)
	if err != nil {
		log.Fatal(err)
	}
	known := make(map[string]bool, len(existing))
	for _, unit := range existing {
		known[strings.ToUpper(strings.TrimSpace(unit.Nopol))] = true
	}
	log.Printf("%d units already in the sheet", len(existing))

	now := func() time.Time { return time.Now().In(cfg.Timezone) }
	unitService := service.NewUnitDTService(store, cfg.Timezone, now)
	seeder := &model.User{UserID: "seed", NamaLengkap: "Seed Unit DT", StatusPengguna: model.StatusAktif}

	var created, skipped, failed int
	for _, row := range rows {
		nopol, err := service.NormalizeNopol(row.input.Nopol)
		if err != nil {
			log.Printf("line %d %-12s INVALID: %v", row.line, row.input.Nopol, err)
			failed++
			continue
		}
		if known[nopol] {
			log.Printf("line %d %-12s skipped, already registered", row.line, nopol)
			skipped++
			continue
		}
		if !*apply {
			log.Printf("line %d %-12s would be created", row.line, nopol)
			created++
			continue
		}
		unit, err := unitService.Create(ctx, seeder, row.input)
		if err != nil {
			if errors.Is(err, service.ErrDuplicateUnitDT) {
				log.Printf("line %d %-12s skipped, already registered", row.line, nopol)
				skipped++
				continue
			}
			log.Printf("line %d %-12s FAILED: %v", row.line, nopol, err)
			failed++
			continue
		}
		known[nopol] = true
		log.Printf("line %d %-12s created as %s", row.line, unit.Nopol, unit.UnitID)
		created++
	}

	verb := "would create"
	if *apply {
		verb = "created"
	}
	log.Printf("done: %s %d, skipped %d, failed %d", verb, created, skipped, failed)
	if !*apply {
		log.Print("nothing was written; re-run with -apply to write to the sheet")
	}
	if failed > 0 {
		os.Exit(1)
	}
}

func readSeed(path string) ([]seedRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rows []seedRow
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimRight(scanner.Text(), "\r")
		if lineNumber == 1 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("line %d: expected 6 columns, got %d", lineNumber, len(fields))
		}
		rows = append(rows, seedRow{
			line: lineNumber,
			input: service.UnitDTInput{
				Nopol:      strings.TrimSpace(fields[0]),
				Panjang:    strings.TrimSpace(fields[1]),
				Lebar:      strings.TrimSpace(fields[2]),
				Tinggi:     strings.TrimSpace(fields[3]),
				Driver:     strings.TrimSpace(fields[4]),
				Keterangan: strings.TrimSpace(fields[5]),
			},
		})
	}
	return rows, scanner.Err()
}
