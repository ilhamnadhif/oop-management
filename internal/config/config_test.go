package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func prepareLoadTest(t *testing.T) {
	t.Helper()
	t.Setenv("ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("GOOGLE_SPREADSHEET_ID", "test-spreadsheet")
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/tmp/test-credentials.json")
	t.Setenv("APP_TIMEZONE", "Asia/Jakarta")
	t.Setenv("SESSION_TTL", "24h")
	t.Setenv("MAX_UPLOAD_BYTES", "2097152")
	t.Setenv("MAX_PHOTO_CHARS", "45000")
	t.Setenv("ATTENDANCE_LATE_TOLERANCE_MINUTES", "15")
}

func TestLoadMiMoDefaults(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("MIMO_API_KEY", "")
	t.Setenv("MIMO_BASE_URL", "")
	t.Setenv("MIMO_MODEL", "")
	t.Setenv("MIMO_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MiMoAPIKey != "" {
		t.Fatalf("MiMoAPIKey = %q, want empty", cfg.MiMoAPIKey)
	}
	if cfg.MiMoBaseURL != "https://api.xiaomimimo.com/v1" {
		t.Fatalf("MiMoBaseURL = %q, want official API URL", cfg.MiMoBaseURL)
	}
	if cfg.MiMoModel != "mimo-v2.5" {
		t.Fatalf("MiMoModel = %q, want mimo-v2.5", cfg.MiMoModel)
	}
	if cfg.MiMoTimeout != 25*time.Second {
		t.Fatalf("MiMoTimeout = %v, want 25s", cfg.MiMoTimeout)
	}
}

func TestLoadMiMoOverrides(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("MIMO_API_KEY", "  secret-key  ")
	t.Setenv("MIMO_BASE_URL", "https://example.test/v1/")
	t.Setenv("MIMO_MODEL", "custom-model")
	t.Setenv("MIMO_TIMEOUT", "12s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.MiMoAPIKey != "secret-key" {
		t.Fatalf("MiMoAPIKey = %q, want trimmed key", cfg.MiMoAPIKey)
	}
	if cfg.MiMoBaseURL != "https://example.test/v1" {
		t.Fatalf("MiMoBaseURL = %q, want normalized URL", cfg.MiMoBaseURL)
	}
	if cfg.MiMoModel != "custom-model" {
		t.Fatalf("MiMoModel = %q, want custom-model", cfg.MiMoModel)
	}
	if cfg.MiMoTimeout != 12*time.Second {
		t.Fatalf("MiMoTimeout = %v, want 12s", cfg.MiMoTimeout)
	}
}

func TestLoadRejectsInvalidMiMoTimeout(t *testing.T) {
	for _, value := range []string{"invalid", "0s", "-1s", "30s", "1m"} {
		t.Run(value, func(t *testing.T) {
			prepareLoadTest(t)
			t.Setenv("MIMO_TIMEOUT", value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "MIMO_TIMEOUT") {
				t.Fatalf("Load() error = %v, want MIMO_TIMEOUT validation error", err)
			}
		})
	}
}

// A receipt answers in seconds; a page of ruled lines does not, and the two
// used to share one budget sized for the receipt.
func TestLoadReadsTheSheetScanTimeoutSeparately(t *testing.T) {
	prepareLoadTest(t)
	t.Setenv("MIMO_SHEET_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MiMoSheetTimeout != 150*time.Second {
		t.Fatalf("MiMoSheetTimeout = %v, want 150s", cfg.MiMoSheetTimeout)
	}

	t.Setenv("MIMO_SHEET_TIMEOUT", "90s")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MiMoSheetTimeout != 90*time.Second {
		t.Fatalf("MiMoSheetTimeout = %v, want 90s", cfg.MiMoSheetTimeout)
	}

	for _, value := range []string{"0s", "-1s", "11m", "sepuluh"} {
		t.Setenv("MIMO_SHEET_TIMEOUT", value)
		if _, err := Load(); err == nil {
			t.Fatalf("MIMO_SHEET_TIMEOUT=%q was accepted", value)
		}
	}
}
