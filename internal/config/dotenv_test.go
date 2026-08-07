package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnvMissingFileIsIgnored(t *testing.T) {
	if err := LoadDotEnv(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatalf("expected nil error for missing file, got %v", err)
	}
}

func TestLoadDotEnvParsesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# comment\n\nPORT=9090\nexport APP_TIMEZONE=Asia/Jakarta\nQUOTED=\"hello world\"\nSINGLE='raw value'\nTRAILING=8080 # inline comment\nnot a pair\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	for _, key := range []string{"PORT", "APP_TIMEZONE", "QUOTED", "SINGLE", "TRAILING"} {
		t.Setenv(key, "")
		os.Unsetenv(key)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	want := map[string]string{
		"PORT":         "9090",
		"APP_TIMEZONE": "Asia/Jakarta",
		"QUOTED":       "hello world",
		"SINGLE":       "raw value",
		"TRAILING":     "8080",
	}
	for key, expected := range want {
		if got := os.Getenv(key); got != expected {
			t.Fatalf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestLoadDotEnvExpandsVariables(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "CREDS=$PWD/credentials/account.json\nFROM_ENV=\"${SOME_BASE}/x\"\nLITERAL='$PWD/raw'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("SOME_BASE", "/base")
	for _, key := range []string{"CREDS", "FROM_ENV", "LITERAL"} {
		os.Unsetenv(key)
	}

	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("load: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if got, want := os.Getenv("CREDS"), wd+"/credentials/account.json"; got != want {
		t.Fatalf("CREDS = %q, want %q", got, want)
	}
	if got := os.Getenv("FROM_ENV"); got != "/base/x" {
		t.Fatalf("FROM_ENV = %q, want /base/x", got)
	}
	if got := os.Getenv("LITERAL"); got != "$PWD/raw" {
		t.Fatalf("LITERAL = %q, want literal $PWD/raw", got)
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("PORT=9090\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	t.Setenv("PORT", "3000")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := os.Getenv("PORT"); got != "3000" {
		t.Fatalf("PORT = %q, want 3000", got)
	}
}
