package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// hoursInADay bounds how far a machine's hour meter can move in a day. It
// cannot run longer than the day is, so a jump past this is arithmetic, not a
// judgement about how hard the site works.
const hoursInADay = 24.0

// checkHourMeter judges one reading against the machine's own dispensing
// history. A reading that cannot be true - the meter running backwards - is
// refused; one that is merely improbable is saved with a caution, because the
// operator at the pump can see the machine and this code cannot.
//
// The reading is optional, and a fill nobody read the meter at says nothing
// about the ones around it, so an empty reading passes without a word.
func (s *FuelKeluarService) checkHourMeter(ctx context.Context, unit FuelUnitOption, tanggal string, reading *float64) (string, error) {
	if reading == nil {
		return "", nil
	}
	before, after, err := s.neighbouringReadings(ctx, unit.IDUnit, tanggal)
	if err != nil {
		return "", err
	}
	value := *reading

	// The row may be written up late, in which case it has to fit between the
	// fills on either side of it rather than merely follow the last one.
	if before != nil && value < before.meter {
		return "", fmt.Errorf("%w: HM alat berat %s lebih kecil dari pengisian terakhir %s (%s pada %s). Periksa lagi angkanya",
			ErrValidation, meterText(value), unit.IDUnit, meterText(before.meter), hrDateLabel(before.tanggal))
	}
	if after != nil && value > after.meter {
		return "", fmt.Errorf("%w: HM alat berat %s lebih besar dari pengisian berikutnya %s (%s pada %s). Periksa lagi angkanya",
			ErrValidation, meterText(value), unit.IDUnit, meterText(after.meter), hrDateLabel(after.tanggal))
	}
	if before == nil {
		// Nothing to measure the jump from. The first reading a machine ever
		// gets is whatever its meter says.
		return "", nil
	}

	jump := value - before.meter
	allowed := allowedMeterJump(before.tanggal, tanggal)
	if jump <= allowed {
		return "", nil
	}
	return fmt.Sprintf("HM alat berat naik %s jam sejak %s (%s), padahal hanya ada %s jam di antaranya. "+
		"Cek kalau-kalau kelebihan digit.",
		meterText(jump), hrDateLabel(before.tanggal), meterText(before.meter), meterText(allowed)), nil
}

// allowedMeterJump is how many hours can have passed between two fills. Fills
// on the same day still get a full day, because the dispensing sheet dates a
// fill without timing it.
func allowedMeterJump(from, to string) float64 {
	days := daysBetween(from, to)
	if days < 1 {
		days = 1
	}
	return float64(days) * hoursInADay
}

// daysBetween counts the days from one date to another, or zero when either
// cannot be read.
func daysBetween(from, to string) int {
	start, err := time.Parse("2006-01-02", from)
	if err != nil {
		return 0
	}
	end, err := time.Parse("2006-01-02", to)
	if err != nil {
		return 0
	}
	return int(end.Sub(start).Hours() / 24)
}

// meterReading is one dispense's hour meter and the day it was read.
type meterReading struct {
	tanggal string
	meter   float64
}

// neighbouringReadings finds the machine's nearest hour meter reading before a
// date and the nearest one after it. Fills on the same day count as before,
// since a reading has to sit above whatever the machine already showed that
// day. Either may be nil.
func (s *FuelKeluarService) neighbouringReadings(ctx context.Context, idUnit, tanggal string) (*meterReading, *meterReading, error) {
	rows, err := s.store.ListFuelKeluar(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read fuel keluar: %w", err)
	}
	var before, after *meterReading
	for _, row := range rows {
		if row.HMAlatBerat == nil {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(row.IDUnit), strings.TrimSpace(idUnit)) {
			continue
		}
		candidate := meterReading{tanggal: strings.TrimSpace(row.Tanggal), meter: *row.HMAlatBerat}
		if candidate.tanggal <= tanggal {
			if before == nil || candidate.tanggal > before.tanggal ||
				(candidate.tanggal == before.tanggal && candidate.meter > before.meter) {
				value := candidate
				before = &value
			}
			continue
		}
		if after == nil || candidate.tanggal < after.tanggal ||
			(candidate.tanggal == after.tanggal && candidate.meter < after.meter) {
			value := candidate
			after = &value
		}
	}
	return before, after, nil
}

// meterText prints an hour meter the way the form takes it: as many decimals as
// it needs and no more, so 1294.90 reads back as 1294.9.
func meterText(value float64) string {
	return strconv.FormatFloat(round2(value), 'f', -1, 64)
}
