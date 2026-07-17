package workflowrepos

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	workflowstore "relay/internal/store/workflow"
)

const (
	InspectionStateReady                = "ready"
	InspectionStateNeedsRemoteSelection = "needs_remote_selection"
	InspectionStateNeedsTargetOverride  = "needs_target_override"
	InspectionStateConflict             = "conflict"

	RegistrationDispositionCreate = "create"
	RegistrationDispositionReuse  = "reuse"

	RegistrationOutcomeCreated = "created"
	RegistrationOutcomeReused  = "reused"

	RepoTargetSourceRemoteBasename   = "remote_basename"
	RepoTargetSourceOperatorOverride = "operator_override"

	TargetOverrideReasonNoUsableRemote    = "no_usable_remote"
	TargetOverrideReasonUnsupportedRemote = "unsupported_remote"

	ConflictKindTarget = "target"
	ConflictKindPath   = "path"
)

var (
	ErrInvalidRepositoryPath             = errors.New("invalid repository path")
	ErrGitUnavailable                    = errors.New("Git executable is unavailable")
	ErrGitTimeout                        = errors.New("Git command timed out")
	ErrGitOutputLimit                    = errors.New("Git command output exceeded the limit")
	ErrRegistrationFinalStateUnavailable = errors.New("repository registration final state is unavailable")
)

const (
	gitCommandTimeout          = 5 * time.Second
	gitOutputLimit             = 64 * 1024
	registrationRecheckTimeout = 5 * time.Second
	registrationRecheckDelay   = 20 * time.Millisecond
)

type InspectionInput struct {
	LocalPath                   string
	RemoteName                  string
	RepoTargetOverride          string
	ProposedConfiguredBranchRef string
}

type ConfirmationInput struct {
	LocalPath                   string
	RemoteName                  string
	RepoTargetOverride          string
	ProposedConfiguredBranchRef string
	ExpectedConfirmationHash    string
}

type RemoteCandidate struct {
	Name                string
	URL                 string
	SuggestedRepoTarget string
}

type Inspection struct {
	State                        string
	SelectedPath                 string
	ResolvedLocalPath            string
	Remotes                      []RemoteCandidate
	SelectedRemote               *RemoteCandidate
	SuggestedRepoTarget          string
	TargetOverrideReason         string
	RepoTarget                   string
	RepoTargetSource             string
	RegistrationDisposition      string
	ExistingRepository           *workflowstore.RepositoryTarget
	CurrentConfiguredBranchRef   sql.NullString
	ExpectedConfigurationVersion int64
	ProposedConfiguredBranchRef  sql.NullString
	ProposedConfigurationVersion int64
	ProposedBranchCommitOID      string
	ProposedBranchTreeOID        string
	ConfigurationDisposition     string
	ConflictKind                 string
	ConfirmationHash             string
	Notices                      []string
}

type RegistrationResult struct {
	Outcome                  string
	ConfigurationDisposition string
	Repository               workflowstore.RepositoryTarget
}

type ConfirmationError struct {
	Reason     string
	Inspection Inspection
}

func (e *ConfirmationError) Error() string {
	switch e.Reason {
	case "stale":
		return "repository inspection is stale and must be confirmed again"
	case "conflict":
		return "repository registration conflicts with an existing registration"
	default:
		return "repository inspection is not ready for confirmation"
	}
}

type GitCommandResult struct {
	Stdout string
	Stderr string
}

type GitRunner interface {
	Run(context.Context, string, ...string) (GitCommandResult, error)
}

type gitCommandFactory func(context.Context, ...string) *exec.Cmd

type execInspectionRunner struct {
	commandFactory gitCommandFactory
	timeout        time.Duration
	outputLimit    int
}

func newExecGitRunner() execInspectionRunner {
	return execInspectionRunner{
		commandFactory: func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "git", args...)
		},
		timeout:     gitCommandTimeout,
		outputLimit: gitOutputLimit,
	}
}

