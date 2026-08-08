// Command seedunita2b loads the initial A2B unit register into the Unit A2B
// sheet.
//
// It goes through the same service the web form uses, so every row is validated
// the same way, running numbers continue from whatever is already in the sheet,
// and an identifier that already exists is skipped rather than duplicated.
// Re-running it is safe.
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

// defaultSeedFile is read from disk rather than embedded: this repository is
// public and the register is operational data.
const defaultSeedFile = "cmd/seedunita2b/units.tsv"

type seedRow struct {
	line  int
	input service.UnitA2BInput
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

	now := func() time.Time { return time.Now().In(cfg.Timezone) }
	unitService := service.NewUnitA2BService(store, cfg.Timezone, now)
	seeder := &model.User{UserID: "seed", NamaLengkap: "Seed Unit A2B", StatusPengguna: model.StatusAktif}

	next, err := unitService.NextNumber(ctx)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("next running number is %d", next)

	var created, skipped, failed int
	for _, row := range rows {
		idUnit := strings.ToUpper(strings.Join(strings.Fields(row.input.IDUnit), " "))
		exists, err := store.UnitA2BExists(ctx, idUnit)
		if err != nil {
			log.Fatal(err)
		}
		if exists {
			log.Printf("line %d %-8s skipped, already registered", row.line, idUnit)
			skipped++
			continue
		}
		if !*apply {
			log.Printf("line %d %-8s would be created", row.line, idUnit)
			created++
			continue
		}
		unit, err := unitService.Create(ctx, seeder, row.input)
		if err != nil {
			if errors.Is(err, service.ErrDuplicateUnitA2B) {
				log.Printf("line %d %-8s skipped, already registered", row.line, idUnit)
				skipped++
				continue
			}
			log.Printf("line %d %-8s FAILED: %v", row.line, idUnit, err)
			failed++
			continue
		}
		log.Printf("line %d %-8s created as number %d", row.line, unit.IDUnit, unit.NoUrut)
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
		if len(fields) != 8 {
			return nil, fmt.Errorf("line %d: expected 8 columns, got %d", lineNumber, len(fields))
		}
		rows = append(rows, seedRow{
			line: lineNumber,
			input: service.UnitA2BInput{
				Tanggal:     strings.TrimSpace(fields[0]),
				IDUnit:      strings.TrimSpace(fields[1]),
				NamaUnit:    strings.TrimSpace(fields[2]),
				MerekType:   strings.TrimSpace(fields[3]),
				FuelStorage: strings.TrimSpace(fields[4]),
				FRUnit:      strings.TrimSpace(fields[5]),
				Lokasi:      strings.TrimSpace(fields[6]),
				HMAwal:      strings.TrimSpace(fields[7]),
			},
		})
	}
	return rows, scanner.Err()
}
