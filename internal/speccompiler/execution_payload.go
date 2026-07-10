// Package speccompiler projects optional deterministic-first metadata from a
// canonical Execution Spec or packet without changing execution behavior.
package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Diagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ExecutionPayloadProjection struct {
	ValidationCommands      []ProjectedValidationCommand
	ValidationCommandSource string
	DeterministicOperations []ProjectedDeterministicOperation
	OperationGroups         []ProjectedOperationGroup
	ChangedFilePolicy       json.RawMessage
	SourceGuards            json.RawMessage
	ExecutionMode           ProjectedExecutionMode
}

type ProjectedValidationCommand struct {
	ID               string `json:"id,omitempty"`
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Expected         string `json:"expected,omitempty"`
	Required         bool   `json:"required,omitempty"`
	Purpose          string `json:"purpose,omitempty"`
	SuccessSignal    string `json:"success_signal,omitempty"`
	FailureHandling  string `json:"failure_handling,omitempty"`
	Phase            string `json:"phase,omitempty"`
	Severity         string `json:"severity,omitempty"`
	MutationPolicy   string `json:"mutation_policy,omitempty"`
	WorktreePolicy   string `json:"worktree_policy,omitempty"`

	presence projectedValidationCommandPresence `json:"-"`
}

type projectedValidationCommandPresence struct {
	ID, Command, WorkingDirectory, Expected, Required, Purpose, SuccessSignal, FailureHandling, Phase, Severity, MutationPolicy, WorktreePolicy bool
}

