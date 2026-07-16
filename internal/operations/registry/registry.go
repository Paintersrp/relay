package registry

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
)

const (
	PublicContractVersion = "relay.mcp.public-contract.v1"
	PublicContractBytes   = 301441
	PublicContractSHA256  = "662b4055b5ae188c52bd8d5114af84cfda9aa0d7e5621586217b3dc38a8c42a4"

	RegistryVersion         = "relay.operation-registry.v1"
	OperationRegistryBytes  = 24114
	OperationRegistrySHA256 = "3f9d3c0ab4814c40c3f33f2f868914a8e37d20da0af3eb2bd38acda23252e57e"
)

type Role string
type SurfaceContractID string
type OperationID string
type ManifestDomain string
type InputRole string
type InputSourceKind string
type AttestationKind string
type WorkflowReferenceKind string
type AnchorPurpose string
type AllowedAction string
type SourcePolicy string
type HistoricalAuthorityPolicy string

type InputSlotDefinition struct {
	InputName            string            `json:"input_name"`
	InputRole            InputRole         `json:"input_role"`
	AttestationKind      AttestationKind   `json:"attestation_kind"`
	AllowedSourceKinds   []InputSourceKind `json:"allowed_source_kinds"`
	WorkflowRecordPolicy string            `json:"workflow_record_policy"`
	SelectedMode         string            `json:"selected_mode,omitempty"`
}

type OperationDefinition struct {
	OperationID              OperationID               `json:"operation_id"`
	Role                     Role                      `json:"role"`
	SurfaceContract          SurfaceContractID         `json:"surface_contract"`
	ManifestDomain           ManifestDomain            `json:"manifest_domain"`
	OutputKind               string                    `json:"output_kind"`
	OutputPersistence        string                    `json:"output_persistence"`
	RequiredInputs           []InputSlotDefinition     `json:"required_inputs"`
	ConditionalRefreshInputs []InputSlotDefinition     `json:"conditional_refresh_inputs"`
	DerivedInputs            []InputSlotDefinition     `json:"derived_inputs"`
	WorkflowReferenceKinds   []WorkflowReferenceKind   `json:"workflow_reference_kinds"`
	ComparisonAnchorPurposes []AnchorPurpose           `json:"comparison_anchor_purposes"`
	SourcePolicy             SourcePolicy              `json:"source_policy"`
	HistoricalAuthority      HistoricalAuthorityPolicy `json:"historical_authority"`
	AllowedNonSourceActions  []AllowedAction           `json:"allowed_non_source_actions"`
	PacketSemanticProjection string                    `json:"packet_semantic_projection"`
}

type registryDocument struct {
	RegistryVersion            string                              `json:"registry_version"`
	SurfaceManifestSHA256      map[SurfaceContractID]string        `json:"-"`
	OperationOrder             []OperationID                       `json:"operation_order"`
	Operations                 map[OperationID]OperationDefinition `json:"operations"`
	WorkflowReferenceRank      []WorkflowReferenceKind             `json:"workflow_reference_rank"`
	AttestationRank            []AttestationKind                   `json:"attestation_rank"`
	TransportExcludedKeys      []string                            `json:"transport_excluded_keys"`
	PacketOptionalEmptyArrays  []string                            `json:"packet_optional_empty_arrays"`
	SemanticProjectionVersions map[string]string                   `json:"semantic_projection_versions"`
}

type publicContractEnvelope struct {
	CatalogVersion string `json:"catalog_version"`
	Definitions    map[string]struct {
		Enum []string `json:"enum"`
	} `json:"definitions"`
	Surfaces map[string]struct {
		Role           string   `json:"role"`
		Operations     []string `json:"operations"`
		ManifestSHA256 string   `json:"manifest_sha256"`
	} `json:"surfaces"`
}

//go:embed public_contract.json
var publicContractJSON []byte

//go:embed operations.json
var operationsJSON []byte

var (
	loadOnce sync.Once
	loaded   registryDocument
	loadErr  error
)

func RawPublicContract() []byte {
	return append([]byte(nil), publicContractJSON...)
}

func RawRegistryDocument() []byte {
	return append([]byte(nil), operationsJSON...)
}

func Validate() error {
	load()
	return loadErr
}

