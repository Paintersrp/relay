package surfacecontracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"relay/internal/operations/registry"
)

type Annotations struct {
	ReadOnlyHint    bool
	DestructiveHint bool
	IdempotentHint  bool
	OpenWorldHint   bool
}

type ToolContract struct {
	Name            string
	Title           string
	Description     string
	SemanticToolID  string
	Invoking        string
	Invoked         string
	Annotations     Annotations
	FileParams      []string
	InputSchema     []byte
	InputSHA256     string
	InputSizeBytes  int
	OutputSchema    []byte
	OutputSHA256    string
	OutputSizeBytes int
	SurfaceContract registry.SurfaceContractID
	ManifestSHA256  string
}

type SurfaceManifest struct {
	SurfaceContract   registry.SurfaceContractID
	Role              registry.Role
	Operations        []registry.OperationID
	Tools             []ToolContract
	ManifestBasis     []byte
	ManifestBasisSize int
	ManifestSHA256    string
}

type member struct {
	name  string
	value *node
}

type node struct {
	kind    byte
	object  []member
	array   []*node
	text    string
	boolean bool
}

type catalogState struct {
	order           []registry.SurfaceContractID
	surfaces        map[registry.SurfaceContractID]SurfaceManifest
	definitionCount int
}

var (
	loadOnce sync.Once
	state    catalogState
	loadErr  error
)

func Validate() error {
	load()
	return loadErr
}

func DefinitionCount() int {
	load()
	if loadErr != nil {
		return 0
	}
	return state.definitionCount
}

func All() ([]SurfaceManifest, error) {
	load()
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]SurfaceManifest, 0, len(state.order))
	for _, surface := range state.order {
		out = append(out, cloneManifest(state.surfaces[surface]))
	}
	return out, nil
}

func Get(surface registry.SurfaceContractID) (SurfaceManifest, bool) {
	load()
	if loadErr != nil {
		return SurfaceManifest{}, false
	}
	value, ok := state.surfaces[surface]
	if !ok {
		return SurfaceManifest{}, false
	}
	return cloneManifest(value), true
}

func load() {
	loadOnce.Do(func() {
		loadErr = buildCatalog()
	})
}

func buildCatalog() error {
	if err := registry.Validate(); err != nil {
		return err
	}
	raw := registry.RawPublicContract()
	root, err := parse(raw)
	if err != nil {
		return fmt.Errorf("decode public contract: %w", err)
	}
	if value, ok := stringMember(root, "catalog_version"); !ok || value != registry.PublicContractVersion {
		return fmt.Errorf("catalog_version %q does not equal %q", value, registry.PublicContractVersion)
	}
	schemaDialect, ok := stringMember(root, "schema_dialect")
	if !ok || schemaDialect != "https://json-schema.org/draft/2020-12/schema" {
		return fmt.Errorf("unsupported schema dialect %q", schemaDialect)
	}
	manifestVersion, ok := stringMember(root, "manifest_basis_schema_version")
	if !ok || manifestVersion != "relay.mcp.surface-manifest.v1" {
		return fmt.Errorf("manifest basis version %q is invalid", manifestVersion)
	}

	definitionOrderNode, ok := objectMember(root, "definition_order")
	if !ok || definitionOrderNode.kind != 'a' {
		return errors.New("definition_order is missing")
	}
	definitionOrder, err := stringArray(definitionOrderNode)
	if err != nil {
		return err
	}
	definitionsNode, ok := objectMember(root, "definitions")
	if !ok || definitionsNode.kind != 'o' {
		return errors.New("definitions are missing")
	}
	if len(definitionOrder) != len(definitionsNode.object) {
		return errors.New("definition order and definition object cardinality differ")
	}
	definitions := make(map[string]*node, len(definitionsNode.object))
	for _, definition := range definitionsNode.object {
		if _, duplicate := definitions[definition.name]; duplicate {
			return fmt.Errorf("definition %q is duplicated", definition.name)
		}
		definitions[definition.name] = definition.value
	}
	for _, definitionName := range definitionOrder {
		if _, ok := definitions[definitionName]; !ok {
			return fmt.Errorf("definition %q is missing", definitionName)
		}
	}

	surfacesNode, ok := objectMember(root, "surfaces")
	if !ok || surfacesNode.kind != 'o' {
		return errors.New("surfaces are missing")
	}
	if len(surfacesNode.object) != 6 {
		return fmt.Errorf("surface count %d does not equal 6", len(surfacesNode.object))
	}

	built := catalogState{
		order:           make([]registry.SurfaceContractID, 0, len(surfacesNode.object)),
		surfaces:        make(map[registry.SurfaceContractID]SurfaceManifest, len(surfacesNode.object)),
		definitionCount: len(definitionOrder),
	}
	for _, surfaceMember := range surfacesNode.object {
		surfaceID := registry.SurfaceContractID(surfaceMember.name)
		manifest, err := buildSurface(root, surfaceID, surfaceMember.value, schemaDialect, manifestVersion, definitionOrder, definitions)
		if err != nil {
			return err
		}
		if _, duplicate := built.surfaces[surfaceID]; duplicate {
			return fmt.Errorf("surface %q is duplicated", surfaceID)
		}
		built.order = append(built.order, surfaceID)
		built.surfaces[surfaceID] = manifest
	}
	state = built
	return nil
}

