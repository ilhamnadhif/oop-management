package service

import "testing"

// A report nobody narrowed asks for every machine.
func TestUnitFilterWithoutIdsMatchesEverything(t *testing.T) {
	for _, given := range [][]string{nil, {}, {""}, {"  ", ""}} {
		filter := newUnitFilter(given)
		if !filter.all() {
			t.Fatalf("%v does not read as every machine", given)
		}
		if !filter.matches("exc01") {
			t.Fatalf("%v refused a machine", given)
		}
		if len(filter.given) != 0 {
			t.Fatalf("%v kept %v to put back on the page", given, filter.given)
		}
	}
}

// The register holds one spelling and a query may carry another.
func TestUnitFilterMatchesWhateverCaseWasTyped(t *testing.T) {
	filter := newUnitFilter([]string{"EXC-01", " bld03 "})
	for _, id := range []string{"exc-01", "EXC-01", "BLD03", "bld03"} {
		if !filter.matches(id) {
			t.Fatalf("%q was refused", id)
		}
	}
	if filter.matches("svd01") {
		t.Fatal("a machine outside the filter was matched")
	}
}

// A machine named twice is one machine, and the ticks go back where the page
// put them.
func TestUnitFilterKeepsWhatThePageSent(t *testing.T) {
	filter := newUnitFilter([]string{"BLD03", "exc-01", "bld03", ""})
	if len(filter.given) != 2 || filter.given[0] != "BLD03" || filter.given[1] != "exc-01" {
		t.Fatalf("given = %v, want the two machines in the order they arrived", filter.given)
	}
}
