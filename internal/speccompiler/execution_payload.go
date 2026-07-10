package speccompiler

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	ID               bool
	Command          bool
	WorkingDirectory bool
	Expected         bool
	Required         bool
	Purpose          bool
	SuccessSignal    bool
	FailureHandling  bool
	Phase            bool
	Severity         bool
	MutationPolicy   bool
	WorktreePolicy   bool
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
		c.ID = *fields.ID
		c.presence.ID = true
	}
	if fields.Command != nil {
		c.Command = *fields.Command
		c.presence.Command = true
	}
	if fields.WorkingDirectory != nil {
		c.WorkingDirectory = *fields.WorkingDirectory
		c.presence.WorkingDirectory = true
	}
	if fields.Expected != nil {
		c.Expected = *fields.Expected
		c.presence.Expected = true
	}
	if fields.Required != nil {
		c.Required = *fields.Required
		c.presence.Required = true
	}
	if fields.Purpose != nil {
		c.Purpose = *fields.Purpose
		c.presence.Purpose = true
	}
	if fields.SuccessSignal != nil {
		c.SuccessSignal = *fields.SuccessSignal
		c.presence.SuccessSignal = true
	}
	if fields.FailureHandling != nil {
		c.FailureHandling = *fields.FailureHandling
		c.presence.FailureHandling = true
	}
	if fields.Phase != nil {
		c.Phase = *fields.Phase
		c.presence.Phase = true
	}
	if fields.Severity != nil {
		c.Severity = *fields.Severity
		c.presence.Severity = true
	}
	if fields.MutationPolicy != nil {
		c.MutationPolicy = *fields.MutationPolicy
		c.presence.MutationPolicy = true
	}
	if fields.WorktreePolicy != nil {
		c.WorktreePolicy = *fields.WorktreePolicy
		c.presence.WorktreePolicy = true
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
	Validation       topLevelValidationEnvelope `json:"validation"`
	ExecutionPayload executionPayloadEnvelope   `json:"execution_payload"`
}

type topLevelValidationEnvelope struct {
	Commands []ProjectedValidationCommand `json:"commands"`
}

type executionPayloadEnvelope struct {
	DeterministicOperations []json.RawMessage            `json:"deterministic_operations"`
	OperationGroups         []ProjectedOperationGroup    `json:"operation_groups"`
	ChangedFilePolicy       json.RawMessage              `json:"changed_file_policy"`
	SourceGuards            json.RawMessage              `json:"source_guards"`
	ExecutionMode           ProjectedExecutionMode       `json:"execution_mode"`
	ValidationContract      ProjectedValidationContract  `json:"validation_contract"`
	ValidationCommands      []ProjectedValidationCommand `json:"validation_commands"`
}

