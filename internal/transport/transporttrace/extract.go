package transporttrace

import (
	"relay/internal/mcp"
)

var sourceTools = map[string]struct{}{
	"list_source_tree":    {},
	"search_source":       {},
	"read_source_text":    {},
	"read_source_blob":    {},
	"get_source_commit":   {},
	"list_source_history": {},
	"compare_source":      {},
	"read_source_diff":    {},
}

type Classification struct {
	Completion CompletionState
	Outcome    OutcomeClass
	Error      ErrorClass
	Source     SourceIdentity
}

func Classify(request mcp.TraceRequestIdentity, response mcp.TraceResponseOutcome, statusCode int, write DownstreamWrite) Classification {
	classification := Classification{
		Completion: CompletionNotApplicable,
		Outcome:    OutcomeSuccess,
		Error:      ErrorNone,
		Source:     mergeSourceIdentity(request.Source, response.Source),
	}
	_, sourceTool := sourceTools[request.ToolName]
	if sourceTool {
		switch {
		case response.CompleteSet && response.Complete && !response.HasCursor:
			classification.Completion = CompletionComplete
		case response.HasCursor || (response.BoundedSet && response.Bounded) || (response.CompleteSet && !response.Complete):
			classification.Completion = CompletionBounded
		default:
			classification.Completion = CompletionUnknown
		}
	}
	if !write.Complete {
		classification.Outcome = OutcomeResponseWrite
		classification.Error = write.ErrorClass
		return classification
	}
	switch statusCode {
	case 401, 403:
		classification.Outcome = OutcomeAdmissionRejection
		classification.Error = ErrorAuthorizationRejected
		return classification
	case 404:
		classification.Outcome = OutcomeAdmissionRejection
		classification.Error = ErrorPathNotFound
		return classification
	case 405:
		classification.Outcome = OutcomeAdmissionRejection
		classification.Error = ErrorMethodNotAllowed
		return classification
	}
	if statusCode < 200 || statusCode >= 300 {
		classification.Outcome = OutcomeApplicationFailure
		classification.Error = ErrorUpstreamUnavailable
		return classification
	}
	if response.RPCErrorCode != 0 {
		classification.Outcome = OutcomeAdmissionRejection
		if response.RPCErrorCode == mcp.CodeMethodNotFound && request.JSONRPCMethod == "tools/call" {
			classification.Error = ErrorToolRejected
		} else {
			classification.Error = ErrorProtocolRejected
		}
		return classification
	}
	if response.ToolIsError {
		if sourceTool {
			classification.Outcome = OutcomeSourceFailure
			classification.Error = sourceErrorClass(response.ErrorClass)
		} else {
			classification.Outcome = OutcomeApplicationFailure
			classification.Error = ErrorApplicationBlocked
		}
	}
	return classification
}

func mergeSourceIdentity(request, response mcp.TraceSourceIdentity) SourceIdentity {
	value := SourceIdentity{
		RepositoryKey:   request.RepositoryKey,
		RevisionKind:    request.RevisionKind,
		CommitOID:       request.CommitOID,
		BeforeCommitOID: request.BeforeCommitOID,
		AfterCommitOID:  request.AfterCommitOID,
		AnchorName:      request.AnchorName,
		PathID:          request.PathID,
		BlobOID:         request.BlobOID,
		CursorSHA256:    request.CursorSHA256,
	}
	if response.RepositoryKey != "" {
		value.RepositoryKey = response.RepositoryKey
	}
	if response.RevisionKind != "" {
		value.RevisionKind = response.RevisionKind
	}
	if response.CommitOID != "" {
		value.CommitOID = response.CommitOID
	}
	if response.BeforeCommitOID != "" {
		value.BeforeCommitOID = response.BeforeCommitOID
	}
	if response.AfterCommitOID != "" {
		value.AfterCommitOID = response.AfterCommitOID
	}
	if response.AnchorName != "" {
		value.AnchorName = response.AnchorName
	}
	if response.PathID != "" {
		value.PathID = response.PathID
	}
	if response.BlobOID != "" {
		value.BlobOID = response.BlobOID
	}
	if response.CursorSHA256 != "" {
		value.CursorSHA256 = response.CursorSHA256
	}
	return value
}

func sourceErrorClass(value string) ErrorClass {
	switch value {
	case "budget_exhausted", "source_budget_boundary":
		return ErrorSourceBudgetBoundary
	default:
		return ErrorSourceBlocked
	}
}
