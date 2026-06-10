package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDotenvLineKeyValue(t *testing.T) {
	key, value, ok := parseDotenvLine("FOO=bar")
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "FOO" {
		t.Fatalf("expected FOO, got %q", key)
	}
	if value != "bar" {
		t.Fatalf("expected bar, got %q", value)
	}
}

func TestParseDotenvLineQuotedValue(t *testing.T) {
	key, value, ok := parseDotenvLine(`FOO="bar baz"`)
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "FOO" {
		t.Fatalf("expected FOO, got %q", key)
	}
	if value != "bar baz" {
		t.Fatalf("expected bar baz, got %q", value)
	}
}

func TestParseDotenvLineSingleQuotedValue(t *testing.T) {
	key, value, ok := parseDotenvLine(`FOO='bar baz'`)
	if !ok {
		t.Fatal("expected ok")
	}
	if key != "FOO" {
		t.Fatalf("expected FOO, got %q", key)
	}
	if value != "bar baz" {
		t.Fatalf("expected bar baz, got %q", value)
	}
}

func TestParseDotenvIgnoresBlankAndCommentLines(t *testing.T) {
	for _, line := range []string{"", "   ", "# comment", "  # indented comment"} {
		_, _, ok := parseDotenvLine(line)
		if ok {
			t.Fatalf("expected not ok for line %q", line)
		}
	}
}

func TestParseDotenvIgnoresNoEquals(t *testing.T) {
	_, _, ok := parseDotenvLine("justakey")
	if ok {
		t.Fatal("expected not ok for line without equals")
	}
}

func TestParseDotenvIgnoresEmptyKey(t *testing.T) {
	_, _, ok := parseDotenvLine("=value")
	if ok {
		t.Fatal("expected not ok for empty key")
	}
}

func TestLoadDotenvDoesNotOverrideExistingEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("RELAY_TEST_DOTENV_EXISTING=from-file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("RELAY_TEST_DOTENV_EXISTING", "from-shell")
	if err := LoadDotenvFiles(dir, ".env"); err != nil {
		t.Fatalf("load dotenv: %v", err)
	}
	if got := os.Getenv("RELAY_TEST_DOTENV_EXISTING"); got != "from-shell" {
		t.Fatalf("expected from-shell, got %q", got)
	}
}

func TestLoadDotenvLaterFileOverridesEarlierFileWhenEnvNotSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("RELAY_TEST_DOTENV_OVERRIDE=base\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.local"), []byte("RELAY_TEST_DOTENV_OVERRIDE=local\n"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("RELAY_TEST_DOTENV_OVERRIDE")
	t.Cleanup(func() { os.Unsetenv("RELAY_TEST_DOTENV_OVERRIDE") })

	if err := LoadDotenvFiles(dir, ".env", ".env.local"); err != nil {
		t.Fatalf("load dotenv: %v", err)
	}
	if got := os.Getenv("RELAY_TEST_DOTENV_OVERRIDE"); got != "local" {
		t.Fatalf("expected local, got %q", got)
	}
}

func TestLoadDotenvMissingFilesIsNotFatal(t *testing.T) {
	dir := t.TempDir()
	if err := LoadDotenvFiles(dir, ".env", ".env.local", ".nonexistent"); err != nil {
		t.Fatalf("expected no error for missing files, got: %v", err)
	}
}

func TestLoadDotenvLoadsUnsetKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("RELAY_TEST_DOTENV_NEWKEY=loaded\n"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("RELAY_TEST_DOTENV_NEWKEY")
	t.Cleanup(func() { os.Unsetenv("RELAY_TEST_DOTENV_NEWKEY") })

	if err := LoadDotenvFiles(dir, ".env"); err != nil {
		t.Fatalf("load dotenv: %v", err)
	}
	if got := os.Getenv("RELAY_TEST_DOTENV_NEWKEY"); got != "loaded" {
		t.Fatalf("expected loaded, got %q", got)
	}
}
