package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"

	sourcecontract "relay/contracts/source"
)

const (
	operationsSource = "published_operations.source.json"
	routesSource     = "published_routes.source.json"
	familySource     = "published_family_tools.source.json"
	metadataSource   = "published_tool_metadata.source.json"
	schemasSource    = "published_tool_schemas.source.json"
	bindingsSource   = "published_runtime_bindings.source.json"
	operationsOutput = "published_operations.json"
	publicOutput     = "published_public_contract.json"
	pinsOutput       = "published_contract_pins.go"
)

type annotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type routeDocument struct {
	SchemaVersion                     string               `json:"schema_version"`
	SchemaDialect                     string               `json:"schema_dialect"`
	AuthorityLockSHA256               string               `json:"authority_lock_sha256"`
	OperationContractSHA256           string               `json:"operation_contract_sha256"`
	RouteOrder                        []string             `json:"route_order"`
	Routes                            []routeDefinition    `json:"routes"`
	ToolContracts                     map[string]routeTool `json:"tool_contracts"`
	SourceToolContractVersion         string               `json:"source_tool_contract_version"`
	OperationFamilyToolContractSHA256 string               `json:"operation_family_tool_contract_sha256"`
	AllToolMetadataContractSHA256     string               `json:"all_tool_metadata_contract_sha256"`
}

type routeDefinition struct {
	Path       string   `json:"path"`
	Role       string   `json:"role"`
	Surface    string   `json:"surface"`
	Operations []string `json:"operations"`
	Tools      []string `json:"tools"`
	Authority  string   `json:"authority"`
}

type routeTool struct {
	Category                string          `json:"category"`
	SemanticToolID          string          `json:"semantic_tool_id"`
	OperationID             string          `json:"operation_id"`
	Annotations             annotations     `json:"annotations"`
	FileParams              []string        `json:"file_params"`
	MetadataSource          string          `json:"metadata_source"`
	SchemaOwner             string          `json:"schema_owner"`
	DispatcherOwner         string          `json:"dispatcher_owner"`
	InputSchemaIDPattern    string          `json:"input_schema_id_pattern"`
	OutputSchemaIDPattern   string          `json:"output_schema_id_pattern"`
	InputSchemaID           string          `json:"input_schema_id"`
	OutputSchemaID          string          `json:"output_schema_id"`
	InputRoot               json.RawMessage `json:"input_root"`
	OutputRoot              json.RawMessage `json:"output_root"`
	InputSchemaSHA256       string          `json:"input_schema_sha256"`
	InputSchemaSizeBytes    int             `json:"input_schema_size_bytes"`
	OutputPayloadDefinition string          `json:"output_payload_definition"`
	OutputSchemaSHA256      string          `json:"output_schema_sha256"`
	OutputSchemaSizeBytes   int             `json:"output_schema_size_bytes"`
}

type metadataDocument struct {
	SchemaVersion           string         `json:"schema_version"`
	MetadataPropertyOrder   []string       `json:"metadata_property_order"`
	AnnotationPropertyOrder []string       `json:"annotation_property_order"`
	Tools                   []metadataTool `json:"tools"`
}
type schemaDocument struct {
	SchemaVersion string                `json:"schema_version"`
	Tools         map[string]schemaTool `json:"tools"`
}
type schemaTool struct {
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Invoking     string          `json:"invoking"`
	Invoked      string          `json:"invoked"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema"`
}
type metadataTool struct {
	Name            string      `json:"name"`
	Category        string      `json:"category"`
	SemanticToolID  string      `json:"semantic_tool_id"`
	OperationID     string      `json:"operation_id"`
	Annotations     annotations `json:"annotations"`
	FileParams      []string    `json:"file_params"`
	MetadataSource  string      `json:"metadata_source"`
	SchemaOwner     string      `json:"schema_owner"`
	DispatcherOwner string      `json:"dispatcher_owner"`
}

