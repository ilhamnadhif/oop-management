package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"opp-management/internal/id"
	"opp-management/internal/model"
	"opp-management/internal/repository"
)

type NowFunc func() time.Time

type AuthService struct {
	store    repository.Store
	location *time.Location
	now      NowFunc
	hashCost int
	mu       sync.Mutex

	// jabatanCached holds the configured menu rights between saves, guarded by
	// the same mutex as account creation.
	jabatanCached   []model.JabatanAccess
	jabatanCachedAt time.Time
}

// PasswordHashCost is the work factor stored passwords are hashed with. It is
// deliberately slow: the cost is paid once per sign-in and thousands of times
// per second by anyone working through a stolen sheet.
const PasswordHashCost = 12

type RegisterInput struct {
	TanggalGabung string
	NamaLengkap   string
	NRP           string
	Jabatan       string
	Email         string
	Password      string
	Status        string
	// Project is the site this account works in. Empty on the bootstrap
	// registration, which happens before any project exists.
	Project string
}

// DefaultPassword is what an account made by HR starts with. It is not a secret
// and is not meant to be one: it is handed over out loud, and the first sign-in
// with it will not let the person do anything until they have replaced it.
const DefaultPassword = "password"

// IsDefaultPassword reports whether a password typed at sign-in is the one every
// new account starts with. It compares the plaintext, which is only ever in hand
// at that moment - checking the stored hash instead would mean a bcrypt compare
// on a password nobody supplied.
func IsDefaultPassword(password string) bool {
	return password == DefaultPassword
}

type ActivityMeta struct {
	IPAddress string
	UserAgent string
}

// JabatanOptions is the closed set of positions a new account may hold. The
// register form renders it and normalizeRegisterInput enforces it, so a direct
// POST cannot slip an unlisted position past the dropdown.
var JabatanOptions = []string{
	"Flagman",
	"Security",
	"SHE",
	"Surveyor",
	"Logistik",
	"HR",
	"SPV",
	"Management",
	"Produksi",
}

// canonicalJabatan matches case-insensitively and returns the listed spelling,
// so "spv" and "SPV" never become two different values in the sheet.
func canonicalJabatan(value string) (string, bool) {
	for _, option := range JabatanOptions {
		if strings.EqualFold(option, value) {
			return option, true
		}
	}
	return "", false
}

func isAllDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}

func NewAuthService(store repository.Store, location *time.Location, now NowFunc) *AuthService {
	if location == nil {
		location = time.Local
	}
	if now == nil {
		now = time.Now
	}
	return &AuthService{store: store, location: location, now: now, hashCost: PasswordHashCost}
}

