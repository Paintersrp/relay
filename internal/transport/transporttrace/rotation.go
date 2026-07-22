package transporttrace

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultMaxAge       = 14 * 24 * time.Hour
	MinimumMaxAge       = time.Hour
	DefaultMaxBytes     = int64(100 << 20)
	MinimumMaxBytes     = int64(1 << 20)
	DefaultSegmentBytes = int64(8 << 20)
)

type RetentionPolicy struct {
	MaxAge   time.Duration
	MaxBytes int64
}

func (policy RetentionPolicy) Validate() error {
	if policy.MaxAge < MinimumMaxAge || policy.MaxAge > DefaultMaxAge {
		return fmt.Errorf("trace maximum age is outside the allowed range")
	}
	if policy.MaxBytes < MinimumMaxBytes || policy.MaxBytes > DefaultMaxBytes {
		return fmt.Errorf("trace maximum bytes is outside the allowed range")
	}
	return nil
}

type segmentInfo struct {
	path     string
	name     string
	sequence uint64
	modTime  time.Time
	size     int64
}

func listSegments(directory, mappingID string) ([]segmentInfo, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	prefix := mappingID + "-"
	segments := make([]segmentInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		sequence, ok := segmentSequence(entry.Name())
		if !ok {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		segments = append(segments, segmentInfo{
			path:     filepath.Join(directory, entry.Name()),
			name:     entry.Name(),
			sequence: sequence,
			modTime:  info.ModTime().UTC(),
			size:     info.Size(),
		})
	}
	sort.Slice(segments, func(left, right int) bool {
		if segments[left].sequence == segments[right].sequence {
			return segments[left].name < segments[right].name
		}
		return segments[left].sequence < segments[right].sequence
	})
	return segments, nil
}

func segmentSequence(name string) (uint64, bool) {
	trimmed := strings.TrimSuffix(name, ".jsonl")
	parts := strings.Split(trimmed, "-")
	if len(parts) < 3 {
		return 0, false
	}
	value, err := strconv.ParseUint(parts[len(parts)-1], 10, 64)
	return value, err == nil
}

func pruneSegments(directory, mappingID, activePath string, policy RetentionPolicy, now time.Time) (int, error) {
	segments, err := listSegments(directory, mappingID)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, segment := range segments {
		if segment.path == activePath || now.Sub(segment.modTime) <= policy.MaxAge {
			continue
		}
		if err := os.Remove(segment.path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		removed++
	}
	segments, err = listSegments(directory, mappingID)
	if err != nil {
		return removed, err
	}
	var total int64
	for _, segment := range segments {
		total += segment.size
	}
	for _, segment := range segments {
		if total <= policy.MaxBytes {
			break
		}
		if segment.path == activePath {
			continue
		}
		if err := os.Remove(segment.path); err != nil && !os.IsNotExist(err) {
			return removed, err
		}
		total -= segment.size
		removed++
	}
	return removed, nil
}
