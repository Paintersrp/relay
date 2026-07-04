package pipeline

import (
	"encoding/json"
	"errors"
	"os/exec"
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

func (p ProcessIdentity) Encode() string {
	data, _ := json.Marshal(p)
	return string(data)
}

func DecodeProcessIdentity(raw string) (ProcessIdentity, error) {
	var id ProcessIdentity
	if raw == "" {
		return id, ErrProcessUnverifiable
	}
	if err := json.Unmarshal([]byte(raw), &id); err != nil {
		return id, err
	}
	if id.PID <= 0 || id.StartedAt == "" {
		return id, ErrProcessUnverifiable
	}
	return id, nil
}

type ProcessController interface {
	PrepareCommand(cmd *exec.Cmd) error
	Identity(cmd *exec.Cmd, startedAt time.Time) (ProcessIdentity, error)
	IsRunning(identity ProcessIdentity) (bool, error)
	TerminateTree(identity ProcessIdentity, gracefulTimeout time.Duration) (ProcessTerminationResult, error)
}

func DefaultProcessController() ProcessController {
	return defaultProcessController{}
}

type ProcessTerminationResult struct {
	VerifiedAbsent bool
	AlreadyAbsent  bool
	Forced         bool
}

func processStartedAtString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
