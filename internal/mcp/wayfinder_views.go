package mcp

import (
	appwayfinder "relay/internal/app/wayfinder"
	workflowstore "relay/internal/store/workflow"
)

// These projections are the public MCP readback contract. They deliberately
// contain no generated store models, database row identities, or sql.Null*
// values.
type wayfinderWorkspaceReadback struct {
	Workspace      wayfinderWorkspaceView       `json:"workspace"`
	Inputs         []wayfinderInputView         `json:"inputs"`
	Destinations   []wayfinderDestinationView   `json:"destinations"`
	Tickets        []wayfinderTicketView        `json:"tickets"`
	Routes         []wayfinderRouteView         `json:"routes"`
	Investigations []wayfinderInvestigationView `json:"investigations"`
}

type wayfinderWorkspaceView struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	FeatureSlug string `json:"feature_slug"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
}

type wayfinderInputView struct {
	InputID         string `json:"input_id"`
	Sequence        int64  `json:"sequence"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	SourceKind      string `json:"source_kind"`
	SourceReference string `json:"source_reference"`
	Digest          string `json:"digest,omitempty"`
}

type wayfinderDestinationView struct {
	DestinationID string  `json:"destination_id"`
	Sequence      int64   `json:"sequence"`
	Kind          string  `json:"kind"`
	Key           string  `json:"key"`
	RepoTarget    *string `json:"repo_target,omitempty"`
}

type wayfinderTicketView struct {
	TicketID     string                    `json:"ticket_id"`
	TicketKey    string                    `json:"ticket_key"`
	Subject      string                    `json:"subject"`
	State        string                    `json:"state"`
	Version      int64                     `json:"version"`
	Dependencies []wayfinderDependencyView `json:"dependencies"`
	Resolutions  []wayfinderResolutionView `json:"resolutions"`
}

type wayfinderDependencyView struct {
	DependsOnTicketID string `json:"depends_on_ticket_id"`
	Kind              string `json:"kind"`
}

type wayfinderResolutionView struct {
	ResolutionID string `json:"resolution_id"`
	Sequence     int64  `json:"sequence"`
	Kind         string `json:"kind"`
	Digest       string `json:"digest"`
}

type wayfinderResolveDiscoveryTicketResponse struct {
	Resolution wayfinderResolveResolutionView `json:"resolution"`
	Ticket     wayfinderResolveTicketView     `json:"ticket"`
	Workspace  wayfinderResolveWorkspaceView  `json:"workspace"`
}

type wayfinderResolveResolutionView struct {
	ResolutionID   string `json:"resolution_id"`
	Sequence       int64  `json:"sequence"`
	ResolutionKind string `json:"resolution_kind"`
	PacketID       string `json:"packet_id"`
	InputName      string `json:"input_name"`
	ArtifactSHA256 string `json:"artifact_sha256"`
	CreatedAt      string `json:"created_at"`
}