func (r execInspectionRunner) Run(ctx context.Context, directory string, args ...string) (GitCommandResult, error) {
	timeout := r.timeout
	if timeout <= 0 {
		timeout = gitCommandTimeout
	}
	outputLimit := r.outputLimit
	if outputLimit <= 0 {
		outputLimit = gitOutputLimit
	}
	commandFactory := r.commandFactory
	if commandFactory == nil {
		commandFactory = newExecGitRunner().commandFactory
	}

	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	commandArgs := append([]string{"-C", directory}, args...)
	command := commandFactory(commandContext, commandArgs...)
	var stdout boundedBuffer
	var stderr boundedBuffer
	stdout.limit = outputLimit
	stderr.limit = outputLimit
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := GitCommandResult{
		Stdout: strings.TrimSpace(stdout.String()),
		Stderr: strings.TrimSpace(stderr.String()),
	}
	if errors.Is(stdout.err, ErrGitOutputLimit) || errors.Is(stderr.err, ErrGitOutputLimit) {
		return result, ErrGitOutputLimit
	}
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		return result, ErrGitTimeout
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return result, ErrGitUnavailable
	}
	return result, err
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
	err    error
}

func (b *boundedBuffer) Write(value []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.err = ErrGitOutputLimit
		return 0, b.err
	}
	if len(value) > remaining {
		_, _ = b.buffer.Write(value[:remaining])
		b.err = ErrGitOutputLimit
		return remaining, b.err
	}
	return b.buffer.Write(value)
}

func (b *boundedBuffer) String() string {
	return b.buffer.String()
}

