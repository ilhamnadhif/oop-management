package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"opp-management/internal/model"
)

// ProduksiPlanInput is one planned location and the volume set for it. The
// identifier and the audit trail are derived, never posted.
type ProduksiPlanInput struct {
	Tanggal  string
	Project  string
	Supplier string
	Lokasi   string
	Volume   string
}

// planIDPrefix numbers plans within a day, so the identifier says when the
// target was set without opening the row.
func (s *ProduksiService) planIDPrefix(now time.Time) string {
	return "PLN-" + now.Format("20060102") + "-"
}

// CreatePlan records the volume planned for a location. The pickers accept new
// values, so the job is to require one and settle on a single spelling: two
// spellings of the same location would each get their own plan and neither
// would match what was actually produced there.
func (s *ProduksiService) CreatePlan(ctx context.Context, user *model.User, input ProduksiPlanInput) (*model.ProduksiPlan, error) {
	if user == nil {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	tanggal := strings.TrimSpace(input.Tanggal)
	if _, err := time.Parse("2006-01-02", tanggal); err != nil {
		return nil, fmt.Errorf("%w: tanggal wajib valid", ErrValidation)
	}

	options, err := s.Options(ctx)
	if err != nil {
		return nil, err
	}
	project, err := adoptOption("Project", input.Project, options.Project)
	if err != nil {
		return nil, err
	}
	supplier, err := adoptOption("Supplier", input.Supplier, options.Supplier)
	if err != nil {
		return nil, err
	}
	lokasi, err := adoptOption("Lokasi", input.Lokasi, options.Lokasi)
	if err != nil {
		return nil, err
	}
	volume, err := parseVolume(input.Volume)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().In(s.location)
	prefix := s.planIDPrefix(now)
	highest, err := s.store.MaxProduksiPlanSequence(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("read last plan id: %w", err)
	}

	plan := &model.ProduksiPlan{
		PlanID:      fmt.Sprintf("%s%04d", prefix, highest+1),
		Tanggal:     tanggal,
		Project:     project,
		Supplier:    supplier,
		Lokasi:      lokasi,
		Volume:      round2(volume),
		CreatedBy:   user.NamaLengkap,
		CreatedByID: user.UserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateProduksiPlan(ctx, plan); err != nil {
		return nil, fmt.Errorf("create produksi plan: %w", err)
	}
	// A newly planned location has to appear in the Lokasi picker of the next
	// form, which reads from the cached option list.
	s.invalidateOptions()
	return plan, nil
}

// parseVolume reads the planned volume. Zero is refused rather than stored: a
// plan of nothing is not a target, and it would make every achievement figure
// against it a division by zero.
func parseVolume(value string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%w: volume rencana wajib diisi", ErrValidation)
	}
	// The dimension parser already handles decimal commas and rejects minus,
	// NaN and infinity; a plan only adds that zero is not a target either.
	volume, err := parseOptionalDimension("Volume rencana", value)
	if err != nil {
		return 0, err
	}
	if volume <= 0 {
		return 0, fmt.Errorf("%w: volume rencana harus lebih dari nol", ErrValidation)
	}
	return volume, nil
}

// Plans lists every plan, newest first, so the page shows what has been set
// without the reader having to open the sheet.
func (s *ProduksiService) Plans(ctx context.Context) ([]model.ProduksiPlan, error) {
	plans, err := s.store.ListProduksiPlan(ctx)
	if err != nil {
		return nil, fmt.Errorf("read produksi plan: %w", err)
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Tanggal != plans[j].Tanggal {
			return plans[i].Tanggal > plans[j].Tanggal
		}
		return plans[i].PlanID > plans[j].PlanID
	})
	return plans, nil
}
