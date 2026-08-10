package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"opp-management/internal/model"
	"opp-management/internal/photo"
)

// ProfilePhotoMaxChars caps the stored avatar. It is far below the 45k a
// receipt is allowed: the picture is only ever shown at a few dozen pixels, and
// a sheet cell holds 50k characters in total.
const ProfilePhotoMaxChars = 20000

// ProfileTextMaxLength bounds the free-text fields. A sheet cell would take far
// more, but a name that long is a paste accident rather than a name.
const ProfileTextMaxLength = 120

// ProfileInput is the part of an account its owner may change. NRP, jabatan,
// email and join date are deliberately absent: the first two identify the
// person to everyone else, and the last two are HR's record, not the
// employee's. A form post cannot reach them.
type ProfileInput struct {
	NamaLengkap  string
	NoTelp       string
	TanggalLahir string
	// Foto holds the raw bytes of an uploaded picture, empty when none was
	// chosen. HapusFoto removes the stored one; the two are mutually exclusive
	// and an upload wins, because choosing a new picture is the clearer intent.
	Foto      []byte
	HapusFoto bool
}

// UpdateProfile saves the fields an employee owns and returns the account as it
// now stands. The photo column is written only when the picture actually
// changed, so saving a phone number neither reads nor rewrites the image.
func (s *AuthService) UpdateProfile(ctx context.Context, userID string, input ProfileInput) (*model.User, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}

	nama := strings.TrimSpace(input.NamaLengkap)
	if nama == "" {
		return nil, fmt.Errorf("%w: nama lengkap wajib diisi", ErrValidation)
	}
	if len(nama) > ProfileTextMaxLength {
		return nil, fmt.Errorf("%w: nama lengkap terlalu panjang", ErrValidation)
	}
	noTelp, err := normalizePhone(input.NoTelp)
	if err != nil {
		return nil, err
	}
	tanggalLahir, err := s.normalizeBirthDate(input.TanggalLahir)
	if err != nil {
		return nil, err
	}

	// Encoding before the lock: it resizes and re-encodes an image, which is
	// slow enough that holding the write lock through it would stall every
	// other profile save.
	updatePhoto := false
	fotoProfil := ""
	switch {
	case len(input.Foto) > 0:
		fotoProfil, err = photo.Normalize(input.Foto, ProfilePhotoMaxChars)
		if err != nil {
			return nil, fmt.Errorf("%w: foto profil tidak dapat diproses", ErrInvalidPhoto)
		}
		updatePhoto = true
	case input.HapusFoto:
		updatePhoto = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	user, rowNumber, err := s.store.FindUserRow(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}

	user.NamaLengkap = nama
	user.NoTelp = noTelp
	user.TanggalLahir = tanggalLahir
	user.UpdatedAt = s.now().In(s.location)
	if updatePhoto {
		user.FotoProfil = fotoProfil
		user.PunyaFoto = fotoProfil != ""
	}
	if err := s.store.UpdateUserProfile(ctx, rowNumber, user, updatePhoto); err != nil {
		return nil, fmt.Errorf("update profile: %w", err)
	}
	// The stored image never travels back to the caller; the page asks for it
	// through ProfilePhoto when it needs to show it.
	user.FotoProfil = ""
	return user, nil
}

// ProfilePhoto returns one account's stored picture, or an empty string when
// there is none. It is the only read that touches that column.
func (s *AuthService) ProfilePhoto(ctx context.Context, userID string) (string, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return "", fmt.Errorf("%w: pengguna tidak dikenal", ErrValidation)
	}
	user, rowNumber, err := s.store.FindUserRow(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("find user: %w", err)
	}
	if !user.PunyaFoto {
		return "", nil
	}
	return s.store.ReadUserPhoto(ctx, rowNumber)
}

// normalizePhone keeps the digits and a leading plus, and nothing else. People
// type spaces, dashes and brackets; storing those makes the same number look
// like several different ones.
func normalizePhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	var cleaned strings.Builder
	for index, character := range value {
		switch {
		case character >= '0' && character <= '9':
			cleaned.WriteRune(character)
		case character == '+' && index == 0:
			cleaned.WriteRune(character)
		case character == ' ' || character == '-' || character == '.' ||
			character == '(' || character == ')':
			// Punctuation people type but nobody dials.
		default:
			return "", fmt.Errorf("%w: nomor telepon hanya boleh angka", ErrValidation)
		}
	}
	normalized := cleaned.String()
	digits := strings.TrimPrefix(normalized, "+")
	if len(digits) < 8 || len(digits) > 15 {
		return "", fmt.Errorf("%w: nomor telepon harus 8 sampai 15 digit", ErrValidation)
	}
	return normalized, nil
}

// normalizeBirthDate refuses a date that has not happened. An unborn employee
// is a typo, and it would sit in the sheet looking like a fact.
func (s *AuthService) normalizeBirthDate(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return "", fmt.Errorf("%w: tanggal lahir wajib valid", ErrValidation)
	}
	today := s.now().In(s.location)
	if parsed.After(time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)) {
		return "", fmt.Errorf("%w: tanggal lahir tidak boleh di masa depan", ErrValidation)
	}
	return parsed.Format("2006-01-02"), nil
}
