package export

import (
	"strconv"
	"strings"

	"opp-management/internal/model"
)

// The report is one row per line of a nota, not one row per nota: the header
// fields repeat, but every purchase stays visible and the subtotals add up to
// the money spent. A nota-level export would hide what was actually bought.
//
// The receipt photo stays out of the table - it is a base64 image tens of
// thousands of characters long, which no cell can show - and is printed whole
// on the appendix pages of the PDF instead.
var notaColumns = []Column{
	{Header: "No", Width: 7},
	{Header: "No Transaksi", Width: 30},
	{Header: "Tanggal", Width: 16},
	{Header: "PIC", Width: 20},
	{Header: "Metode", Width: 20},
	{Header: "Status Bayar", Width: 22},
	{Header: "Penerima", Width: 20},
	{Header: "Kategori", Width: 18},
	{Header: "Sub Kategori", Width: 22},
	{Header: "Jenis", Width: 11},
	{Header: "Nama Produk", Width: 36},
	{Header: "Satuan", Width: 12},
	{Header: "Vol", Width: 9, Numeric: true, Decimals: 2},
	{Header: "Harga (Rp)", Width: 19, Numeric: true, Decimals: 2, Money: true},
	{Header: "Subtotal (Rp)", Width: 19, Numeric: true, Decimals: 2, Money: true},
}

// notaSubtotalColumn is the one column worth totalling. The nota total is the
// sum of its own lines, so totalling a repeated header figure would count the
// same money once per line.
const notaSubtotalColumn = 14

// NotaTable describes the expense report in both formats at once.
func NotaTable(rows []model.Nota) Table {
	table := Table{
		SheetName: "Nota",
		Columns:   notaColumns,
		Rows:      make([][]string, 0, len(rows)),
		Values:    make([][]interface{}, 0, len(rows)),
		Totals:    map[int]float64{},
	}

	subtotal := 0.0
	number := 0
	for _, nota := range rows {
		for _, item := range nota.Items {
			number++
			table.Rows = append(table.Rows, []string{
				strconv.Itoa(number),
				nota.NotaID, nota.Tanggal, nota.PIC, nota.MetodePembayaran, nota.StatusPembayaran,
				nota.PenerimaReimburse, nota.Kategori, nota.SubKategori, nota.JenisPerjalanan,
				item.NamaProduk, item.Satuan,
				FormatFloat(item.Volume, 2), FormatMoney(item.Harga), FormatMoney(item.Subtotal),
			})
			// Numbers stay numbers here, so the spreadsheet can sum them.
			table.Values = append(table.Values, []interface{}{
				number,
				nota.NotaID, nota.Tanggal, nota.PIC, nota.MetodePembayaran, nota.StatusPembayaran,
				nota.PenerimaReimburse, nota.Kategori, nota.SubKategori, nota.JenisPerjalanan,
				item.NamaProduk, item.Satuan,
				item.Volume, item.Harga, item.Subtotal,
			})
			subtotal += item.Subtotal
		}
	}

	table.Totals[notaSubtotalColumn] = subtotal
	table.Attachments = notaAttachments(rows)
	return table
}

// notaAttachments gathers the receipt photos, one nota at a time. Only the
// kwitansi: it is the evidence the expense happened, which is what the report
// is asked to stand on. The transfer and settlement proofs say how the money
// moved afterwards and belong to reconciliation, not to this document.
//
// A nota without a photo contributes nothing - an empty frame in the appendix
// would read as a missing receipt rather than one not yet uploaded.
func notaAttachments(rows []model.Nota) []Attachment {
	attachments := make([]Attachment, 0, len(rows))
	for _, nota := range rows {
		attachment, ok := decodeAttachment(nota.NotaID, notaAttachmentDetail(nota), nota.FotoKwitansi)
		if ok {
			attachments = append(attachments, attachment)
		}
	}
	return attachments
}

// notaAttachmentDetail is the line under the caption. A photo of a receipt says
// nothing about which claim it settles, so the date, the PIC and the amount
// travel with it.
func notaAttachmentDetail(nota model.Nota) string {
	parts := make([]string, 0, 3)
	for _, part := range []string{nota.Tanggal, nota.PIC} {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(append(parts, "Rp "+FormatMoney(nota.Total)), " - ")
}

func NotaXLSX(rows []model.Nota, meta Meta) ([]byte, error) {
	return RenderXLSX(NotaTable(rows), meta)
}

func NotaPDF(rows []model.Nota, meta Meta) ([]byte, error) {
	return RenderPDF(NotaTable(rows), meta)
}
