package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"
)

var (
	ErrProcessIdentityMismatch = errors.New("process identity mismatch")
	ErrProcessNotRunning       = errors.New("process is not running")
	ErrProcessUnverifiable     = errors.New("process identity is unverifiable")
)

type ProcessIdentity struct {
	PID       int    `json:"pid"`
	GroupID   int    `json:"group_id,omitempty"`
	StartedAt string `json:"started_at"`
	Platform  string `json:"platform"`
	Nonce     string `json:"nonce,omitempty"`
}

type CommandSpec struct {
	WorkDir string
	Binary  string
	Args    []string
	Stdin   string
	Timeout time.Duration
}

type AgentLaunchDisposition string

const (
	AgentLaunchNotStarted      AgentLaunchDisposition = "not_started"
	AgentLaunchOwned           AgentLaunchDisposition = "owned"
	AgentLaunchCleanupVerified AgentLaunchDisposition = "started_cleanup_verified"
	AgentLaunchCleanupPending  AgentLaunchDisposition = "started_cleanup_pending"
)

type OwnedStartError struct {
	Cause         error
	NativeStarted bool
	Identity      ProcessIdentity
	Cleanup       ProcessTerminationResult
	CleanupError  error
}

func (e *OwnedStartError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return "owned process start failed"
	}
	return e.Cause.Error()
}

func (e *OwnedStartError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (p ProcessIdentity) Encode() string {
	data, _ := json.Marshal(p)
	return string(data)
}

func ValidateProcessIdentity(id ProcessIdentity) error {
	if id.PID <= 0 || id.Platform == "" {
		return ErrProcessUnverifiable
	}
	switch id.Platform {
	case "windows":
		if id.Nonce == "" {
			return ErrProcessUnverifiable
		}
		return nil
	case "linux":
		if id.StartedAt == "" {
			return ErrProcessUnverifiable
		}
		return nil
	default:
		if id.StartedAt == "" {
			return ErrProcessUnverifiable
		}
		return nil
	}
}

func DecodeProcessIdentity(raw string) (ProcessIdentity, error) {
	var id ProcessIdentity
	if raw == "" {
		return id, ErrProcessUnverifiable
	}
	if err := json.Unmarshal([]byte(raw), &id); err != nil {
		return id, err
	}
	if err := ValidateProcessIdentity(id); err != nil {
		return id, err
	}
	return id, nil
}

type ProcessController interface {
	StartOwned(ctx context.Context, spec CommandSpec) (OwnedProcess, error)
	OpenOwned(identity ProcessIdentity) (OwnedProcess, error)
}

func DefaultProcessController() ProcessController {
	return defaultProcessController{}
}

type OwnedProcess interface {
	Identity() ProcessIdentity
	Stdout() io.ReadCloser
	Stderr() io.ReadCloser
	Wait() error
	TreeRunning() (bool, error)
	Terminate(gracefulTimeout time.Duration) (ProcessTerminationResult, error)
	Release() error
}

type ProcessTerminationResult struct {
	VerifiedAbsent bool
	AlreadyAbsent  bool
	Forced         bool
}

func processStartedAtString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
