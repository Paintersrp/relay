package routecontracts

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"relay/internal/operations/registry"
)

//go:embed relay_specs_authority_lock.json
var authorityLockJSON []byte

type StandingAuthorityIdentity struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Path       string `json:"path"`
	BlobOID    string `json:"blob_oid"`
}
type DomainMemberIdentity struct {
	Path    string `json:"path"`
	BlobOID string `json:"blob_oid"`
}
type DomainAuthorityIdentity struct {
	ManifestPath    string                 `json:"manifest_path"`
	ManifestBlobOID string                 `json:"manifest_blob_oid"`
	Domain          string                 `json:"domain"`
	Members         []DomainMemberIdentity `json:"members"`
}
type ToolAnnotations struct {
	ReadOnlyHint    bool `json:"readOnlyHint"`
	DestructiveHint bool `json:"destructiveHint"`
	IdempotentHint  bool `json:"idempotentHint"`
	OpenWorldHint   bool `json:"openWorldHint"`
}

type OperationManifest struct {
	OperationID              string   `json:"operation_id"`
	DefinitionSHA256         string   `json:"definition_sha256"`
	ManifestDomain           string   `json:"manifest_domain"`
	SourcePolicy             string   `json:"source_policy"`
	HistoricalAuthority      string   `json:"historical_authority"`
	PacketSemanticProjection string   `json:"packet_semantic_projection"`
	AllowedNonSourceActions  []string `json:"allowed_non_source_actions"`
}

type ToolManifest struct {
	Name               string          `json:"name"`
	Category           string          `json:"category"`
	SemanticToolID     string          `json:"semantic_tool_id"`
	OperationID        string          `json:"operation_id"`
	InputSchemaSHA256  string          `json:"input_schema_sha256"`
	OutputSchemaSHA256 string          `json:"output_schema_sha256"`
	Annotations        ToolAnnotations `json:"annotations"`
	FileParams         []string        `json:"file_params"`
	SchemaOwner        string          `json:"schema_owner"`
	DispatcherOwner    string          `json:"dispatcher_owner"`
	Adapter            string          `json:"adapter"`
	Title              string          `json:"-"`
	Description        string          `json:"-"`
	Invoking           string          `json:"-"`
	Invoked            string          `json:"-"`
	InputSchema        json.RawMessage `json:"-"`
	OutputSchema       json.RawMessage `json:"-"`
}

type RouteManifest struct {
	SchemaVersion             string                    `json:"schema_version"`
	RoutePath                 string                    `json:"route_path"`
	Role                      string                    `json:"role"`
	SurfaceContract           string                    `json:"surface_contract"`
	StandingAuthority         StandingAuthorityIdentity `json:"standing_authority"`
	DomainAuthority           []DomainAuthorityIdentity `json:"domain_authority"`
	Operations                []OperationManifest       `json:"operations"`
	Tools                     []ToolManifest            `json:"tools"`
	SourceToolContractVersion string                    `json:"source_tool_contract_version"`
	ManifestBasis             []byte                    `json:"-"`
	ManifestBasisSizeBytes    int                       `json:"manifest_basis_size_bytes"`
	ManifestSHA256            string                    `json:"manifest_sha256"`
}
type RouteSet struct{ Manifests []RouteManifest }

type authorityLock struct {
	SchemaVersion        string `json:"schema_version"`
	Repository           string `json:"repository"`
	Commit               string `json:"commit"`
	StandingInstructions map[string]struct {
		Path    string `json:"path"`
		BlobOID string `json:"blob_oid"`
	} `json:"standing_instructions"`
	Manifests map[string]struct {
		Path            string `json:"path"`
		BlobOID         string `json:"blob_oid"`
		ManifestVersion string `json:"manifest_version"`
		Domains         []struct {
			Name    string                 `json:"name"`
			Members []DomainMemberIdentity `json:"members"`
		} `json:"domains"`
	} `json:"manifests"`
}

