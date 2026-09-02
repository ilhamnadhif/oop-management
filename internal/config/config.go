package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port                  string
	GoogleSpreadsheetID   string
	GoogleCredentialsFile string
	TimezoneName          string
	Timezone              *time.Location
	SessionTTL            time.Duration
	SessionCookieSecure   bool
	MaxUploadBytes        int64
	MaxPhotoChars         int
	MiMoAPIKey            string
	MiMoBaseURL           string
	MiMoModel             string
	MiMoTimeout           time.Duration
	MiMoSheetTimeout      time.Duration
	// SignatoryPlace is the town printed above the signature block. Who signs
	// is decided per export, per project; this is only where.
	SignatoryPlace string
	CompanyName    string
	// ProjectName names the project the app was already keeping books for. It
	// is used once: to write the opening row of the project sheet on the first
	// start after projects existed. After that the sheet is the authority and
	// this is ignored.
	ProjectName string
	// The working day every attendance record is judged against.
	WorkStart            string
	WorkEnd              string
	LateToleranceMinutes int
	// A2BWorkMinutes is one shift's worth of minutes for an A2B machine. Hour
	// meter readings are judged against it: whatever the machine did not spend
	// working has to be accounted for as standby or breakdown.
	A2BWorkMinutes int
}

func Load() (Config, error) {
	envFile := getenv("ENV_FILE", ".env")
	if err := LoadDotEnv(envFile); err != nil {
		return Config{}, fmt.Errorf("load %s: %w", envFile, err)
	}

	timezoneName := getenv("APP_TIMEZONE", "Asia/Jakarta")
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return Config{}, fmt.Errorf("load APP_TIMEZONE %q: %w", timezoneName, err)
	}

	sessionTTL, err := time.ParseDuration(getenv("SESSION_TTL", "24h"))
	if err != nil || sessionTTL <= 0 {
		return Config{}, fmt.Errorf("SESSION_TTL must be a positive duration")
	}

	maxUploadBytes, err := parseInt64("MAX_UPLOAD_BYTES", getenv("MAX_UPLOAD_BYTES", "10485760"))
	if err != nil || maxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_UPLOAD_BYTES must be a positive integer")
	}

	maxPhotoChars, err := parseInt("MAX_PHOTO_CHARS", getenv("MAX_PHOTO_CHARS", "45000"))
	if err != nil || maxPhotoChars <= 0 {
		return Config{}, fmt.Errorf("MAX_PHOTO_CHARS must be a positive integer")
	}

	miMoTimeout, err := time.ParseDuration(getenv("MIMO_TIMEOUT", "25s"))
	if err != nil || miMoTimeout <= 0 || miMoTimeout >= 30*time.Second {
		return Config{}, fmt.Errorf("MIMO_TIMEOUT must be a positive duration shorter than 30s")
	}
	// The sheet scan gets its own budget. A receipt answers in a few seconds; a
	// page of ruled lines has the model writing for far longer, and sharing the
	// receipt's budget ended the read while the answer was still arriving.
	miMoSheetTimeout, err := time.ParseDuration(getenv("MIMO_SHEET_TIMEOUT", "150s"))
	if err != nil || miMoSheetTimeout <= 0 || miMoSheetTimeout > 10*time.Minute {
		return Config{}, fmt.Errorf("MIMO_SHEET_TIMEOUT must be a positive duration of at most 10m")
	}
	miMoBaseURL := strings.TrimRight(strings.TrimSpace(getenv("MIMO_BASE_URL", "https://api.xiaomimimo.com/v1")), "/")
	if miMoBaseURL == "" {
		miMoBaseURL = "https://api.xiaomimimo.com/v1"
	}
	miMoModel := strings.TrimSpace(getenv("MIMO_MODEL", "mimo-v2.5"))
	if miMoModel == "" {
		miMoModel = "mimo-v2.5"
	}

	a2bWorkMinutes, err := parseInt("A2B_WORK_MINUTES", getenv("A2B_WORK_MINUTES", "480"))
	if err != nil {
		return Config{}, err
	}
	if a2bWorkMinutes <= 0 {
		return Config{}, fmt.Errorf("A2B_WORK_MINUTES harus lebih dari 0")
	}

	lateTolerance, err := parseInt("ATTENDANCE_LATE_TOLERANCE_MINUTES", getenv("ATTENDANCE_LATE_TOLERANCE_MINUTES", "15"))
	if err != nil || lateTolerance < 0 {
		return Config{}, fmt.Errorf("ATTENDANCE_LATE_TOLERANCE_MINUTES must be a non-negative integer")
	}

	cfg := Config{
		Port:                  getenv("PORT", "8080"),
		GoogleSpreadsheetID:   os.Getenv("GOOGLE_SPREADSHEET_ID"),
		GoogleCredentialsFile: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		TimezoneName:          timezoneName,
		Timezone:              loc,
		SessionTTL:            sessionTTL,
		SessionCookieSecure:   parseBool(getenv("SESSION_COOKIE_SECURE", "false")),
		MaxUploadBytes:        maxUploadBytes,
		MaxPhotoChars:         maxPhotoChars,
		MiMoAPIKey:            strings.TrimSpace(os.Getenv("MIMO_API_KEY")),
		MiMoBaseURL:           miMoBaseURL,
		MiMoModel:             miMoModel,
		MiMoTimeout:           miMoTimeout,
		MiMoSheetTimeout:      miMoSheetTimeout,
		SignatoryPlace:        strings.TrimSpace(os.Getenv("SIGNATORY_PLACE")),
		CompanyName:           getenv("COMPANY_NAME", "PT Orecon Putra Perkasa"),
		ProjectName:           getenv("PROJECT_NAME", "PCPM"),
		WorkStart:             getenv("ATTENDANCE_START", "07:00"),
		WorkEnd:               getenv("ATTENDANCE_END", "17:00"),
		LateToleranceMinutes:  lateTolerance,
		A2BWorkMinutes:        a2bWorkMinutes,
	}

	if cfg.GoogleSpreadsheetID == "" {
		return Config{}, fmt.Errorf("GOOGLE_SPREADSHEET_ID is required")
	}
	if cfg.GoogleCredentialsFile == "" {
		return Config{}, fmt.Errorf("GOOGLE_APPLICATION_CREDENTIALS is required")
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseBool(value string) bool {
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
}

func parseInt(key, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func parseInt64(key, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