type familyDocument struct {
	SchemaVersion  string          `json:"schema_version"`
	InputContract  json.RawMessage `json:"input_contract"`
	OutputContract json.RawMessage `json:"output_contract"`
	Behavior       json.RawMessage `json:"behavior"`
	Tools          []familyTool    `json:"tools"`
}
type familyTool struct {
	Name            string          `json:"name"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	SemanticToolID  string          `json:"semantic_tool_id"`
	OperationID     string          `json:"operation_id"`
	Invoking        string          `json:"invoking"`
	Invoked         string          `json:"invoked"`
	Category        string          `json:"category"`
	SchemaOwner     string          `json:"schema_owner"`
	InputRoot       json.RawMessage `json:"input_root"`
	InputRequired   []string        `json:"input_required"`
	OutputRoot      json.RawMessage `json:"output_root"`
	OutputRequired  []string        `json:"output_required"`
	Annotations     annotations     `json:"annotations"`
	FileParams      []string        `json:"file_params"`
	DispatcherOwner string          `json:"dispatcher_owner"`
}

type bindingDocument struct {
	SchemaVersion string             `json:"schema_version"`
	BindingOrder  []string           `json:"binding_order"`
	Bindings      map[string]binding `json:"bindings"`
}
type binding struct {
	ToolName        string `json:"tool_name"`
	Category        string `json:"category"`
	Adapter         string `json:"adapter"`
	OperationID     string `json:"operation_id"`
	DispatcherOwner string `json:"dispatcher_owner"`
}

type operationDocument struct {
	SchemaVersion  string                         `json:"schema_version"`
	OperationOrder []string                       `json:"operation_order"`
	Operations     map[string]operationDefinition `json:"operations"`
}

type operationDefinition struct {
	OperationID              string          `json:"operation_id"`
	Role                     string          `json:"role"`
	SurfaceContract          string          `json:"surface_contract"`
	ManifestDomain           json.RawMessage `json:"manifest_domain"`
	OutputKind               string          `json:"output_kind"`
	OutputPersistence        string          `json:"output_persistence"`
	RequiredInputs           []operationSlot `json:"required_inputs"`
	OptionalInputs           []operationSlot `json:"optional_inputs"`
	ConditionalRefreshInputs []operationSlot `json:"conditional_refresh_inputs"`
	DerivedInputs            []operationSlot `json:"derived_inputs"`
	WorkflowReferenceKinds   []string        `json:"workflow_reference_kinds"`
	ComparisonAnchorPurposes []string        `json:"comparison_anchor_purposes"`
	SourcePolicy             string          `json:"source_policy"`
	HistoricalAuthority      string          `json:"historical_authority"`
	AllowedNonSourceActions  []string        `json:"allowed_non_source_actions"`
	PacketSemanticProjection string          `json:"packet_semantic_projection"`
}

type operationSlot struct {
	InputName            string   `json:"input_name"`
	InputRole            string   `json:"input_role"`
	AttestationKind      string   `json:"attestation_kind"`
	AllowedSourceKinds   []string `json:"allowed_source_kinds"`
	WorkflowRecordPolicy string   `json:"workflow_record_policy"`
}

type generatedDocument struct {
	SchemaVersion                     string                   `json:"schema_version"`
	SchemaDialect                     string                   `json:"schema_dialect"`
	AuthorityLockSHA256               string                   `json:"authority_lock_sha256"`
	OperationContractSHA256           string                   `json:"operation_contract_sha256"`
	RouteOrder                        []string                 `json:"route_order"`
	Routes                            []routeDefinition        `json:"routes"`
	ToolOrder                         []string                 `json:"tool_order"`
	Tools                             map[string]generatedTool `json:"tools"`
	RuntimeBindingSHA256              string                   `json:"runtime_binding_sha256"`
	SourceToolContractVersion         string                   `json:"source_tool_contract_version"`
	OperationFamilyToolContractSHA256 string                   `json:"operation_family_tool_contract_sha256"`
	AllToolMetadataContractSHA256     string                   `json:"all_tool_metadata_contract_sha256"`
}

type generatedTool struct {
	Name            string          `json:"name"`
	Category        string          `json:"category"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	SemanticToolID  string          `json:"semantic_tool_id"`
	OperationID     string          `json:"operation_id"`
	Invoking        string          `json:"invoking"`
	Invoked         string          `json:"invoked"`
	Annotations     annotations     `json:"annotations"`
	FileParams      []string        `json:"file_params"`
	MetadataSource  string          `json:"metadata_source"`
	SchemaOwner     string          `json:"schema_owner"`
	DispatcherOwner string          `json:"dispatcher_owner"`
	Adapter         string          `json:"adapter"`
	InputSchema     json.RawMessage `json:"input_schema"`
	OutputSchema    json.RawMessage `json:"output_schema"`
}