func All() ([]OperationDefinition, error) {
	load()
	if loadErr != nil {
		return nil, loadErr
	}
	out := make([]OperationDefinition, 0, len(loaded.OperationOrder))
	for _, id := range loaded.OperationOrder {
		out = append(out, cloneOperation(loaded.Operations[id]))
	}
	return out, nil
}

func Lookup(id OperationID) (OperationDefinition, bool) {
	load()
	if loadErr != nil {
		return OperationDefinition{}, false
	}
	value, ok := loaded.Operations[id]
	if !ok {
		return OperationDefinition{}, false
	}
	return cloneOperation(value), true
}

func OperationsForSurface(surface SurfaceContractID) ([]OperationDefinition, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	out := make([]OperationDefinition, 0)
	for _, operation := range all {
		if operation.SurfaceContract == surface {
			out = append(out, operation)
		}
	}
	return out, nil
}

func WorkflowReferenceRank() []WorkflowReferenceKind {
	load()
	if loadErr != nil {
		return nil
	}
	return append([]WorkflowReferenceKind(nil), loaded.WorkflowReferenceRank...)
}

func AttestationRank() []AttestationKind {
	load()
	if loadErr != nil {
		return nil
	}
	return append([]AttestationKind(nil), loaded.AttestationRank...)
}

func TransportExcludedKeys() []string {
	load()
	if loadErr != nil {
		return nil
	}
	return append([]string(nil), loaded.TransportExcludedKeys...)
}

func PacketOptionalEmptyArrays() []string {
	load()
	if loadErr != nil {
		return nil
	}
	return append([]string(nil), loaded.PacketOptionalEmptyArrays...)
}

func SurfaceManifestSHA256(surface SurfaceContractID) (string, bool) {
	load()
	if loadErr != nil {
		return "", false
	}
	value, ok := loaded.SurfaceManifestSHA256[surface]
	return value, ok
}

func SemanticProjectionVersion(tool string) (string, bool) {
	load()
	if loadErr != nil {
		return "", false
	}
	value, ok := loaded.SemanticProjectionVersions[tool]
	return value, ok
}

func load() {
	loadOnce.Do(func() {
		loadErr = loadRegistry()
	})
}

func loadRegistry() error {
	document, err := validateRegistryBytes(publicContractJSON, operationsJSON)
	if err != nil {
		return err
	}
	loaded = document
	return nil
}

