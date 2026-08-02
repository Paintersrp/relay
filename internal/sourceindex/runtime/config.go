package sourceindexruntime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"relay/internal/sourceindex"
)

const fixedFileLimit int64 = 67108864

var ErrUnsupportedPlatform = errors.New("source-index runtime unsupported on this platform")

type Config struct {
	Enabled          bool
	IndexRoot        string
	IndexerPath      string
	FileLimitBytes   int64
	BuildParallelism int
	QueryTimeout     time.Duration
	ProtectedStorage sourceindex.ProtectedStorage
}

func LoadConfig(protected sourceindex.ProtectedStorage) (Config, error) {
	names := []string{"RELAY_SOURCE_INDEX_DIR", "RELAY_SOURCE_INDEXER_PATH", "RELAY_SOURCE_INDEX_FILE_LIMIT_BYTES", "RELAY_SOURCE_INDEX_BUILD_PARALLELISM", "RELAY_SOURCE_INDEX_QUERY_TIMEOUT_MS"}
	values := make(map[string]string, len(names))
	for _, name := range names {
		values[name] = os.Getenv(name)
		if strings.TrimSpace(values[name]) != values[name] {
			return Config{}, fmt.Errorf("%s contains surrounding whitespace", name)
		}
	}
	if values[names[0]] == "" {
		for _, name := range names[1:] {
			if values[name] != "" {
				return Config{}, errors.New("source-index settings require RELAY_SOURCE_INDEX_DIR")
			}
		}
		return Config{FileLimitBytes: fixedFileLimit, BuildParallelism: 1, QueryTimeout: 5 * time.Second}, nil
	}
	if !runtimeSupported() {
		return Config{}, ErrUnsupportedPlatform
	}
	if values[names[1]] == "" {
		return Config{}, errors.New("RELAY_SOURCE_INDEXER_PATH is required")
	}
	root, err := filepath.Abs(values[names[0]])
	if err != nil {
		return Config{}, errors.New("invalid source-index directory")
	}
	indexer, err := filepath.Abs(values[names[1]])
	if err != nil {
		return Config{}, errors.New("invalid source-index executable")
	}
	c := Config{Enabled: true, IndexRoot: filepath.Clean(root), IndexerPath: filepath.Clean(indexer), FileLimitBytes: fixedFileLimit, BuildParallelism: 1, QueryTimeout: 5 * time.Second, ProtectedStorage: protected}
	resolvedIndexer, err := filepath.EvalSymlinks(c.IndexerPath)
	if err != nil || filepath.Clean(resolvedIndexer) != c.IndexerPath {
		return Config{}, errors.New("unsafe source-index executable")
	}
	if v := values[names[2]]; v != "" {
		c.FileLimitBytes, err = strconv.ParseInt(v, 10, 64)
		if err != nil || c.FileLimitBytes != fixedFileLimit {
			return Config{}, errors.New("unsupported source-index file limit")
		}
	}
	if v := values[names[3]]; v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 16 {
			return Config{}, errors.New("invalid source-index build parallelism")
		}
		c.BuildParallelism = n
	}
	if v := values[names[4]]; v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 60000 {
			return Config{}, errors.New("invalid source-index query timeout")
		}
		c.QueryTimeout = time.Duration(n) * time.Millisecond
	}
	if err := sourceindex.ValidateIndexRoot(c.IndexRoot, protected); err != nil {
		return Config{}, fmt.Errorf("invalid source-index storage: %w", err)
	}
	return c, nil
}