func (r *Registry) Inspect(ctx context.Context, input InspectionInput) (Inspection, error) {
	input.LocalPath = strings.TrimSpace(input.LocalPath)
	input.RemoteName = strings.TrimSpace(input.RemoteName)
	input.RepoTargetOverride = strings.TrimSpace(input.RepoTargetOverride)

	if input.LocalPath == "" {
		return Inspection{}, fmt.Errorf("%w: repository path is required", ErrInvalidRepositoryPath)
	}
	if input.RepoTargetOverride != "" {
		if err := validateRepoTarget(input.RepoTargetOverride); err != nil {
			return Inspection{}, err
		}
	}

	selectedPath, err := resolveDirectory(input.LocalPath)
	if err != nil {
		return Inspection{}, fmt.Errorf("%w: %v", ErrInvalidRepositoryPath, err)
	}
	selectedBareResult, selectedBareErr := r.runner.Run(
		ctx,
		selectedPath,
		"rev-parse",
		"--is-bare-repository",
	)
	if selectedBareErr == nil && strings.EqualFold(strings.TrimSpace(selectedBareResult.Stdout), "true") {
		return Inspection{}, fmt.Errorf("%w: bare repositories cannot be registered", ErrInvalidRepositoryPath)
	}
	if errors.Is(selectedBareErr, ErrGitUnavailable) ||
		errors.Is(selectedBareErr, ErrGitTimeout) ||
		errors.Is(selectedBareErr, ErrGitOutputLimit) {
		return Inspection{}, selectedBareErr
	}
	rootResult, err := r.runner.Run(ctx, selectedPath, "rev-parse", "--show-toplevel")
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return Inspection{}, err
		}
		return Inspection{}, fmt.Errorf("%w: path is not inside a Git worktree: %s", ErrInvalidRepositoryPath, gitDiagnostic(rootResult))
	}
	resolvedRoot, err := resolveDirectory(rootResult.Stdout)
	if err != nil {
		return Inspection{}, fmt.Errorf("%w: resolve Git worktree root: %v", ErrInvalidRepositoryPath, err)
	}
	bareResult, err := r.runner.Run(ctx, resolvedRoot, "rev-parse", "--is-bare-repository")
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return Inspection{}, err
		}
		return Inspection{}, fmt.Errorf("%w: inspect Git worktree: %s", ErrInvalidRepositoryPath, gitDiagnostic(bareResult))
	}
	if strings.EqualFold(strings.TrimSpace(bareResult.Stdout), "true") {
		return Inspection{}, fmt.Errorf("%w: bare repositories cannot be registered", ErrInvalidRepositoryPath)
	}

	inspection := Inspection{
		SelectedPath:      input.LocalPath,
		ResolvedLocalPath: resolvedRoot,
		Remotes:           []RemoteCandidate{},
		Notices:           []string{},
	}
	remoteResult, err := r.runner.Run(ctx, resolvedRoot, "remote")
	if err != nil {
		if errors.Is(err, ErrGitUnavailable) || errors.Is(err, ErrGitTimeout) || errors.Is(err, ErrGitOutputLimit) {
			return Inspection{}, err
		}
		return Inspection{}, fmt.Errorf("list configured Git remotes: %s", gitDiagnostic(remoteResult))
	}
	for _, name := range splitNonblankLines(remoteResult.Stdout) {
		urlResult, urlErr := r.runner.Run(ctx, resolvedRoot, "remote", "get-url", name)
		if urlErr != nil {
			if errors.Is(urlErr, ErrGitUnavailable) || errors.Is(urlErr, ErrGitTimeout) || errors.Is(urlErr, ErrGitOutputLimit) {
				return Inspection{}, urlErr
			}
			inspection.Notices = append(
				inspection.Notices,
				fmt.Sprintf("Remote %q has no usable fetch URL and was ignored.", name),
			)
			continue
		}
		candidate := RemoteCandidate{Name: name, URL: strings.TrimSpace(urlResult.Stdout)}
		if candidate.URL == "" {
			inspection.Notices = append(
				inspection.Notices,
				fmt.Sprintf("Remote %q has an empty fetch URL and was ignored.", name),
			)
			continue
		}
		if suggested, deriveErr := deriveRepoTarget(candidate.URL); deriveErr == nil {
			candidate.SuggestedRepoTarget = suggested
		}
		inspection.Remotes = append(inspection.Remotes, candidate)
	}
	sort.Slice(inspection.Remotes, func(i, j int) bool {
		return inspection.Remotes[i].Name < inspection.Remotes[j].Name
	})

	selectedRemote, selectErr := selectRemote(inspection.Remotes, input.RemoteName)
	if selectErr != nil {
		return Inspection{}, selectErr
	}
	if selectedRemote == nil && len(inspection.Remotes) > 1 {
		inspection.State = InspectionStateNeedsRemoteSelection
		return inspection, nil
	}
	inspection.SelectedRemote = selectedRemote

	if selectedRemote != nil {
		inspection.SuggestedRepoTarget = selectedRemote.SuggestedRepoTarget
	}
	switch {
	case input.RepoTargetOverride != "":
		inspection.RepoTarget = input.RepoTargetOverride
		inspection.RepoTargetSource = RepoTargetSourceOperatorOverride
	case inspection.SuggestedRepoTarget != "":
		inspection.RepoTarget = inspection.SuggestedRepoTarget
		inspection.RepoTargetSource = RepoTargetSourceRemoteBasename
	default:
		inspection.State = InspectionStateNeedsTargetOverride
		if selectedRemote == nil {
			inspection.TargetOverrideReason = TargetOverrideReasonNoUsableRemote
			inspection.Notices = append(
				inspection.Notices,
				"No usable configured Git remote was found. Enter a valid slash-free Relay repository target to continue.",
			)
		} else {
			inspection.TargetOverrideReason = TargetOverrideReasonUnsupportedRemote
			inspection.Notices = append(
				inspection.Notices,
				fmt.Sprintf(
					"Remote %q uses URL %q, which Relay cannot normalize into a repository target. Enter a valid slash-free Relay repository target to continue.",
					selectedRemote.Name,
					selectedRemote.URL,
				),
			)
		}
		return inspection, nil
	}

	disposition, existing, conflictKind, err := classifyRegistration(
		ctx,
		r.store,
		inspection.RepoTarget,
		inspection.ResolvedLocalPath,
	)
	if err != nil {
		return Inspection{}, err
	}
	if conflictKind != "" {
		inspection.State = InspectionStateConflict
		inspection.ConflictKind = conflictKind
		inspection.ExistingRepository = existing
		return inspection, nil
	}
	inspection.State = InspectionStateReady
	inspection.RegistrationDisposition = disposition
	inspection.ExistingRepository = existing
	if err := r.inspectBranchConfiguration(
		ctx,
		&inspection,
		input.ProposedConfiguredBranchRef,
	); err != nil {
		return Inspection{}, err
	}
	inspection.ConfirmationHash, err = confirmationHash(inspection)
	if err != nil {
		return Inspection{}, err
	}
	return inspection, nil
}

