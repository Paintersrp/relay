package registry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const draft202012Dialect = "https://json-schema.org/draft/2020-12/schema"

type RequestError struct {
	Code string
	Path string
}

func (e *RequestError) Error() string {
	if e == nil {
		return "request_rejected"
	}
	if e.Path == "" {
		return e.Code
	}
	return e.Code + ":" + e.Path
}

func requestError(code, path string) error {
	return &RequestError{Code: code, Path: path}
}

func boundedRequestError(err error) error {
	if err == nil {
		return nil
	}
	var requestErr *RequestError
	if errors.As(err, &requestErr) {
		return requestErr
	}
	return requestError("request_rejected", "$")
}

func ValidateRequest(surface SurfaceContractID, tool string, raw []byte) (map[string]any, error) {
	if err := Validate(); err != nil {
		return nil, requestError("request_contract_unavailable", "$")
	}
	request, err := decodeRequest(raw)
	if err != nil {
		return nil, requestError("request_json_invalid", "$")
	}
	schema, definitions, err := inputSchema(surface, tool)
	if err != nil {
		return nil, requestError("request_contract_unavailable", "$")
	}
	if err := validateInstance(request, schema, definitions, "$"); err != nil {
		return nil, boundedRequestError(err)
	}
	return request, nil
}

func ValidateSchemaDocument(raw []byte) error {
	root, err := parseOrderedJSON(raw)
	if err != nil {
		return fmt.Errorf("decode schema document: %w", err)
	}
	if root.kind != 'o' {
		return errors.New("schema document must be an object")
	}
	dialect, ok := objectValue(root, "$schema")
	if !ok || dialect.kind != 's' || dialect.text != draft202012Dialect {
		return errors.New("schema document has an invalid draft dialect")
	}
	definitions, ok := objectValue(root, "$defs")
	if !ok || definitions.kind != 'o' {
		return errors.New("schema document is missing $defs")
	}
	if err := validateSchemaNode(root, true); err != nil {
		return err
	}
	return nil
}