func BuildMCPRouteManifests() (RouteSet, error) {
	if err := registry.ValidatePublishedContracts(); err != nil {
		return RouteSet{}, err
	}
	var lock authorityLock
	if err := decodeStrict(authorityLockJSON, &lock); err != nil {
		return RouteSet{}, fmt.Errorf("MCP_AUTHORITY_LOCK_INVALID: %w", err)
	}
	sum := sha256.Sum256(authorityLockJSON)
	if hex.EncodeToString(sum[:]) != registry.AuthorityLockSHA256() {
		return RouteSet{}, fmt.Errorf("MCP_AUTHORITY_LOCK_INVALID: sha256 differs")
	}
	routes, err := registry.ListRouteDefinitions()
	if err != nil {
		return RouteSet{}, err
	}
	routeSet := RouteSet{Manifests: make([]RouteManifest, 0, len(routes))}
	for _, route := range routes {
		manifest, err := buildManifest(lock, route)
		if err != nil {
			return RouteSet{}, err
		}
		routeSet.Manifests = append(routeSet.Manifests, manifest)
	}
	if len(routeSet.Manifests) != 7 {
		return RouteSet{}, fmt.Errorf("MCP_ROUTE_SET_INCOMPLETE")
	}
	return cloneRouteSet(routeSet), nil
}

func buildManifest(lock authorityLock, route registry.RouteDefinition) (RouteManifest, error) {
	standing, ok := lock.StandingInstructions[string(route.Role)]
	if !ok {
		return RouteManifest{}, fmt.Errorf("MCP_AUTHORITY_LOCK_INVALID: role %q", route.Role)
	}
	manifest := RouteManifest{SchemaVersion: "relay.mcp.route-manifest.v1", RoutePath: route.Path, Role: string(route.Role), SurfaceContract: string(route.Surface), StandingAuthority: StandingAuthorityIdentity{Repository: lock.Repository, Commit: lock.Commit, Path: standing.Path, BlobOID: standing.BlobOID}, DomainAuthority: []DomainAuthorityIdentity{}, Operations: []OperationManifest{}, Tools: []ToolManifest{}, SourceToolContractVersion: registry.SourceToolContractVersion()}
	for _, id := range route.Operations {
		op, ok := registry.LookupPublishedOperation(id)
		if !ok {
			return RouteManifest{}, fmt.Errorf("MCP_OPERATION_IDENTITY_MISMATCH: %q", id)
		}
		raw, err := json.Marshal(op)
		if err != nil {
			return RouteManifest{}, err
		}
		sum := sha256.Sum256(raw)
		actions := make([]string, len(op.AllowedNonSourceActions))
		for i, a := range op.AllowedNonSourceActions {
			actions[i] = string(a)
		}
		manifest.Operations = append(manifest.Operations, OperationManifest{OperationID: string(op.OperationID), DefinitionSHA256: hex.EncodeToString(sum[:]), ManifestDomain: string(op.ManifestDomain), SourcePolicy: string(op.SourcePolicy), HistoricalAuthority: string(op.HistoricalAuthority), PacketSemanticProjection: op.PacketSemanticProjection, AllowedNonSourceActions: actions})
		if op.ManifestDomain != "" {
			domain, err := resolveDomain(lock, route.Role, string(op.ManifestDomain))
			if err != nil {
				return RouteManifest{}, err
			}
			manifest.DomainAuthority = appendDomainOnce(manifest.DomainAuthority, domain)
		}
	}
	for _, name := range route.Tools {
		tool, ok := registry.LookupPublishedToolContract(name)
		if !ok {
			return RouteManifest{}, fmt.Errorf("MCP_TOOL_CONTRACT_INVALID: %q", name)
		}
		in := sha256.Sum256(tool.InputSchema)
		outputSchemaDigest := sha256.Sum256(tool.OutputSchema)
		manifest.Tools = append(manifest.Tools, ToolManifest{Name: tool.Name, Category: tool.Category, SemanticToolID: tool.SemanticToolID, OperationID: string(tool.OperationID), InputSchemaSHA256: hex.EncodeToString(in[:]), OutputSchemaSHA256: hex.EncodeToString(outputSchemaDigest[:]), Annotations: ToolAnnotations{tool.Annotations.ReadOnlyHint, tool.Annotations.DestructiveHint, tool.Annotations.IdempotentHint, tool.Annotations.OpenWorldHint}, FileParams: append([]string(nil), tool.FileParams...), SchemaOwner: tool.SchemaOwner, DispatcherOwner: tool.DispatcherOwner, Adapter: tool.Adapter, Title: tool.Title, Description: tool.Description, Invoking: tool.Invoking, Invoked: tool.Invoked, InputSchema: append(json.RawMessage(nil), tool.InputSchema...), OutputSchema: append(json.RawMessage(nil), tool.OutputSchema...)})
	}
	basis, err := encodeBasis(manifest)
	if err != nil {
		return RouteManifest{}, fmt.Errorf("MCP_ROUTE_MANIFEST_INVALID: %w", err)
	}
	digest := sha256.Sum256(basis)
	manifest.ManifestBasis = basis
	manifest.ManifestBasisSizeBytes = len(basis)
	manifest.ManifestSHA256 = hex.EncodeToString(digest[:])
	return manifest, nil
}

