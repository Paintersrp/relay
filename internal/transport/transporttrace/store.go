package transporttrace

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	mu           sync.Mutex
	directory    string
	mappingID    string
	policy       RetentionPolicy
	segmentBytes int64
	now          func() time.Time
	active       *os.File
	activePath   string
	activeSize   int64
	sequence     uint64
}

type storeOptions struct {
	segmentBytes int64
	now          func() time.Time
}

func NewStore(root, mappingID string, policy RetentionPolicy) (*Store, error) {
	return newStore(root, mappingID, policy, storeOptions{})
}

func newStore(root, mappingID string, policy RetentionPolicy, options storeOptions) (*Store, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if mappingID == "" {
		return nil, fmt.Errorf("trace mapping ID is required")
	}
	if options.segmentBytes <= 0 {
		options.segmentBytes = DefaultSegmentBytes
	}
	if options.now == nil {
		options.now = time.Now
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create trace root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("protect trace root: %w", err)
	}
	directory := filepath.Join(root, mappingID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create mapping trace directory: %w", err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("protect mapping trace directory: %w", err)
	}
	segments, err := listSegments(directory, mappingID)
	if err != nil {
		return nil, fmt.Errorf("list trace segments: %w", err)
	}
	var sequence uint64
	if len(segments) > 0 {
		sequence = segments[len(segments)-1].sequence
	}
	return &Store{
		directory:    directory,
		mappingID:    mappingID,
		policy:       policy,
		segmentBytes: options.segmentBytes,
		now:          options.now,
		sequence:     sequence,
	}, nil
}

func (store *Store) Append(record Record) (int, error) {
	line, err := MarshalLine(record)
	if err != nil {
		return 0, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	now := store.now().UTC()
	removed, err := pruneSegments(store.directory, store.mappingID, store.activePath, store.policy, now)
	if err != nil {
		return removed, fmt.Errorf("prune trace segments before append: %w", err)
	}
	if err := store.ensureSegment(now, int64(len(line))); err != nil {
		return removed, err
	}
	written := 0
	for written < len(line) {
		count, writeErr := store.active.Write(line[written:])
		written += count
		if writeErr != nil {
			return removed, fmt.Errorf("append trace record: %w", writeErr)
		}
		if count == 0 {
			return removed, fmt.Errorf("append trace record: short write")
		}
	}
	store.activeSize += int64(written)
	if err := store.active.Sync(); err != nil {
		return removed, fmt.Errorf("sync trace record: %w", err)
	}
	after, err := pruneSegments(store.directory, store.mappingID, store.activePath, store.policy, now)
	if err != nil {
		return removed, fmt.Errorf("prune trace segments after append: %w", err)
	}
	return removed + after, nil
}

func (store *Store) Prune() (int, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return pruneSegments(store.directory, store.mappingID, store.activePath, store.policy, store.now().UTC())
}

func (store *Store) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.active == nil {
		return nil
	}
	err := store.active.Close()
	store.active = nil
	store.activePath = ""
	store.activeSize = 0
	return err
}

func (store *Store) ensureSegment(now time.Time, nextRecordBytes int64) error {
	limit := store.segmentBytes
	if store.policy.MaxBytes < limit {
		limit = store.policy.MaxBytes
	}
	if store.active != nil && store.activeSize > 0 && store.activeSize+nextRecordBytes > limit {
		if err := store.active.Close(); err != nil {
			return fmt.Errorf("close trace segment: %w", err)
		}
		store.active = nil
		store.activePath = ""
		store.activeSize = 0
	}
	if store.active != nil {
		return nil
	}
	store.sequence++
	name := fmt.Sprintf("%s-%s-%06d.jsonl", store.mappingID, now.Format("20060102T150405.000000000Z"), store.sequence)
	path := filepath.Join(store.directory, name)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create trace segment: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect trace segment: %w", err)
	}
	store.active = file
	store.activePath = path
	store.activeSize = 0
	return nil
}
