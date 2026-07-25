// Package source contains the immutable route-independent source-tool
// contracts. It deliberately has no runtime dependencies so both the registry
// and its contract generator consume the same schema authority.
package source

import (
	"encoding/json"
	"strconv"
)

// The published request and response contract for the packet-authorized source
// read tools is owned here. Every other source tool keeps the legacy aggregate
// schema copied from public_contract.json.
const (
	ToolListTree = "list_source_tree"
	ToolSearch   = "search_source"
	ToolReadText = "read_source_text"
)

// Request and response bounds are restated here because internal/sourcegateway
// imports this package and cannot be imported back. A parity test pins every
// value to the exact runtime constant it describes.
const (
	sourceTreePageEntryLimit    = 512
	sourceSearchPageMatchLimit  = 512
	sourceSearchPrefixLimit     = 512
	sourceSearchLiteralBytes    = 1 << 20
	sourceTextPageMinimumBytes  = 4
	sourceTextPageMaximumBytes  = 1 << 20
	sourceInlinePathBase64Bytes = 4 * ((8192 + 2) / 3)
	sourceCursorTokenBytes      = 32 << 10
	sourceIdentifierBytes       = 255
	sourceDigestHexLength       = 64
)

// Owns reports whether this package owns the published
// request and response schema for one source tool.
func Owns(tool string) bool {
	switch tool {
	case ToolListTree, ToolSearch, ToolReadText:
		return true
	default:
		return false
	}
}

// Schemas returns the route-independent published input and
// output schemas for one owned source tool. Route binding specializes the
// surface contract and operation identity members afterwards.
func Schemas(tool string) (json.RawMessage, json.RawMessage, bool) {
	switch tool {
	case ToolListTree:
		return sourceSchemaBytes(listSourceTreeInputSchema()), sourceSchemaBytes(listSourceTreeOutputSchema()), true
	case ToolSearch:
		return sourceSchemaBytes(searchSourceInputSchema()), sourceSchemaBytes(searchSourceOutputSchema()), true
	case ToolReadText:
		return sourceSchemaBytes(readSourceTextInputSchema()), sourceSchemaBytes(readSourceTextOutputSchema()), true
	default:
		return nil, nil, false
	}
}

func listSourceTreeInputSchema() map[string]any {
	properties := sourceRouteProperties()
	properties["directory"] = sourcePathSelectorSchema()
	properties["recursive"] = sourceBooleanSchema()
	properties["limit"] = sourceBoundedInteger(1, sourceTreePageEntryLimit)
	properties["cursor"] = sourceCursorSchema()
	return sourceObjectSchema([]string{"surface_contract", "packet_id", "repository_key", "limit"}, properties)
}

func searchSourceInputSchema() map[string]any {
	properties := sourceRouteProperties()
	properties["revision"] = sourceRevisionSelectorSchema()
	properties["mode"] = sourceStringEnum("text_literal", "byte_literal")
	properties["text_literal"] = sourceBoundedString(1, sourceSearchLiteralBytes)
	properties["byte_literal_base64"] = sourceBoundedString(1, 4*((sourceSearchLiteralBytes+2)/3))
	properties["prefixes"] = sourceArraySchema(1, sourceSearchPrefixLimit, sourcePathSelectorSchema())
	properties["limit"] = sourceBoundedInteger(1, sourceSearchPageMatchLimit)
	properties["examined_objects"] = sourceMinimumInteger(1)
	properties["examined_bytes"] = sourceMinimumInteger(sourceTextPageMinimumBytes)
	properties["cursor"] = sourceCursorSchema()
	return sourceObjectSchema([]string{"surface_contract", "packet_id", "repository_key", "mode", "limit", "examined_objects", "examined_bytes"}, properties)
}

func readSourceTextInputSchema() map[string]any {
	properties := sourceRouteProperties()
	properties["revision"] = sourceRevisionSelectorSchema()
	properties["path"] = sourcePathSelectorSchema()
	properties["offset"] = sourceMinimumInteger(0)
	properties["limit"] = sourceBoundedInteger(sourceTextPageMinimumBytes, sourceTextPageMaximumBytes)
	properties["cursor"] = sourceCursorSchema()
	return sourceObjectSchema([]string{"surface_contract", "packet_id", "repository_key", "path", "limit"}, properties)
}

