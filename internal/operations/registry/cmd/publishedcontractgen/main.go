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
)

const (
	operationsSource = "published_operations.source.json"
	routesSource     = "published_routes.source.json"
	familySource     = "published_family_tools.source.json"
	metadataSource   = "published_tool_metadata.source.json"
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
	SchemaVersion           string          `json:"schema_version"`
	MetadataPropertyOrder   []string        `json:"metadata_property_order"`
	AnnotationPropertyOrder []string        `json:"annotation_property_order"`
	InheritanceRule         json.RawMessage `json:"inheritance_rule"`
	LegacyExactCopyTools    []string        `json:"legacy_exact_copy_tools"`
	PublishedExplicitTools  []string        `json:"published_explicit_tools"`
	Tools                   []metadataTool  `json:"tools"`
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

	var routes routeDocument
	var family familyDocument
	var metadata metadataDocument
	var bindings bindingDocument
	decodeStrict(routesRaw, &routes)
	decodeStrict(familyRaw, &family)
	decodeStrict(metadataRaw, &metadata)
	decodeStrict(bindingsRaw, &bindings)

	if digest(operationsRaw) != routes.OperationContractSHA256 {
		fatalf("operation source digest differs")
	}
	if digest(familyRaw) != routes.OperationFamilyToolContractSHA256 {
		fatalf("family source digest differs")
	}
	if digest(metadataRaw) != routes.AllToolMetadataContractSHA256 {
		fatalf("metadata source digest differs")
	}

	aggregateToolSchemas := loadAggregateToolSchemas(dir)
	packetToolSchemas := buildPacketToolSchemas(routes.Routes)
	familyToolSchemas := buildFamilyToolSchemas(family)
	publishedExplicitToolSchemas := buildPublishedExplicitToolSchemas()
	metadataByName := map[string]metadataTool{}
	for _, item := range metadata.Tools {
		metadataByName[item.Name] = item
	}
	order := orderedTools(routes.Routes)
	if len(order) != 40 || len(metadataByName) != 40 || len(bindings.Bindings) != 40 {
		fatalf("published tool cardinality differs")
	}

	publishedContract := generatedDocument{
		SchemaVersion:                     routes.SchemaVersion,
		SchemaDialect:                     routes.SchemaDialect,
		AuthorityLockSHA256:               routes.AuthorityLockSHA256,
		OperationContractSHA256:           routes.OperationContractSHA256,
		RouteOrder:                        append([]string(nil), routes.RouteOrder...),
		Routes:                            cloneRoutes(routes.Routes),
		ToolOrder:                         append([]string(nil), order...),
		Tools:                             make(map[string]generatedTool, 40),
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
		if meta.MetadataSource == "published_explicit" {
			tool, ok = packetToolSchemas[name]
			if !ok {
				tool, ok = familyToolSchemas[name]
			}
			if !ok {
				tool, ok = publishedExplicitToolSchemas[name]
			}
		} else if meta.MetadataSource == "legacy_exact_copy" {
			tool, ok = aggregateToolSchemas[name]
		} else {
			ok = false
		}
		if !ok {
			fatalf("schema %q missing", name)
		}
		tool.Name = name
		tool.Category = meta.Category
		tool.SemanticToolID = meta.SemanticToolID
		tool.OperationID = meta.OperationID
		tool.Annotations = meta.Annotations
		tool.FileParams = append([]string(nil), meta.FileParams...)
		tool.MetadataSource = meta.MetadataSource
		tool.SchemaOwner = meta.SchemaOwner
		tool.DispatcherOwner = meta.DispatcherOwner
		tool.Adapter = bind.Adapter
		publishedContract.Tools[name] = tool
	}
	writeJSON(filepath.Join(dir, operationsOutput), json.RawMessage(operationsRaw))
	writeJSON(filepath.Join(dir, publicOutput), publishedContract)
	writePins(filepath.Join(dir, pinsOutput), mustRead(filepath.Join(dir, operationsOutput)), mustRead(filepath.Join(dir, publicOutput)), bindingsRaw)
}

func buildPacketToolSchemas(routes []routeDefinition) map[string]generatedTool {
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
		input, output := explicitPacketSchemas(name, operationIDs)
		result[name] = generatedTool{Name: name, Title: title, Description: descriptions[name], SemanticToolID: semanticIDs[name], Invoking: invoking[name], Invoked: invoked[name], InputSchema: input, OutputSchema: output}
	}
	return result
}