func validateRegistryBytes(publicRaw, registryRaw []byte) (registryDocument, error) {
	if len(publicRaw) != PublicContractBytes {
		return registryDocument{}, fmt.Errorf("public contract byte length %d does not equal %d", len(publicRaw), PublicContractBytes)
	}
	publicSum := sha256.Sum256(publicRaw)
	if got := hex.EncodeToString(publicSum[:]); got != PublicContractSHA256 {
		return registryDocument{}, fmt.Errorf("public contract sha256 %s does not equal %s", got, PublicContractSHA256)
	}
	if len(registryRaw) != OperationRegistryBytes {
		return registryDocument{}, fmt.Errorf("operation registry byte length %d does not equal %d", len(registryRaw), OperationRegistryBytes)
	}
	registrySum := sha256.Sum256(registryRaw)
	if got := hex.EncodeToString(registrySum[:]); got != OperationRegistrySHA256 {
		return registryDocument{}, fmt.Errorf("operation registry sha256 %s does not equal %s", got, OperationRegistrySHA256)
	}

	var public publicContractEnvelope
	if err := json.Unmarshal(publicRaw, &public); err != nil {
		return registryDocument{}, fmt.Errorf("decode public contract: %w", err)
	}
	if public.CatalogVersion != PublicContractVersion {
		return registryDocument{}, fmt.Errorf("public contract version %q does not equal %q", public.CatalogVersion, PublicContractVersion)
	}

	var document registryDocument
	if err := decodeStrict(registryRaw, &document); err != nil {
		return registryDocument{}, fmt.Errorf("decode operation registry: %w", err)
	}
	if document.RegistryVersion != RegistryVersion {
		return registryDocument{}, fmt.Errorf("operation registry version %q does not equal %q", document.RegistryVersion, RegistryVersion)
	}
	if len(document.OperationOrder) != len(document.Operations) {
		return registryDocument{}, errors.New("operation registry order and map cardinality differ")
	}

	operationEnum, ok := public.Definitions["OperationID"]
	if !ok {
		return registryDocument{}, errors.New("public contract is missing OperationID")
	}
	if len(operationEnum.Enum) != len(document.OperationOrder) {
		return registryDocument{}, errors.New("public contract and operation registry cardinality differ")
	}

	document.SurfaceManifestSHA256 = make(map[SurfaceContractID]string, len(public.Surfaces))
	publicSurfaceByOperation := make(map[string]string, len(document.OperationOrder))
	publicRoleByOperation := make(map[string]string, len(document.OperationOrder))
	for surfaceID, surface := range public.Surfaces {
		if surface.Role != "planner" && surface.Role != "auditor" {
			return registryDocument{}, fmt.Errorf("surface %q has invalid role %q", surfaceID, surface.Role)
		}
		if len(surface.ManifestSHA256) != 64 {
			return registryDocument{}, fmt.Errorf("surface %q manifest sha256 is invalid", surfaceID)
		}
		document.SurfaceManifestSHA256[SurfaceContractID(surfaceID)] = surface.ManifestSHA256
		for _, operationID := range surface.Operations {
			if _, exists := publicSurfaceByOperation[operationID]; exists {
				return registryDocument{}, fmt.Errorf("operation %q belongs to more than one surface", operationID)
			}
			publicSurfaceByOperation[operationID] = surfaceID
			publicRoleByOperation[operationID] = surface.Role
		}
	}

	seen := make(map[OperationID]struct{}, len(document.OperationOrder))
	for index, operationID := range document.OperationOrder {
		if _, exists := seen[operationID]; exists {
			return registryDocument{}, fmt.Errorf("operation %q is duplicated in operation_order", operationID)
		}
		seen[operationID] = struct{}{}
		if index >= len(operationEnum.Enum) || operationEnum.Enum[index] != string(operationID) {
			return registryDocument{}, fmt.Errorf("operation %q does not match public contract order", operationID)
		}
		operation, exists := document.Operations[operationID]
		if !exists {
			return registryDocument{}, fmt.Errorf("operation %q is missing", operationID)
		}
		if operation.OperationID != operationID {
			return registryDocument{}, fmt.Errorf("operation map key %q does not equal embedded id %q", operationID, operation.OperationID)
		}
		if string(operation.SurfaceContract) != publicSurfaceByOperation[string(operationID)] {
			return registryDocument{}, fmt.Errorf("operation %q surface %q does not equal public contract surface %q", operationID, operation.SurfaceContract, publicSurfaceByOperation[string(operationID)])
		}
		if string(operation.Role) != publicRoleByOperation[string(operationID)] {
			return registryDocument{}, fmt.Errorf("operation %q role %q does not equal public contract role %q", operationID, operation.Role, publicRoleByOperation[string(operationID)])
		}
		if err := validateOperation(operation); err != nil {
			return registryDocument{}, err
		}
	}

	if err := validateUniqueStrings("workflow reference rank", toStrings(document.WorkflowReferenceRank)); err != nil {
		return registryDocument{}, err
	}
	if err := validateUniqueStrings("attestation rank", toStrings(document.AttestationRank)); err != nil {
		return registryDocument{}, err
	}
	if err := validateUniqueStrings("transport excluded keys", document.TransportExcludedKeys); err != nil {
		return registryDocument{}, err
	}
	if err := validateUniqueStrings("optional empty arrays", document.PacketOptionalEmptyArrays); err != nil {
		return registryDocument{}, err
	}

	requiredProjectionTools := []string{
		"create_operation_packet",
		"refresh_operation_packet",
		"close_operation_packet",
		"validate_artifact",
		"submit_plan",
		"create_run",
		"record_audit_decision",
	}
	if len(document.SemanticProjectionVersions) != len(requiredProjectionTools) {
		return registryDocument{}, errors.New("semantic projection registry has unexpected cardinality")
	}
	for _, tool := range requiredProjectionTools {
		if document.SemanticProjectionVersions[tool] == "" {
			return registryDocument{}, fmt.Errorf("semantic projection version for %q is missing", tool)
		}
	}
	return document, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("document contains trailing JSON")
	}
	return nil
}

