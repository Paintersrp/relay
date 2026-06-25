package drift

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

//go:embed schema/intent_drift_review.schema.json
var intentDriftReviewSchemaBytes []byte

// ValidateIntentDriftReviewJSON validates a raw JSON byte slice against the intent_drift_review schema.
func ValidateIntentDriftReviewJSON(raw []byte) error {
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("invalid JSON syntax: %w", err)
	}

	schemaStr := sanitizeSchemaRegexes(string(intentDriftReviewSchemaBytes))

	schemaLoader := gojsonschema.NewStringLoader(schemaStr)
	documentLoader := gojsonschema.NewGoLoader(doc)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return fmt.Errorf("schema validation error: %w", err)
	}

	if !result.Valid() {
		var errMsgs []string
		for _, desc := range result.Errors() {
			errMsgs = append(errMsgs, desc.String())
		}
		return fmt.Errorf("schema validation failed: %s", strings.Join(errMsgs, "; "))
	}

	return nil
}

func sanitizeSchemaRegexes(schemaContent string) string {
	schemaContent = strings.ReplaceAll(schemaContent, `(?!/)`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*(^|/)\.\.($|/))`, "")
	schemaContent = strings.ReplaceAll(schemaContent, `(?!.*\\)`, "")
	return schemaContent
}