func validateSchemaNode(schema *orderedValue, documentRoot bool) error {
	if schema == nil || schema.kind != 'o' {
		return errors.New("schema node must be an object")
	}
	allowed := map[string]struct{}{
		"$schema": {}, "$id": {}, "$ref": {}, "$defs": {}, "title": {}, "description": {},
		"type": {}, "const": {}, "enum": {}, "additionalProperties": {}, "properties": {},
		"required": {}, "items": {}, "oneOf": {}, "minLength": {}, "maxLength": {},
		"pattern": {}, "format": {}, "minimum": {}, "maximum": {}, "minItems": {},
		"maxItems": {}, "uniqueItems": {},
	}
	seen := make(map[string]struct{}, len(schema.object))
	for _, member := range schema.object {
		if _, duplicate := seen[member.Name]; duplicate {
			return fmt.Errorf("schema keyword %q is duplicated", member.Name)
		}
		seen[member.Name] = struct{}{}
		if _, ok := allowed[member.Name]; !ok {
			return fmt.Errorf("unsupported schema keyword %q", member.Name)
		}
		switch member.Name {
		case "$schema":
			if !documentRoot || member.Value.kind != 's' || member.Value.text != draft202012Dialect {
				return errors.New("invalid $schema")
			}
		case "$id", "title", "description":
			if member.Value.kind != 's' || member.Value.text == "" {
				return fmt.Errorf("%s must be a non-empty string", member.Name)
			}
		case "$ref":
			if member.Value.kind != 's' || !strings.HasPrefix(member.Value.text, "#/$defs/") || strings.TrimPrefix(member.Value.text, "#/$defs/") == "" {
				return errors.New("unsupported $ref")
			}
		case "$defs", "properties":
			if member.Value.kind != 'o' {
				return fmt.Errorf("%s must be an object", member.Name)
			}
			for _, child := range member.Value.object {
				if child.Name == "" {
					return fmt.Errorf("%s contains an empty name", member.Name)
				}
				if err := validateSchemaNode(child.Value, false); err != nil {
					return fmt.Errorf("%s.%s: %w", member.Name, child.Name, err)
				}
			}
		case "type":
			if member.Value.kind != 's' || !containsString([]string{"object", "array", "string", "integer", "number", "boolean", "null"}, member.Value.text) {
				return errors.New("invalid type keyword")
			}
		case "additionalProperties", "uniqueItems":
			if member.Value.kind != 'b' {
				return fmt.Errorf("%s must be boolean", member.Name)
			}
		case "required":
			if _, err := schemaStringArray(member.Value); err != nil {
				return fmt.Errorf("required: %w", err)
			}
		case "items":
			if err := validateSchemaNode(member.Value, false); err != nil {
				return fmt.Errorf("items: %w", err)
			}
		case "oneOf":
			if member.Value.kind != 'a' || len(member.Value.array) == 0 {
				return errors.New("oneOf must be a non-empty array")
			}
			for index, branch := range member.Value.array {
				if err := validateSchemaNode(branch, false); err != nil {
					return fmt.Errorf("oneOf[%d]: %w", index, err)
				}
			}
		case "enum":
			if member.Value.kind != 'a' || len(member.Value.array) == 0 {
				return errors.New("enum must be a non-empty array")
			}
		case "minLength", "maxLength", "minItems", "maxItems":
			if _, err := nonNegativeSchemaInteger(member.Value); err != nil {
				return fmt.Errorf("%s: %w", member.Name, err)
			}
		case "minimum", "maximum":
			if member.Value.kind != 'n' {
				return fmt.Errorf("%s must be numeric", member.Name)
			}
			if _, ok := new(big.Rat).SetString(member.Value.text); !ok {
				return fmt.Errorf("%s is invalid", member.Name)
			}
		case "pattern":
			if member.Value.kind != 's' {
				return errors.New("pattern must be a string")
			}
			if _, err := regexp.Compile(member.Value.text); err != nil {
				return fmt.Errorf("invalid pattern: %w", err)
			}
		case "format":
			if member.Value.kind != 's' || !containsString([]string{"uri", "date-time"}, member.Value.text) {
				return errors.New("unsupported format")
			}
		case "const":
			// Any JSON value is permitted.
		}
	}
	if minimum, ok := objectValue(schema, "minLength"); ok {
		if maximum, ok := objectValue(schema, "maxLength"); ok {
			minValue, _ := nonNegativeSchemaInteger(minimum)
			maxValue, _ := nonNegativeSchemaInteger(maximum)
			if minValue > maxValue {
				return errors.New("minLength exceeds maxLength")
			}
		}
	}
	if minimum, ok := objectValue(schema, "minItems"); ok {
		if maximum, ok := objectValue(schema, "maxItems"); ok {
			minValue, _ := nonNegativeSchemaInteger(minimum)
			maxValue, _ := nonNegativeSchemaInteger(maximum)
			if minValue > maxValue {
				return errors.New("minItems exceeds maxItems")
			}
		}
	}
	return nil
}