// WithHashCost lowers the work factor. It exists for tests, which sign in
// dozens of times per run and otherwise spend minutes hashing passwords nobody
// is trying to crack. Production keeps PasswordHashCost.
func (s *AuthService) WithHashCost(cost int) *AuthService {
	if cost > 0 {
		s.hashCost = cost
	}
	return s
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*model.User, error) {
	input, err := normalizeRegisterInput(input)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	exists, err := s.store.UserExists(ctx, input.NRP, input.Email)
	if err != nil {
		return nil, fmt.Errorf("check user uniqueness: %w", err)
	}
	if exists {
		return nil, ErrDuplicateUser
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.hashCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}
	userID, err := id.New("usr")
	if err != nil {
		return nil, err
	}
	now := s.now().In(s.location)
	user := &model.User{
		UserID:         userID,
		TanggalGabung:  input.TanggalGabung,
		NamaLengkap:    input.NamaLengkap,
		NRP:            input.NRP,
		Jabatan:        input.Jabatan,
		Email:          input.Email,
		PasswordHash:   string(passwordHash),
		StatusPengguna: input.Status,
		Project:        input.Project,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// AddEmployee is Register as HR uses it: the person being added is not present
// to choose a password, so the account starts on the default one and cannot be
// used for anything until it is replaced.
//
// allowManagement is the caller's own authority. HR may add every position but
// Management: an HR who could mint a Management account could hand themselves
// every project, which is the one escalation this form must not allow.
func (s *AuthService) AddEmployee(ctx context.Context, input RegisterInput, allowManagement bool) (*model.User, error) {
	if !allowManagement {
		if canonical, ok := canonicalJabatan(input.Jabatan); ok && canonical == model.JabatanManagement {
			return nil, fmt.Errorf("%w: hanya Management yang bisa menambah akun Management", ErrValidation)
		}
	}
	input.Password = DefaultPassword
	return s.Register(ctx, input)
}

// ChangePassword replaces one account's password. It is what the sign-in
// onboarding writes, and it refuses to set the default back: an account that
// could be handed the shared password again would be asked to change it again
// on the next sign-in, forever.
func (s *AuthService) ChangePassword(ctx context.Context, userID, password, confirmation string) error {
	if password != confirmation {
		return fmt.Errorf("%w: konfirmasi password tidak sama", ErrValidation)
	}
	passwordBytes := []byte(password)
	if len(passwordBytes) < 8 || len(passwordBytes) > 72 {
		return fmt.Errorf("%w: password harus 8 sampai 72 byte", ErrValidation)
	}
	if IsDefaultPassword(password) {
		return fmt.Errorf("%w: password baru tidak boleh sama dengan password awal", ErrValidation)
	}

	_, rowNumber, err := s.store.FindUserRow(ctx, userID)
	if err != nil {
		return fmt.Errorf("read user %s: %w", userID, err)
	}
	hash, err := bcrypt.GenerateFromPassword(passwordBytes, s.hashCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.store.UpdateUserPassword(ctx, rowNumber, string(hash), s.now().In(s.location)); err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	return nil
}

// HasAccounts reports whether this deployment has any accounts yet. The public
// registration page is open only while it does not: somebody has to be able to
// make the first one, and nobody should be able to make the rest.
func (s *AuthService) HasAccounts(ctx context.Context) (bool, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

// jabatanAccessCacheTTL bounds how long the configured menu rights are kept in
// memory. Rights are edited from the HR screen, so a minute of staleness costs
// nothing; the screen clears the cache itself the moment it writes.
const jabatanAccessCacheTTL = time.Minute

// JabatanAccess returns every position's configured menu rights. It is the raw
// sheet content: the handler merges these rows with the built-in defaults.
// The result is cached because the sidebar consults it on every request.
func (s *AuthService) JabatanAccess(ctx context.Context) ([]model.JabatanAccess, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jabatanCachedAt.After(time.Time{}) && s.now().Sub(s.jabatanCachedAt) < jabatanAccessCacheTTL {
		return cloneJabatanAccess(s.jabatanCached), nil
	}
	stored, err := s.store.ListJabatanAccess(ctx)
	if err != nil {
		return nil, fmt.Errorf("read jabatan access: %w", err)
	}
	s.jabatanCached = cloneJabatanAccess(stored)
	s.jabatanCachedAt = s.now()
	return cloneJabatanAccess(s.jabatanCached), nil
}

// SaveJabatanAccess writes one position's menu rights and drops the cache so
// the new rule is in force on the very next request.
func (s *AuthService) SaveJabatanAccess(ctx context.Context, jabatan string, menus []string) error {
	if canonical, ok := canonicalJabatan(jabatan); ok {
		jabatan = canonical
	} else {
		return fmt.Errorf("%w: jabatan tidak terdaftar", ErrValidation)
	}
	if strings.EqualFold(jabatan, model.JabatanManagement) {
		return fmt.Errorf("%w: akses Management tidak bisa diubah", ErrValidation)
	}
	menus = cleanMenus(menus)
	if err := s.store.SaveJabatanAccess(ctx, jabatan, menus); err != nil {
		return fmt.Errorf("save jabatan access: %w", err)
	}
	s.invalidateJabatanAccess()
	return nil
}

// ChangeJabatan moves one account to a different position. It mirrors the
// guard AddEmployee applies: only somebody who already reaches everything may
// create or demote a Management account.
func (s *AuthService) ChangeJabatan(ctx context.Context, userID, newJabatan string, allowManagement bool) (*model.User, error) {
	newJabatan, ok := canonicalJabatan(newJabatan)
	if !ok {
		return nil, fmt.Errorf("%w: jabatan tidak terdaftar", ErrValidation)
	}
	target, rowNumber, err := s.store.FindUserRow(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("read user %s: %w", userID, err)
	}
	if !allowManagement {
		current, _ := canonicalJabatan(target.Jabatan)
		if current == model.JabatanManagement || newJabatan == model.JabatanManagement {
			return nil, fmt.Errorf("%w: hanya Management yang bisa mengubah jabatan Management", ErrValidation)
		}
	}
	if strings.EqualFold(strings.TrimSpace(target.Jabatan), newJabatan) {
		return target, nil
	}
	now := s.now().In(s.location)
	if err := s.store.UpdateUserJabatan(ctx, rowNumber, newJabatan, now); err != nil {
		return nil, fmt.Errorf("update user jabatan: %w", err)
	}
	target.Jabatan = newJabatan
	target.UpdatedAt = now
	return target, nil
}

func cloneJabatanAccess(stored []model.JabatanAccess) []model.JabatanAccess {
	result := make([]model.JabatanAccess, 0, len(stored))
	for _, access := range stored {
		result = append(result, model.JabatanAccess{
			Jabatan:   access.Jabatan,
			MenuAktif: append([]string(nil), access.MenuAktif...),
		})
	}
	return result
}

// invalidateJabatanAccess drops the cache after a save.
func (s *AuthService) invalidateJabatanAccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jabatanCachedAt = time.Time{}
}

func (s *AuthService) Authenticate(ctx context.Context, identifier, password string, meta ActivityMeta) (*model.User, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || password == "" {
		activity := failedActivity(identifier, meta, s.now().In(s.location), "NRP/email atau password kosong")
		if logErr := s.store.AppendActivity(ctx, activity); logErr != nil {
			return nil, fmt.Errorf("record failed login: %w", logErr)
		}
		return nil, ErrInvalidCredentials
	}

	user, err := s.store.FindUserByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			activity := failedActivity(identifier, meta, s.now().In(s.location), "User tidak ditemukan atau password salah")
			if logErr := s.store.AppendActivity(ctx, activity); logErr != nil {
				return nil, fmt.Errorf("record failed login: %w", logErr)
			}
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("find user: %w", err)
	}

	now := s.now().In(s.location)
	if user.StatusPengguna != model.StatusAktif {
		if err := s.appendActivity(ctx, user, meta, model.ActivityFailed, "User tidak aktif", now); err != nil {
			return nil, err
		}
		return nil, ErrInactiveUser
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		if logErr := s.appendActivity(ctx, user, meta, model.ActivityFailed, "Password salah", now); logErr != nil {
			return nil, logErr
		}
		return nil, ErrInvalidCredentials
	}
	if err := s.appendActivity(ctx, user, meta, model.ActivitySuccess, "Login sukses", now); err != nil {
		return nil, err
	}
	if err := s.store.UpdateLastLogin(ctx, user.UserID, now); err != nil {
		return nil, fmt.Errorf("update last login: %w", err)
	}
	user.LastLoginAt = &now
	user.UpdatedAt = now
	return user, nil
}

func (s *AuthService) RecordLogout(ctx context.Context, user *model.User, meta ActivityMeta) error {
	if user == nil {
		return nil
	}
	return s.appendActivity(ctx, user, meta, model.ActivitySuccess, "Logout sukses", s.now().In(s.location))
}

func (s *AuthService) LoadUser(ctx context.Context, userID string) (*model.User, error) {
	return s.store.FindUserByID(ctx, userID)
}

func (s *AuthService) appendActivity(ctx context.Context, user *model.User, meta ActivityMeta, status, message string, at time.Time) error {
	activityID, err := id.New("act")
	if err != nil {
		return err
	}
	activity := &model.LoginActivity{
		ActivityID:   activityID,
		UserID:       user.UserID,
		NRP:          user.NRP,
		Email:        user.Email,
		ActivityType: model.ActivityLogin,
		ActivityTime: at,
		Status:       status,
		IPAddress:    meta.IPAddress,
		UserAgent:    meta.UserAgent,
		Message:      message,
	}
	if message == "Logout sukses" {
		activity.ActivityType = model.ActivityLogout
	}
	return s.store.AppendActivity(ctx, activity)
}

func failedActivity(identifier string, meta ActivityMeta, at time.Time, message string) *model.LoginActivity {
	activity := &model.LoginActivity{
		ActivityID:   "",
		ActivityType: model.ActivityLogin,
		ActivityTime: at,
		Status:       model.ActivityFailed,
		IPAddress:    meta.IPAddress,
		UserAgent:    meta.UserAgent,
		Message:      message,
	}
	if strings.Contains(identifier, "@") {
		activity.Email = strings.ToLower(identifier)
	} else {
		activity.NRP = identifier
	}
	activityID, err := id.New("act")
	if err != nil {
		activityID = fmt.Sprintf("act_failed_%d", at.UnixNano())
	}
	activity.ActivityID = activityID
	return activity
}

func normalizeRegisterInput(input RegisterInput) (RegisterInput, error) {
	input.TanggalGabung = strings.TrimSpace(input.TanggalGabung)
	input.NamaLengkap = strings.TrimSpace(input.NamaLengkap)
	input.NRP = strings.TrimSpace(input.NRP)
	input.Jabatan = strings.TrimSpace(input.Jabatan)
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Status = strings.ToUpper(strings.TrimSpace(input.Status))
	if input.Status == "" {
		input.Status = model.StatusAktif
	}

	if _, err := time.Parse("2006-01-02", input.TanggalGabung); err != nil {
		return RegisterInput{}, fmt.Errorf("%w: tanggal gabung wajib valid", ErrValidation)
	}
	if input.NamaLengkap == "" {
		return RegisterInput{}, fmt.Errorf("%w: nama lengkap wajib diisi", ErrValidation)
	}
	if !isAllDigits(input.NRP) {
		return RegisterInput{}, fmt.Errorf("%w: NRP wajib diisi dan hanya boleh angka", ErrValidation)
	}
	jabatan, ok := canonicalJabatan(input.Jabatan)
	if !ok {
		return RegisterInput{}, fmt.Errorf("%w: jabatan tidak terdaftar", ErrValidation)
	}
	input.Jabatan = jabatan
	parsedEmail, err := mail.ParseAddress(input.Email)
	if err != nil || parsedEmail.Address != input.Email {
		return RegisterInput{}, fmt.Errorf("%w: email tidak valid", ErrValidation)
	}
	passwordBytes := []byte(input.Password)
	if len(passwordBytes) < 8 || len(passwordBytes) > 72 {
		return RegisterInput{}, fmt.Errorf("%w: password harus 8 sampai 72 byte", ErrValidation)
	}
	if input.Status != model.StatusAktif && input.Status != model.StatusTidakAktif {
		return RegisterInput{}, fmt.Errorf("%w: status pengguna tidak valid", ErrValidation)
	}
	return input, nil
}
