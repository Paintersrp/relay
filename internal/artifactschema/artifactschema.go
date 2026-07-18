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
	AuthorityCommit     = "cba7f79963e457266dafc2fd1865fd605a40b7bc"
)

type Kind string

const (
	KindPlan           Kind = "plan"
	KindExecutionSpec  Kind = "execution_spec"
	KindAuditPacket    Kind = "audit_packet"
	KindDeliveryTicket Kind = "delivery_ticket"
	KindTransitionPlan Kind = "transition_plan"
)

type Definition struct {
	Kind            Kind
	ProducerVersion string
	AuthorityPath   string
	SHA256          string
	Bytes           []byte
}

type catalogEntry struct {
	Kind            Kind
	ProducerVersion string
	AuthorityPath   string
	Filename        string
	SHA256          string
}

//go:embed schemas/*.json
var schemaFS embed.FS

var authoritativeCatalog = []catalogEntry{
	{Kind: KindPlan, ProducerVersion: "1.0", AuthorityPath: "schemas/plan.schema.json", Filename: "plan.schema.json", SHA256: "03a75ab1352d27193ec27b5aec9f449e65daf69de66d6897ab74672bdc705cf8"},
	{Kind: KindExecutionSpec, ProducerVersion: "2.0", AuthorityPath: "schemas/execution-spec.schema.json", Filename: "execution-spec.schema.json", SHA256: "92a1e8f1c2b9cc7bd4382f69f3d8bf8668c1ab72f2985c10fd746ba23a3df4d7"},
	{Kind: KindAuditPacket, ProducerVersion: "2.0", AuthorityPath: "schemas/audit-packet.schema.json", Filename: "audit-packet.schema.json", SHA256: "91aaed33acca520d0ad2f511a472be7296993b37cb1dcd0b1976025b648850cf"},
	{Kind: KindDeliveryTicket, ProducerVersion: "1.0", AuthorityPath: "schemas/delivery-ticket.schema.json", Filename: "delivery-ticket.schema.json", SHA256: "663845dfe1191d397102e689fd09f1ff1d26823ae6dc6798a2ec9cd623a02ee7"},
	{Kind: KindTransitionPlan, ProducerVersion: "1.0", AuthorityPath: "schemas/transition-plan.schema.json", Filename: "transition-plan.schema.json", SHA256: "73b552bac0201d9aa6ad907b8faad1fe6b5b88367fff18840306c8da82e5e9ec"},
}

func Current(kind Kind) (Definition, bool) {
	entry, ok := catalogEntryFor(kind)
	if !ok {
		return Definition{}, false
	}
	raw, err := schemaFS.ReadFile("schemas/" + entry.Filename)
	if err != nil {
		panic(fmt.Sprintf("read embedded %s schema: %v", kind, err))
	}
	definition, err := definitionFromEmbeddedBytes(entry, raw)
	if err != nil {
		panic(fmt.Sprintf("verify embedded %s schema against %s@%s: %v", kind, AuthorityRepository, AuthorityCommit, err))
	}
	return definition, true
}

func Definitions() []Definition {
	out := make([]Definition, 0, len(authoritativeCatalog))
	for _, entry := range authoritativeCatalog {
		definition, _ := Current(entry.Kind)
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

func catalogEntryFor(kind Kind) (catalogEntry, bool) {
	for _, entry := range authoritativeCatalog {
		if entry.Kind == kind {
			return entry, true
		}
	}
	return catalogEntry{}, false
}

func schemaFilename(kind Kind) string {
	entry, ok := catalogEntryFor(kind)
	if !ok {
		return ""
	}
	return entry.Filename
}

func definitionFromEmbeddedBytes(entry catalogEntry, raw []byte) (Definition, error) {
	if entry.AuthorityPath != "schemas/"+entry.Filename {
		return Definition{}, fmt.Errorf("catalog authority path %q does not identify embedded file %q", entry.AuthorityPath, entry.Filename)
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(raw)
	actualSHA256 := hex.EncodeToString(sum[:])
	if actualSHA256 != entry.SHA256 {
		return Definition{}, fmt.Errorf("embedded bytes SHA-256 %s does not match pinned %s", actualSHA256, entry.SHA256)
	}
	return Definition{
		Kind:            entry.Kind,
		ProducerVersion: entry.ProducerVersion,
		AuthorityPath:   entry.AuthorityPath,
		SHA256:          actualSHA256,
		Bytes:           append([]byte(nil), raw...),
	}, nil
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
	case `^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`:
		return []any{patternConstraint(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)*$`)}, nil
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