type wayfinderResolveTicketView struct {
	DiscoveryTicketID string `json:"discovery_ticket_id"`
	TicketKey         string `json:"ticket_key"`
	Subject           string `json:"subject"`
	State             string `json:"state"`
	Version           int64  `json:"version"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type wayfinderResolveWorkspaceView struct {
	WorkspaceID string `json:"workspace_id"`
	FeatureSlug string `json:"feature_slug"`
	State       string `json:"state"`
	Version     int64  `json:"version"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

func buildWayfinderResolveDiscoveryTicketResponse(resolution workflowstore.FeatureWorkspaceTicketResolution, ticket workflowstore.FeatureWorkspaceDiscoveryTicket, workspace workflowstore.FeatureWorkspace, packetID, inputName, artifactSHA256 string) wayfinderResolveDiscoveryTicketResponse {
	return wayfinderResolveDiscoveryTicketResponse{
		Resolution: wayfinderResolveResolutionView{
			ResolutionID: resolution.ResolutionID, Sequence: resolution.Sequence,
			ResolutionKind: resolution.ResolutionKind, PacketID: packetID, InputName: inputName,
			ArtifactSHA256: artifactSHA256, CreatedAt: resolution.CreatedAt,
		},
		Ticket: wayfinderResolveTicketView{
			DiscoveryTicketID: ticket.DiscoveryTicketID, TicketKey: ticket.TicketKey,
			Subject: ticket.Subject, State: ticket.State, Version: ticket.Version,
			CreatedAt: ticket.CreatedAt, UpdatedAt: ticket.UpdatedAt,
		},
		Workspace: wayfinderResolveWorkspaceView{
			WorkspaceID: workspace.WorkspaceID, FeatureSlug: workspace.FeatureSlug,
			State: workspace.State, Version: workspace.Version,
			CreatedAt: workspace.CreatedAt, UpdatedAt: workspace.UpdatedAt,
		},
	}
}

type wayfinderRouteView struct {
	RouteStateID string  `json:"route_state_id"`
	Sequence     int64   `json:"sequence"`
	State        string  `json:"state"`
	TicketID     *string `json:"ticket_id,omitempty"`
}

type wayfinderInvestigationView struct {
	InvestigationID string  `json:"investigation_id"`
	TicketID        *string `json:"ticket_id,omitempty"`
	Sequence        int64   `json:"sequence"`
	Kind            string  `json:"kind"`
	Digest          string  `json:"digest"`
}

func buildWayfinderWorkspaceReadback(detail appwayfinder.WorkspaceDetail) wayfinderWorkspaceReadback {
	ticketIDs := make(map[int64]string, len(detail.Tickets))
	for _, ticket := range detail.Tickets {
		ticketIDs[ticket.Ticket.ID] = ticket.Ticket.DiscoveryTicketID
	}

	result := wayfinderWorkspaceReadback{
		Workspace: wayfinderWorkspaceView{
			WorkspaceID: detail.Workspace.WorkspaceID,
			ProjectID:   detail.Project.ProjectID,
			FeatureSlug: detail.Workspace.FeatureSlug,
			State:       detail.Workspace.State,
			Version:     detail.Workspace.Version,
		},
		Inputs:         make([]wayfinderInputView, 0, len(detail.Inputs)),
		Destinations:   make([]wayfinderDestinationView, 0, len(detail.Destinations)),
		Tickets:        make([]wayfinderTicketView, 0, len(detail.Tickets)),
		Routes:         make([]wayfinderRouteView, 0, len(detail.Routes)),
		Investigations: make([]wayfinderInvestigationView, 0, len(detail.Investigations)),
	}
	for _, input := range detail.Inputs {
		digest := ""
		if input.ArtifactSha256.Valid {
			digest = input.ArtifactSha256.String
		}
		result.Inputs = append(result.Inputs, wayfinderInputView{
			InputID: input.AdmittedInputID, Sequence: input.Sequence, Name: input.InputName,
			Role: input.InputRole, SourceKind: input.SourceKind, SourceReference: input.SourceReference,
			Digest: digest,
		})
	}
	for _, destination := range detail.Destinations {
		var repoTarget *string
		if destination.RepoTarget.Valid {
			value := destination.RepoTarget.String
			repoTarget = &value
		}
		result.Destinations = append(result.Destinations, wayfinderDestinationView{
			DestinationID: destination.DestinationID, Sequence: destination.Sequence,
			Kind: destination.DestinationKind, Key: destination.DestinationKey, RepoTarget: repoTarget,
		})
	}
	for _, ticket := range detail.Tickets {
		view := wayfinderTicketView{
			TicketID: ticket.Ticket.DiscoveryTicketID, TicketKey: ticket.Ticket.TicketKey,
			Subject: ticket.Ticket.Subject, State: ticket.Ticket.State, Version: ticket.Ticket.Version,
			Dependencies: make([]wayfinderDependencyView, 0, len(ticket.Dependencies)),
			Resolutions:  make([]wayfinderResolutionView, 0, len(ticket.Resolutions)),
		}
		for _, dependency := range ticket.Dependencies {
			if dependencyID, ok := ticketIDs[dependency.DependsOnTicketRowID]; ok {
				view.Dependencies = append(view.Dependencies, wayfinderDependencyView{DependsOnTicketID: dependencyID, Kind: dependency.DependencyKind})
			}
		}
		for _, resolution := range ticket.Resolutions {
			view.Resolutions = append(view.Resolutions, wayfinderResolutionView{
				ResolutionID: resolution.ResolutionID, Sequence: resolution.Sequence,
				Kind: resolution.ResolutionKind, Digest: resolution.ArtifactSha256,
			})
		}
		result.Tickets = append(result.Tickets, view)
	}
	for _, route := range detail.Routes {
		var ticketID *string
		if route.TicketRowID.Valid {
			if value, ok := ticketIDs[route.TicketRowID.Int64]; ok {
				ticketID = &value
			}
		}
		result.Routes = append(result.Routes, wayfinderRouteView{
			RouteStateID: route.RouteStateID, Sequence: route.Sequence, State: route.State, TicketID: ticketID,
		})
	}
	for _, investigation := range detail.Investigations {
		var ticketID *string
		if investigation.TicketRowID.Valid {
			if value, ok := ticketIDs[investigation.TicketRowID.Int64]; ok {
				ticketID = &value
			}
		}
		result.Investigations = append(result.Investigations, wayfinderInvestigationView{
			InvestigationID: investigation.InvestigationID, TicketID: ticketID,
			Sequence: investigation.Sequence, Kind: investigation.InvestigationKind,
			Digest: investigation.ArtifactSHA256,
		})
	}
	return result
}
