package artifactschema

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

const (
	AuthorityRepository = "Paintersrp/relay-specs"
	AuthorityCommit     = "7bd061c3ad989260345da5c5b2f42b3833561242"
)

type Kind string

const (
	KindPlan          Kind = "plan"
	KindExecutionSpec Kind = "execution_spec"
	KindAuditPacket   Kind = "audit_packet"
)

type Definition struct {
	Kind            Kind
	ProducerVersion string
	AuthorityPath   string
	SHA256          string
	Bytes           []byte
}

//go:embed schemas/*.json
var schemaFS embed.FS

var orderedKinds = []Kind{KindPlan, KindExecutionSpec, KindAuditPacket}

func Current(kind Kind) (Definition, bool) {
	version, path, ok := definitionMetadata(kind)
	if !ok {
		return Definition{}, false
	}
	raw, err := schemaFS.ReadFile("schemas/" + schemaFilename(kind))
	if err != nil {
		panic(fmt.Sprintf("read embedded %s schema: %v", kind, err))
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(raw)
	return Definition{
		Kind:            kind,
		ProducerVersion: version,
		AuthorityPath:   path,
		SHA256:          hex.EncodeToString(sum[:]),
		Bytes:           append([]byte(nil), raw...),
	}, true
}

func Definitions() []Definition {
	out := make([]Definition, 0, len(orderedKinds))
	for _, kind := range orderedKinds {
		definition, _ := Current(kind)
		out = append(out, definition)
	}
	return out
}

func Validate(kind Kind, raw []byte) (bool, error) {
	root, ok := Current(kind)
	if !ok {
		return false, fmt.Errorf("unsupported artifact schema kind %q", kind)
	}
	prepared, err := prepareSchema(root)
	if err != nil {
		return false, err
	}
	loader := gojsonschema.NewSchemaLoader()
	loader.AutoDetect = false
	loader.Draft = gojsonschema.Draft7
	if kind == KindAuditPacket {
		for _, dependencyKind := range []Kind{KindPlan, KindExecutionSpec} {
			dependency, _ := Current(dependencyKind)
			dependencyPrepared, err := prepareSchema(dependency)
			if err != nil {
				return false, err
			}
			if err := loader.AddSchema(schemaURL(dependencyKind), gojsonschema.NewGoLoader(dependencyPrepared)); err != nil {
				return false, fmt.Errorf("register %s schema: %w", dependencyKind, err)
			}
		}
	}
	schema, err := loader.Compile(gojsonschema.NewGoLoader(prepared))
	if err != nil {
		return false, fmt.Errorf("compile %s schema: %w", kind, err)
	}
	result, err := schema.Validate(gojsonschema.NewBytesLoader(raw))
	if err != nil {
		return false, fmt.Errorf("validate %s artifact: %w", kind, err)
	}
	return result.Valid(), nil
}

func definitionMetadata(kind Kind) (string, string, bool) {
	switch kind {
	case KindPlan:
		return "1.0", "schemas/plan.schema.json", true
	case KindExecutionSpec:
		return "2.0", "schemas/execution-spec.schema.json", true
	case KindAuditPacket:
		return "2.0", "schemas/audit-packet.schema.json", true
	default:
		return "", "", false
	}
}

func schemaFilename(kind Kind) string {
	switch kind {
	case KindPlan:
		return "plan.schema.json"
	case KindExecutionSpec:
		return "execution-spec.schema.json"
	case KindAuditPacket:
		return "audit-packet.schema.json"
	default:
		return ""
	}
}

func schemaURL(kind Kind) string {
	return "https://relay.local/schemas/" + schemaFilename(kind)
}

func prepareSchema(definition Definition) (any, error) {
	var document any
	if err := json.Unmarshal(definition.Bytes, &document); err != nil {
		return nil, fmt.Errorf("decode %s schema: %w", definition.Kind, err)
	}
	normalized, err := normalizeSchemaNode(document)
	if err != nil {
		return nil, fmt.Errorf("normalize %s schema: %w", definition.Kind, err)
	}
	object, ok := normalized.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s schema root is not an object", definition.Kind)
	}
	object["$id"] = schemaURL(definition.Kind)
	return object, nil
}

