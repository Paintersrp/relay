package registry

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed published_operations.json
var publishedOperationsJSON []byte

//go:embed published_public_contract.json
var publishedPublicContractJSON []byte

//go:embed published_runtime_bindings.source.json
var publishedRuntimeBindingsJSON []byte

type PublishedOperationDefinition struct {
	OperationID              OperationID               `json:"operation_id"`
	Role                     Role                      `json:"role"`
	SurfaceContract          SurfaceContractID         `json:"surface_contract"`
	ManifestDomain           ManifestDomain            `json:"manifest_domain"`
	OutputKind               string                    `json:"output_kind"`
	OutputPersistence        string                    `json:"output_persistence"`
	RequiredInputs           []InputSlotDefinition     `json:"required_inputs"`
	OptionalInputs           []InputSlotDefinition     `json:"optional_inputs"`
	ConditionalRefreshInputs []InputSlotDefinition     `json:"conditional_refresh_inputs"`
	DerivedInputs            []InputSlotDefinition     `json:"derived_inputs"`
	WorkflowReferenceKinds   []WorkflowReferenceKind   `json:"workflow_reference_kinds"`
	ComparisonAnchorPurposes []AnchorPurpose           `json:"comparison_anchor_purposes"`
	SourcePolicy             SourcePolicy              `json:"source_policy"`
	HistoricalAuthority      HistoricalAuthorityPolicy `json:"historical_authority"`
	AllowedNonSourceActions  []AllowedAction           `json:"allowed_non_source_actions"`
	PacketSemanticProjection string                    `json:"packet_semantic_projection"`
}

type LookupPublishedToolContractAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type LookupPublishedToolContractContract struct {
	Name            string                                 `json:"name"`
	Category        string                                 `json:"category"`
	Title           string                                 `json:"title"`
	Description     string                                 `json:"description"`
	SemanticToolID  string                                 `json:"semantic_tool_id"`
	OperationID     OperationID                            `json:"operation_id"`
	Invoking        string                                 `json:"invoking"`
	Invoked         string                                 `json:"invoked"`
	Annotations     LookupPublishedToolContractAnnotations `json:"annotations"`
	FileParams      []string                               `json:"file_params"`
	MetadataSource  string                                 `json:"metadata_source"`
	SchemaOwner     string                                 `json:"schema_owner"`
	DispatcherOwner string                                 `json:"dispatcher_owner"`
	Adapter         string                                 `json:"adapter"`
	InputSchema     json.RawMessage                        `json:"input_schema"`
	OutputSchema    json.RawMessage                        `json:"output_schema"`
}

type RouteDefinition struct {
	Path       string            `json:"path"`
	Role       Role              `json:"role"`
	Surface    SurfaceContractID `json:"surface"`
	Operations []OperationID     `json:"operations"`
	Tools      []string          `json:"tools"`
	Authority  string            `json:"authority"`
}

type publishedOperationDocument struct {
	SchemaVersion  string                                       `json:"schema_version"`
	OperationOrder []OperationID                                `json:"operation_order"`
	Operations     map[OperationID]PublishedOperationDefinition `json:"operations"`
}

type publishedPublicDocument struct {
	SchemaVersion                     string                                         `json:"schema_version"`
	SchemaDialect                     string                                         `json:"schema_dialect"`
	AuthorityLockSHA256               string                                         `json:"authority_lock_sha256"`
	OperationContractSHA256           string                                         `json:"operation_contract_sha256"`
	RouteOrder                        []string                                       `json:"route_order"`
	Routes                            []RouteDefinition                              `json:"routes"`
	ToolOrder                         []string                                       `json:"tool_order"`
	Tools                             map[string]LookupPublishedToolContractContract `json:"tools"`
	RuntimeBindingSHA256              string                                         `json:"runtime_binding_sha256"`
	SourceToolContractVersion         string                                         `json:"source_tool_contract_version"`
	OperationFamilyToolContractSHA256 string                                         `json:"operation_family_tool_contract_sha256"`
	AllToolMetadataContractSHA256     string                                         `json:"all_tool_metadata_contract_sha256"`
}

var (
	publishedOnce   sync.Once
	publishedOps    publishedOperationDocument
	publishedPublic publishedPublicDocument
	publishedErr    error
)

func ValidatePublishedContracts() error { loadPublishedContracts(); return publishedErr }

func ListPublishedOperations() ([]PublishedOperationDefinition, error) {
	loadPublishedContracts()
	if publishedErr != nil {
		return nil, publishedErr
	}
	operations := make([]PublishedOperationDefinition, 0, len(publishedOps.OperationOrder))
	for _, id := range publishedOps.OperationOrder {
		operations = append(operations, clonePublishedOperation(publishedOps.Operations[id]))
	}
	return operations, nil
}

func LookupPublishedOperation(id OperationID) (PublishedOperationDefinition, bool) {
	loadPublishedContracts()
	if publishedErr != nil {
		return PublishedOperationDefinition{}, false
	}
	value, ok := publishedOps.Operations[id]
	if !ok {
		return PublishedOperationDefinition{}, false
	}
	return clonePublishedOperation(value), true
}

func ListRouteDefinitions() ([]RouteDefinition, error) {
	loadPublishedContracts()
	if publishedErr != nil {
		return nil, publishedErr
	}
	routes := make([]RouteDefinition, len(publishedPublic.Routes))
	for i, route := range publishedPublic.Routes {
		route.Operations = append([]OperationID(nil), route.Operations...)
		route.Tools = append([]string(nil), route.Tools...)
		routes[i] = route
	}
	return routes, nil
}