func listSourceTreeOutputSchema() map[string]any {
	entry := sourceObjectSchema([]string{"Path", "Basename", "Mode", "ObjectType", "ObjectOID", "Directory"}, map[string]any{
		"Path":       sourcePathIdentitySchema(),
		"Basename":   sourcePathIdentitySchema(),
		"Mode":       sourceAnyString(),
		"ObjectType": sourceAnyString(),
		"ObjectOID":  sourceAnyString(),
		"Directory":  sourceBooleanSchema(),
	})
	return sourceOneOf(sourceObjectSchema([]string{"Source", "Directory", "Entries", "Complete", "Cursor"}, map[string]any{
		"Source":    sourceIdentitySchema(),
		"Directory": sourcePathIdentitySchema(),
		"Entries":   sourceArraySchema(0, sourceTreePageEntryLimit, entry),
		"Complete":  sourceBooleanSchema(),
		"Cursor":    sourceAnyString(),
	}))
}

func searchSourceOutputSchema() map[string]any {
	match := sourceObjectSchema([]string{"MatchID", "Path", "FileMode", "BlobOID", "ByteOffset", "MatchLength", "OccurrenceOrdinal"}, map[string]any{
		"MatchID":           sourceAnyString(),
		"Path":              sourcePathIdentitySchema(),
		"FileMode":          sourceAnyString(),
		"BlobOID":           sourceAnyString(),
		"ByteOffset":        sourceAnyInteger(),
		"MatchLength":       sourceAnyInteger(),
		"OccurrenceOrdinal": sourceAnyInteger(),
	})
	return sourceOneOf(sourceObjectSchema([]string{"Source", "Mode", "QueryID", "FilterID", "Matches", "ExaminedObjects", "ExaminedBytes", "ObjectBudgetExhausted", "ByteBudgetExhausted", "Completion", "Cursor"}, map[string]any{
		"Source":                sourceIdentitySchema(),
		"Mode":                  sourceStringEnum("text_literal", "byte_literal"),
		"QueryID":               sourceAnyString(),
		"FilterID":              sourceAnyString(),
		"Matches":               sourceArraySchema(0, sourceSearchPageMatchLimit, match),
		"ExaminedObjects":       sourceAnyInteger(),
		"ExaminedBytes":         sourceAnyInteger(),
		"ObjectBudgetExhausted": sourceBooleanSchema(),
		"ByteBudgetExhausted":   sourceBooleanSchema(),
		"Completion":            sourceStringEnum("complete", "page_incomplete", "budget_incomplete"),
		"Cursor":                sourceAnyString(),
	}))
}

func readSourceTextOutputSchema() map[string]any {
	segment := sourceObjectSchema([]string{"StartOffset", "EndOffset", "Bytes", "Terminator", "ContinuesLine", "LineComplete", "FinalLine"}, map[string]any{
		"StartOffset": sourceAnyInteger(),
		"EndOffset":   sourceAnyInteger(),
		// Exact segment bytes are a canonical base64 string, or null for an
		// empty terminator or empty final line.
		"Bytes":         map[string]any{},
		"Terminator":    map[string]any{},
		"ContinuesLine": sourceBooleanSchema(),
		"LineComplete":  sourceBooleanSchema(),
		"FinalLine":     sourceBooleanSchema(),
	})
	return sourceOneOf(sourceObjectSchema([]string{"Source", "Path", "Mode", "ObjectOID", "Segments", "Offset", "NextOffset", "TotalSize", "Complete", "Cursor"}, map[string]any{
		"Source":     sourceIdentitySchema(),
		"Path":       sourcePathIdentitySchema(),
		"Mode":       sourceAnyString(),
		"ObjectOID":  sourceAnyString(),
		"Segments":   sourceArraySchema(0, sourceTextPageMaximumBytes+1, segment),
		"Offset":     sourceAnyInteger(),
		"NextOffset": sourceAnyInteger(),
		"TotalSize":  sourceAnyInteger(),
		"Complete":   sourceBooleanSchema(),
		"Cursor":     sourceAnyString(),
	}))
}