func main() {
	root := repositoryRoot()
	dir := filepath.Join(root, "internal", "operations", "registry")
	operationsRaw := mustRead(filepath.Join(dir, operationsSource))
	routesRaw := mustRead(filepath.Join(dir, routesSource))
	familyRaw := mustRead(filepath.Join(dir, familySource))
	metadataRaw := mustRead(filepath.Join(dir, metadataSource))
	bindingsRaw := mustRead(filepath.Join(dir, bindingsSource))
	schemasRaw := mustRead(filepath.Join(dir, schemasSource))

	var routes routeDocument
	var operations operationDocument
	var family familyDocument
	var metadata metadataDocument
	var bindings bindingDocument
	var schemas schemaDocument
	decodeStrict(operationsRaw, &operations)
	decodeStrict(routesRaw, &routes)
	decodeStrict(familyRaw, &family)
	decodeStrict(metadataRaw, &metadata)
	decodeStrict(bindingsRaw, &bindings)
	decodeStrict(schemasRaw, &schemas)

	if digest(operationsRaw) != routes.OperationContractSHA256 {
		fatalf("operation source digest differs")
	}
	if digest(familyRaw) != routes.OperationFamilyToolContractSHA256 {
		fatalf("family source digest differs")
	}
	if digest(metadataRaw) != routes.AllToolMetadataContractSHA256 {
		fatalf("metadata source digest differs")
	}

	packetToolSchemas := buildPacketToolSchemas(routes.Routes, operations)
	familyToolSchemas := buildFamilyToolSchemas(family)
	explicitToolSchemas := buildExplicitToolSchemas(schemas)
	order := orderedTools(routes.Routes)
	metadataByName := map[string]metadataTool{}
	if err := validatePublishedToolCardinality(order, metadata.Tools, bindings); err != nil {
		fatalf("published tool cardinality: %v", err)
	}
	for _, item := range metadata.Tools {
		metadataByName[item.Name] = item
	}

	publishedContract := generatedDocument{
		SchemaVersion:                     routes.SchemaVersion,
		SchemaDialect:                     routes.SchemaDialect,
		AuthorityLockSHA256:               routes.AuthorityLockSHA256,
		OperationContractSHA256:           routes.OperationContractSHA256,
		RouteOrder:                        append([]string(nil), routes.RouteOrder...),
		Routes:                            cloneRoutes(routes.Routes),
		ToolOrder:                         append([]string(nil), order...),
		Tools:                             make(map[string]generatedTool, len(order)),
		RuntimeBindingSHA256:              digest(bindingsRaw),
		SourceToolContractVersion:         routes.SourceToolContractVersion,
		OperationFamilyToolContractSHA256: routes.OperationFamilyToolContractSHA256,
		AllToolMetadataContractSHA256:     routes.AllToolMetadataContractSHA256,
	}
	for _, name := range order {
		routeTool, ok := routes.ToolContracts[name]
		if !ok {
			fatalf("route tool %q missing", name)
		}
		meta, ok := metadataByName[name]
		if !ok {
			fatalf("metadata %q missing", name)
		}
		bind, ok := bindings.Bindings[name]
		if !ok || bind.ToolName != name || bind.Category != meta.Category || bind.OperationID != meta.OperationID || bind.DispatcherOwner != meta.DispatcherOwner {
			fatalf("binding %q differs", name)
		}
		if routeTool.Category != meta.Category || routeTool.SemanticToolID != meta.SemanticToolID || routeTool.OperationID != meta.OperationID || routeTool.MetadataSource != meta.MetadataSource || routeTool.DispatcherOwner != meta.DispatcherOwner {
			fatalf("route metadata %q differs", name)
		}
		var tool generatedTool
		tool, ok = explicitToolSchemas[name]
		if !ok {
			tool, ok = packetToolSchemas[name]
		}
		if !ok {
			tool, ok = familyToolSchemas[name]
		}
		if input, output, owned := sourcecontract.Schemas(name); owned {
			tool = generatedTool{InputSchema: input, OutputSchema: output}
			ok = true
		}
		if !ok {
			fatalf("schema %q missing", name)
		}
		if input, output, owned := sourcecontract.Schemas(name); owned {
			if len(input) == 0 || len(output) == 0 {
				fatalf("source tool schema %q is empty", name)
			}
			tool.InputSchema = input
			tool.OutputSchema = output
		}
		tool.Name = name
		tool.Category = meta.Category
		tool.SemanticToolID = meta.SemanticToolID
		tool.OperationID = meta.OperationID
		tool.Annotations = meta.Annotations
		tool.FileParams = append([]string(nil), meta.FileParams...)
		tool.MetadataSource = "published_explicit"
		tool.SchemaOwner = meta.SchemaOwner
		tool.DispatcherOwner = meta.DispatcherOwner
		tool.Adapter = bind.Adapter
		publishedContract.Tools[name] = tool
	}
	writeJSON(filepath.Join(dir, operationsOutput), json.RawMessage(operationsRaw))
	writeJSON(filepath.Join(dir, publicOutput), publishedContract)
	writePins(filepath.Join(dir, pinsOutput), mustRead(filepath.Join(dir, operationsOutput)), mustRead(filepath.Join(dir, publicOutput)), bindingsRaw)
}

func buildPacketToolSchemas(routes []routeDefinition, operations operationDocument) map[string]generatedTool {
	operationIDs := make([]string, 0)
	seenOperations := map[string]struct{}{}
	for _, route := range routes {
		for _, operation := range route.Operations {
			if _, exists := seenOperations[operation]; exists {
				continue
			}
			seenOperations[operation] = struct{}{}
			operationIDs = append(operationIDs, operation)
		}
	}
	titles := map[string]string{
		"get_active_operation_packet": "Get active operation packet",
		"create_operation_packet":     "Create operation packet",
		"refresh_operation_packet":    "Refresh operation packet",
		"close_operation_packet":      "Close operation packet",
		"read_operation_input":        "Read operation input",
		"list_operation_repositories": "List operation repositories",
	}
	descriptions := map[string]string{
		"get_active_operation_packet": "Return the active immutable operation packet selected by Project, operation, and surface.",
		"create_operation_packet":     "Create one complete immutable independent operation packet for an operation allowed by this surface, acquiring any bounded direct uploaded inputs atomically. Relay publishes only a fully ready packet and recovers the same result on retry.",
		"refresh_operation_packet":    "Create one complete immutable replacement for the supplied active packet, acquire any bounded direct uploaded inputs atomically, supersede only that packet, and recover the same replacement on retry.",
		"close_operation_packet":      "Close only the supplied active packet without deleting retained packet evidence or changing any unrelated packet; retries recover the same close result.",
		"read_operation_input":        "Read bounded exact bytes and identity metadata for one input bound to the supplied active or retained operation packet.",
		"list_operation_repositories": "List packet-bound repositories, configured-branch resolutions, primary revisions, trees, and authorized named anchors with deterministic continuation.",
	}
	invoking := map[string]string{
		"get_active_operation_packet": "Reading active operation packet", "create_operation_packet": "Creating operation packet", "refresh_operation_packet": "Refreshing operation packet", "close_operation_packet": "Closing operation packet", "read_operation_input": "Reading operation input", "list_operation_repositories": "Listing operation repositories",
	}
	invoked := map[string]string{
		"get_active_operation_packet": "Active operation packet read", "create_operation_packet": "Operation packet created", "refresh_operation_packet": "Operation packet refreshed", "close_operation_packet": "Operation packet closed", "read_operation_input": "Operation input read", "list_operation_repositories": "Operation repositories listed",
	}
	semanticIDs := map[string]string{
		"get_active_operation_packet": "relay.operation-packet.active.v1", "create_operation_packet": "relay.operation-packet.create.v1", "refresh_operation_packet": "relay.operation-packet.refresh.v1", "close_operation_packet": "relay.operation-packet.close.v1", "read_operation_input": "relay.operation-packet.input.v1", "list_operation_repositories": "relay.operation-packet.repositories.v1",
	}
	result := make(map[string]generatedTool, len(titles))
	for name, title := range titles {
		input, output := explicitPacketSchemas(name, operationIDs, operations)
		result[name] = generatedTool{Name: name, Title: title, Description: descriptions[name], SemanticToolID: semanticIDs[name], Invoking: invoking[name], Invoked: invoked[name], InputSchema: input, OutputSchema: output}
	}
	return result
}