func (r *Registry) Confirm(ctx context.Context, input ConfirmationInput) (RegistrationResult, error) {
	expectedHash := strings.TrimSpace(input.ExpectedConfirmationHash)
	if expectedHash == "" {
		return RegistrationResult{}, fmt.Errorf("expected confirmation hash is required")
	}
	current, err := r.Inspect(ctx, InspectionInput{
		LocalPath:                   input.LocalPath,
		RemoteName:                  input.RemoteName,
		RepoTargetOverride:          input.RepoTargetOverride,
		ProposedConfiguredBranchRef: input.ProposedConfiguredBranchRef,
	})
	if err != nil {
		return RegistrationResult{}, err
	}
	if current.State != InspectionStateReady {
		return RegistrationResult{}, &ConfirmationError{
			Reason:     "not_ready",
			Inspection: current,
		}
	}
	if subtle.ConstantTimeCompare([]byte(current.ConfirmationHash), []byte(expectedHash)) != 1 {
		return RegistrationResult{}, &ConfirmationError{
			Reason:     "stale",
			Inspection: current,
		}
	}

	var result RegistrationResult
	var insertErr error
	err = r.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		disposition, existing, conflictKind, classifyErr := classifyRegistration(
			ctx,
			tx,
			current.RepoTarget,
			current.ResolvedLocalPath,
		)
		if classifyErr != nil {
			return classifyErr
		}
		if conflictKind != "" {
			return registrationConflictError(current, existing, conflictKind)
		}
		if disposition == RegistrationDispositionReuse {
			if existing.ConfigurationVersion != current.ExpectedConfigurationVersion ||
				!sameNullableString(existing.ConfiguredBranchRef, current.CurrentConfiguredBranchRef) {
				return ErrStaleRepositoryConfiguration
			}
			if current.ConfigurationDisposition == ConfigurationDispositionPreserve {
				result = RegistrationResult{
					Outcome:                  RegistrationOutcomeReused,
					ConfigurationDisposition: current.ConfigurationDisposition,
					Repository:               *existing,
				}
				return nil
			}
			configured, configureErr := tx.ConfigureRepositoryTarget(
				ctx,
				workflowstore.ConfigureRepositoryTargetParams{
					RepoTarget:                   current.RepoTarget,
					ExpectedConfigurationVersion: current.ExpectedConfigurationVersion,
					ConfiguredBranchRef:          current.ProposedConfiguredBranchRef.String,
				},
			)
			if errors.Is(configureErr, sql.ErrNoRows) {
				return ErrStaleRepositoryConfiguration
			}
			if configureErr != nil {
				return configureErr
			}
			result = RegistrationResult{
				Outcome:                  RegistrationOutcomeReused,
				ConfigurationDisposition: current.ConfigurationDisposition,
				Repository:               configured,
			}
			return nil
		}

		if r.beforeCreate != nil {
			r.beforeCreate()
		}
		created, createErr := tx.CreateRepositoryTargetWithConfiguration(
			ctx,
			workflowstore.CreateRepositoryTargetParams{
				RepoTarget:          current.RepoTarget,
				LocalPath:           current.ResolvedLocalPath,
				ConfiguredBranchRef: current.ProposedConfiguredBranchRef,
			},
		)
		if createErr != nil {
			insertErr = createErr
			return createErr
		}
		result = RegistrationResult{
			Outcome:                  RegistrationOutcomeCreated,
			ConfigurationDisposition: current.ConfigurationDisposition,
			Repository:               created,
		}
		return nil
	})
	if err == nil {
		return result, nil
	}
	if errors.Is(err, ErrStaleRepositoryConfiguration) {
		fresh, inspectErr := r.Inspect(ctx, InspectionInput{
			LocalPath:                   input.LocalPath,
			RemoteName:                  input.RemoteName,
			RepoTargetOverride:          input.RepoTargetOverride,
			ProposedConfiguredBranchRef: input.ProposedConfiguredBranchRef,
		})
		if inspectErr != nil {
			return RegistrationResult{}, errors.Join(err, inspectErr)
		}
		return RegistrationResult{}, &ConfirmationError{
			Reason:     "stale",
			Inspection: fresh,
		}
	}
	if insertErr == nil {
		return RegistrationResult{}, err
	}

	return r.resolveAfterFailedInsert(
		ctx,
		current,
		insertErr,
	)
}

