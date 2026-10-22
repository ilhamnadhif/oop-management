package export

import (
	"strconv"

	"opp-management/internal/service"
)

// The fuel bill, one row per month. Fuel is charged to the month it was burned
// in rather than the month it arrived, so the litres in and the litres out are
// printed side by side: a month that took a delivery it has not burned yet
// should not read as a month that overspent, and the two columns are what shows
// the difference.
//
// The widths add up to the usable width of a landscape A4 page, so the table
// lines up with the letterhead rule above it.
var fuelCostColumns = []Column{
	{Header: "No", Width: 14},
	{Header: "Bulan", Width: 51},
	{Header: "Fuel Masuk (L)", Width: 34, Numeric: true, Decimals: 2},
	{Header: "Pembelian (Rp)", Width: 44, Numeric: true, Decimals: 0},
	{Header: "Fuel Keluar (L)", Width: 34, Numeric: true, Decimals: 2},
	{Header: "Biaya Pemakaian (Rp)", Width: 48, Numeric: true, Decimals: 0},
	{Header: "Sisa Stok (L)", Width: 26, Numeric: true, Decimals: 2},
	{Header: "Nilai Sisa (Rp)", Width: 30, Numeric: true, Decimals: 0},
}

// FuelCostTable describes the monthly fuel bill for both formats. The litres
// and the money add up; the closing stock does not, since it is the same tank
// carried from month to month rather than a quantity per month.
func FuelCostTable(report *service.FuelCostReport) Table {
	table := Table{
		SheetName: "Biaya Fuel",
		Columns:   fuelCostColumns,
		Rows:      make([][]string, 0, len(report.Months)),
		Values:    make([][]interface{}, 0, len(report.Months)),
	}
	for i, month := range report.Months {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number), month.Label,
			FormatFloat(month.MasukLiter, 2), FormatFloat(month.MasukBiaya, 0),
			FormatFloat(month.KeluarLiter, 2), FormatFloat(month.Biaya, 0),
			FormatFloat(month.SisaLiter, 2), FormatFloat(month.SisaNilai, 0),
		})
		table.Values = append(table.Values, []interface{}{
			number, month.Label,
			month.MasukLiter, month.MasukBiaya,
			month.KeluarLiter, month.Biaya,
			month.SisaLiter, month.SisaNilai,
		})
	}
	if len(report.Months) > 0 {
		table.Totals = map[int]float64{
			2: report.MasukLiter,
			3: report.MasukBiaya,
			4: report.KeluarLiter,
			5: report.Biaya,
		}
	}
	return table
}

// FuelCostXLSX writes the monthly bill as a spreadsheet.
func FuelCostXLSX(report *service.FuelCostReport, meta Meta) ([]byte, error) {
	return RenderXLSX(FuelCostTable(report), meta)
}

// FuelCostPDF prints the same bill as the signable report.
func FuelCostPDF(report *service.FuelCostReport, meta Meta) ([]byte, error) {
	return RenderPDF(FuelCostTable(report), meta)
}