func explicitPacketSchemas(tool string, operationIDs []string, operations operationDocument) (json.RawMessage, json.RawMessage) {
	ref := func(name string) map[string]any { return map[string]any{"$ref": "#/$defs/" + name} }
	array := func(item string, max int) map[string]any {
		return map[string]any{"type": "array", "minItems": 0, "maxItems": max, "items": ref(item)}
	}
	stringProperty := func() map[string]any { return map[string]any{"type": "string"} }
	inputProperties := map[string]any{"surface_contract": stringProperty()}
	var required []string
	switch tool {
	case "get_active_operation_packet":
		inputProperties["project_id"] = ref("OpaqueID")
		inputProperties["operation_id"] = map[string]any{"type": "string", "enum": operationIDs}
		required = []string{"surface_contract", "project_id", "operation_id"}
	case "create_operation_packet":
		inputProperties["mutation_id"] = ref("MutationID")
		inputProperties["operation_id"] = map[string]any{"type": "string", "enum": operationIDs}
		inputProperties["project_id"] = ref("OpaqueID")
		inputProperties["input_files"] = array("OpenAIFileParameter", 64)
		inputProperties["inputs"] = array("InputBinding", 64)
		inputProperties["workflow_references"] = array("WorkflowReferenceRequest", 32)
		inputProperties["attestations"] = array("AttestationRequest", 128)
		inputProperties["primary_revisions"] = array("PrimaryRevisionRequest", 64)
		inputProperties["comparison_anchors"] = array("ComparisonAnchorRequest", 128)
		inputProperties["relay_specs_revision"] = ref("GitObjectID")
		required = []string{"surface_contract", "mutation_id", "operation_id", "project_id", "inputs", "workflow_references", "attestations"}
	case "refresh_operation_packet":
		inputProperties["mutation_id"] = ref("MutationID")
		inputProperties["expected_packet_id"] = ref("OpaqueID")
		inputProperties["input_files"] = array("OpenAIFileParameter", 64)
		inputProperties["inputs"] = array("InputBinding", 64)
		inputProperties["workflow_references"] = array("WorkflowReferenceRequest", 32)
		inputProperties["attestations"] = array("AttestationRequest", 128)
		inputProperties["primary_revisions"] = array("PrimaryRevisionRequest", 64)
		inputProperties["comparison_anchors"] = array("ComparisonAnchorRequest", 128)
		inputProperties["relay_specs_revision"] = ref("GitObjectID")
		required = []string{"surface_contract", "mutation_id", "expected_packet_id", "inputs", "workflow_references", "attestations"}
	case "close_operation_packet":
		inputProperties["mutation_id"] = ref("MutationID")
		inputProperties["expected_packet_id"] = ref("OpaqueID")
		required = []string{"surface_contract", "mutation_id", "expected_packet_id"}
	case "read_operation_input":
		inputProperties["packet_id"] = ref("OpaqueID")
		inputProperties["input_name"] = ref("SlotName")
		inputProperties["limit_bytes"] = ref("ByteLimit")
		inputProperties["cursor"] = ref("Cursor")
		required = []string{"surface_contract", "packet_id", "input_name", "limit_bytes"}
	case "list_operation_repositories":
		inputProperties["packet_id"] = ref("OpaqueID")
		inputProperties["limit"] = ref("RepositoryListLimit")
		inputProperties["cursor"] = ref("Cursor")
		required = []string{"surface_contract", "packet_id"}
	}
	input := objectSchema(required, inputProperties)
	if tool == "create_operation_packet" {
		input["oneOf"] = operationRequestAdmissionBranches(operations, inputProperties)
	}
	if tool == "create_operation_packet" || tool == "refresh_operation_packet" {
		input["$defs"] = map[string]any{
			"WorkflowReferenceRequest": workflowReferenceRequestSchema(),
			"InputBinding":             inputBindingSchema(),
			"AttestationRequest":       attestationRequestSchema(),
		}
		if tool == "create_operation_packet" {
			input["$defs"].(map[string]any)["OperationAdmission"] = operationAdmissionSchema(operations)
		}
	}
	var output map[string]any
	switch tool {
	case "get_active_operation_packet":
		output = objectSchema([]string{"summary", "document_media_type", "document_size_bytes", "document_bytes_base64"}, map[string]any{"summary": ref("OperationPacketSummary"), "document_media_type": map[string]any{"type": "string", "const": "application/vnd.relay.operation-packet+json;version=1"}, "document_size_bytes": ref("NonNegativeInteger"), "document_bytes_base64": ref("CanonicalBase64")})
	case "create_operation_packet":
		output = objectSchema([]string{"packet", "mutation"}, map[string]any{"packet": ref("OperationPacketView"), "mutation": mutationSchema()})
	case "refresh_operation_packet":
		output = objectSchema([]string{"prior_packet", "packet", "mutation"}, map[string]any{"prior_packet": ref("OperationPacketSummary"), "packet": ref("OperationPacketView"), "mutation": mutationSchema()})
	case "close_operation_packet":
		output = objectSchema([]string{"packet", "mutation"}, map[string]any{"packet": ref("OperationPacketSummary"), "mutation": mutationSchema()})
	case "read_operation_input":
		output = objectSchema([]string{"packet", "input"}, map[string]any{"packet": ref("OperationPacketSummary"), "input": map[string]any{}})
	case "list_operation_repositories":
		output = objectSchema([]string{"packet", "repositories"}, map[string]any{"packet": ref("OperationPacketSummary"), "repositories": map[string]any{"type": "array", "minItems": 0, "maxItems": 64, "items": ref("OperationRepositoryView")}})
	}
	output = map[string]any{"oneOf": []any{output}}
	return marshalSchema(input), marshalSchema(output)
}