func buildSurface(root *node, surfaceID registry.SurfaceContractID, surface *node, schemaDialect, manifestVersion string, definitionOrder []string, definitions map[string]*node) (SurfaceManifest, error) {
	role, ok := stringMember(surface, "role")
	if !ok {
		return SurfaceManifest{}, fmt.Errorf("surface %q role is missing", surfaceID)
	}
	operationsNode, ok := objectMember(surface, "operations")
	if !ok {
		return SurfaceManifest{}, fmt.Errorf("surface %q operations are missing", surfaceID)
	}
	operationStrings, err := stringArray(operationsNode)
	if err != nil {
		return SurfaceManifest{}, err
	}
	operations := make([]registry.OperationID, len(operationStrings))
	for index, operation := range operationStrings {
		operations[index] = registry.OperationID(operation)
	}
	registryOperations, err := registry.OperationsForSurface(surfaceID)
	if err != nil {
		return SurfaceManifest{}, err
	}
	if len(registryOperations) != len(operations) {
		return SurfaceManifest{}, fmt.Errorf("surface %q registry operation count differs", surfaceID)
	}
	for index := range operations {
		if registryOperations[index].OperationID != operations[index] {
			return SurfaceManifest{}, fmt.Errorf("surface %q operation %d differs from registry", surfaceID, index)
		}
	}

	toolOrderNode, ok := objectMember(surface, "tool_order")
	if !ok {
		return SurfaceManifest{}, fmt.Errorf("surface %q tool_order is missing", surfaceID)
	}
	toolOrder, err := stringArray(toolOrderNode)
	if err != nil {
		return SurfaceManifest{}, err
	}
	toolsNode, ok := objectMember(surface, "tools")
	if !ok || toolsNode.kind != 'o' {
		return SurfaceManifest{}, fmt.Errorf("surface %q tools are missing", surfaceID)
	}
	if len(toolOrder) != len(toolsNode.object) {
		return SurfaceManifest{}, fmt.Errorf("surface %q tool order cardinality differs", surfaceID)
	}

	manifestSHA, ok := stringMember(surface, "manifest_sha256")
	if !ok {
		return SurfaceManifest{}, fmt.Errorf("surface %q manifest_sha256 is missing", surfaceID)
	}
	manifestSize, ok := intMember(surface, "manifest_basis_size_bytes")
	if !ok {
		return SurfaceManifest{}, fmt.Errorf("surface %q manifest size is missing", surfaceID)
	}

	tools := make([]ToolContract, 0, len(toolOrder))
	toolNodes := make(map[string]*node, len(toolsNode.object))
	for _, toolMember := range toolsNode.object {
		toolNodes[toolMember.name] = toolMember.value
	}
	for _, toolName := range toolOrder {
		toolNode, ok := toolNodes[toolName]
		if !ok {
			return SurfaceManifest{}, fmt.Errorf("surface %q tool %q is missing", surfaceID, toolName)
		}
		tool, err := buildTool(surfaceID, manifestSHA, toolName, toolNode, schemaDialect, definitionOrder, definitions)
		if err != nil {
			return SurfaceManifest{}, err
		}
		tools = append(tools, tool)
	}

	basis, err := encodeManifestBasis(surfaceID, registry.Role(role), operations, tools, manifestVersion)
	if err != nil {
		return SurfaceManifest{}, err
	}
	if len(basis) != manifestSize {
		return SurfaceManifest{}, fmt.Errorf("surface %q manifest basis size %d does not equal %d", surfaceID, len(basis), manifestSize)
	}
	if digest(basis) != manifestSHA {
		return SurfaceManifest{}, fmt.Errorf("surface %q manifest sha256 %s does not equal %s", surfaceID, digest(basis), manifestSHA)
	}

	return SurfaceManifest{
		SurfaceContract:   surfaceID,
		Role:              registry.Role(role),
		Operations:        operations,
		Tools:             tools,
		ManifestBasis:     basis,
		ManifestBasisSize: manifestSize,
		ManifestSHA256:    manifestSHA,
	}, nil
}

