package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"opp-management/internal/model"
)

// JabatanNameMaxLength bounds a made-up position. It is what a dropdown and a
// table column can show without the rest of the row being pushed off the page.
// The form renders it as the field's own limit, so the page and the server
// agree about what will be taken.
const JabatanNameMaxLength = 40

// CreateJabatan adds one position to one project. The eight built-in positions
// exist at every site and are not stored; these are the ones a site invented
// for itself, and they never leave it.
//
// The position is always beneath Management: it cannot be named Management,
// and reaching every project is decided by model.User.ReachesEveryProject,
// which asks only whether somebody is Management. A made-up position therefore
// has no way of crossing projects however its menus are later set.
//
// It is born holding nothing. What it may open is decided afterwards on the
// access matrix, which is where every other position's rights live too.
func (s *AuthService) CreateJabatan(ctx context.Context, project, nama, dibuatOleh string) error {
	project = strings.TrimSpace(project)
	if project == "" {
		return fmt.Errorf("%w: project tidak dikenal", ErrValidation)
	}
	// Collapsed rather than merely trimmed: "Kepala  Teknik" and "Kepala
	// Teknik" are the same job, and storing both would put two of it in the
	// dropdown.
	nama = strings.Join(strings.Fields(nama), " ")
	if nama == "" {
		return fmt.Errorf("%w: nama jabatan wajib diisi", ErrValidation)
	}
	if len([]rune(nama)) > JabatanNameMaxLength {
		return fmt.Errorf("%w: nama jabatan maksimal %d karakter", ErrValidation, JabatanNameMaxLength)
	}
	if strings.EqualFold(nama, model.JabatanManagement) {
		return fmt.Errorf("%w: jabatan Management tidak bisa dibuat", ErrValidation)
	}
	if _, builtIn := builtInJabatan(nama); builtIn {
		return fmt.Errorf("%w: jabatan %s sudah ada di seluruh project", ErrValidation, nama)
	}

	existing, err := s.store.ListJabatan(ctx)
	if err != nil {
		return fmt.Errorf("read jabatan: %w", err)
	}
	for _, row := range existing {
		if strings.EqualFold(strings.TrimSpace(row.Project), project) &&
			strings.EqualFold(strings.TrimSpace(row.Nama), nama) {
			return fmt.Errorf("%w: jabatan %s sudah ada di project ini", ErrValidation, nama)
		}
	}

	if err := s.store.CreateJabatan(ctx, &model.Jabatan{
		Project:    project,
		Nama:       nama,
		DibuatOleh: strings.TrimSpace(dibuatOleh),
		CreatedAt:  s.now().In(s.location),
	}); err != nil {
		return fmt.Errorf("create jabatan: %w", err)
	}
	return nil
}

// JabatanOptionsFor is every position an account in one project may hold: the
// built-in eight, then whatever that project made for itself, in alphabetical
// order so the dropdown does not reshuffle as rows are added.
//
// An empty project gets the built-ins alone. That is the bootstrap
// registration, which happens before any project exists.
func (s *AuthService) JabatanOptionsFor(ctx context.Context, project string) ([]string, error) {
	options := append([]string(nil), JabatanOptions...)
	project = strings.TrimSpace(project)
	if project == "" {
		return options, nil
	}
	stored, err := s.store.ListJabatan(ctx)
	if err != nil {
		return nil, fmt.Errorf("read jabatan: %w", err)
	}
	own := make([]string, 0, len(stored))
	for _, row := range stored {
		if !strings.EqualFold(strings.TrimSpace(row.Project), project) {
			continue
		}
		if nama := strings.TrimSpace(row.Nama); nama != "" {
			own = append(own, nama)
		}
	}
	sort.SliceStable(own, func(i, j int) bool {
		return strings.ToLower(own[i]) < strings.ToLower(own[j])
	})
	return append(options, own...), nil
}

// builtInJabatan matches case-insensitively against the positions every site
// has, returning the listed spelling so "spv" and "SPV" never become two
// different values in the sheet.
func builtInJabatan(value string) (string, bool) {
	for _, option := range JabatanOptions {
		if strings.EqualFold(option, value) {
			return option, true
		}
	}
	return "", false
}

// canonicalJabatanIn resolves a position within one project: the built-in
// spelling, or the project's own. A position another project made is not a
// position here, which is what keeps a made-up one from crossing sites.
func (s *AuthService) canonicalJabatanIn(ctx context.Context, project, value string) (string, bool, error) {
	if canonical, ok := builtInJabatan(value); ok {
		return canonical, true, nil
	}
	project = strings.TrimSpace(project)
	if project == "" {
		return "", false, nil
	}
	stored, err := s.store.ListJabatan(ctx)
	if err != nil {
		return "", false, fmt.Errorf("read jabatan: %w", err)
	}
	for _, row := range stored {
		if strings.EqualFold(strings.TrimSpace(row.Project), project) &&
			strings.EqualFold(strings.TrimSpace(row.Nama), value) {
			return strings.TrimSpace(row.Nama), true, nil
		}
	}
	return "", false, nil
}
