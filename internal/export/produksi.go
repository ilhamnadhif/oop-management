package export

import (
	"strconv"

	"opp-management/internal/model"
)

// produksiColumns is the report layout. The widths add up to the usable width
// of a landscape A4 page; the audit columns from the sheet are left out because
// twenty columns is already the limit of what stays readable in print.
var produksiColumns = []Column{
	{Header: "No", Width: 7},
	{Header: "ID Produksi", Width: 20},
	{Header: "Tanggal", Width: 17},
	{Header: "Project", Width: 14},
	{Header: "Supplier", Width: 14},
	{Header: "Quary", Width: 12},
	{Header: "Kategori", Width: 16},
	{Header: "Lokasi", Width: 18},
	{Header: "Layer", Width: 10},
	{Header: "Nopol", Width: 20},
	{Header: "Driver", Width: 20},
	{Header: "Jenis DT", Width: 15},
	{Header: "P", Width: 10, Numeric: true, Decimals: 2},
	{Header: "L", Width: 10, Numeric: true, Decimals: 2},
	{Header: "T", Width: 10, Numeric: true, Decimals: 2},
	{Header: "TT", Width: 10, Numeric: true, Decimals: 2},
	{Header: "TF", Width: 10, Numeric: true, Decimals: 2},
	{Header: "Volume", Width: 14, Numeric: true, Decimals: 4},
	{Header: "Vol OPP", Width: 12, Numeric: true, Decimals: 2},
	{Header: "Deviasi", Width: 12, Numeric: true, Decimals: 4},
}

// ProduksiTable describes the production report in both formats at once.
func ProduksiTable(rows []model.Produksi) Table {
	table := Table{
		SheetName: "Produksi",
		Columns:   produksiColumns,
		Rows:      make([][]string, 0, len(rows)),
		Values:    make([][]interface{}, 0, len(rows)),
		Totals:    map[int]float64{},
	}

	var volume, volumeOPP, deviasi float64
	for i, row := range rows {
		number := i + 1
		table.Rows = append(table.Rows, []string{
			strconv.Itoa(number),
			row.ProduksiID, row.Tanggal, row.Project, row.Supplier, row.Quary,
			row.Kategori, row.Lokasi, row.Layer, row.Nopol, row.Driver, row.JenisDT,
			FormatFloat(row.Panjang, 2), FormatFloat(row.Lebar, 2), FormatFloat(row.Tinggi, 2),
			FormatFloat(row.TT, 2), FormatFloat(row.TF, 2),
			FormatFloat(row.Volume, 4), FormatFloat(row.VolumeOPP, 2), FormatFloat(row.Deviasi, 4),
		})
		// Numbers stay numbers here, so the spreadsheet can sum them.
		table.Values = append(table.Values, []interface{}{
			number,
			row.ProduksiID, row.Tanggal, row.Project, row.Supplier, row.Quary,
			row.Kategori, row.Lokasi, row.Layer, row.Nopol, row.Driver, row.JenisDT,
			row.Panjang, row.Lebar, row.Tinggi, row.TT, row.TF,
			row.Volume, row.VolumeOPP, row.Deviasi,
		})
		volume += row.Volume
		volumeOPP += row.VolumeOPP
		deviasi += row.Deviasi
	}

	table.Totals[17] = volume
	table.Totals[18] = volumeOPP
	table.Totals[19] = deviasi
	return table
}

// ProduksiXLSX and ProduksiPDF are the two files the export page offers.
func ProduksiXLSX(rows []model.Produksi, meta Meta) ([]byte, error) {
	return RenderXLSX(ProduksiTable(rows), meta)
}

func ProduksiPDF(rows []model.Produksi, meta Meta) ([]byte, error) {
	return RenderPDF(ProduksiTable(rows), meta)
}
