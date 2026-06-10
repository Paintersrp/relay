package config

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func LoadDotenvFiles(root string, files ...string) error {
	originalEnv := currentEnvKeys()
	loaded := map[string]bool{}

	var errs []error
	for _, name := range files {
		path := filepath.Join(root, name)
		if err := loadDotenvFile(path, originalEnv, loaded); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func currentEnvKeys() map[string]bool {
	keys := map[string]bool{}
	for _, item := range os.Environ() {
		if key, _, ok := strings.Cut(item, "="); ok {
			keys[key] = true
		}
	}
	return keys
}

func loadDotenvFile(path string, originalEnv map[string]bool, loaded map[string]bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotenvLine(scanner.Text())
		if !ok {
			continue
		}
		if originalEnv[key] {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
		loaded[key] = true
	}
	return scanner.Err()
}

func parseDotenvLine(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}

	key, value, ok := strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" {
		return "", "", false
	}

	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') ||
			(value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}

	return key, value, true
}