func encodeBasis(value RouteManifest) ([]byte, error) {
	type basis struct {
		SchemaVersion             string                    `json:"schema_version"`
		RoutePath                 string                    `json:"route_path"`
		Role                      string                    `json:"role"`
		SurfaceContract           string                    `json:"surface_contract"`
		StandingAuthority         StandingAuthorityIdentity `json:"standing_authority"`
		DomainAuthority           []DomainAuthorityIdentity `json:"domain_authority"`
		Operations                []OperationManifest       `json:"operations"`
		Tools                     []ToolManifest            `json:"tools"`
		SourceToolContractVersion string                    `json:"source_tool_contract_version"`
	}
	return json.Marshal(basis{value.SchemaVersion, value.RoutePath, value.Role, value.SurfaceContract, value.StandingAuthority, value.DomainAuthority, value.Operations, value.Tools, value.SourceToolContractVersion})
}
func resolveDomain(lock authorityLock, role registry.Role, name string) (DomainAuthorityIdentity, error) {
	manifestName := "planner"
	if role == "auditor" {
		manifestName = "auditor"
	}
	manifest, ok := lock.Manifests[manifestName]
	if !ok {
		return DomainAuthorityIdentity{}, fmt.Errorf("MCP_DOMAIN_IDENTITY_MISMATCH: %s", manifestName)
	}
	for _, domain := range manifest.Domains {
		if domain.Name == name {
			return DomainAuthorityIdentity{ManifestPath: manifest.Path, ManifestBlobOID: manifest.BlobOID, Domain: domain.Name, Members: append([]DomainMemberIdentity(nil), domain.Members...)}, nil
		}
	}
	return DomainAuthorityIdentity{}, fmt.Errorf("MCP_DOMAIN_IDENTITY_MISMATCH: %s/%s", manifestName, name)
}
func appendDomainOnce(values []DomainAuthorityIdentity, value DomainAuthorityIdentity) []DomainAuthorityIdentity {
	for _, existing := range values {
		if existing.ManifestPath == value.ManifestPath && existing.Domain == value.Domain {
			return values
		}
	}
	return append(values, value)
}
func decodeStrict(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}
func cloneRouteSet(value RouteSet) RouteSet {
	routeSetCopy := RouteSet{Manifests: make([]RouteManifest, len(value.Manifests))}
	copy(routeSetCopy.Manifests, value.Manifests)
	for i := range routeSetCopy.Manifests {
		routeSetCopy.Manifests[i].ManifestBasis = append([]byte(nil), routeSetCopy.Manifests[i].ManifestBasis...)
		routeSetCopy.Manifests[i].DomainAuthority = append([]DomainAuthorityIdentity(nil), routeSetCopy.Manifests[i].DomainAuthority...)
		routeSetCopy.Manifests[i].Operations = append([]OperationManifest(nil), routeSetCopy.Manifests[i].Operations...)
		routeSetCopy.Manifests[i].Tools = append([]ToolManifest(nil), routeSetCopy.Manifests[i].Tools...)
	}
	return routeSetCopy
}