func (r *Registry) resolveAfterFailedInsert(
	ctx context.Context,
	current Inspection,
	insertErr error,
) (RegistrationResult, error) {
	recheckContext, cancel := context.WithTimeout(ctx, registrationRecheckTimeout)
	defer cancel()

	ticker := time.NewTicker(registrationRecheckDelay)
	defer ticker.Stop()

	var lastReadErr error
	for {
		disposition, existing, conflictKind, classifyErr := classifyRegistration(
			recheckContext,
			r.store,
			current.RepoTarget,
			current.ResolvedLocalPath,
		)
		if classifyErr == nil {
			switch {
			case conflictKind != "":
				return RegistrationResult{}, registrationConflictError(
					current,
					existing,
					conflictKind,
				)
			case disposition == RegistrationDispositionReuse:
				if !repositoryMatchesInspection(*existing, current) {
					return RegistrationResult{}, &ConfirmationError{
						Reason:     "stale",
						Inspection: current,
					}
				}
				return RegistrationResult{
					Outcome:                  RegistrationOutcomeReused,
					ConfigurationDisposition: current.ConfigurationDisposition,
					Repository:               *existing,
				}, nil
			}
		} else {
			lastReadErr = classifyErr
		}

		select {
		case <-recheckContext.Done():
			joined := errors.Join(
				ErrRegistrationFinalStateUnavailable,
				insertErr,
				lastReadErr,
				recheckContext.Err(),
			)
			return RegistrationResult{}, joined
		case <-ticker.C:
		}
	}
}

func registrationConflictError(
	current Inspection,
	existing *workflowstore.RepositoryTarget,
	conflictKind string,
) error {
	conflictInspection := current
	conflictInspection.State = InspectionStateConflict
	conflictInspection.ConflictKind = conflictKind
	conflictInspection.ExistingRepository = existing
	conflictInspection.RegistrationDisposition = ""
	conflictInspection.ConfirmationHash = ""
	return &ConfirmationError{
		Reason:     "conflict",
		Inspection: conflictInspection,
	}
}

type repositoryReader interface {
	GetRepositoryTarget(context.Context, string) (workflowstore.RepositoryTarget, error)
	GetRepositoryTargetByLocalPath(context.Context, string) (workflowstore.RepositoryTarget, error)
}

func classifyRegistration(
	ctx context.Context,
	reader repositoryReader,
	repoTarget string,
	localPath string,
) (string, *workflowstore.RepositoryTarget, string, error) {
	byTarget, targetErr := reader.GetRepositoryTarget(ctx, repoTarget)
	if targetErr != nil && !errors.Is(targetErr, sql.ErrNoRows) {
		return "", nil, "", targetErr
	}
	byPath, pathErr := reader.GetRepositoryTargetByLocalPath(ctx, localPath)
	if pathErr != nil && !errors.Is(pathErr, sql.ErrNoRows) {
		return "", nil, "", pathErr
	}

	if targetErr == nil {
		if sameRepositoryPath(byTarget.LocalPath, localPath) {
			return RegistrationDispositionReuse, &byTarget, "", nil
		}
		return "", &byTarget, ConflictKindTarget, nil
	}
	if pathErr == nil {
		if strings.EqualFold(byPath.RepoTarget, repoTarget) {
			return RegistrationDispositionReuse, &byPath, "", nil
		}
		return "", &byPath, ConflictKindPath, nil
	}
	return RegistrationDispositionCreate, nil, "", nil
}

func selectRemote(remotes []RemoteCandidate, requested string) (*RemoteCandidate, error) {
	if requested != "" {
		for index := range remotes {
			if remotes[index].Name == requested {
				selected := remotes[index]
				return &selected, nil
			}
		}
		return nil, fmt.Errorf("configured Git remote %q was not found", requested)
	}
	for index := range remotes {
		if remotes[index].Name == "origin" {
			selected := remotes[index]
			return &selected, nil
		}
	}
	if len(remotes) == 1 {
		selected := remotes[0]
		return &selected, nil
	}
	return nil, nil
}