func operationAdmissionSchema(document operationDocument) map[string]any {
	branches := make([]any, 0, len(document.OperationOrder))
	for _, operationID := range document.OperationOrder {
		operation, ok := document.Operations[operationID]
		if !ok {
			fatalf("operation %q missing from source authority", operationID)
		}
		callerSlots := append(append([]operationSlot{}, operation.RequiredInputs...), operation.OptionalInputs...)
		requiredAttestations := operationAttestationKinds(operation.RequiredInputs)
		properties := map[string]any{
			"operation_id":               map[string]any{"type": "string", "const": operation.OperationID},
			"caller_input_cardinality":   cardinalitySchema(len(operation.RequiredInputs), len(callerSlots)),
			"caller_inputs":              operationSlotArraySchemaWithCardinality(callerSlots, len(operation.RequiredInputs), len(callerSlots)),
			"required_inputs":            operationSlotArraySchema(operation.RequiredInputs),
			"optional_inputs":            operationSlotArraySchema(operation.OptionalInputs),
			"conditional_refresh_inputs": operationSlotArraySchema(operation.ConditionalRefreshInputs),
			"derived_inputs":             operationSlotArraySchema(operation.DerivedInputs),
			"required_attestation_kinds": stringArraySchema(requiredAttestations),
			"workflow_reference_kinds":   stringArraySchema(operation.WorkflowReferenceKinds),
			"comparison_anchor_purposes": stringArraySchema(operation.ComparisonAnchorPurposes),
			"source_policy":              map[string]any{"type": "string", "const": operation.SourcePolicy},
			"historical_authority":       map[string]any{"type": "string", "const": operation.HistoricalAuthority},
		}
		required := []string{"operation_id", "caller_input_cardinality", "caller_inputs", "required_inputs", "optional_inputs", "conditional_refresh_inputs", "derived_inputs", "required_attestation_kinds", "workflow_reference_kinds", "comparison_anchor_purposes", "source_policy", "historical_authority"}
		branches = append(branches, objectSchema(required, properties))
	}
	return map[string]any{
		"title":       "Operation admission semantics",
		"description": "Machine-readable operation-specific packet admission authority. Caller inputs and Relay-derived inputs are represented separately.",
		"oneOf":       branches,
	}
}

// operationRequestAdmissionBranches links operation_id to the operation's
// caller-input projection on the actual create request. The generic
// InputBinding union remains available on inputs, while this operation branch
// makes the published operation authority discoverable at the request surface.
func operationRequestAdmissionBranches(document operationDocument, baseProperties map[string]any) []any {
	branches := make([]any, 0, len(document.OperationOrder))
	for _, operationID := range document.OperationOrder {
		operation, ok := document.Operations[operationID]
		if !ok {
			fatalf("operation %q missing from source authority", operationID)
		}
		callerSlots := append(append([]operationSlot{}, operation.RequiredInputs...), operation.OptionalInputs...)
		properties := cloneJSONMap(baseProperties)
		properties["operation_id"] = map[string]any{"type": "string", "const": operation.OperationID}
		properties["inputs"] = operationCallerInputsSchema(callerSlots, len(operation.RequiredInputs), len(callerSlots))
		properties["workflow_references"] = operationWorkflowReferencesSchema(operation.WorkflowReferenceKinds)
		properties["attestations"] = operationAttestationsSchema(operation)
		branches = append(branches, map[string]any{
			"type":       "object",
			"required":   []string{"operation_id", "inputs"},
			"properties": properties,
		})
	}
	return branches
}

func operationCallerInputsSchema(slots []operationSlot, minimum, maximum int) map[string]any {
	branches := make([]any, 0, len(slots))
	for _, slot := range slots {
		branches = append(branches, map[string]any{
			"allOf": []any{
				map[string]any{"$ref": "#/$defs/InputBinding"},
				operationCallerSlotSchema(slot),
			},
		})
	}
	items := map[string]any{"type": "object"}
	if len(branches) == 1 {
		items = branches[0].(map[string]any)
	} else if len(branches) > 1 {
		items = map[string]any{"oneOf": branches}
	}
	return map[string]any{
		"type":     "array",
		"minItems": minimum,
		"maxItems": maximum,
		"items":    items,
	}
}

func operationCallerSlotSchema(slot operationSlot) map[string]any {
	return map[string]any{
		"type":     "object",
		"required": []string{"input_name", "source_kind"},
		"properties": map[string]any{
			"input_name":  map[string]any{"type": "string", "const": slot.InputName},
			"source_kind": map[string]any{"type": "string", "enum": append([]string(nil), slot.AllowedSourceKinds...)},
		},
	}
}

func cloneJSONMap(value map[string]any) map[string]any {
	encoded, err := json.Marshal(value)
	if err != nil {
		fatalf("clone schema: %v", err)
	}
	var clone map[string]any
	if err := json.Unmarshal(encoded, &clone); err != nil {
		fatalf("clone schema: %v", err)
	}
	return clone
}

func operationSlotArraySchema(slots []operationSlot) map[string]any {
	return operationSlotArraySchemaWithCardinality(slots, len(slots), len(slots))
}

