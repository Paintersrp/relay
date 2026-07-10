package executor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	WorkflowLiveOutputLimitBytes    = 64 * 1024
	WorkflowRunnerCaptureLimitBytes = 256 * 1024
)

type workflowOutputSnapshot struct {
	Text       string
	Truncated  bool
	TotalBytes int64
}

type workflowTailBuffer struct {
	limit     int
	total     int64
	truncated bool
	data      []byte
}

func newWorkflowTailBuffer(limit int) *workflowTailBuffer {
	return &workflowTailBuffer{limit: limit}
}

func (b *workflowTailBuffer) Write(p []byte) {
	b.total += int64(len(p))
	if len(p) == 0 {
		return
	}
	if b.limit <= 0 {
		b.data = append(b.data, p...)
		return
	}
	if len(p) >= b.limit {
		b.data = append(b.data[:0], p[len(p)-b.limit:]...)
		b.truncated = b.total > int64(b.limit)
		return
	}
	b.data = append(b.data, p...)
	if overflow := len(b.data) - b.limit; overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:b.limit]
		b.truncated = true
	}
}

func (b *workflowTailBuffer) Snapshot() workflowOutputSnapshot {
	return workflowOutputSnapshot{
		Text:       string(append([]byte(nil), b.data...)),
		Truncated:  b.truncated,
		TotalBytes: b.total,
	}
}

type streamSecretRedactor struct {
	secrets      [][]byte
	maxSecretLen int
	pending      []byte
}

func newStreamSecretRedactor() *streamSecretRedactor {
	seen := map[string]struct{}{}
	values := make([]string, 0, len(knownSecrets))
	for _, key := range knownSecrets {
		value := strings.TrimSpace(os.Getenv(key))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})
	redactor := &streamSecretRedactor{secrets: make([][]byte, 0, len(values))}
	for _, value := range values {
		secret := []byte(value)
		redactor.secrets = append(redactor.secrets, secret)
		if len(secret) > redactor.maxSecretLen {
			redactor.maxSecretLen = len(secret)
		}
	}
	return redactor
}

func (r *streamSecretRedactor) Write(p []byte) []byte {
	r.pending = append(r.pending, p...)
	return r.drain(false)
}

func (r *streamSecretRedactor) Close() []byte {
	return r.drain(true)
}

func (r *streamSecretRedactor) drain(final bool) []byte {
	var output bytes.Buffer
	for len(r.pending) > 0 {
		longestFullMatch := 0
		longerMatchStillPossible := false
		for _, secret := range r.secrets {
			if len(r.pending) >= len(secret) && bytes.Equal(r.pending[:len(secret)], secret) {
				if len(secret) > longestFullMatch {
					longestFullMatch = len(secret)
				}
			}
			if len(r.pending) < r.maxSecretLen && len(r.pending) < len(secret) && bytes.Equal(secret[:len(r.pending)], r.pending) {
				longerMatchStillPossible = true
			}
		}
		if longerMatchStillPossible && !final {
			break
		}
		if longestFullMatch > 0 {
			output.WriteString("[REDACTED]")
			r.pending = r.pending[longestFullMatch:]
			continue
		}
		output.WriteByte(r.pending[0])
		r.pending = r.pending[1:]
	}
	return output.Bytes()
}

type workflowOutputCapture struct {
	mu       sync.Mutex
	file     *os.File
	path     string
	tail     *workflowTailBuffer
	redactor *streamSecretRedactor
	writeErr error
	closed   bool
}

func newWorkflowOutputCapture(path string, liveLimit int) (*workflowOutputCapture, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create workflow output spool: %w", err)
	}
	return &workflowOutputCapture{
		file:     file,
		path:     path,
		tail:     newWorkflowTailBuffer(liveLimit),
		redactor: newStreamSecretRedactor(),
	}, nil
}

func (c *workflowOutputCapture) Write(chunk []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.writeErr != nil {
		return
	}
	c.writeRedacted(c.redactor.Write(chunk))
}

func (c *workflowOutputCapture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return c.writeErr
	}
	c.writeRedacted(c.redactor.Close())
	if syncErr := c.file.Sync(); c.writeErr == nil && syncErr != nil {
		c.writeErr = syncErr
	}
	if closeErr := c.file.Close(); c.writeErr == nil && closeErr != nil {
		c.writeErr = closeErr
	}
	c.closed = true
	return c.writeErr
}

func (c *workflowOutputCapture) Snapshot() workflowOutputSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tail.Snapshot()
}

func (c *workflowOutputCapture) Path() string {
	return c.path
}

func (c *workflowOutputCapture) writeRedacted(data []byte) {
	if len(data) == 0 || c.writeErr != nil {
		return
	}
	if _, err := c.file.Write(data); err != nil {
		c.writeErr = err
		return
	}
	c.tail.Write(data)
}

func redactFileToPath(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	redactor := newStreamSecretRedactor()
	buffer := make([]byte, 32*1024)
	var writeErr error
	for {
		count, readErr := source.Read(buffer)
		if count > 0 {
			if _, err := destination.Write(redactor.Write(buffer[:count])); err != nil {
				writeErr = err
				break
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				writeErr = readErr
			}
			break
		}
	}
	if writeErr == nil {
		if _, err := destination.Write(redactor.Close()); err != nil {
			writeErr = err
		}
	}
	if syncErr := destination.Sync(); writeErr == nil && syncErr != nil {
		writeErr = syncErr
	}
	if closeErr := destination.Close(); writeErr == nil && closeErr != nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(destinationPath)
	}
	return writeErr
}

func readFileTail(path string, limit int) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	capture := newWorkflowTailBuffer(limit)
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := file.Read(buffer)
		if count > 0 {
			capture.Write(buffer[:count])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return nil, false, readErr
			}
			break
		}
	}
	snapshot := capture.Snapshot()
	return []byte(snapshot.Text), snapshot.Truncated, nil
}
