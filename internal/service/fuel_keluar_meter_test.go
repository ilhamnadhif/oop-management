package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"opp-management/internal/repository"
)

// fillWithMeter dispenses into one machine on a given day, reading its hour
// meter at the pump. The flow meter figures only have to move forward; what is
// under test is the machine's own reading.
func fillWithMeter(t *testing.T, service *FuelKeluarService, idUnit, tanggal, meter string, awal, akhir string) (string, error) {
	t.Helper()
	input := fuelKeluarTestInput(t)
	input.IDUnit = idUnit
	input.Tanggal = tanggal
	input.HMAlatBerat = meter
	input.HMAwalFlowMeter = awal
	input.HMAkhirFlowMeter = akhir
	_, warning, err := service.Create(context.Background(), fuelTestUser("Logistik"), input)
	return warning, err
}

func newMeterService(t *testing.T) *FuelKeluarService {
	t.Helper()
	store := repository.NewTestRepository()
	seedFuelMachine(t, store, "bld03", "Bulldozer D65 Komatsu 2 (Rent)")
	seedFuelMachine(t, store, "exc01", "Excavator PC200 Kobelco (Rent)")
	location := time.FixedZone("WIB", 7*60*60)
	return NewFuelKeluarService(store, location,
		func() time.Time { return time.Date(2026, 9, 1, 10, 0, 0, 0, location) })
}

// An hour meter cannot run backwards. This is the typo that made a machine's
// fuel ratio read zero: 1368 typed as 136.8.
func TestFuelKeluarRefusesAMeterThatWentBackwards(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-31", "1294.9", "20", "106"); err != nil {
		t.Fatalf("first fill: %v", err)
	}

	_, err := fillWithMeter(t, service, "bld03", "2026-09-01", "136.8", "106", "196")
	if err == nil {
		t.Fatal("a meter reading below the last one was accepted")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	// The message has to say what to compare against, or the operator is left
	// guessing which of the two numbers is wrong.
	for _, want := range []string{"136.8", "1294.9", "bld03"} {
		if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(want)) {
			t.Fatalf("message %q does not name %q", err.Error(), want)
		}
	}
}

// Filling without the meter having moved is ordinary: a machine topped up twice
// in a shift reads the same both times.
func TestFuelKeluarAcceptsAMeterThatStoodStill(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-22", "1257.6", "20", "150"); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	warning, err := fillWithMeter(t, service, "bld03", "2026-08-23", "1257.6", "150", "320")
	if err != nil {
		t.Fatalf("a repeated reading was refused: %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want none for a meter that did not move", warning)
	}
}

// A day's work moves the meter by a day's hours, and nothing is said about it.
func TestFuelKeluarAcceptsAnOrdinaryAdvanceQuietly(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-26", "1276.2", "20", "180"); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	warning, err := fillWithMeter(t, service, "bld03", "2026-08-27", "1286.2", "180", "340")
	if err != nil {
		t.Fatalf("an ordinary advance was refused: %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want none", warning)
	}
}

// A machine cannot work more hours than have passed. An extra digit is saved
// with a warning rather than refused: the range is wide enough that a real
// reading could sit near its edge.
func TestFuelKeluarWarnsWhenTheMeterJumpsFurtherThanTimeAllows(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-31", "1294.9", "20", "106"); err != nil {
		t.Fatalf("first fill: %v", err)
	}

	warning, err := fillWithMeter(t, service, "bld03", "2026-09-01", "12949", "106", "196")
	if err != nil {
		t.Fatalf("a large jump was refused, and it should only be flagged: %v", err)
	}
	if warning == "" {
		t.Fatal("a jump of 11654 hours over one day passed without a word")
	}
	if !strings.Contains(warning, "1294.9") {
		t.Fatalf("warning %q does not name the reading it was measured from", warning)
	}
}

// The column is optional, and a fill nobody read the meter at is still a fill.
func TestFuelKeluarAcceptsAnEmptyMeter(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-31", "1294.9", "20", "106"); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	warning, err := fillWithMeter(t, service, "bld03", "2026-09-01", "", "106", "196")
	if err != nil {
		t.Fatalf("a fill without a reading was refused: %v", err)
	}
	if warning != "" {
		t.Fatalf("warning = %q, want none when there is no reading to judge", warning)
	}
}

// A row written up late is checked against the readings on both sides of it,
// or it breaks the chain it was inserted into.
func TestFuelKeluarChecksABackdatedRowAgainstWhatFollowsIt(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-26", "1276.2", "20", "180"); err != nil {
		t.Fatalf("first fill: %v", err)
	}
	if _, err := fillWithMeter(t, service, "bld03", "2026-08-29", "1292", "180", "280"); err != nil {
		t.Fatalf("second fill: %v", err)
	}

	// 27 August sits between the two, so its reading has to as well.
	_, err := fillWithMeter(t, service, "bld03", "2026-08-27", "1300", "280", "380")
	if err == nil {
		t.Fatal("a backdated reading above the fill that follows it was accepted")
	}
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}

	if _, err := fillWithMeter(t, service, "bld03", "2026-08-27", "1280", "280", "380"); err != nil {
		t.Fatalf("a backdated reading that fits between the two was refused: %v", err)
	}
}

// Each machine has its own meter. One machine's reading must not judge another.
func TestFuelKeluarJudgesEachMachineOnItsOwnMeter(t *testing.T) {
	service := newMeterService(t)
	if _, err := fillWithMeter(t, service, "exc01", "2026-08-31", "5000", "20", "106"); err != nil {
		t.Fatalf("fill the excavator: %v", err)
	}
	if _, err := fillWithMeter(t, service, "bld03", "2026-09-01", "1294.9", "106", "196"); err != nil {
		t.Fatalf("the bulldozer was judged against the excavator's meter: %v", err)
	}
}
