package config

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// LoadDotEnv reads key=value pairs from path and puts them into the process
// environment. Variables already present in the environment win, so an
// explicit export or a container env still overrides the file. A missing file
// is not an error.
func LoadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")

	key, value, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}

	value = strings.TrimSpace(value)
	switch {
	case len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`):
		value = expand(strings.ReplaceAll(value[1:len(value)-1], `\n`, "\n"))
	case len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'"):
		// Single quotes are literal, matching shell behaviour.
		value = value[1 : len(value)-1]
	default:
		if idx := strings.Index(value, " #"); idx >= 0 {
			value = strings.TrimSpace(value[:idx])
		}
		value = expand(value)
	}
	return key, value, true
}

// expand resolves $VAR and ${VAR} references the same way `source .env` would.
// PWD falls back to the process working directory because it is only exported
// by interactive shells.
func expand(value string) string {
	return os.Expand(value, func(key string) string {
		if resolved, ok := os.LookupEnv(key); ok {
			return resolved
		}
		if key == "PWD" {
			if wd, err := os.Getwd(); err == nil {
				return wd
			}
		}
		return ""
	})
}
