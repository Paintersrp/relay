package mcp

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"

	appwayfinder "relay/internal/app/wayfinder"
)

// Wayfinder wire requests are the explicit public MCP boundary. Application
// commands remain transport-independent and retain their database-native
// optional values; these requests map published snake-case JSON to them.
type createWorkspaceWireInput struct {
	ProjectID   string `json:"project_id"`
	FeatureSlug string `json:"feature_slug"`
}

func (in createWorkspaceWireInput) application() appwayfinder.CreateWorkspaceInput {
	return appwayfinder.CreateWorkspaceInput{ProjectID: in.ProjectID, FeatureSlug: in.FeatureSlug}
}

type readWorkspaceWireInput struct {
	WorkspaceID string `json:"workspace_id"`
}

type admitWorkspaceInputWireInput struct {
	WorkspaceID     string        `json:"workspace_id"`
	ExpectedVersion int64         `json:"expected_version"`
	Sequence        int64         `json:"sequence"`
	Name            string        `json:"name"`
	Role            string        `json:"role"`
	SourceKind      string        `json:"source_kind"`
	SourceReference string        `json:"source_reference"`
	ArtifactRowID   optionalInt64 `json:"artifact_row_id,omitempty"`
	ArtifactSHA256  string        `json:"artifact_sha256,omitempty"`
	SourceClosureID optionalInt64 `json:"source_closure_id,omitempty"`
}

func (in admitWorkspaceInputWireInput) application() appwayfinder.AdmitInputInput {
	return appwayfinder.AdmitInputInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, Sequence: in.Sequence,
		Name: in.Name, Role: in.Role, SourceKind: in.SourceKind, SourceReference: in.SourceReference,
		ArtifactRowID: nullableInt64(in.ArtifactRowID), ArtifactSHA256: nullableString(in.ArtifactSHA256),
		SourceClosureID: nullableInt64(in.SourceClosureID),
	}
}

type addWorkspaceDestinationWireInput struct {
	WorkspaceID     string `json:"workspace_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Sequence        int64  `json:"sequence"`
	Kind            string `json:"kind"`
	Key             string `json:"key"`
}

func (in addWorkspaceDestinationWireInput) application() appwayfinder.AddDestinationInput {
	return appwayfinder.AddDestinationInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, Sequence: in.Sequence,
		Kind: in.Kind, Key: in.Key,
	}
}

type routeWorkspaceWireInput struct {
	WorkspaceID     string `json:"workspace_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Sequence        int64  `json:"sequence"`
	State           string `json:"state"`
	TicketID        string `json:"ticket_id"`
}

func (in routeWorkspaceWireInput) application() appwayfinder.RouteWorkspaceInput {
	return appwayfinder.RouteWorkspaceInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, Sequence: in.Sequence,
		State: in.State, TicketID: in.TicketID,
	}
}

type createDiscoveryTicketWireInput struct {
	WorkspaceID        string   `json:"workspace_id"`
	ExpectedVersion    int64    `json:"expected_version"`
	TicketKey          string   `json:"ticket_key"`
	Subject            string   `json:"subject"`
	DependsOnTicketIDs []string `json:"depends_on_ticket_ids"`
	DependencyKind     string   `json:"dependency_kind"`
}

func (in createDiscoveryTicketWireInput) application() appwayfinder.CreateDiscoveryTicketInput {
	return appwayfinder.CreateDiscoveryTicketInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, TicketKey: in.TicketKey,
		Subject: in.Subject, DependsOnTicketIDs: in.DependsOnTicketIDs, DependencyKind: in.DependencyKind,
	}
}

type resolveDiscoveryTicketWireInput struct {
	PacketID           string        `json:"packet_id"`
	WorkspaceID        string        `json:"workspace_id"`
	ExpectedVersion    int64         `json:"expected_version"`
	TicketID           string        `json:"ticket_id"`
	ExpectedTicketVer  int64         `json:"expected_ticket_ver"`
	ResolutionSequence int64         `json:"resolution_sequence"`
	ResolutionKind     string        `json:"resolution_kind"`
	InputName          string        `json:"input_name"`
	SourceClosureID    optionalInt64 `json:"source_closure_id,omitempty"`
}

func (in resolveDiscoveryTicketWireInput) application() appwayfinder.ResolveDiscoveryTicketInput {
	return appwayfinder.ResolveDiscoveryTicketInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, TicketID: in.TicketID,
		ExpectedTicketVer: in.ExpectedTicketVer, ResolutionSequence: in.ResolutionSequence,
		ResolutionKind: in.ResolutionKind, SourceClosureID: nullableInt64(in.SourceClosureID),
	}
}

type attachInvestigationWireInput struct {
	WorkspaceID     string        `json:"workspace_id"`
	ExpectedVersion int64         `json:"expected_version"`
	TicketID        string        `json:"ticket_id,omitempty"`
	Sequence        int64         `json:"sequence"`
	Kind            string        `json:"kind"`
	ArtifactRowID   optionalInt64 `json:"artifact_row_id"`
	ArtifactSHA256  string        `json:"artifact_sha256"`
	SourceClosureID optionalInt64 `json:"source_closure_id,omitempty"`
}

func (in attachInvestigationWireInput) application() appwayfinder.AttachInvestigationInput {
	return appwayfinder.AttachInvestigationInput{
		WorkspaceID: in.WorkspaceID, ExpectedVersion: in.ExpectedVersion, TicketID: in.TicketID,
		Sequence: in.Sequence, Kind: in.Kind, ArtifactRowID: nullableInt64(in.ArtifactRowID),
		ArtifactSHA256: in.ArtifactSHA256, SourceClosureID: nullableInt64(in.SourceClosureID),
	}
}

func nullableString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func nullableInt64(value optionalInt64) sql.NullInt64 {
	if !value.set {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value.value, Valid: true}
}

type optionalInt64 struct {
	value int64
	set   bool
}

func (value *optionalInt64) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("null is not permitted")
	}
	var decoded int64
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	value.value = decoded
	value.set = true
	return nil
}