func validateOperation(operation OperationDefinition) error {
	if operation.OperationID == "" || operation.Role == "" || operation.SurfaceContract == "" || operation.ManifestDomain == "" {
		return fmt.Errorf("operation %q has an empty authority field", operation.OperationID)
	}
	if operation.Role != "planner" && operation.Role != "auditor" {
		return fmt.Errorf("operation %q has invalid role %q", operation.OperationID, operation.Role)
	}
	if operation.OutputKind == "" || operation.OutputPersistence == "" || operation.SourcePolicy == "" || operation.HistoricalAuthority == "" || operation.PacketSemanticProjection == "" {
		return fmt.Errorf("operation %q has incomplete policy", operation.OperationID)
	}
	if operation.Role != "planner" && len(operation.ConditionalRefreshInputs) != 0 {
		return fmt.Errorf("non-Planner operation %q has conditional Planner refresh inputs", operation.OperationID)
	}
	if err := validateSlots(operation.OperationID, "required", operation.RequiredInputs); err != nil {
		return err
	}
	if err := validateSlots(operation.OperationID, "conditional", operation.ConditionalRefreshInputs); err != nil {
		return err
	}
	if err := validateSlots(operation.OperationID, "derived", operation.DerivedInputs); err != nil {
		return err
	}
	if err := validateUniqueStrings("workflow references for "+string(operation.OperationID), toStrings(operation.WorkflowReferenceKinds)); err != nil {
		return err
	}
	if err := validateUniqueStrings("comparison anchors for "+string(operation.OperationID), toStrings(operation.ComparisonAnchorPurposes)); err != nil {
		return err
	}
	if err := validateUniqueStrings("actions for "+string(operation.OperationID), toStrings(operation.AllowedNonSourceActions)); err != nil {
		return err
	}
	if operation.OperationID == "auditor.audit" {
		if len(operation.RequiredInputs) != 0 || len(operation.DerivedInputs) != 6 {
			return errors.New("auditor.audit must have no caller inputs and six derived inputs")
		}
	}
	return nil
}

func validateSlots(operationID OperationID, label string, slots []InputSlotDefinition) error {
	seen := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		if slot.InputName == "" || slot.InputRole == "" || slot.AttestationKind == "" || slot.WorkflowRecordPolicy == "" {
			return fmt.Errorf("operation %q has incomplete %s input slot", operationID, label)
		}
		if _, exists := seen[slot.InputName]; exists {
			return fmt.Errorf("operation %q duplicates %s input slot %q", operationID, label, slot.InputName)
		}
		seen[slot.InputName] = struct{}{}
		if err := validateUniqueStrings("source kinds for "+string(operationID)+"."+slot.InputName, toStrings(slot.AllowedSourceKinds)); err != nil {
			return err
		}
	}
	return nil
}

func validateUniqueStrings(label string, values []string) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			return fmt.Errorf("%s contains an empty value", label)
		}
		if _, exists := seen[value]; exists {
			return fmt.Errorf("%s duplicates %q", label, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}

func cloneOperation(value OperationDefinition) OperationDefinition {
	value.RequiredInputs = cloneSlots(value.RequiredInputs)
	value.ConditionalRefreshInputs = cloneSlots(value.ConditionalRefreshInputs)
	value.DerivedInputs = cloneSlots(value.DerivedInputs)
	value.WorkflowReferenceKinds = append([]WorkflowReferenceKind(nil), value.WorkflowReferenceKinds...)
	value.ComparisonAnchorPurposes = append([]AnchorPurpose(nil), value.ComparisonAnchorPurposes...)
	value.AllowedNonSourceActions = append([]AllowedAction(nil), value.AllowedNonSourceActions...)
	return value
}

func cloneSlots(values []InputSlotDefinition) []InputSlotDefinition {
	out := make([]InputSlotDefinition, len(values))
	for index, value := range values {
		value.AllowedSourceKinds = append([]InputSourceKind(nil), value.AllowedSourceKinds...)
		out[index] = value
	}
	return out
}

func toStrings[T ~string](values []T) []string {
	out := make([]string, len(values))
	for index, value := range values {
		out[index] = string(value)
	}
	return out
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
