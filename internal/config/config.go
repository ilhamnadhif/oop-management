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
	// Signatory prints on exported reports. Left empty the report shows a blank
	// signature line, which is the safe default: a guessed name on a signed
	// document is worse than none.
	SignatoryName  string
	SignatoryTitle string
	SignatoryPlace string
	CompanyName    string
	// The working day every attendance record is judged against.
	WorkStart            string
	WorkEnd              string
	LateToleranceMinutes int
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

	maxUploadBytes, err := parseInt64("MAX_UPLOAD_BYTES", getenv("MAX_UPLOAD_BYTES", "2097152"))
	if err != nil || maxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_UPLOAD_BYTES must be a positive integer")
	}

	maxPhotoChars, err := parseInt("MAX_PHOTO_CHARS", getenv("MAX_PHOTO_CHARS", "45000"))
	if err != nil || maxPhotoChars <= 0 {
		return Config{}, fmt.Errorf("MAX_PHOTO_CHARS must be a positive integer")
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
		SignatoryName:         strings.TrimSpace(os.Getenv("SIGNATORY_NAME")),
		SignatoryTitle:        getenv("SIGNATORY_TITLE", "Direktur"),
		SignatoryPlace:        strings.TrimSpace(os.Getenv("SIGNATORY_PLACE")),
		CompanyName:           getenv("COMPANY_NAME", "PT Orecon Putra Perkasa"),
		WorkStart:             getenv("ATTENDANCE_START", "09:00"),
		WorkEnd:               getenv("ATTENDANCE_END", "17:00"),
		LateToleranceMinutes:  lateTolerance,
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
