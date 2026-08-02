package sourceindexruntime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"relay/internal/sourceindex"
)

func clearIndexEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"RELAY_SOURCE_INDEX_DIR", "RELAY_SOURCE_INDEXER_PATH", "RELAY_SOURCE_INDEX_FILE_LIMIT_BYTES", "RELAY_SOURCE_INDEX_BUILD_PARALLELISM", "RELAY_SOURCE_INDEX_QUERY_TIMEOUT_MS"} {
		t.Setenv(name, "")
	}
}

func TestLoadConfigDisabledAndRejectsStraySettings(t *testing.T) {
	clearIndexEnv(t)
	c, err := LoadConfig(sourceindex.ProtectedStorage{})
	if err != nil || c.Enabled || c.FileLimitBytes != fixedFileLimit || c.BuildParallelism != 1 || c.QueryTimeout != 5*time.Second {
		t.Fatalf("disabled config = %#v, err=%v", c, err)
	}
	t.Setenv("RELAY_SOURCE_INDEX_QUERY_TIMEOUT_MS", "1")
	if _, err := LoadConfig(sourceindex.ProtectedStorage{}); err == nil {
		t.Fatal("stray setting accepted")
	}
}

func TestLoadConfigEnabledValidation(t *testing.T) {
	clearIndexEnv(t)
	root := t.TempDir()
	indexer := filepath.Join(root, "indexer")
	mode := os.FileMode(0o755)
	if runtime.GOOS == "windows" {
		mode = 0o644
	}
	if err := os.WriteFile(indexer, []byte("indexer"), mode); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELAY_SOURCE_INDEX_DIR", filepath.Join(root, "indexes"))
	t.Setenv("RELAY_SOURCE_INDEXER_PATH", indexer)
	t.Setenv("RELAY_SOURCE_INDEX_BUILD_PARALLELISM", "2")
	t.Setenv("RELAY_SOURCE_INDEX_QUERY_TIMEOUT_MS", "25")
	c, err := LoadConfig(sourceindex.ProtectedStorage{})
	if err != nil || !c.Enabled || c.BuildParallelism != 2 || c.QueryTimeout != 25*time.Millisecond {
		t.Fatalf("enabled config = %#v, err=%v", c, err)
	}
	for name, value := range map[string]string{"RELAY_SOURCE_INDEX_BUILD_PARALLELISM": "17", "RELAY_SOURCE_INDEX_QUERY_TIMEOUT_MS": "0", "RELAY_SOURCE_INDEX_FILE_LIMIT_BYTES": "1"} {
		clearIndexEnv(t)
		t.Setenv("RELAY_SOURCE_INDEX_DIR", filepath.Join(root, "other"))
		t.Setenv("RELAY_SOURCE_INDEXER_PATH", indexer)
		t.Setenv(name, value)
		if _, err := LoadConfig(sourceindex.ProtectedStorage{}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestLoadConfigRejectsProtectedOverlap(t *testing.T) {
	clearIndexEnv(t)
	root := t.TempDir()
	indexer := filepath.Join(root, "indexer")
	if err := os.WriteFile(indexer, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	protected := filepath.Join(root, "vault")
	if err := os.Mkdir(protected, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RELAY_SOURCE_INDEX_DIR", protected)
	t.Setenv("RELAY_SOURCE_INDEXER_PATH", indexer)
	if _, err := LoadConfig(sourceindex.ProtectedStorage{SourceVaultRoot: protected}); err == nil {
		t.Fatal("protected overlap accepted")
	}
}