func validateInstance(value any, schema *orderedValue, definitions map[string]*orderedValue, path string) error {
	resolved, err := resolveSchema(schema, definitions)
	if err != nil {
		return requestError("request_schema_invalid", path)
	}
	if branches, ok := objectValue(resolved, "oneOf"); ok {
		matches := 0
		for _, branch := range branches.array {
			if validateInstance(value, branch, definitions, path) == nil {
				matches++
			}
		}
		if matches != 1 {
			return requestError("request_union_invalid", path)
		}
		return nil
	}
	if constant, ok := objectValue(resolved, "const"); ok && !orderedEqualValue(constant, value) {
		return requestError("request_const_invalid", path)
	}
	if enum, ok := objectValue(resolved, "enum"); ok {
		matched := false
		for _, candidate := range enum.array {
			if orderedEqualValue(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return requestError("request_enum_invalid", path)
		}
	}
	typeNode, hasType := objectValue(resolved, "type")
	if hasType {
		if typeNode.kind != 's' || !instanceMatchesType(value, typeNode.text) {
			return requestError("request_type_invalid", path)
		}
	}
	if !hasType {
		return nil
	}
	switch typeNode.text {
	case "object":
		object := value.(map[string]any)
		properties, _ := objectValue(resolved, "properties")
		propertyByName := make(map[string]*orderedValue)
		if properties != nil {
			for _, property := range properties.object {
				propertyByName[property.Name] = property.Value
			}
		}
		if required, ok := objectValue(resolved, "required"); ok {
			names, err := schemaStringArray(required)
			if err != nil {
				return requestError("request_schema_invalid", path)
			}
			for _, name := range names {
				if _, exists := object[name]; !exists {
					return requestError("request_required_missing", joinSchemaPath(path, name))
				}
			}
		}
		additionalAllowed := true
		if additional, ok := objectValue(resolved, "additionalProperties"); ok && additional.kind == 'b' {
			additionalAllowed = additional.boolean
		}
		for name, child := range object {
			childSchema, exists := propertyByName[name]
			if !exists {
				if !additionalAllowed {
					return requestError("request_unknown_property", path)
				}
				continue
			}
			if err := validateInstance(child, childSchema, definitions, joinSchemaPath(path, name)); err != nil {
				return err
			}
		}
	case "array":
		array := value.([]any)
		if minimum, ok := objectValue(resolved, "minItems"); ok {
			limit, _ := nonNegativeSchemaInteger(minimum)
			if len(array) < limit {
				return requestError("request_array_too_short", path)
			}
		}
		if maximum, ok := objectValue(resolved, "maxItems"); ok {
			limit, _ := nonNegativeSchemaInteger(maximum)
			if len(array) > limit {
				return requestError("request_array_too_long", path)
			}
		}
		if unique, ok := objectValue(resolved, "uniqueItems"); ok && unique.kind == 'b' && unique.boolean {
			seen := make(map[string]struct{}, len(array))
			for _, child := range array {
				key, err := canonicalInstance(child)
				if err != nil {
					return requestError("request_schema_invalid", path)
				}
				if _, duplicate := seen[key]; duplicate {
					return requestError("request_array_duplicate", path)
				}
				seen[key] = struct{}{}
			}
		}
		if items, ok := objectValue(resolved, "items"); ok {
			for index, child := range array {
				if err := validateInstance(child, items, definitions, joinArrayPath(path, index)); err != nil {
					return err
				}
			}
		}
	case "string":
		text := value.(string)
		length := utf8.RuneCountInString(text)
		if minimum, ok := objectValue(resolved, "minLength"); ok {
			limit, _ := nonNegativeSchemaInteger(minimum)
			if length < limit {
				return requestError("request_string_too_short", path)
			}
		}
		if maximum, ok := objectValue(resolved, "maxLength"); ok {
			limit, _ := nonNegativeSchemaInteger(maximum)
			if length > limit {
				return requestError("request_string_too_long", path)
			}
		}
		if pattern, ok := objectValue(resolved, "pattern"); ok {
			compiled, err := regexp.Compile(pattern.text)
			if err != nil {
				return requestError("request_schema_invalid", path)
			}
			if !compiled.MatchString(text) {
				return requestError("request_pattern_invalid", path)
			}
		}
		if format, ok := objectValue(resolved, "format"); ok {
			switch format.text {
			case "uri":
				parsed, err := url.Parse(text)
				if err != nil || parsed.Scheme == "" {
					return requestError("request_format_invalid", path)
				}
			case "date-time":
				// Input schemas currently use only uri. Date-time remains a
				// recognized output-schema vocabulary and is not accepted here
				// without an exact input contract.
				return requestError("request_format_invalid", path)
			default:
				return requestError("request_schema_invalid", path)
			}
		}
	case "integer", "number":
		number := value.(json.Number)
		rat, ok := parseJSONNumberRat(number.String())
		if !ok {
			return requestError("request_number_invalid", path)
		}
		if typeNode.text == "integer" && !rat.IsInt() {
			return requestError("request_integer_invalid", path)
		}
		if minimum, ok := objectValue(resolved, "minimum"); ok {
			limit, _ := new(big.Rat).SetString(minimum.text)
			if rat.Cmp(limit) < 0 {
				return requestError("request_number_too_small", path)
			}
		}
		if maximum, ok := objectValue(resolved, "maximum"); ok {
			limit, _ := new(big.Rat).SetString(maximum.text)
			if rat.Cmp(limit) > 0 {
				return requestError("request_number_too_large", path)
			}
		}
	}
	return nil
}

func instanceMatchesType(value any, expected string) bool {
	switch expected {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		rat, ok := parseJSONNumberRat(number.String())
		return ok && rat.IsInt()
	case "number":
		_, ok := value.(json.Number)
		return ok
	default:
		return false
	}
}

func parseJSONNumberRat(text string) (*big.Rat, bool) {
	sign := 1
	if strings.HasPrefix(text, "-") {
		sign = -1
		text = strings.TrimPrefix(text, "-")
	}
	exponent := 0
	if index := strings.IndexAny(text, "eE"); index >= 0 {
		parsed, err := strconv.Atoi(text[index+1:])
		if err != nil {
			return nil, false
		}
		exponent = parsed
		text = text[:index]
	}
	fractionDigits := 0
	if index := strings.IndexByte(text, '.'); index >= 0 {
		fractionDigits = len(text) - index - 1
		text = text[:index] + text[index+1:]
	}
	if text == "" {
		return nil, false
	}
	numerator := new(big.Int)
	if _, ok := numerator.SetString(text, 10); !ok {
		return nil, false
	}
	if sign < 0 {
		numerator.Neg(numerator)
	}
	denominator := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(fractionDigits)), nil)
	if exponent > 0 {
		numerator.Mul(numerator, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil))
	} else if exponent < 0 {
		denominator.Mul(denominator, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(-exponent)), nil))
	}
	return new(big.Rat).SetFrac(numerator, denominator), true
}

