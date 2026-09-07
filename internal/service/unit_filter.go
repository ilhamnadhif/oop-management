package service

import "strings"

// unitFilter is a set of machine ids a report was narrowed to. An empty filter
// means every machine, which is what a report nobody narrowed asks for.
//
// It exists because the three A2B exports all answer the same question about
// the same dropdown, and each writing its own loop would let them drift on the
// details: which case matches, what a repeated id means, whether blanks count.
type unitFilter struct {
	wanted map[string]bool
	// given is the list as the page sent it, cleaned but in its own order, so
	// the page can put the ticks back exactly where they were.
	given []string
}

// newUnitFilter reads the ids a page submitted. Blanks are dropped and a id
// named twice is one machine, not two.
func newUnitFilter(idUnits []string) unitFilter {
	filter := unitFilter{wanted: make(map[string]bool, len(idUnits))}
	for _, id := range idUnits {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if filter.wanted[key] {
			continue
		}
		filter.wanted[key] = true
		filter.given = append(filter.given, id)
	}
	return filter
}

// all reports a filter that narrows nothing.
func (f unitFilter) all() bool { return len(f.wanted) == 0 }

// matches reports whether one machine is in the filter. Matching ignores case:
// the register holds one spelling and a query may carry another.
func (f unitFilter) matches(idUnit string) bool {
	if f.all() {
		return true
	}
	return f.wanted[strings.ToLower(strings.TrimSpace(idUnit))]
}