func operationSlotArraySchemaWithCardinality(slots []operationSlot, minimum, maximum int) map[string]any {
	branches := make([]any, 0, len(slots))
	for _, slot := range slots {
		branches = append(branches, operationSlotSchema(slot))
	}
	array := map[string]any{"type": "array", "minItems": minimum, "maxItems": maximum}
	if len(branches) == 1 {
		array["items"] = branches[0]
	} else if len(branches) > 1 {
		array["items"] = map[string]any{"oneOf": branches}
	} else {
		array["items"] = map[string]any{"type": "object"}
	}
	return array
}

func operationSlotSchema(slot operationSlot) map[string]any {
	properties := map[string]any{
		"input_name":                 map[string]any{"type": "string", "const": slot.InputName},
		"input_role":                 map[string]any{"type": "string", "const": slot.InputRole},
		"attestation_kind":           map[string]any{"type": "string", "const": slot.AttestationKind},
		"required_attestation_kinds": stringArraySchema(operationSlotAttestationKinds(slot)),
		"allowed_source_kinds":       stringArraySchema(slot.AllowedSourceKinds),
		"workflow_record_policy":     map[string]any{"type": "string", "const": slot.WorkflowRecordPolicy},
	}
	return objectSchema([]string{"input_name", "input_role", "attestation_kind", "required_attestation_kinds", "allowed_source_kinds", "workflow_record_policy"}, properties)
}

func operationSlotAttestationKinds(slot operationSlot) []string {
	if len(slot.AllowedSourceKinds) == 0 {
		return []string{}
	}
	result := []string{slot.AttestationKind}
	if operationUsesExternalSource(slot.AllowedSourceKinds) {
		result = append(result, "sensitive_data_clearance")
	}
	return result
}

func operationUsesExternalSource(sourceKinds []string) bool {
	for _, sourceKind := range sourceKinds {
		switch sourceKind {
		case "uploaded_file", "relay_artifact", "inline_text", "workflow_record":
			return true
		}
	}
	return false
}

func cardinalitySchema(minimum, maximum int) map[string]any {
	return objectSchema([]string{"minimum", "maximum"}, map[string]any{
		"minimum": map[string]any{"type": "integer", "const": minimum},
		"maximum": map[string]any{"type": "integer", "const": maximum},
	})
}

func stringArraySchema(values []string) map[string]any {
	array := map[string]any{"type": "array", "minItems": len(values), "maxItems": len(values)}
	if len(values) == 0 {
		array["items"] = map[string]any{"type": "string"}
		return array
	}
	items := map[string]any{"type": "string", "enum": append([]string(nil), values...)}
	array["items"] = items
	return array
}

func operationAttestationKinds(slots []operationSlot) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(slots)+1)
	for _, slot := range slots {
		for _, kind := range operationSlotAttestationKinds(slot) {
			if kind == "" {
				continue
			}
			if _, ok := seen[kind]; !ok {
				seen[kind] = struct{}{}
				result = append(result, kind)
			}
		}
	}
	return result
}

func operationAttestationRequirementSchema(requirement attestationRequirement) map[string]any {
	return map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/$defs/AttestationRequest"},
			map[string]any{
				"type": "object",
				"properties": map[string]any{
					"kind":       map[string]any{"type": "string", "const": requirement.Kind},
					"input_name": map[string]any{"type": "string", "const": requirement.InputName},
				},
				"required": []string{"kind", "input_name"},
			},
		},
	}
}

type attestationRequirement struct {
	Kind      string
	InputName string
}

func operationAttestationRequirements(slots []operationSlot) []attestationRequirement {
	result := make([]attestationRequirement, 0, len(slots)*2)
	seen := map[string]struct{}{}
	appendRequirement := func(kind, inputName string) {
		if kind == "" || inputName == "" {
			return
		}
		key := kind + "\x00" + inputName
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		result = append(result, attestationRequirement{Kind: kind, InputName: inputName})
	}
	for _, slot := range slots {
		appendRequirement(slot.AttestationKind, slot.InputName)
		if operationUsesExternalSource(slot.AllowedSourceKinds) {
			appendRequirement("sensitive_data_clearance", slot.InputName)
		}
	}
	return result
}

func operationAttestationsSchema(operation operationDefinition) map[string]any {
	allSlots := append(append([]operationSlot{}, operation.RequiredInputs...), operation.OptionalInputs...)
	requiredSlots := operationAttestationRequirements(operation.RequiredInputs)
	allRequirements := operationAttestationRequirements(allSlots)
	branches := make([]any, 0, len(allRequirements))
	for _, requirement := range allRequirements {
		branches = append(branches, operationAttestationRequirementSchema(requirement))
	}
	items := map[string]any{"$ref": "#/$defs/AttestationRequest"}
	if len(branches) == 1 {
		items = branches[0].(map[string]any)
	} else if len(branches) > 1 {
		items = map[string]any{"oneOf": branches}
	}
	base := map[string]any{
		"type":     "array",
		"minItems": len(requiredSlots),
		"maxItems": len(allRequirements),
		"items":    items,
	}
	constraints := []any{base}
	for _, requirement := range requiredSlots {
		constraints = append(constraints, map[string]any{
			"contains":    operationAttestationRequirementSchema(requirement),
			"minContains": 1,
			"maxContains": 1,
		})
	}
	if len(constraints) == 1 {
		return base
	}
	return map[string]any{"allOf": constraints}
}

