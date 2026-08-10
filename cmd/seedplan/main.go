// Command seedplan loads production plans into the Produksi Plan sheet.
//
// It goes through the same service the web form uses, so every row is validated
// the same way and identifiers continue from whatever the sheet already holds.
// A location that already has a plan is skipped rather than given a second one,
// because two plans for one location would silently add up into a target nobody
// set. Re-running it is safe.
//
// It refuses to write unless -apply is passed, so the default run only reports
// what it would do.
package main

import (
	"bufio"
	"context"
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

// defaultSeedFile holds the starting plans. It is read from disk rather than
// embedded so the list can be edited without rebuilding.
const defaultSeedFile = "cmd/seedplan/plans.tsv"

type seedRow struct {
	line  int
	input service.ProduksiPlanInput
}

func main() {
	apply := flag.Bool("apply", false, "write the rows to the sheet; without it nothing is written")
	path := flag.String("file", defaultSeedFile, "TSV holding the plans to seed")
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

	existing, err := store.ListProduksiPlan(ctx)
	if err != nil {
		log.Fatal(err)
	}
	planned := make(map[string]bool, len(existing))
	for _, plan := range existing {
		planned[strings.ToUpper(strings.TrimSpace(plan.Lokasi))] = true
	}
	log.Printf("%d plans already in the sheet", len(existing))

	now := func() time.Time { return time.Now().In(cfg.Timezone) }
	planService := service.NewProduksiService(store, cfg.Timezone, now)
	seeder := &model.User{UserID: "seed", NamaLengkap: "Seed Produksi Plan", StatusPengguna: model.StatusAktif}

	var created, skipped, failed int
	for _, row := range rows {
		key := strings.ToUpper(strings.TrimSpace(row.input.Lokasi))
		if planned[key] {
			log.Printf("line %d %-32s skipped, already planned", row.line, row.input.Lokasi)
			skipped++
			continue
		}
		if !*apply {
			log.Printf("line %d %-32s would be planned at %s m3", row.line, row.input.Lokasi, row.input.Volume)
			created++
			continue
		}
		plan, err := planService.CreatePlan(ctx, seeder, row.input)
		if err != nil {
			log.Printf("line %d %-32s FAILED: %v", row.line, row.input.Lokasi, err)
			failed++
			continue
		}
		planned[key] = true
		log.Printf("line %d %-32s created as %s", row.line, plan.Lokasi, plan.PlanID)
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

// readSeed parses the tab-separated seed file: tanggal, project, supplier,
// lokasi, volume. Blank lines and lines starting with # are ignored, so the
// file can carry headings and notes.
func readSeed(path string) ([]seedRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open seed file: %w", err)
	}
	defer file.Close()

	var rows []seedRow
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		if len(fields) != 5 {
			return nil, fmt.Errorf("line %d: expected 5 tab separated fields, got %d", line, len(fields))
		}
		rows = append(rows, seedRow{line: line, input: service.ProduksiPlanInput{
			Tanggal:  strings.TrimSpace(fields[0]),
			Project:  strings.TrimSpace(fields[1]),
			Supplier: strings.TrimSpace(fields[2]),
			Lokasi:   strings.TrimSpace(fields[3]),
			Volume:   strings.TrimSpace(fields[4]),
		}})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read seed file: %w", err)
	}
	return rows, nil
}