func LookupPublishedToolContract(name string) (LookupPublishedToolContractContract, bool) {
	loadPublishedContracts()
	if publishedErr != nil {
		return LookupPublishedToolContractContract{}, false
	}
	value, ok := publishedPublic.Tools[name]
	if !ok {
		return LookupPublishedToolContractContract{}, false
	}
	value.FileParams = append([]string(nil), value.FileParams...)
	value.InputSchema = append(json.RawMessage(nil), value.InputSchema...)
	value.OutputSchema = append(json.RawMessage(nil), value.OutputSchema...)
	return value, true
}

func AuthorityLockSHA256() string {
	loadPublishedContracts()
	return publishedPublic.AuthorityLockSHA256
}
func SourceToolContractVersion() string {
	loadPublishedContracts()
	return publishedPublic.SourceToolContractVersion
}

func loadPublishedContracts() {
	publishedOnce.Do(func() { publishedErr = validatePublishedContractsDocument() })
}

func validatePublishedContractsDocument() error {
	values := []struct {
		name   string
		data   []byte
		size   int
		digest string
	}{
		{"operations", publishedOperationsJSON, publishedOperationsSizeBytes, publishedOperationsSHA256},
		{"public", publishedPublicContractJSON, publishedPublicContractSizeBytes, publishedPublicContractSHA256},
		{"bindings", publishedRuntimeBindingsJSON, publishedRuntimeBindingsSizeBytes, publishedRuntimeBindingsSHA256},
	}
	for _, contract := range values {
		if len(contract.data) != contract.size {
			return fmt.Errorf("MCP_%s_CONTRACT_INVALID: size differs", contract.name)
		}
		sum := sha256.Sum256(contract.data)
		if hex.EncodeToString(sum[:]) != contract.digest {
			return fmt.Errorf("MCP_%s_CONTRACT_INVALID: sha256 differs", contract.name)
		}
	}
	if err := decodeStrict(publishedOperationsJSON, &publishedOps); err != nil {
		return fmt.Errorf("MCP_OPERATION_CONTRACT_INVALID: %w", err)
	}
	if err := decodeStrict(publishedPublicContractJSON, &publishedPublic); err != nil {
		return fmt.Errorf("MCP_PUBLIC_CONTRACT_INVALID: %w", err)
	}
	if len(publishedOps.OperationOrder) != 17 || len(publishedOps.Operations) != 17 {
		return fmt.Errorf("published operation cardinality is not 17")
	}
	if len(publishedPublic.RouteOrder) != 7 || len(publishedPublic.Routes) != 7 {
		return fmt.Errorf("MCP route cardinality is not 7")
	}
	if len(publishedPublic.ToolOrder) != 40 || len(publishedPublic.Tools) != 40 {
		return fmt.Errorf("published tool cardinality is not 40")
	}
	seenOps := map[OperationID]struct{}{}
	for _, id := range publishedOps.OperationOrder {
		if _, dup := seenOps[id]; dup {
			return fmt.Errorf("duplicate operation %q", id)
		}
		seenOps[id] = struct{}{}
		op, ok := publishedOps.Operations[id]
		if !ok || op.OperationID != id || op.Role == "" || op.SurfaceContract == "" || op.PacketSemanticProjection == "" {
			return fmt.Errorf("invalid operation %q", id)
		}
	}
	seenRoutes := map[string]struct{}{}
	seenTools := map[string]struct{}{}
	for i, route := range publishedPublic.Routes {
		if route.Path != publishedPublic.RouteOrder[i] {
			return fmt.Errorf("route order differs at %d", i)
		}
		if _, dup := seenRoutes[route.Path]; dup {
			return fmt.Errorf("duplicate route %q", route.Path)
		}
		seenRoutes[route.Path] = struct{}{}
		local := map[string]struct{}{}
		for _, name := range route.Tools {
			if _, dup := local[name]; dup {
				return fmt.Errorf("route %q duplicates %q", route.Path, name)
			}
			local[name] = struct{}{}
			tool, ok := publishedPublic.Tools[name]
			if !ok || tool.Name != name || tool.Adapter == "" || tool.SemanticToolID == "" || tool.SchemaOwner == "" || tool.DispatcherOwner == "" || len(tool.InputSchema) == 0 || len(tool.OutputSchema) == 0 || !json.Valid(tool.InputSchema) || !json.Valid(tool.OutputSchema) {
				return fmt.Errorf("invalid tool %q", name)
			}
			seenTools[name] = struct{}{}
		}
	}
	if len(seenTools) != 40 {
		return fmt.Errorf("route membership does not cover 40 tools")
	}
	return nil
}

func clonePublishedOperation(operation PublishedOperationDefinition) PublishedOperationDefinition {
	operation.RequiredInputs = clonePublishedSlots(operation.RequiredInputs)
	operation.OptionalInputs = clonePublishedSlots(operation.OptionalInputs)
	operation.ConditionalRefreshInputs = clonePublishedSlots(operation.ConditionalRefreshInputs)
	operation.DerivedInputs = clonePublishedSlots(operation.DerivedInputs)
	operation.WorkflowReferenceKinds = append([]WorkflowReferenceKind(nil), operation.WorkflowReferenceKinds...)
	operation.ComparisonAnchorPurposes = append([]AnchorPurpose(nil), operation.ComparisonAnchorPurposes...)
	operation.AllowedNonSourceActions = append([]AllowedAction(nil), operation.AllowedNonSourceActions...)
	return operation
}
func clonePublishedSlots(values []InputSlotDefinition) []InputSlotDefinition {
	slotCopies := make([]InputSlotDefinition, len(values))
	for i, slot := range values {
		slot.AllowedSourceKinds = append([]InputSourceKind(nil), slot.AllowedSourceKinds...)
		slotCopies[i] = slot
	}
	return slotCopies
}