func explicitPacketSchemas(tool string, operationIDs []string) (json.RawMessage, json.RawMessage) {
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

func loadAggregateToolSchemas(dir string) map[string]generatedTool {
	var document struct {
		Surfaces map[string]struct {
			Tools map[string]json.RawMessage `json:"tools"`
		} `json:"surfaces"`
	}
	if err := json.Unmarshal(mustRead(filepath.Join(dir, "public_contract.json")), &document); err != nil {
		fatalf("aggregate schema source: %v", err)
	}
	toolsByName := map[string]generatedTool{}
	for _, surface := range document.Surfaces {
		for name, raw := range surface.Tools {
			var tool struct {
				Name           string          `json:"name"`
				Title          string          `json:"title"`
				Description    string          `json:"description"`
				SemanticToolID string          `json:"semantic_tool_id"`
				Invoking       string          `json:"invoking"`
				Invoked        string          `json:"invoked"`
				Annotations    annotations     `json:"annotations"`
				FileParams     []string        `json:"file_params"`
				InputSchema    json.RawMessage `json:"input_root"`
				OutputSchema   json.RawMessage `json:"output_root"`
			}
			if err := json.Unmarshal(raw, &tool); err != nil {
				fatalf("aggregate schema %q: %v", name, err)
			}
			if tool.Name == "" {
				tool.Name = name
			}
			if _, exists := toolsByName[tool.Name]; exists {
				continue
			}
			toolsByName[tool.Name] = generatedTool{
				Name: tool.Name, Title: tool.Title, Description: tool.Description,
				SemanticToolID: tool.SemanticToolID, Invoking: tool.Invoking, Invoked: tool.Invoked,
				Annotations: tool.Annotations, FileParams: append([]string(nil), tool.FileParams...),
				InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...),
			}
		}
	}
	if packet, ok := toolsByName["get_operation_packet"]; ok {
		toolsByName["get_active_operation_packet"] = packet
		toolsByName["get_audit_packet"] = packet
	}
	return toolsByName
}

func buildFamilyToolSchemas(source familyDocument) map[string]generatedTool {
	toolsByName := map[string]generatedTool{}
	for _, tool := range source.Tools {
		toolsByName[tool.Name] = generatedTool{Name: tool.Name, Title: tool.Title, Description: tool.Description, SemanticToolID: tool.SemanticToolID, OperationID: tool.OperationID, Invoking: tool.Invoking, Invoked: tool.Invoked, InputSchema: buildMinimalSchema(tool.Name+" input", tool.InputRequired), OutputSchema: buildMinimalSchema(tool.Name+" output", tool.OutputRequired)}
	}
	return toolsByName
}

func buildPublishedExplicitToolSchemas() map[string]generatedTool {
	return map[string]generatedTool{
		"create_workspace":          buildExplicitTool("Create workspace", "Create one Feature Workspace for an existing Project and feature slug.", "Creating workspace", "Workspace created", []string{"project_id", "feature_slug"}, []string{"workspace"}),
		"admit_workspace_input":     buildExplicitTool("Admit workspace input", "Admit one exact input into a Feature Workspace.", "Admitting input", "Input admitted", []string{"workspace_id", "expected_version", "sequence", "name", "role", "source_kind", "source_reference", "files"}, []string{"input", "workspace"}),
		"add_workspace_destination": buildExplicitTool("Add workspace destination", "Add one destination or fog entry to a Feature Workspace.", "Adding destination", "Destination added", []string{"workspace_id", "expected_version", "sequence", "kind", "key", "repo_target"}, []string{"destination", "workspace"}),
		"route_workspace":           buildExplicitTool("Route workspace", "Record the next Feature Workspace route state.", "Routing workspace", "Workspace routed", []string{"workspace_id", "expected_version", "sequence", "state", "ticket_id"}, []string{"route", "workspace"}),
		"create_discovery_ticket":   buildExplicitTool("Create discovery ticket", "Create one Wayfinder discovery ticket and exact dependencies.", "Creating discovery ticket", "Discovery ticket created", []string{"workspace_id", "expected_version", "ticket_key", "subject", "depends_on_ticket_ids", "dependency_kind"}, []string{"ticket", "workspace"}),
		"resolve_discovery_ticket":  buildExplicitTool("Resolve discovery ticket", "Resolve, reject, or defer one exact discovery ticket.", "Resolving discovery ticket", "Discovery ticket resolved", []string{"workspace_id", "expected_version", "ticket_id", "expected_ticket_version", "resolution_sequence", "resolution_kind", "artifact_sha256"}, []string{"resolution", "ticket", "workspace"}),
		"attach_investigation":      buildExplicitTool("Attach investigation", "Attach exact source, artifact, or dependency investigation evidence.", "Attaching investigation", "Investigation attached", []string{"workspace_id", "expected_version", "ticket_id", "sequence", "kind", "artifact_sha256"}, []string{"investigation", "workspace"}),
		"read_ticket_frontier":      buildExplicitTool("Read ticket frontier", "Read one current Delivery Ticket revision and readiness.", "Reading ticket frontier", "Ticket frontier ready", []string{"packet_id", "ticket_id"}, []string{"ticket", "revision", "members", "dependencies", "approvals", "readiness"}),
		"get_audit_effects":         buildExplicitTool("Get audit effects", "Read persisted ticket effects for one recorded decision.", "Reading audit effects", "Audit effects ready", []string{"packet_id", "audit_decision_id"}, []string{"audit_decision", "ticket_revision_decisions", "ticket_satisfactions", "remediation_seeds"}),
		"get_remediation_seed":      buildExplicitTool("Get remediation seed", "Read one exact remediation seed and findings.", "Reading remediation seed", "Remediation seed ready", []string{"packet_id", "remediation_seed_id"}, []string{"remediation_seed", "material_findings"}),
	}
}

func buildExplicitTool(title, description, invoking, invoked string, input, output []string) generatedTool {
	return generatedTool{Title: title, Description: description, Invoking: invoking, Invoked: invoked, InputSchema: buildMinimalSchema(title+" input", input), OutputSchema: buildMinimalSchema(title+" output", output)}
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