func buildTool(surfaceID registry.SurfaceContractID, manifestSHA, toolName string, tool *node, schemaDialect string, definitionOrder []string, definitions map[string]*node) (ToolContract, error) {
	name, ok := stringMember(tool, "name")
	if !ok || name != toolName {
		return ToolContract{}, fmt.Errorf("tool key %q does not equal name %q", toolName, name)
	}
	title, ok := stringMember(tool, "title")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q title is missing", toolName)
	}
	description, ok := stringMember(tool, "description")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q description is missing", toolName)
	}
	semanticToolID, ok := stringMember(tool, "semantic_tool_id")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q semantic_tool_id is missing", toolName)
	}
	invoking, ok := stringMember(tool, "invoking")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q invoking text is missing", toolName)
	}
	invoked, ok := stringMember(tool, "invoked")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q invoked text is missing", toolName)
	}
	annotationsNode, ok := objectMember(tool, "annotations")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q annotations are missing", toolName)
	}
	annotations, err := decodeAnnotations(annotationsNode)
	if err != nil {
		return ToolContract{}, fmt.Errorf("tool %q: %w", toolName, err)
	}
	fileParamsNode, ok := objectMember(tool, "file_params")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q file_params are missing", toolName)
	}
	fileParams, err := stringArray(fileParamsNode)
	if err != nil {
		return ToolContract{}, err
	}
	if err := validateFileParams(toolName, fileParams); err != nil {
		return ToolContract{}, err
	}

	inputRoot, ok := objectMember(tool, "input_root")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q input_root is missing", toolName)
	}
	outputRoot, ok := objectMember(tool, "output_root")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q output_root is missing", toolName)
	}
	inputSHA, ok := stringMember(tool, "input_schema_sha256")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q input_schema_sha256 is missing", toolName)
	}
	outputSHA, ok := stringMember(tool, "output_schema_sha256")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q output_schema_sha256 is missing", toolName)
	}
	inputSize, ok := intMember(tool, "input_schema_size_bytes")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q input schema size is missing", toolName)
	}
	outputSize, ok := intMember(tool, "output_schema_size_bytes")
	if !ok {
		return ToolContract{}, fmt.Errorf("tool %q output schema size is missing", toolName)
	}

	inputSchema, err := assembleSchema(schemaDialect, surfaceID, toolName, title, "input", inputRoot, definitionOrder, definitions)
	if err != nil {
		return ToolContract{}, err
	}
	outputSchema, err := assembleSchema(schemaDialect, surfaceID, toolName, title, "output", outputRoot, definitionOrder, definitions)
	if err != nil {
		return ToolContract{}, err
	}
	if len(inputSchema) != inputSize || digest(inputSchema) != inputSHA {
		return ToolContract{}, fmt.Errorf("tool %q input schema identity mismatch", toolName)
	}
	if len(outputSchema) != outputSize || digest(outputSchema) != outputSHA {
		return ToolContract{}, fmt.Errorf("tool %q output schema identity mismatch", toolName)
	}

	return ToolContract{
		Name:            name,
		Title:           title,
		Description:     description,
		SemanticToolID:  semanticToolID,
		Invoking:        invoking,
		Invoked:         invoked,
		Annotations:     annotations,
		FileParams:      fileParams,
		InputSchema:     inputSchema,
		InputSHA256:     inputSHA,
		InputSizeBytes:  inputSize,
		OutputSchema:    outputSchema,
		OutputSHA256:    outputSHA,
		OutputSizeBytes: outputSize,
		SurfaceContract: surfaceID,
		ManifestSHA256:  manifestSHA,
	}, nil
}

