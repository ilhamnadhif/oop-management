package main

import (
	"strings"
	"testing"

	"opp-management/internal/service"
)

// The seed list is edited by hand and then written straight to production, so a
// bad row must fail here rather than halfway through a run.
func TestSeedFileIsValid(t *testing.T) {
	rows, err := readSeed()
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("seed file is empty")
	}

	seen := make(map[string]int, len(rows))
	for _, row := range rows {
		nopol, err := service.NormalizeNopol(row.input.Nopol)
		if err != nil {
			t.Fatalf("line %d: nopol %q rejected: %v", row.line, row.input.Nopol, err)
		}
		if first, clash := seen[nopol]; clash {
			t.Fatalf("line %d: nopol %s already on line %d", row.line, nopol, first)
		}
		seen[nopol] = row.line

		if strings.TrimSpace(row.input.Driver) == "" {
			t.Fatalf("line %d: %s has no driver", row.line, nopol)
		}
		for _, dimension := range []struct{ label, value string }{
			{"panjang", row.input.Panjang},
			{"lebar", row.input.Lebar},
			{"tinggi", row.input.Tinggi},
		} {
			if strings.TrimSpace(dimension.value) == "" {
				t.Fatalf("line %d: %s has no %s", row.line, nopol, dimension.label)
			}
		}

		listed := false
		for _, option := range service.KeteranganOptions {
			if strings.EqualFold(option, row.input.Keterangan) {
				listed = true
				break
			}
		}
		if !listed {
			t.Fatalf("line %d: %s has unlisted keterangan %q", row.line, nopol, row.input.Keterangan)
		}
	}
}