func deriveRepoTarget(remoteURL string) (string, error) {
	value := strings.TrimSpace(remoteURL)
	if value == "" {
		return "", fmt.Errorf("remote URL is empty")
	}

	var repositoryPath string
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("parse remote URL: %w", err)
		}
		repositoryPath = parsed.EscapedPath()
		if repositoryPath == "" {
			repositoryPath = parsed.Path
		}
		if decoded, err := url.PathUnescape(repositoryPath); err == nil {
			repositoryPath = decoded
		}
	} else {
		colon := strings.Index(value, ":")
		slash := strings.Index(value, "/")
		if colon <= 0 || (slash >= 0 && slash < colon) {
			return "", fmt.Errorf("unsupported remote URL")
		}
		repositoryPath = value[colon+1:]
	}

	repositoryPath = strings.TrimRight(repositoryPath, "/")
	base := pathpkg.Base(repositoryPath)
	if base == "." || base == "/" || base == "" {
		return "", fmt.Errorf("remote URL has no repository basename")
	}
	if len(base) >= 4 && strings.EqualFold(base[len(base)-4:], ".git") {
		base = base[:len(base)-4]
	}
	if err := validateRepoTarget(base); err != nil {
		return "", err
	}
	return base, nil
}

func confirmationHash(inspection Inspection) (string, error) {
	payload := struct {
		Version                      string                          `json:"version"`
		SelectedPath                 string                          `json:"selectedPath"`
		ResolvedLocalPath            string                          `json:"resolvedLocalPath"`
		SelectedRemote               *RemoteCandidate                `json:"selectedRemote"`
		SuggestedRepoTarget          string                          `json:"suggestedRepoTarget"`
		RepoTarget                   string                          `json:"repoTarget"`
		RepoTargetSource             string                          `json:"repoTargetSource"`
		RegistrationDisposition      string                          `json:"registrationDisposition"`
		ExistingRepository           *workflowstore.RepositoryTarget `json:"existingRepository"`
		CurrentConfiguredBranchRef   sql.NullString                  `json:"currentConfiguredBranchRef"`
		ExpectedConfigurationVersion int64                           `json:"expectedConfigurationVersion"`
		ProposedConfiguredBranchRef  sql.NullString                  `json:"proposedConfiguredBranchRef"`
		ProposedConfigurationVersion int64                           `json:"proposedConfigurationVersion"`
		ProposedBranchCommitOID      string                          `json:"proposedBranchCommitOid"`
		ProposedBranchTreeOID        string                          `json:"proposedBranchTreeOid"`
		ConfigurationDisposition     string                          `json:"configurationDisposition"`
	}{
		Version:                      "2",
		SelectedPath:                 inspection.SelectedPath,
		ResolvedLocalPath:            inspection.ResolvedLocalPath,
		SelectedRemote:               inspection.SelectedRemote,
		SuggestedRepoTarget:          inspection.SuggestedRepoTarget,
		RepoTarget:                   inspection.RepoTarget,
		RepoTargetSource:             inspection.RepoTargetSource,
		RegistrationDisposition:      inspection.RegistrationDisposition,
		ExistingRepository:           inspection.ExistingRepository,
		CurrentConfiguredBranchRef:   inspection.CurrentConfiguredBranchRef,
		ExpectedConfigurationVersion: inspection.ExpectedConfigurationVersion,
		ProposedConfiguredBranchRef:  inspection.ProposedConfiguredBranchRef,
		ProposedConfigurationVersion: inspection.ProposedConfigurationVersion,
		ProposedBranchCommitOID:      inspection.ProposedBranchCommitOID,
		ProposedBranchTreeOID:        inspection.ProposedBranchTreeOID,
		ConfigurationDisposition:     inspection.ConfigurationDisposition,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode repository confirmation payload: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

func splitNonblankLines(value string) []string {
	var values []string
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			values = append(values, line)
		}
	}
	return values
}

func gitDiagnostic(result GitCommandResult) string {
	if result.Stderr != "" {
		return result.Stderr
	}
	if result.Stdout != "" {
		return result.Stdout
	}
	return "Git command failed"
}

func sameRepositoryPath(left string, right string) bool {
	left = filepath.Clean(left)
	right = filepath.Clean(right)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}