func operationWorkflowReferencesSchema(kinds []string) map[string]any {
	branches := make([]any, 0, len(kinds))
	for _, kind := range kinds {
		branches = append(branches, map[string]any{
			"allOf": []any{
				map[string]any{"$ref": "#/$defs/WorkflowReferenceRequest"},
				map[string]any{
					"type":       "object",
					"properties": map[string]any{"kind": map[string]any{"type": "string", "const": kind}},
					"required":   []string{"kind"},
				},
			},
		})
	}
	items := map[string]any{"$ref": "#/$defs/WorkflowReferenceRequest"}
	if len(branches) == 1 {
		items = branches[0].(map[string]any)
	} else if len(branches) > 1 {
		items = map[string]any{"oneOf": branches}
	}
	return map[string]any{
		"type":        "array",
		"minItems":    len(kinds),
		"maxItems":    len(kinds),
		"uniqueItems": true,
		"items":       items,
	}
}

func attestationRequestSchema() map[string]any {
	sha256 := map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}
	identifier := map[string]any{"type": "string", "minLength": 1, "maxLength": 255}
	declarationNames := []string{
		"password", "api_key_or_access_token", "refresh_token_or_session_material",
		"cookie_or_authorization_header", "private_or_ssh_key", "credential",
		"complete_secret_bearing_environment_file", "avoidable_signed_secret_bearing_url",
	}
	declarationProperties := make(map[string]any, len(declarationNames))
	for _, name := range declarationNames {
		declarationProperties[name] = map[string]any{"type": "boolean", "const": false}
	}
	clearance := objectSchema([]string{"policy_version", "subject_sha256", "declaration", "confirmed"}, map[string]any{
		"policy_version": map[string]any{"type": "string", "const": "relay.canonical-artifact-sensitive-data.v1"},
		"subject_sha256": sha256,
		"declaration":    objectSchema(declarationNames, declarationProperties),
		"confirmed":      map[string]any{"type": "boolean", "const": true},
	})
	return objectSchema([]string{"kind", "input_name"}, map[string]any{
		"kind":                      map[string]any{"type": "string", "enum": []string{"approved_artifact", "candidate_for_review", "complete_review_result", "completed_dependency_outcomes", "confirmed_intent", "derived_authority", "exact_evidence", "sensitive_data_clearance"}},
		"input_name":                identifier,
		"subject_sha256":            sha256,
		"confirmed":                 map[string]any{"type": "boolean"},
		"approved":                  map[string]any{"type": "boolean"},
		"complete_transfer":         map[string]any{"type": "boolean"},
		"selected_mode":             map[string]any{"type": "string", "enum": []string{"plan", "one_shot"}},
		"reviewed_candidate_sha256": sha256,
		"review_result":             map[string]any{"type": "string", "enum": []string{"ready_for_approval", "needs_revision"}},
		"complete":                  map[string]any{"type": "boolean"},
		"clearance":                 clearance,
	})
}

func workflowReferenceRequestSchema() map[string]any {
	identifier := func() map[string]any { return map[string]any{"type": "string", "minLength": 1, "maxLength": 255} }
	branch := func(kind string, required []string, properties map[string]any) map[string]any {
		properties["kind"] = map[string]any{"type": "string", "const": kind}
		return objectSchema(append([]string{"kind"}, required...), properties)
	}
	return map[string]any{"oneOf": []any{
		branch("feature_workspace", []string{"workspace_id"}, map[string]any{"workspace_id": identifier()}),
		branch("delivery_ticket", []string{"workspace_id", "ticket_id"}, map[string]any{"workspace_id": identifier(), "ticket_id": identifier()}),
		branch("run", []string{"run_id"}, map[string]any{"run_id": identifier()}),
		branch("audit_decision", []string{"run_id", "audit_decision_id"}, map[string]any{"run_id": identifier(), "audit_decision_id": identifier()}),
	}}
}