func assembleSchema(schemaDialect string, surfaceID registry.SurfaceContractID, toolName, title, direction string, root *node, definitionOrder []string, definitions map[string]*node) ([]byte, error) {
	needed := make(map[string]struct{})
	collectReferences(root, needed, definitions)
	var output bytes.Buffer
	output.WriteByte('{')
	writeNameString(&output, "$schema", schemaDialect, false)
	writeNameString(&output, "$id", fmt.Sprintf("urn:relay:mcp:%s:%s:%s:v1", surfaceID, toolName, direction), true)
	writeNameString(&output, "title", title+" "+direction, true)
	if direction == "input" {
		for _, key := range []string{"type", "additionalProperties", "required", "properties"} {
			value, ok := objectMember(root, key)
			if !ok {
				return nil, fmt.Errorf("%s root is missing %s", direction, key)
			}
			writeNameNode(&output, key, value, true)
		}
	} else {
		value, ok := objectMember(root, "oneOf")
		if !ok {
			return nil, errors.New("output root is missing oneOf")
		}
		writeNameNode(&output, "oneOf", value, true)
	}
	output.WriteString(`,"$defs":{`)
	written := 0
	for _, name := range definitionOrder {
		if _, ok := needed[name]; !ok {
			continue
		}
		if written > 0 {
			output.WriteByte(',')
		}
		appendJSONString(&output, name)
		output.WriteByte(':')
		encodeNode(&output, definitions[name])
		written++
	}
	output.WriteString("}}")
	return output.Bytes(), nil
}

func collectReferences(value *node, needed map[string]struct{}, definitions map[string]*node) {
	if value == nil {
		return
	}
	if value.kind == 'o' {
		if reference, ok := stringMember(value, "$ref"); ok && strings.HasPrefix(reference, "#/$defs/") {
			name := strings.TrimPrefix(reference, "#/$defs/")
			if _, exists := needed[name]; !exists {
				needed[name] = struct{}{}
				collectReferences(definitions[name], needed, definitions)
			}
		}
		for _, member := range value.object {
			collectReferences(member.value, needed, definitions)
		}
		return
	}
	for _, child := range value.array {
		collectReferences(child, needed, definitions)
	}
}

