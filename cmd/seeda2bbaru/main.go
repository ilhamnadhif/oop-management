// Command seeda2bbaru loads the site's "Monitoring Konsumsi BBM" workbook into
// the A2B Baru sheets: the units, every dispense on the "Fuel Keluar dan HM"
// sheet, and the purchase orders beside them.
//
// It refuses to write unless -apply is passed, so the default run only reports
// what it read. It also refuses to write into A2B Baru sheets that already
// hold rows, so running it twice cannot load the workbook twice.
//
// The workbook is read from disk and never committed: this repository is
// public and the figures are operational data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"

	"opp-management/internal/config"
	"opp-management/internal/model"
	"opp-management/internal/repository"
)

const fuelSheet = "Fuel Keluar dan HM"

// firstDataRow is where the workbook's rows begin, below its title, the
// headers and the totals.
const firstDataRow = 8

func main() {
	apply := flag.Bool("apply", false, "write the rows to the sheets; without it nothing is written")
	path := flag.String("file", "Monitoring Konsumsi BBM 03112026 R.1.xlsx", "the workbook to load")
	spreadsheet := flag.String("spreadsheet", "", "the project spreadsheet to write into; the master when empty")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	book, err := readWorkbook(*path, cfg.Timezone)
	if err != nil {
		log.Fatal(err)
	}
	report(book)
	if !*apply {
		log.Print("dry run: nothing written. Pass -apply to load these rows.")
		return
	}

	ctx := context.Background()
	sheetService, err := sheets.NewService(ctx,
		option.WithCredentialsFile(cfg.GoogleCredentialsFile),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		log.Fatal(err)
	}
	target := strings.TrimSpace(*spreadsheet)
	if target == "" {
		target = cfg.GoogleSpreadsheetID
	}
	store := repository.NewGoogleSheetsRepository(sheetService, target, cfg.Timezone)
	if err := store.EnsureSchema(ctx); err != nil {
		log.Fatal(err)
	}
	units, err := store.ListA2BBaruUnits(ctx)
	if err != nil {
		log.Fatal(err)
	}
	orders, err := store.ListA2BBaruFuelMasuk(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fills, err := store.ListA2BBaruFuelKeluar(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if len(units)+len(orders)+len(fills) > 0 {
		log.Fatalf("the A2B Baru sheets already hold %d units, %d orders and %d fills; nothing written", len(units), len(orders), len(fills))
	}
	if err := store.AppendA2BBaruUnits(ctx, book.units); err != nil {
		log.Fatal(err)
	}
	if err := store.AppendA2BBaruFuelMasuk(ctx, book.orders); err != nil {
		log.Fatal(err)
	}
	if err := store.AppendA2BBaruFuelKeluar(ctx, book.fills); err != nil {
		log.Fatal(err)
	}
	log.Printf("written: %d units, %d orders, %d fills", len(book.units), len(book.orders), len(book.fills))
}

type workbook struct {
	// unitless are dispenses that name no unit, kept as the workbook has
	// them, by their transaction number.
	unitless []string
	units    []model.A2BBaruUnit
	orders   []model.A2BBaruFuelMasuk
	fills    []model.A2BBaruFuelKeluar
}

func report(book workbook) {
	var fuel, received float64
	for _, fill := range book.fills {
		fuel += fill.FuelLiter
	}
	for _, order := range book.orders {
		received += order.SiteVolume
	}
	log.Printf("read %d units, %d orders (%.0f L received), %d fills (%.0f L out)",
		len(book.units), len(book.orders), received, len(book.fills), fuel)
	var poJumlah, siteJumlah float64
	for _, order := range book.orders {
		poJumlah += order.POJumlah()
		siteJumlah += order.SiteJumlah()
		log.Printf("  order %-5s %s %-8s PO %.0f L @ %.2f, site %.0f L @ %.2f",
			order.Phase, order.Tanggal, order.NoPO, order.POVolume, order.POHargaSatuan, order.SiteVolume, order.SiteHargaSatuan)
	}
	log.Printf("  orders: PO Rp %.0f, site Rp %.0f, deviasi Rp %.0f", poJumlah, siteJumlah, poJumlah-siteJumlah)
	for _, fill := range book.unitless {
		log.Printf("  kept without a unit: %s", fill)
	}
	for _, unit := range book.units {
		log.Printf("  unit %-10s %s", unit.IDUnit, unit.NamaUnit)
	}
}

// readWorkbook reads the raw cell values, so a price keeps the decimals the
// workbook shows rounded and a date is the serial it is stored as.
func readWorkbook(path string, location *time.Location) (workbook, error) {
	var book workbook
	file, err := excelize.OpenFile(path)
	if err != nil {
		return book, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	rows, err := file.GetRows(fuelSheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return book, fmt.Errorf("read %s: %w", fuelSheet, err)
	}
	now := time.Now().In(location)
	const by = "Import Monitoring Konsumsi BBM"
	type named struct{ name, day string }
	latest := make(map[string]named)
	for index := firstDataRow - 1; index < len(rows); index++ {
		row := rows[index]
		cell := func(column string) string {
			number, _ := excelize.ColumnNameToNumber(column)
			if number-1 < len(row) {
				return strings.TrimSpace(row[number-1])
			}
			return ""
		}
		line := index + 1

		if id := cell("B"); id != "" {
			day, err := dateCell(cell("C"))
			if err != nil {
				return book, fmt.Errorf("row %d tanggal: %w", line, err)
			}
			fuel, err := number(cell("J"))
			if err != nil {
				return book, fmt.Errorf("row %d fuel: %w", line, err)
			}
			jam, _ := number(cell("L"))
			fill := model.A2BBaruFuelKeluar{
				FuelOutID: id, Tanggal: day, IDUnit: cell("D"), NamaUnit: cell("E"),
				HMAwalA2B: optional(cell("F")), HMAkhirA2B: optional(cell("G")),
				HMAwalFM: optional(cell("H")), HMAkhirFM: optional(cell("I")),
				FuelLiter: fuel, HMSaatIsi: optional(cell("K")), JamKerja: jam,
				Operator: cell("M"), CreatedBy: by, CreatedAt: now,
			}
			book.fills = append(book.fills, fill)
			if fill.IDUnit == "" {
				book.unitless = append(book.unitless, fmt.Sprintf("%s (baris %d, %s)", id, line, fill.NamaUnit))
				continue
			}
			if seen, ok := latest[fill.IDUnit]; !ok || day >= seen.day {
				latest[fill.IDUnit] = named{name: fill.NamaUnit, day: day}
			}
		}

		if phase := cell("S"); phase != "" {
			day, err := dateCell(cell("T"))
			if err != nil {
				return book, fmt.Errorf("row %d tanggal PO: %w", line, err)
			}
			order := model.A2BBaruFuelMasuk{
				FuelInID: fmt.Sprintf("FUELIN-%s-%04d", now.Format("20060102"), len(book.orders)+1),
				Phase:    phase, Tanggal: day, NoPO: cell("U"), CreatedBy: by, CreatedAt: now,
			}
			for _, value := range []struct {
				column string
				into   *float64
			}{{"V", &order.POVolume}, {"W", &order.POHargaSatuan}, {"Y", &order.SiteVolume}, {"Z", &order.SiteHargaSatuan}} {
				if *value.into, err = number(cell(value.column)); err != nil {
					return book, fmt.Errorf("row %d column %s: %w", line, value.column, err)
				}
			}
			book.orders = append(book.orders, order)
		}
	}
	for id, unit := range latest {
		book.units = append(book.units, model.A2BBaruUnit{IDUnit: id, NamaUnit: unit.name, CreatedBy: by, CreatedAt: now})
	}
	sort.Slice(book.units, func(i, j int) bool { return book.units[i].IDUnit < book.units[j].IDUnit })
	return book, nil
}

// dateCell reads a date stored as a serial number, or typed as the workbook
// shows it, month first.
func dateCell(value string) (string, error) {
	if serial, err := strconv.ParseFloat(value, 64); err == nil {
		day, err := excelize.ExcelDateToTime(serial, false)
		if err != nil {
			return "", err
		}
		return day.Format("2006-01-02"), nil
	}
	for _, layout := range []string{"01-02-06", "2006-01-02", "01/02/2006"} {
		if day, err := time.Parse(layout, value); err == nil {
			return day.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("unreadable date %q", value)
}

func number(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}

func optional(value string) *float64 {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := number(value)
	if err != nil {
		return nil
	}
	return &parsed
}
