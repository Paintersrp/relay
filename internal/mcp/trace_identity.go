package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const maxTraceIdentityBytes = 512

type TraceSourceIdentity struct {
	RepositoryKey   string
	RevisionKind    string
	CommitOID       string
	BeforeCommitOID string
	AfterCommitOID  string
	AnchorName      string
	PathID          string
	BlobOID         string
	CursorSHA256    string
}

type TraceRequestIdentity struct {
	JSONRPCMethod string
	ToolName      string
	OperationID   string
	PacketID      string
	ProjectID     string
	Source        TraceSourceIdentity
}

type TraceResponseOutcome struct {
	RPCErrorCode int
	ToolIsError  bool
	CompleteSet  bool
	Complete     bool
	BoundedSet   bool
	Bounded      bool
	HasCursor    bool
	ErrorClass   string
	Source       TraceSourceIdentity
}

type traceOccurrence struct {
	count    uint8
	accepted bool
	value    string
}

type traceOccurrences map[string]traceOccurrence

func (o traceOccurrences) observe(slot, value string, accepted bool) {
	state := o[slot]
	if state.count < 2 {
		state.count++
	}
	if state.count == 1 {
		state.accepted = accepted
		if accepted {
			state.value = value
		}
	} else {
		state.accepted = false
		state.value = ""
	}
	o[slot] = state
}

func (o traceOccurrences) value(slot string) string {
	state := o[slot]
	if state.count != 1 || !state.accepted {
		return ""
	}
	return state.value
}

func ObserveTraceRequest(reader io.Reader) (TraceRequestIdentity, error) {
	values := traceOccurrences{}
	err := walkTraceJSON(json.NewDecoder(reader), nil, func(path []string, value any) {
		switch exactTracePath(path) {
		case "method":
			observeTraceString(values, "method", value)
		case "params.name":
			observeTraceString(values, "tool_name", value)
		case "params.arguments.operation_id":
			observeTraceString(values, "operation_id", value)
		case "params.arguments.packet_id":
			observeTraceString(values, "packet_id", value)
		case "params.arguments.project_id":
			observeTraceString(values, "project_id", value)
		case "params.arguments.repository_key":
			observeTraceString(values, "repository_key", value)
		case "params.arguments.revision.kind":
			observeTraceString(values, "revision_kind", value)
		case "params.arguments.revision.commit_oid":
			observeTraceHex(values, "commit_oid", value, 40)
		case "params.arguments.before.commit_oid":
			observeTraceHex(values, "before_commit_oid", value, 40)
		case "params.arguments.after.commit_oid":
			observeTraceHex(values, "after_commit_oid", value, 40)
		case "params.arguments.anchor_name":
			observeTraceString(values, "anchor_name", value)
		case "params.arguments.path.path_id", "params.arguments.directory.path_id":
			observeTraceHex(values, "path_id", value, 64)
		case "params.arguments.blob_oid":
			observeTraceHex(values, "blob_oid", value, 40)
		case "params.arguments.cursor":
			observeTraceCursor(values, value)
		}
	})
	if err != nil {
		return TraceRequestIdentity{}, err
	}
	return TraceRequestIdentity{
		JSONRPCMethod: values.value("method"),
		ToolName:      values.value("tool_name"),
		OperationID:   values.value("operation_id"),
		PacketID:      values.value("packet_id"),
		ProjectID:     values.value("project_id"),
		Source: TraceSourceIdentity{
			RepositoryKey:   values.value("repository_key"),
			RevisionKind:    values.value("revision_kind"),
			CommitOID:       values.value("commit_oid"),
			BeforeCommitOID: values.value("before_commit_oid"),
			AfterCommitOID:  values.value("after_commit_oid"),
			AnchorName:      values.value("anchor_name"),
			PathID:          values.value("path_id"),
			BlobOID:         values.value("blob_oid"),
			CursorSHA256:    values.value("cursor_sha256"),
		},
	}, nil
}