func inputBindingSchema() map[string]any {
	stringField := func(maximum int) map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum}
	}
	base := map[string]any{
		"input_name":      stringField(255),
		"display_name":    stringField(1024),
		"media_type":      stringField(255),
		"expected_sha256": map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"},
	}
	branch := func(kind string, source map[string]any) map[string]any {
		properties := make(map[string]any, len(base)+2)
		for name, property := range base {
			properties[name] = property
		}
		properties["source_kind"] = map[string]any{"type": "string", "const": kind}
		properties["source"] = source
		return objectSchema([]string{"input_name", "source_kind", "display_name", "media_type", "expected_sha256", "source"}, properties)
	}
	recordBranch := func(kind string, required []string, properties map[string]any) map[string]any {
		properties["kind"] = map[string]any{"type": "string", "const": kind}
		return objectSchema(append([]string{"kind"}, required...), properties)
	}
	workflowRecord := func() map[string]any {
		return map[string]any{"oneOf": []any{
			recordBranch("plan_artifact", []string{"plan_id", "artifact_id", "expected_sha256"}, map[string]any{"plan_id": stringField(255), "artifact_id": stringField(255), "expected_sha256": base["expected_sha256"]}),
			recordBranch("pass_record", []string{"plan_id", "pass_id"}, map[string]any{"plan_id": stringField(255), "pass_id": stringField(255)}),
			recordBranch("run_execution_spec", []string{"run_id", "artifact_id", "expected_sha256"}, map[string]any{"run_id": stringField(255), "artifact_id": stringField(255), "expected_sha256": base["expected_sha256"]}),
			recordBranch("audit_packet", []string{"run_id", "audit_packet_id", "expected_sha256"}, map[string]any{"run_id": stringField(255), "audit_packet_id": stringField(255), "expected_sha256": base["expected_sha256"]}),
			recordBranch("audit_decision", []string{"run_id", "audit_decision_id"}, map[string]any{"run_id": stringField(255), "audit_decision_id": stringField(255)}),
		}}
	}
	pathSelector := map[string]any{"oneOf": []any{
		objectSchema([]string{"path_bytes_base64"}, map[string]any{"path_bytes_base64": map[string]any{"type": "string", "minLength": 1, "maxLength": 10924}}),
		objectSchema([]string{"path_id"}, map[string]any{"path_id": base["expected_sha256"]}),
	}}
	committedSource := objectSchema([]string{"repository_key", "revision", "path", "expected_blob_oid"}, map[string]any{
		"repository_key":    stringField(255),
		"revision":          map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"path":              pathSelector,
		"expected_blob_oid": map[string]any{"type": "string", "pattern": "^[0-9a-f]{40,64}$"},
	})
	return map[string]any{"oneOf": []any{
		branch("uploaded_file", objectSchema([]string{"file_index"}, map[string]any{"file_index": map[string]any{"type": "integer", "minimum": 0, "maximum": 63}})),
		branch("relay_artifact", objectSchema([]string{"artifact_id"}, map[string]any{"artifact_id": stringField(255)})),
		branch("inline_text", objectSchema([]string{"text"}, map[string]any{"text": map[string]any{"type": "string", "minLength": 1, "maxLength": 262144}})),
		branch("workflow_record", objectSchema([]string{"workflow_record"}, map[string]any{"workflow_record": workflowRecord()})),
		branch("committed_source", committedSource),
	}}
}
func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}
func mutationSchema() map[string]any {
	return objectSchema([]string{"result_kind", "result_sha256", "committed_at", "replay"}, map[string]any{"result_kind": map[string]any{"type": "string", "minLength": 1, "maxLength": 255}, "result_sha256": map[string]any{"$ref": "#/$defs/SHA256"}, "committed_at": map[string]any{"$ref": "#/$defs/RFC3339"}, "replay": map[string]any{"type": "boolean"}})
}

func marshalSchema(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		fatalf("encode explicit packet schema: %v", err)
	}
	return raw
}

func buildExplicitToolSchemas(source schemaDocument) map[string]generatedTool {
	result := make(map[string]generatedTool, len(source.Tools))
	for name, tool := range source.Tools {
		if len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 {
			fatalf("explicit schema %q is empty", name)
		}
		result[name] = generatedTool{Title: tool.Title, Description: tool.Description, Invoking: tool.Invoking, Invoked: tool.Invoked, InputSchema: tool.InputSchema, OutputSchema: tool.OutputSchema}
	}
	return result
}

func buildFamilyToolSchemas(source familyDocument) map[string]generatedTool {
	toolsByName := map[string]generatedTool{}
	for _, tool := range source.Tools {
		toolsByName[tool.Name] = generatedTool{Name: tool.Name, Title: tool.Title, Description: tool.Description, SemanticToolID: tool.SemanticToolID, OperationID: tool.OperationID, Invoking: tool.Invoking, Invoked: tool.Invoked, InputSchema: buildMinimalSchema(tool.Name+" input", tool.InputRequired), OutputSchema: buildMinimalSchema(tool.Name+" output", tool.OutputRequired)}
	}
	return toolsByName
}

func buildMinimalSchema(title string, required []string) json.RawMessage {
	props := map[string]any{}
	for _, name := range required {
		props[name] = map[string]any{}
	}
	raw, err := json.Marshal(map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": title, "type": "object", "additionalProperties": false, "required": required, "properties": props})
	if err != nil {
		panic(err)
	}
	return raw
}
func orderedTools(routes []routeDefinition) []string {
	seen := map[string]struct{}{}
	orderedNames := []string{}
	for _, route := range routes {
		for _, name := range route.Tools {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			orderedNames = append(orderedNames, name)
		}
	}
	return orderedNames
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func cloneRoutes(values []routeDefinition) []routeDefinition {
	routeCopies := make([]routeDefinition, len(values))
	for i, route := range values {
		route.Operations = append([]string(nil), route.Operations...)
		route.Tools = append([]string(nil), route.Tools...)
		routeCopies[i] = route
	}
	return routeCopies
}
func decodeStrict(raw []byte, target any) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		fatalf("decode: %v", err)
	}
}
func writeJSON(path string, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		fatalf("encode %s: %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fatalf("write %s: %v", path, err)
	}
}
func writePins(path string, operations, public, bindings []byte) {
	content := fmt.Sprintf("package registry\n\nconst (\n\tpublishedOperationsSizeBytes      = %d\n\tpublishedOperationsSHA256         = %q\n\tpublishedPublicContractSizeBytes  = %d\n\tpublishedPublicContractSHA256     = %q\n\tpublishedRuntimeBindingsSizeBytes = %d\n\tpublishedRuntimeBindingsSHA256    = %q\n)\n", len(operations), digest(operations), len(public), digest(public), len(bindings), digest(bindings))
	formatted, err := format.Source([]byte(content))
	if err != nil {
		fatalf("format pins: %v", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		fatalf("write pins: %v", err)
	}
}
func repositoryRoot() string {
	value, err := os.Getwd()
	if err != nil {
		fatalf("cwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(value, "go.mod")); err == nil {
			return value
		}
		parent := filepath.Dir(value)
		if parent == value {
			fatalf("repository root not found")
		}
		value = parent
	}
}
func mustRead(path string) []byte {
	value, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	return value
}
func digest(value []byte) string        { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }
func fatalf(format string, args ...any) { panic(fmt.Sprintf(format, args...)) }
