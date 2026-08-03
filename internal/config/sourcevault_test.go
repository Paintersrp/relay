package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSourceVaultDirForUsesDurablePlatformData(t *testing.T) {
	tests := []struct{ name, goos, local, xdg, home, want string }{
		{"windows", "windows", `C:\\Users\\relay\\AppData\\Local`, "", `C:\\Users\\relay`, filepath.Join(`C:\\Users\\relay\\AppData\\Local`, "Relay", "source-vaults")},
		{"macos", "darwin", "", "", "/Users/relay", filepath.Join("/Users/relay", "Library", "Application Support", "Relay", "source-vaults")},
		{"xdg", "linux", "", "/var/lib/relay-data", "/home/relay", filepath.Join("/var/lib/relay-data", "relay", "source-vaults")},
		{"xdg without home", "linux", "", "/var/lib/relay-data", "", filepath.Join("/var/lib/relay-data", "relay", "source-vaults")},
		{"unix fallback", "linux", "", "", "/home/relay", filepath.Join("/home/relay", ".local", "share", "relay", "source-vaults")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, explicit, err := ResolveSourceVaultDirFor(test.goos, test.local, test.xdg, test.home)
			want, wantErr := filepath.Abs(test.want)
			if err != nil || wantErr != nil || explicit || got != filepath.Clean(want) {
				t.Fatalf("ResolveSourceVaultDirFor() = %q, %v, %v; want %q, false, nil", got, explicit, err, filepath.Clean(want))
			}
		})
	}
}

func TestResolveSourceVaultDirCollectsOnlyRequiredProcessState(t *testing.T) {
	failingHome := func() (string, error) { return "", errors.New("home unavailable") }
	tests := []struct {
		name    string
		goos    string
		env     map[string]string
		wantErr bool
	}{
		{"windows uses local app data", "windows", map[string]string{"LOCALAPPDATA": `C:\\Relay`}, false},
		{"xdg bypasses home", "linux", map[string]string{"XDG_DATA_HOME": "/var/lib/relay"}, false},
		{"macos requires home", "darwin", nil, true},
		{"unix fallback requires home", "linux", nil, true},
		{"explicit bypasses defaults", "linux", map[string]string{"RELAY_SOURCE_VAULT_DIR": "vault"}, false},
		{"empty explicit override", "linux", map[string]string{"RELAY_SOURCE_VAULT_DIR": " "}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, explicit, err := resolveSourceVaultDir(processSourceVaultState{
				goos: test.goos,
				lookupEnv: func(key string) (string, bool) { value, ok := test.env[key]; return value, ok },
				homeDir: failingHome,
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("ResolveSourceVaultDir() = %q, %v, %v", got, explicit, err)
			}
			if test.name == "explicit bypasses defaults" && !explicit {
				t.Fatal("explicit override indicator was not preserved")
			}
		})
	}
}

func TestResolveSourceVaultDirDoesNotDependOnWorkingDirectory(t *testing.T) {
	t.Setenv("RELAY_SOURCE_VAULT_DIR", "")
	if err := os.Unsetenv("RELAY_SOURCE_VAULT_DIR"); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	first, _, err := ResolveSourceVaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	second, _, err := ResolveSourceVaultDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("default changed with working directory: %q != %q", first, second)
	}
}

func TestResolveSourceVaultDirHonorsExplicitOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "retained-source")
	t.Setenv("RELAY_SOURCE_VAULT_DIR", override)
	got, explicit, err := ResolveSourceVaultDir()
	if err != nil || !explicit || got != filepath.Clean(override) {
		t.Fatalf("ResolveSourceVaultDir() = %q, %v, %v; want %q, true, nil", got, explicit, err, filepath.Clean(override))
	}
}
