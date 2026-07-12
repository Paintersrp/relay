package speccompiler

import (
	"bytes"
	"embed"
	"encoding/json"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schemas/*.json
var schemaFS embed.FS

func validateEmbeddedSchema(registration versionRegistration, raw []byte) (bool, error) {
	schemaBytes, err := schemaFS.ReadFile(registration.SchemaPath)
	if err != nil {
		return false, err
	}
	schemaBytes = bytes.ReplaceAll(schemaBytes, []byte("\r\n"), []byte("\n"))
	prepared, err := prepareSchemaForGoJSONSchema(schemaBytes)
	if err != nil {
		return false, err
	}
	loader := gojsonschema.NewSchemaLoader()
	loader.AutoDetect = false
	loader.Draft = gojsonschema.Draft7
	schema, err := loader.Compile(gojsonschema.NewGoLoader(prepared))
	if err != nil {
		return false, err
	}
	result, err := schema.Validate(gojsonschema.NewBytesLoader(raw))
	if err != nil {
		return false, err
	}
	return result.Valid(), nil
}

// gojsonschema v1.2.0 implements through draft 7 and uses Go regular
// expressions. The authoritative schemas are draft 2020-12 and use ECMA-style
// lookaheads for lexical forms. Prepare an in-memory draft-7-compatible copy
// for shape validation; validate.go enforces every removed pattern exactly.
func prepareSchemaForGoJSONSchema(raw []byte) (any, error) {
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	return normalizeSchemaNode(document), nil
}

func normalizeSchemaNode(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for key, child := range typed {
			switch key {
			case "$schema", "pattern":
				continue
			case "$defs":
				normalized["definitions"] = normalizeSchemaNode(child)
			case "$ref":
				if reference, ok := child.(string); ok {
					normalized[key] = strings.Replace(reference, "#/$defs/", "#/definitions/", 1)
				} else {
					normalized[key] = child
				}
			default:
				normalized[key] = normalizeSchemaNode(child)
			}
		}
		return normalized
	case []any:
		normalized := make([]any, len(typed))
		for i, child := range typed {
			normalized[i] = normalizeSchemaNode(child)
		}
		return normalized
	default:
		return value
	}
}
