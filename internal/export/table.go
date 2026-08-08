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

	// Totals maps a zero-based column index to its sum. The label spans every
	// column before the first total.
	Totals map[int]float64
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