func sourceIdentitySchema() map[string]any {
	return sourceObjectSchema([]string{"PacketID", "PacketSHA256", "LifecycleState", "SurfaceContract", "OperationID", "ProjectID", "RepositoryKey", "DependencyKey", "AnchorName", "PublicationID", "VaultRelationshipRowID", "CommitOID", "TreeOID"}, map[string]any{
		"PacketID":               sourceAnyString(),
		"PacketSHA256":           sourceAnyString(),
		"LifecycleState":         sourceAnyString(),
		"SurfaceContract":        sourceAnyString(),
		"OperationID":            sourceAnyString(),
		"ProjectID":              sourceAnyString(),
		"RepositoryKey":          sourceAnyString(),
		"DependencyKey":          sourceAnyString(),
		"AnchorName":             sourceAnyString(),
		"PublicationID":          sourceAnyString(),
		"VaultRelationshipRowID": sourceAnyInteger(),
		"CommitOID":              sourceAnyString(),
		"TreeOID":                sourceAnyString(),
	})
}

func sourcePathIdentitySchema() map[string]any {
	return sourceObjectSchema([]string{"Version", "PathID", "ByteLength", "InlineBase64", "SelectorID", "Display", "DisplayValid"}, map[string]any{
		"Version":      sourceAnyString(),
		"PathID":       sourceAnyString(),
		"ByteLength":   sourceAnyInteger(),
		"InlineBase64": sourceAnyString(),
		"SelectorID":   sourceAnyString(),
		"Display":      sourceAnyString(),
		"DisplayValid": sourceBooleanSchema(),
	})
}

// sourceRouteProperties declares the route-bound identity members shared by
// every owned source tool. Route binding replaces surface_contract with the
// mounted constant and operation_id with the exact route membership.
func sourceRouteProperties() map[string]any {
	return map[string]any{
		"surface_contract": sourceAnyString(),
		"operation_id":     sourceAnyString(),
		"packet_id":        sourceBoundedString(1, sourceIdentifierBytes),
		"repository_key":   sourceBoundedString(1, sourceIdentifierBytes),
	}
}

func sourcePathSelectorSchema() map[string]any {
	return sourceObjectSchema([]string{"path_id"}, map[string]any{
		"path_id":       map[string]any{"type": "string", "pattern": "^[0-9a-f]{" + strconv.Itoa(sourceDigestHexLength) + "}$"},
		"inline_base64": sourceBoundedString(0, sourceInlinePathBase64Bytes),
		"selector_id":   sourceBoundedString(1, sourceIdentifierBytes),
	})
}

func sourceRevisionSelectorSchema() map[string]any {
	return sourceObjectSchema([]string{}, map[string]any{"anchor_name": sourceBoundedString(0, sourceIdentifierBytes)})
}

func sourceCursorSchema() map[string]any {
	return sourceBoundedString(1, sourceCursorTokenBytes)
}

func sourceObjectSchema(required []string, properties map[string]any) map[string]any {
	if required == nil {
		required = []string{}
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func sourceArraySchema(minimum, maximum int, items map[string]any) map[string]any {
	return map[string]any{"type": "array", "minItems": minimum, "maxItems": maximum, "items": items}
}

func sourceAnyString() map[string]any  { return map[string]any{"type": "string"} }
func sourceAnyInteger() map[string]any { return map[string]any{"type": "integer"} }
func sourceBooleanSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func sourceBoundedString(minimum, maximum int) map[string]any {
	return map[string]any{"type": "string", "minLength": minimum, "maxLength": maximum}
}

func sourceStringEnum(values ...string) map[string]any {
	members := make([]any, len(values))
	for index, value := range values {
		members[index] = value
	}
	return map[string]any{"type": "string", "enum": members}
}

func sourceBoundedInteger(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}

func sourceMinimumInteger(minimum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum}
}

func sourceOneOf(branch map[string]any) map[string]any {
	return map[string]any{"oneOf": []any{branch}}
}

func sourceSchemaBytes(value map[string]any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}