// gojsonschema v1.2.0 implements through draft 7 and uses Go regular
// expressions. The authoritative schemas are draft 2020-12 and use a bounded
// set of ECMA-style lookahead patterns. Convert every known pattern to an
// equivalent draft-7/RE2-compatible constraint and fail closed on any unknown
// pattern instead of silently weakening current-schema validation.
func normalizeSchemaNode(value any) (any, error) {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			switch key {
			case "$schema", "pattern":
				continue
			case "$defs":
				converted, err := normalizeSchemaNode(child)
				if err != nil {
					return nil, err
				}
				normalized["definitions"] = converted
			case "$ref":
				if reference, ok := child.(string); ok {
					normalized[key] = strings.Replace(reference, "#/$defs/", "#/definitions/", 1)
				} else {
					normalized[key] = child
				}
			default:
				converted, err := normalizeSchemaNode(child)
				if err != nil {
					return nil, err
				}
				normalized[key] = converted
			}
		}
		if pattern, ok := typed["pattern"].(string); ok {
			constraints, err := portablePatternConstraints(pattern)
			if err != nil {
				return nil, err
			}
			existing, _ := normalized["allOf"].([]any)
			for _, constraint := range constraints {
				existing = append(existing, constraint)
			}
			normalized["allOf"] = existing
		}
		return normalized, nil
	case []any:
		normalized := make([]any, len(typed))
		for index, child := range typed {
			converted, err := normalizeSchemaNode(child)
			if err != nil {
				return nil, err
			}
			normalized[index] = converted
		}
		return normalized, nil
	default:
		return value, nil
	}
}

func portablePatternConstraints(pattern string) ([]any, error) {
	switch pattern {
	case `^(?!.*[\r\n])[a-z0-9]+(?:-[a-z0-9]+)*$`:
		return []any{patternConstraint(`^[a-z0-9]+(-[a-z0-9]+)*$`)}, nil
	case `^(?!.*[\r\n])(?!\s*$)[^\r\n]*\S[^\r\n]*$`:
		return []any{patternConstraint(`^[^\r\n]*[^\t\n\f\r\v \p{Z}\x{FEFF}][^\r\n]*$`)}, nil
	case `^[\s\S]*\S[\s\S]*$`:
		return []any{patternConstraint(`(?s)^.*[^\t\n\f\r\v \p{Z}\x{FEFF}].*$`)}, nil
	case `^(?!.*[\r\n])(?!\s*$)(?!\s)(?!.*\s$)[^\u0000-\u001F\u007F]+$`:
		return []any{
			patternConstraint(`^[^\x00-\x1F\x7F]+$`),
			notPatternConstraint(`^[\t\n\f\r\v \p{Z}\x{FEFF}]`),
			notPatternConstraint(`[\t\n\f\r\v \p{Z}\x{FEFF}]$`),
		}, nil
	case `^(?!.*[\r\n])[0-9a-f]{40}$`:
		return []any{patternConstraint(`^[0-9a-f]{40}$`)}, nil
	case `^(?!.*[\r\n])[0-9a-f]{64}$`:
		return []any{patternConstraint(`^[0-9a-f]{64}$`)}, nil
	case `^(?!.*[\r\n])(?!/)(?![A-Za-z]:)(?!//)(?!.*\\)(?!.*(?:^|/)\.\.?$)(?!.*(?:^|/)\.\.?/)(?!\s)(?!.*\s$)(?!.*[\u0000-\u001F\u007F]).*\S.*$`:
		return []any{
			patternConstraint(`^[^\x00-\x1F\x7F\\]+$`),
			notPatternConstraint(`^/`),
			notPatternConstraint(`^[A-Za-z]:`),
			notPatternConstraint(`(^|/)\.\.?($|/)`),
			notPatternConstraint(`^[\t\n\f\r\v \p{Z}\x{FEFF}]`),
			notPatternConstraint(`[\t\n\f\r\v \p{Z}\x{FEFF}]$`),
		}, nil
	case `^[1-9][0-9]*\.[1-9][0-9]*$`:
		return []any{patternConstraint(`^[1-9][0-9]*\.[1-9][0-9]*$`)}, nil
	case `^(?!.*[\r\n])(?!\s*$).+\.execution-spec\.json$`:
		return []any{patternConstraint(`^.+\.execution-spec\.json$`)}, nil
	case `^(?!.*[\r\n])(?!\s*$).+\.executor-brief\.md$`:
		return []any{patternConstraint(`^.+\.executor-brief\.md$`)}, nil
	default:
		return nil, fmt.Errorf("unsupported authoritative schema pattern %q", pattern)
	}
}

func patternConstraint(pattern string) map[string]any {
	return map[string]any{"pattern": pattern}
}

func notPatternConstraint(pattern string) map[string]any {
	return map[string]any{"not": map[string]any{"pattern": pattern}}
}