func encodeManifestBasis(surfaceID registry.SurfaceContractID, role registry.Role, operations []registry.OperationID, tools []ToolContract, version string) ([]byte, error) {
	var output bytes.Buffer
	output.WriteByte('{')
	writeNameString(&output, "schema_version", version, false)
	writeNameString(&output, "surface_contract", string(surfaceID), true)
	writeNameString(&output, "role", string(role), true)
	output.WriteString(`,"operations":[`)
	for index, operation := range operations {
		if index > 0 {
			output.WriteByte(',')
		}
		appendJSONString(&output, string(operation))
	}
	output.WriteString(`],"tools":[`)
	for index, tool := range tools {
		if index > 0 {
			output.WriteByte(',')
		}
		output.WriteByte('{')
		writeNameString(&output, "name", tool.Name, false)
		writeNameString(&output, "title", tool.Title, true)
		writeNameString(&output, "description", tool.Description, true)
		writeNameString(&output, "semantic_tool_id", tool.SemanticToolID, true)
		output.WriteString(`,"input_schema":`)
		output.Write(tool.InputSchema)
		output.WriteString(`,"output_schema":`)
		output.Write(tool.OutputSchema)
		output.WriteString(`,"annotations":{`)
		writeNameBool(&output, "readOnlyHint", tool.Annotations.ReadOnlyHint, false)
		writeNameBool(&output, "destructiveHint", tool.Annotations.DestructiveHint, true)
		writeNameBool(&output, "idempotentHint", tool.Annotations.IdempotentHint, true)
		writeNameBool(&output, "openWorldHint", tool.Annotations.OpenWorldHint, true)
		output.WriteString(`},"metadata":{`)
		writeNameBool(&output, "openai/widgetAccessible", false, false)
		writeNameString(&output, "openai/toolInvocation/invoking", tool.Invoking, true)
		writeNameString(&output, "openai/toolInvocation/invoked", tool.Invoked, true)
		if len(tool.FileParams) != 0 {
			output.WriteString(`,"openai/fileParams":[`)
			for fileIndex, fileParam := range tool.FileParams {
				if fileIndex > 0 {
					output.WriteByte(',')
				}
				appendJSONString(&output, fileParam)
			}
			output.WriteByte(']')
		}
		writeNameString(&output, "relay/surfaceContract", string(surfaceID), true)
		writeNameString(&output, "relay/semanticToolID", tool.SemanticToolID, true)
		output.WriteString("}}")
	}
	output.WriteString("]}")
	return output.Bytes(), nil
}

func decodeAnnotations(value *node) (Annotations, error) {
	readOnly, ok := boolMember(value, "readOnlyHint")
	if !ok {
		return Annotations{}, errors.New("readOnlyHint is missing")
	}
	destructive, ok := boolMember(value, "destructiveHint")
	if !ok {
		return Annotations{}, errors.New("destructiveHint is missing")
	}
	idempotent, ok := boolMember(value, "idempotentHint")
	if !ok {
		return Annotations{}, errors.New("idempotentHint is missing")
	}
	openWorld, ok := boolMember(value, "openWorldHint")
	if !ok || openWorld {
		return Annotations{}, errors.New("openWorldHint must be false")
	}
	return Annotations{
		ReadOnlyHint: readOnly, DestructiveHint: destructive,
		IdempotentHint: idempotent, OpenWorldHint: openWorld,
	}, nil
}

func validateFileParams(toolName string, values []string) error {
	expected := map[string][]string{
		"create_operation_packet":  {"input_files"},
		"refresh_operation_packet": {"input_files"},
		"validate_artifact":        {"artifact_file"},
		"submit_plan":              {"artifact_file"},
		"create_run":               {"artifact_file"},
	}
	wanted := expected[toolName]
	if len(values) != len(wanted) {
		return fmt.Errorf("tool %q file_params %v do not equal %v", toolName, values, wanted)
	}
	for index := range wanted {
		if values[index] != wanted[index] {
			return fmt.Errorf("tool %q file_params %v do not equal %v", toolName, values, wanted)
		}
	}
	return nil
}

func cloneManifest(value SurfaceManifest) SurfaceManifest {
	value.Operations = append([]registry.OperationID(nil), value.Operations...)
	value.ManifestBasis = append([]byte(nil), value.ManifestBasis...)
	originalTools := value.Tools
	value.Tools = make([]ToolContract, len(originalTools))
	for index, tool := range originalTools {
		tool.FileParams = append([]string(nil), tool.FileParams...)
		tool.InputSchema = append([]byte(nil), tool.InputSchema...)
		tool.OutputSchema = append([]byte(nil), tool.OutputSchema...)
		value.Tools[index] = tool
	}
	return value
}

func parse(raw []byte) (*node, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeNode(decoder)
	if err != nil {
		return nil, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected trailing token %v: %w", token, err)
	}
	return value, nil
}