func orderedEqualValue(expected *orderedValue, actual any) bool {
	switch expected.kind {
	case 's':
		value, ok := actual.(string)
		return ok && value == expected.text
	case 'n':
		value, ok := actual.(json.Number)
		if !ok {
			return false
		}
		left, leftOK := parseJSONNumberRat(expected.text)
		right, rightOK := parseJSONNumberRat(value.String())
		return leftOK && rightOK && left.Cmp(right) == 0
	case 'b':
		value, ok := actual.(bool)
		return ok && value == expected.boolean
	case '0':
		return actual == nil
	case 'a', 'o':
		expectedBytes, err := orderedValueBytes(expected)
		if err != nil {
			return false
		}
		actualBytes, err := canonicalInstanceBytes(actual)
		return err == nil && bytes.Equal(expectedBytes, actualBytes)
	default:
		return false
	}
}

func orderedValueBytes(value *orderedValue) ([]byte, error) {
	var output bytes.Buffer
	if err := writeOrderedValue(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeOrderedValue(output *bytes.Buffer, value *orderedValue) error {
	switch value.kind {
	case 'o':
		output.WriteByte('{')
		for index, member := range value.object {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, _ := json.Marshal(member.Name)
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeOrderedValue(output, member.Value); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case 'a':
		output.WriteByte('[')
		for index, child := range value.array {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeOrderedValue(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case 's':
		encoded, _ := json.Marshal(value.text)
		output.Write(encoded)
	case 'n':
		output.WriteString(value.text)
	case 'b':
		if value.boolean {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case '0':
		output.WriteString("null")
	default:
		return errors.New("unsupported ordered value")
	}
	return nil
}

func canonicalInstance(value any) (string, error) {
	encoded, err := canonicalInstanceBytes(value)
	return string(encoded), err
}

func canonicalInstanceBytes(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := writeCanonicalInstance(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonicalInstance(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeCanonicalInstance(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	case []any:
		output.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonicalInstance(output, child); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case string:
		encoded, _ := json.Marshal(typed)
		output.Write(encoded)
	case json.Number:
		normalized, err := normalizeNumber(typed)
		if err != nil {
			return err
		}
		output.WriteString(normalized)
	case bool:
		if typed {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case nil:
		output.WriteString("null")
	default:
		return fmt.Errorf("unsupported instance value %T", value)
	}
	return nil
}

func schemaStringArray(value *orderedValue) ([]string, error) {
	if value == nil || value.kind != 'a' {
		return nil, errors.New("must be an array")
	}
	result := make([]string, len(value.array))
	seen := make(map[string]struct{}, len(value.array))
	for index, child := range value.array {
		if child.kind != 's' || child.text == "" {
			return nil, errors.New("contains a non-string or empty member")
		}
		if _, duplicate := seen[child.text]; duplicate {
			return nil, errors.New("contains a duplicate")
		}
		seen[child.text] = struct{}{}
		result[index] = child.text
	}
	return result, nil
}

func nonNegativeSchemaInteger(value *orderedValue) (int, error) {
	if value == nil || value.kind != 'n' || strings.ContainsAny(value.text, ".eE") {
		return 0, errors.New("must be a non-negative integer")
	}
	parsed, err := strconv.Atoi(value.text)
	if err != nil || parsed < 0 {
		return 0, errors.New("must be a non-negative integer")
	}
	return parsed, nil
}

func joinSchemaPath(path, name string) string {
	if path == "$" {
		return "$." + name
	}
	return path + "." + name
}

func joinArrayPath(path string, index int) string {
	return path + "[" + strconv.Itoa(index) + "]"
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