func ProjectExecutionPayload(raw []byte) (ExecutionPayloadProjection, []Diagnostic) {
	var document executionPayloadDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return ExecutionPayloadProjection{}, []Diagnostic{{
			Code:    "invalid_json",
			Path:    "",
			Message: fmt.Sprintf("Decode execution payload projection: %v", err),
		}}
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
		if err := json.Unmarshal(rawOperation, &operation); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "invalid_deterministic_operation",
				Path:    fmt.Sprintf("/execution_payload/deterministic_operations/%d", i),
				Message: fmt.Sprintf("deterministic operation is not a valid object: %v", err),
			})
			continue
		}
		operation.Raw = append(json.RawMessage(nil), rawOperation...)
		projection.DeterministicOperations = append(projection.DeterministicOperations, operation)
	}

	topLevelCommands := normalizeProjectedValidationCommands(document.Validation.Commands)
	contractCommands := normalizeProjectedValidationCommands(document.ExecutionPayload.ValidationContract.Commands)
	legacyCommands := normalizeProjectedValidationCommands(document.ExecutionPayload.ValidationCommands)

	switch {
	case len(contractCommands) > 0:
		projection.ValidationCommands = contractCommands
		projection.ValidationCommandSource = "execution_payload.validation_contract.commands"
		if len(topLevelCommands) > 0 && !sameValidationCommandSequence(contractCommands, topLevelCommands) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "validation_command_conflict",
				Path:    "/execution_payload/validation_contract/commands",
				Message: "execution_payload.validation_contract.commands conflicts with validation.commands; validation_contract.commands is canonical for projected runtime validation.",
			})
		}
		if len(legacyCommands) > 0 && !sameValidationCommandSequence(contractCommands, legacyCommands) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "validation_command_conflict",
				Path:    "/execution_payload/validation_commands",
				Message: "execution_payload.validation_commands conflicts with validation_contract.commands and is supported only as a legacy fallback.",
			})
		}
	case len(topLevelCommands) > 0:
		projection.ValidationCommands = topLevelCommands
		projection.ValidationCommandSource = "validation.commands"
		if len(legacyCommands) > 0 && !sameValidationCommandSequence(topLevelCommands, legacyCommands) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "validation_command_conflict",
				Path:    "/execution_payload/validation_commands",
				Message: "execution_payload.validation_commands conflicts with validation.commands and is supported only when canonical commands are absent.",
			})
		}
	case len(legacyCommands) > 0:
		projection.ValidationCommands = legacyCommands
		projection.ValidationCommandSource = "execution_payload.validation_commands"
	default:
		projection.ValidationCommands = nil
		projection.ValidationCommandSource = ""
	}

	return projection, diagnostics
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
	if left.Command != right.Command {
		return false
	}
	if !sameValidationCommandWorkingDirectory(left, right) {
		return false
	}
	if !sameProjectedString(left.ID, left.presence.ID, right.ID, right.presence.ID) {
		return false
	}
	if !sameProjectedExpected(left, right) {
		return false
	}
	if left.presence.Required && right.presence.Required && left.Required != right.Required {
		return false
	}
	for _, field := range []struct {
		leftValue    string
		leftPresent  bool
		rightValue   string
		rightPresent bool
	}{
		{left.Purpose, left.presence.Purpose, right.Purpose, right.presence.Purpose},
		{left.FailureHandling, left.presence.FailureHandling, right.FailureHandling, right.presence.FailureHandling},
		{left.Phase, left.presence.Phase, right.Phase, right.presence.Phase},
		{left.Severity, left.presence.Severity, right.Severity, right.presence.Severity},
		{left.MutationPolicy, left.presence.MutationPolicy, right.MutationPolicy, right.presence.MutationPolicy},
		{left.WorktreePolicy, left.presence.WorktreePolicy, right.WorktreePolicy, right.presence.WorktreePolicy},
	} {
		if !sameProjectedString(field.leftValue, field.leftPresent, field.rightValue, field.rightPresent) {
			return false
		}
	}
	return true
}

func sameValidationCommandWorkingDirectory(left, right ProjectedValidationCommand) bool {
	if left.WorkingDirectory == right.WorkingDirectory {
		return true
	}
	if left.WorkingDirectory == "" && !left.presence.WorkingDirectory {
		return right.WorkingDirectory == ""
	}
	if right.WorkingDirectory == "" && !right.presence.WorkingDirectory {
		return left.WorkingDirectory == ""
	}
	return false
}

func sameProjectedString(left string, leftPresent bool, right string, rightPresent bool) bool {
	if leftPresent && rightPresent {
		return left == right
	}
	return true
}

func sameProjectedExpected(left, right ProjectedValidationCommand) bool {
	leftExpected := projectedExpected(left)
	rightExpected := projectedExpected(right)
	if leftExpected == "" || rightExpected == "" {
		return true
	}
	return leftExpected == rightExpected
}

func projectedExpected(command ProjectedValidationCommand) string {
	if command.Expected != "" {
		return command.Expected
	}
	return command.SuccessSignal
}