func (c *ProjectedValidationCommand) UnmarshalJSON(raw []byte) error {
	var fields struct {
		ID               *string `json:"id"`
		Command          *string `json:"command"`
		WorkingDirectory *string `json:"working_directory"`
		Expected         *string `json:"expected"`
		Required         *bool   `json:"required"`
		Purpose          *string `json:"purpose"`
		SuccessSignal    *string `json:"success_signal"`
		FailureHandling  *string `json:"failure_handling"`
		Phase            *string `json:"phase"`
		Severity         *string `json:"severity"`
		MutationPolicy   *string `json:"mutation_policy"`
		WorktreePolicy   *string `json:"worktree_policy"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	*c = ProjectedValidationCommand{}
	if fields.ID != nil {
		c.ID, c.presence.ID = *fields.ID, true
	}
	if fields.Command != nil {
		c.Command, c.presence.Command = *fields.Command, true
	}
	if fields.WorkingDirectory != nil {
		c.WorkingDirectory, c.presence.WorkingDirectory = *fields.WorkingDirectory, true
	}
	if fields.Expected != nil {
		c.Expected, c.presence.Expected = *fields.Expected, true
	}
	if fields.Required != nil {
		c.Required, c.presence.Required = *fields.Required, true
	}
	if fields.Purpose != nil {
		c.Purpose, c.presence.Purpose = *fields.Purpose, true
	}
	if fields.SuccessSignal != nil {
		c.SuccessSignal, c.presence.SuccessSignal = *fields.SuccessSignal, true
	}
	if fields.FailureHandling != nil {
		c.FailureHandling, c.presence.FailureHandling = *fields.FailureHandling, true
	}
	if fields.Phase != nil {
		c.Phase, c.presence.Phase = *fields.Phase, true
	}
	if fields.Severity != nil {
		c.Severity, c.presence.Severity = *fields.Severity, true
	}
	if fields.MutationPolicy != nil {
		c.MutationPolicy, c.presence.MutationPolicy = *fields.MutationPolicy, true
	}
	if fields.WorktreePolicy != nil {
		c.WorktreePolicy, c.presence.WorktreePolicy = *fields.WorktreePolicy, true
	}
	return nil
}

type ProjectedDeterministicOperation struct {
	ID                  string          `json:"id,omitempty"`
	Kind                string          `json:"kind,omitempty"`
	Mode                string          `json:"mode,omitempty"`
	Paths               []string        `json:"paths,omitempty"`
	Guards              json.RawMessage `json:"guards,omitempty"`
	ExpectedOccurrences int             `json:"expected_occurrences,omitempty"`
	DependsOn           []string        `json:"depends_on,omitempty"`
	Group               string          `json:"group,omitempty"`
	OnFailure           string          `json:"on_failure,omitempty"`
	Payload             json.RawMessage `json:"payload,omitempty"`
	Raw                 json.RawMessage `json:"-"`
}

type ProjectedOperationGroup struct {
	ID     string `json:"id,omitempty"`
	Atomic bool   `json:"atomic,omitempty"`
}
type ProjectedExecutionMode struct {
	PreferredMode string `json:"preferred_mode,omitempty"`
	FallbackMode  string `json:"fallback_mode,omitempty"`
}
type ProjectedValidationContract struct {
	Mode          string                       `json:"mode,omitempty"`
	FailurePolicy string                       `json:"failure_policy,omitempty"`
	Commands      []ProjectedValidationCommand `json:"commands,omitempty"`
}

type executionPayloadDocument struct {
	Validation struct {
		Commands []ProjectedValidationCommand `json:"commands"`
	} `json:"validation"`
	ExecutionPayload struct {
		DeterministicOperations []json.RawMessage            `json:"deterministic_operations"`
		OperationGroups         []ProjectedOperationGroup    `json:"operation_groups"`
		ChangedFilePolicy       json.RawMessage              `json:"changed_file_policy"`
		SourceGuards            json.RawMessage              `json:"source_guards"`
		ExecutionMode           ProjectedExecutionMode       `json:"execution_mode"`
		ValidationContract      ProjectedValidationContract  `json:"validation_contract"`
		ValidationCommands      []ProjectedValidationCommand `json:"validation_commands"`
	} `json:"execution_payload"`
}

// ProjectExecutionPayload returns deterministic metadata and normalized validation
// commands. It never infers operations from prose or from unrelated packet fields.
func ProjectExecutionPayload(raw []byte) (ExecutionPayloadProjection, []Diagnostic) {
	var document executionPayloadDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return ExecutionPayloadProjection{}, []Diagnostic{{Code: "invalid_json", Message: fmt.Sprintf("Decode execution payload projection: %v", err)}}
	}
	projection := ExecutionPayloadProjection{
		OperationGroups:   append([]ProjectedOperationGroup(nil), document.ExecutionPayload.OperationGroups...),
		ChangedFilePolicy: append(json.RawMessage(nil), document.ExecutionPayload.ChangedFilePolicy...),
		SourceGuards:      append(json.RawMessage(nil), document.ExecutionPayload.SourceGuards...),
		ExecutionMode:     document.ExecutionPayload.ExecutionMode,
	}
	var diagnostics []Diagnostic
	for i, rawOperation := range document.ExecutionPayload.DeterministicOperations {
		var operation ProjectedDeterministicOperation
		if len(rawOperation) == 0 || rawOperation[0] != '{' || json.Unmarshal(rawOperation, &operation) != nil {
			diagnostics = append(diagnostics, Diagnostic{Code: "invalid_deterministic_operation", Path: fmt.Sprintf("/execution_payload/deterministic_operations/%d", i), Message: "deterministic operation is not a valid object"})
			continue
		}
		operation.Raw = append(json.RawMessage(nil), rawOperation...)
		projection.DeterministicOperations = append(projection.DeterministicOperations, operation)
	}

	topLevel := normalizeProjectedValidationCommands(document.Validation.Commands)
	contract := normalizeProjectedValidationCommands(document.ExecutionPayload.ValidationContract.Commands)
	legacy := normalizeProjectedValidationCommands(document.ExecutionPayload.ValidationCommands)
	switch {
	case len(contract) > 0:
		projection.ValidationCommands, projection.ValidationCommandSource = contract, "execution_payload.validation_contract.commands"
		if len(topLevel) > 0 && !sameValidationCommandSequence(contract, topLevel) {
			diagnostics = append(diagnostics, validationConflict("/execution_payload/validation_contract/commands", "execution_payload.validation_contract.commands conflicts with validation.commands"))
		}
		if len(legacy) > 0 && !sameValidationCommandSequence(contract, legacy) {
			diagnostics = append(diagnostics, validationConflict("/execution_payload/validation_commands", "execution_payload.validation_commands conflicts with validation_contract.commands"))
		}
	case len(topLevel) > 0:
		projection.ValidationCommands, projection.ValidationCommandSource = topLevel, "validation.commands"
		if len(legacy) > 0 && !sameValidationCommandSequence(topLevel, legacy) {
			diagnostics = append(diagnostics, validationConflict("/execution_payload/validation_commands", "execution_payload.validation_commands conflicts with validation.commands"))
		}
	case len(legacy) > 0:
		projection.ValidationCommands, projection.ValidationCommandSource = legacy, "execution_payload.validation_commands"
	}
	return projection, diagnostics
}

func validationConflict(path, message string) Diagnostic {
	return Diagnostic{Code: "validation_command_conflict", Path: path, Message: message}
}

func normalizeProjectedValidationCommands(commands []ProjectedValidationCommand) []ProjectedValidationCommand {
	if len(commands) == 0 {
		return nil
	}
	out := make([]ProjectedValidationCommand, 0, len(commands))
	for _, command := range commands {
		command.ID = strings.TrimSpace(command.ID)
		command.Command = strings.TrimSpace(command.Command)
		command.WorkingDirectory = strings.TrimSpace(command.WorkingDirectory)
		command.Expected = strings.TrimSpace(command.Expected)
		command.Purpose = strings.TrimSpace(command.Purpose)
		command.SuccessSignal = strings.TrimSpace(command.SuccessSignal)
		command.FailureHandling = strings.TrimSpace(command.FailureHandling)
		command.Phase = strings.TrimSpace(command.Phase)
		command.Severity = strings.TrimSpace(command.Severity)
		command.MutationPolicy = strings.TrimSpace(command.MutationPolicy)
		command.WorktreePolicy = strings.TrimSpace(command.WorktreePolicy)
		out = append(out, command)
	}
	return out
}

func sameValidationCommandSequence(left, right []ProjectedValidationCommand) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if !sameValidationCommand(left[i], right[i]) {
			return false
		}
	}
	return true
}

func sameValidationCommand(left, right ProjectedValidationCommand) bool {
	if left.Command != right.Command || !sameOptionalString(left.WorkingDirectory, left.presence.WorkingDirectory, right.WorkingDirectory, right.presence.WorkingDirectory) || !sameOptionalString(left.ID, left.presence.ID, right.ID, right.presence.ID) || (left.presence.Required && right.presence.Required && left.Required != right.Required) {
		return false
	}
	if leftExpected, rightExpected := projectedExpected(left), projectedExpected(right); leftExpected != "" && rightExpected != "" && leftExpected != rightExpected {
		return false
	}
	for _, field := range []struct {
		leftValue, rightValue     string
		leftPresent, rightPresent bool
	}{
		{left.Purpose, right.Purpose, left.presence.Purpose, right.presence.Purpose},
		{left.FailureHandling, right.FailureHandling, left.presence.FailureHandling, right.presence.FailureHandling},
		{left.Phase, right.Phase, left.presence.Phase, right.presence.Phase},
		{left.Severity, right.Severity, left.presence.Severity, right.presence.Severity},
		{left.MutationPolicy, right.MutationPolicy, left.presence.MutationPolicy, right.presence.MutationPolicy},
		{left.WorktreePolicy, right.WorktreePolicy, left.presence.WorktreePolicy, right.presence.WorktreePolicy},
	} {
		if !sameOptionalString(field.leftValue, field.leftPresent, field.rightValue, field.rightPresent) {
			return false
		}
	}
	return true
}

func sameOptionalString(left string, leftPresent bool, right string, rightPresent bool) bool {
	return !leftPresent || !rightPresent || left == right
}
func projectedExpected(command ProjectedValidationCommand) string {
	if command.Expected != "" {
		return command.Expected
	}
	return command.SuccessSignal
}