func ObserveTraceResponse(reader io.Reader) (TraceResponseOutcome, error) {
	values := traceOccurrences{}
	flags := map[string]traceFlag{}
	err := walkTraceJSON(json.NewDecoder(reader), nil, func(path []string, value any) {
		key := exactTracePath(path)
		switch key {
		case "error.code":
			observeTraceInteger(values, "rpc_error_code", value)
		case "result.isError":
			recordTraceFlag(flags, "tool_is_error", value)
		case "result.structuredContent.Complete":
			recordTraceFlag(flags, "complete", value)
		case "result.structuredContent.Bounded":
			recordTraceFlag(flags, "bounded", value)
		case "result.structuredContent.Cursor", "result.nextCursor":
			observeTraceResponseCursor(values, value)
		case "result.structuredContent.ErrorClass":
			observeTraceString(values, "error_class", value)
		case "result.structuredContent.Source.RepositoryKey":
			observeTraceString(values, "repository_key", value)
		case "result.structuredContent.Source.RevisionKind":
			observeTraceString(values, "revision_kind", value)
		case "result.structuredContent.Source.CommitOID":
			observeTraceHex(values, "commit_oid", value, 40)
		case "result.structuredContent.Source.BeforeCommitOID":
			observeTraceHex(values, "before_commit_oid", value, 40)
		case "result.structuredContent.Source.AfterCommitOID":
			observeTraceHex(values, "after_commit_oid", value, 40)
		case "result.structuredContent.Source.AnchorName":
			observeTraceString(values, "anchor_name", value)
		case "result.structuredContent.Path.PathID", "result.structuredContent.Directory.PathID":
			observeTraceHex(values, "path_id", value, 64)
		case "result.structuredContent.ObjectOID":
			observeTraceHex(values, "blob_oid", value, 40)
		}
	})
	if err != nil {
		return TraceResponseOutcome{}, err
	}
	var rpcCode int
	if text := values.value("rpc_error_code"); text != "" {
		rpcCode, _ = strconv.Atoi(text)
	}
	complete, completeSet := traceFlagValue(flags, "complete")
	bounded, boundedSet := traceFlagValue(flags, "bounded")
	toolIsError, _ := traceFlagValue(flags, "tool_is_error")
	return TraceResponseOutcome{
		RPCErrorCode: rpcCode,
		ToolIsError:  toolIsError,
		CompleteSet:  completeSet,
		Complete:     complete,
		BoundedSet:   boundedSet,
		Bounded:      bounded,
		HasCursor:    values.value("has_cursor") == "true",
		ErrorClass:   values.value("error_class"),
		Source: TraceSourceIdentity{
			RepositoryKey:   values.value("repository_key"),
			RevisionKind:    values.value("revision_kind"),
			CommitOID:       values.value("commit_oid"),
			BeforeCommitOID: values.value("before_commit_oid"),
			AfterCommitOID:  values.value("after_commit_oid"),
			AnchorName:      values.value("anchor_name"),
			PathID:          values.value("path_id"),
			BlobOID:         values.value("blob_oid"),
			CursorSHA256:    values.value("cursor_sha256"),
		},
	}, nil
}

type traceFlag struct {
	count    uint8
	accepted bool
	value    bool
}

func recordTraceFlag(values map[string]traceFlag, slot string, value any) {
	state := values[slot]
	if state.count < 2 {
		state.count++
	}
	boolean, accepted := value.(bool)
	if state.count == 1 {
		state.accepted = accepted
		if accepted {
			state.value = boolean
		}
	} else {
		state.accepted = false
		state.value = false
	}
	values[slot] = state
}

func traceFlagValue(values map[string]traceFlag, slot string) (bool, bool) {
	state := values[slot]
	if state.count != 1 || !state.accepted {
		return false, false
	}
	return state.value, true
}

func observeTraceString(values traceOccurrences, slot string, value any) {
	text, ok := value.(string)
	if !ok {
		values.observe(slot, "", false)
		return
	}
	bounded := boundedTraceIdentity(text)
	values.observe(slot, bounded, bounded != "")
}

func observeTraceHex(values traceOccurrences, slot string, value any, size int) {
	text, ok := value.(string)
	if !ok {
		values.observe(slot, "", false)
		return
	}
	bounded := boundedLowerHex(text, size)
	values.observe(slot, bounded, bounded != "")
}

func observeTraceInteger(values traceOccurrences, slot string, value any) {
	number, ok := value.(json.Number)
	if !ok {
		values.observe(slot, "", false)
		return
	}
	parsed, err := strconv.Atoi(number.String())
	if err != nil {
		values.observe(slot, "", false)
		return
	}
	values.observe(slot, strconv.Itoa(parsed), true)
}

func observeTraceCursor(values traceOccurrences, value any) {
	text, ok := value.(string)
	if !ok || text == "" {
		values.observe("cursor_sha256", "", false)
		return
	}
	values.observe("cursor_sha256", traceDigest(text), true)
}

func observeTraceResponseCursor(values traceOccurrences, value any) {
	text, ok := value.(string)
	accepted := ok && text != ""
	digest := ""
	if accepted {
		digest = traceDigest(text)
	}
	values.observe("cursor_sha256", digest, accepted)
	values.observe("has_cursor", "true", accepted)
}

func walkTraceJSON(decoder *json.Decoder, path []string, visit func([]string, any)) error {
	decoder.UseNumber()
	return walkTraceValue(decoder, path, visit, 0)
}

func walkTraceValue(decoder *json.Decoder, path []string, visit func([]string, any), depth int) error {
	if depth > 64 {
		return fmt.Errorf("trace JSON exceeds maximum depth")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		visit(path, token)
		return nil
	}
	visit(path, token)
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("trace JSON object key is not a string")
			}
			if err := walkTraceValue(decoder, appendTracePath(path, key), visit, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkTraceValue(decoder, appendTracePath(path, "*"), visit, depth+1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unsupported trace JSON delimiter %q", delimiter)
	}
}

func appendTracePath(path []string, value string) []string {
	result := make([]string, len(path)+1)
	copy(result, path)
	result[len(path)] = value
	return result
}

func exactTracePath(path []string) string {
	return strings.Join(path, ".")
}

func boundedTraceIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxTraceIdentityBytes {
		return ""
	}
	return value
}

func boundedLowerHex(value string, size int) string {
	if len(value) != size || strings.ToLower(value) != value {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func traceDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
