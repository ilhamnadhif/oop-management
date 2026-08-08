package main

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// The seed list is edited by hand and written straight to production, so a bad
// row must fail here rather than halfway through a run.
func TestSeedFileIsValid(t *testing.T) {
	rows, err := readSeed("units.tsv")
	if err != nil {
		// The list is deliberately not committed; skip rather than fail so the
		// suite still passes on a fresh clone.
		t.Skipf("seed file not present: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("seed file is empty")
	}

	seen := make(map[string]int, len(rows))
	for _, row := range rows {
		id := strings.ToUpper(strings.Join(strings.Fields(row.input.IDUnit), " "))
		if id == "" {
			t.Fatalf("line %d: empty ID unit", row.line)
		}
		if first, clash := seen[id]; clash {
			t.Fatalf("line %d: ID %s already on line %d", row.line, id, first)
		}
		seen[id] = row.line

		if _, err := time.Parse("2006-01-02", row.input.Tanggal); err != nil {
			t.Fatalf("line %d: %s has an unparseable date %q", row.line, id, row.input.Tanggal)
		}
		for _, required := range []struct{ label, value string }{
			{"nama unit", row.input.NamaUnit},
			{"merek type", row.input.MerekType},
			{"lokasi", row.input.Lokasi},
		} {
			if strings.TrimSpace(required.value) == "" {
				t.Fatalf("line %d: %s has no %s", row.line, id, required.label)
			}
		}
		for _, positive := range []struct{ label, value string }{
			{"fuel storage", row.input.FuelStorage},
			{"fr unit", row.input.FRUnit},
		} {
			parsed, err := strconv.ParseFloat(strings.ReplaceAll(positive.value, ",", "."), 64)
			if err != nil || parsed <= 0 {
				t.Fatalf("line %d: %s has a non-positive %s %q", row.line, id, positive.label, positive.value)
			}
		}
		hm, err := strconv.ParseFloat(strings.ReplaceAll(row.input.HMAwal, ",", "."), 64)
		if err != nil || hm < 0 {
			t.Fatalf("line %d: %s has an invalid HM awal %q", row.line, id, row.input.HMAwal)
		}
	}
}
