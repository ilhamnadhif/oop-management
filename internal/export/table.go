package export

// Table is one report's data, independent of the format it is written to. Both
// renderers take this, so a new dataset means describing its columns once
// rather than writing a spreadsheet and a PDF from scratch.
type Table struct {
	SheetName string
	Columns   []Column

	// Rows is what the PDF prints; Values is what the spreadsheet stores. They
	// carry the same data, but a spreadsheet needs real numbers to be summable
	// while a PDF needs the string exactly as it should appear.
	Rows   [][]string
	Values [][]interface{}

	// Totals maps a zero-based column index to the figure its summary row
	// carries. The label spans every column before the first one.
	//
	// Most of the time these are sums. A report may put an average here
	// instead, where a sum would mean nothing: a column of percentages added
	// together is a number about nothing.
	Totals map[int]float64

	// FilterColumns limits the spreadsheet's autofilter to the leading columns
	// named here. A report where every day of the month is a column would
	// otherwise put a dropdown on all of them; zero means the whole table.
	FilterColumns int

	// Attachments are the photos the PDF prints after the signature. They are
	// PDF only: a spreadsheet with images embedded stops being a spreadsheet.
	// A report with none simply leaves this empty.
	Attachments []Attachment
}

func (t Table) totalsStart() int {
	first := len(t.Columns)
	for index := range t.Totals {
		if index < first {
			first = index
		}
	}
	return first
}

func (t Table) hasTotals() bool { return len(t.Totals) > 0 }

// usableWidth is the printable width of the page the PDF uses.
func (t Table) totalWidth() float64 {
	total := 0.0
	for _, column := range t.Columns {
		total += column.Width
	}
	return total
}