func decodeNode(decoder *json.Decoder) (*node, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch typed := token.(type) {
	case json.Delim:
		switch typed {
		case '{':
			value := &node{kind: 'o'}
			for decoder.More() {
				nameToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := nameToken.(string)
				if !ok {
					return nil, errors.New("object key is not a string")
				}
				child, err := decodeNode(decoder)
				if err != nil {
					return nil, err
				}
				value.object = append(value.object, member{name: name, value: child})
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return value, nil
		case '[':
			value := &node{kind: 'a'}
			for decoder.More() {
				child, err := decodeNode(decoder)
				if err != nil {
					return nil, err
				}
				value.array = append(value.array, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return value, nil
		default:
			return nil, fmt.Errorf("unexpected delimiter %q", typed)
		}
	case string:
		return &node{kind: 's', text: typed}, nil
	case json.Number:
		return &node{kind: 'n', text: typed.String()}, nil
	case bool:
		return &node{kind: 'b', boolean: typed}, nil
	case nil:
		return &node{kind: '0'}, nil
	default:
		return nil, fmt.Errorf("unsupported token %T", token)
	}
}

func encodeNode(output *bytes.Buffer, value *node) {
	switch value.kind {
	case 'o':
		output.WriteByte('{')
		for index, item := range value.object {
			if index > 0 {
				output.WriteByte(',')
			}
			appendJSONString(output, item.name)
			output.WriteByte(':')
			encodeNode(output, item.value)
		}
		output.WriteByte('}')
	case 'a':
		output.WriteByte('[')
		for index, child := range value.array {
			if index > 0 {
				output.WriteByte(',')
			}
			encodeNode(output, child)
		}
		output.WriteByte(']')
	case 's':
		appendJSONString(output, value.text)
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
	}
}

func objectMember(value *node, name string) (*node, bool) {
	if value == nil || value.kind != 'o' {
		return nil, false
	}
	for _, item := range value.object {
		if item.name == name {
			return item.value, true
		}
	}
	return nil, false
}

func stringMember(value *node, name string) (string, bool) {
	child, ok := objectMember(value, name)
	if !ok || child.kind != 's' {
		return "", false
	}
	return child.text, true
}

func intMember(value *node, name string) (int, bool) {
	child, ok := objectMember(value, name)
	if !ok || child.kind != 'n' {
		return 0, false
	}
	number, err := strconv.Atoi(child.text)
	return number, err == nil
}

func boolMember(value *node, name string) (bool, bool) {
	child, ok := objectMember(value, name)
	if !ok || child.kind != 'b' {
		return false, false
	}
	return child.boolean, true
}

func stringArray(value *node) ([]string, error) {
	if value == nil || value.kind != 'a' {
		return nil, errors.New("value is not an array")
	}
	out := make([]string, len(value.array))
	for index, child := range value.array {
		if child.kind != 's' {
			return nil, errors.New("array value is not a string")
		}
		out[index] = child.text
	}
	return out, nil
}

func writeNameString(output *bytes.Buffer, name, value string, comma bool) {
	if comma {
		output.WriteByte(',')
	}
	appendJSONString(output, name)
	output.WriteByte(':')
	appendJSONString(output, value)
}

func writeNameBool(output *bytes.Buffer, name string, value, comma bool) {
	if comma {
		output.WriteByte(',')
	}
	appendJSONString(output, name)
	output.WriteByte(':')
	if value {
		output.WriteString("true")
	} else {
		output.WriteString("false")
	}
}

func writeNameNode(output *bytes.Buffer, name string, value *node, comma bool) {
	if comma {
		output.WriteByte(',')
	}
	appendJSONString(output, name)
	output.WriteByte(':')
	encodeNode(output, value)
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func appendJSONString(output *bytes.Buffer, value string) {
	output.WriteByte('"')
	for _, runeValue := range value {
		switch runeValue {
		case '"':
			output.WriteString(`\"`)
		case '\\':
			output.WriteString(`\\`)
		case '\b':
			output.WriteString(`\b`)
		case '\f':
			output.WriteString(`\f`)
		case '\n':
			output.WriteString(`\n`)
		case '\r':
			output.WriteString(`\r`)
		case '\t':
			output.WriteString(`\t`)
		default:
			if runeValue < 0x20 {
				fmt.Fprintf(output, `\u%04x`, runeValue)
				continue
			}
			if runeValue == utf8.RuneError {
				output.WriteRune(runeValue)
				continue
			}
			output.WriteRune(runeValue)
		}
	}
	output.WriteByte('"')
}
