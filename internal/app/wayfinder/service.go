package wayfinder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidWorkspaceRequest = errors.New("invalid feature workspace request")
	ErrWorkspaceNotFound       = errors.New("feature workspace not found")
	ErrDiscoveryTicketNotFound = errors.New("discovery ticket not found")
	ErrVersionConflict         = errors.New("feature workspace version conflict")
	ErrSourceAuthorityMissing  = errors.New("source authority is required")
	ErrSourceIsCurrent         = errors.New("source authority is current")
)

type SourceClosureReader interface {
	ReadInvestigationClosure(context.Context, RetainedClosureIdentity) (RetainedClosureIdentity, error)
}

type IDGenerator interface {
	WorkspaceID() string
	InputID() string
	DestinationID() string
	DiscoveryTicketID() string
	ResolutionID() string
	RouteStateID() string
	InvestigationID() string
}

type defaultIDGenerator struct{}

func (defaultIDGenerator) WorkspaceID() string { return workflowstore.NewFeatureWorkspaceID() }
func (defaultIDGenerator) InputID() string     { return workflowstore.NewFeatureWorkspaceInputID() }
func (defaultIDGenerator) DestinationID() string {
	return workflowstore.NewFeatureWorkspaceDestinationID()
}
func (defaultIDGenerator) DiscoveryTicketID() string {
	return workflowstore.NewFeatureWorkspaceDiscoveryTicketID()
}
func (defaultIDGenerator) ResolutionID() string {
	return workflowstore.NewFeatureWorkspaceResolutionID()
}
func (defaultIDGenerator) RouteStateID() string {
	return workflowstore.NewFeatureWorkspaceRouteStateID()
}
func (defaultIDGenerator) InvestigationID() string {
	return "investigation-" + strings.TrimPrefix(workflowstore.NewFeatureWorkspaceInputID(), "input-")
}

type Service struct {
	store           *workflowstore.Store
	ids             IDGenerator
	sourceAuthority SourceClosureReader
}

func NewService(store *workflowstore.Store, sourceAuthority ...SourceClosureReader) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{}, sourceAuthority...)
}

func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator, sourceAuthority ...SourceClosureReader) (*Service, error) {
	if store == nil || ids == nil || len(sourceAuthority) > 1 {
		return nil, ErrInvalidWorkspaceRequest
	}
	service := &Service{store: store, ids: ids}
	if len(sourceAuthority) == 1 {
		service.sourceAuthority = sourceAuthority[0]
	}
	return service, nil
}

type CreateWorkspaceInput struct {
	ProjectID   string
	FeatureSlug string
}

type WorkspaceDetail struct {
	Workspace      workflowstore.FeatureWorkspace
	Inputs         []workflowstore.FeatureWorkspaceAdmittedInput
	Destinations   []workflowstore.FeatureWorkspaceDestination
	Tickets        []TicketDetail
	Routes         []workflowstore.FeatureWorkspaceRouteState
	Investigations []workflowstore.FeatureWorkspaceInvestigation
}

type TicketDetail struct {
	Ticket       workflowstore.FeatureWorkspaceDiscoveryTicket
	Dependencies []workflowstore.FeatureWorkspaceTicketDependency
	Resolutions  []workflowstore.FeatureWorkspaceTicketResolution
}

func (s *Service) CreateWorkspace(ctx context.Context, input CreateWorkspaceInput) (workflowstore.FeatureWorkspace, error) {
	projectID, featureSlug := strings.TrimSpace(input.ProjectID), strings.TrimSpace(input.FeatureSlug)
	if projectID == "" || featureSlug == "" {
		return workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var workspace workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		project, err := tx.GetProjectByProjectID(ctx, projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrWorkspaceNotFound, projectID)
		}
		if err != nil {
			return err
		}
		workspace, err = tx.CreateFeatureWorkspace(ctx, workflowstore.CreateFeatureWorkspaceParams{WorkspaceID: s.ids.WorkspaceID(), ProjectRowID: project.ID, FeatureSlug: featureSlug})
		return err
	})
	return workspace, err
}

func (s *Service) ReadWorkspace(ctx context.Context, workspaceID string) (WorkspaceDetail, error) {
	workspace, err := s.workspace(ctx, strings.TrimSpace(workspaceID))
	if err != nil {
		return WorkspaceDetail{}, err
	}
	detail := WorkspaceDetail{Workspace: workspace}
	if detail.Inputs, err = s.store.ListFeatureWorkspaceAdmittedInputs(ctx, workspace.ID); err != nil {
		return WorkspaceDetail{}, err
	}
	if detail.Destinations, err = s.store.ListFeatureWorkspaceDestinations(ctx, workspace.ID); err != nil {
		return WorkspaceDetail{}, err
	}
	if detail.Routes, err = s.store.ListFeatureWorkspaceRouteStates(ctx, workspace.ID); err != nil {
		return WorkspaceDetail{}, err
	}
	if detail.Investigations, err = s.store.ListFeatureWorkspaceInvestigations(ctx, workspace.ID); err != nil {
		return WorkspaceDetail{}, err
	}
	tickets, err := s.store.ListFeatureWorkspaceDiscoveryTickets(ctx, workspace.ID)
	if err != nil {
		return WorkspaceDetail{}, err
	}
	detail.Tickets = make([]TicketDetail, len(tickets))
	for index, ticket := range tickets {
		detail.Tickets[index].Ticket = ticket
		if detail.Tickets[index].Dependencies, err = s.store.ListFeatureWorkspaceTicketDependencies(ctx, ticket.ID); err != nil {
			return WorkspaceDetail{}, err
		}
		if detail.Tickets[index].Resolutions, err = s.store.ListFeatureWorkspaceTicketResolutions(ctx, ticket.ID); err != nil {
			return WorkspaceDetail{}, err
		}
	}
	return detail, nil
}

type AdmitInputInput struct {
	WorkspaceID      string
	ExpectedVersion  int64
	Sequence         int64
	Name             string
	Role             string
	SourceKind       string
	ArtifactRowID    sql.NullInt64
	RetainedArtifact sql.NullInt64
	SourceClosureID  sql.NullInt64
	ArtifactSHA256   sql.NullString
	SourceReference  string
}

func (s *Service) AdmitInput(ctx context.Context, input AdmitInputInput) (workflowstore.FeatureWorkspaceAdmittedInput, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || input.Sequence < 1 || strings.TrimSpace(input.Name) == "" || !oneOf(input.Role, "candidate", "governing", "authority", "evidence") || !oneOf(input.SourceKind, "uploaded_file", "relay_artifact", "inline_text", "workflow_record", "committed_source") || strings.TrimSpace(input.SourceReference) != input.SourceReference || len(input.SourceReference) > 512 {
		return workflowstore.FeatureWorkspaceAdmittedInput{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var admitted workflowstore.FeatureWorkspaceAdmittedInput
	updated, err := s.mutateWorkspace(ctx, input.WorkspaceID, input.ExpectedVersion, func(tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace) error {
		var err error
		admitted, err = tx.CreateFeatureWorkspaceAdmittedInput(ctx, workflowstore.CreateFeatureWorkspaceAdmittedInputParams{AdmittedInputID: s.ids.InputID(), WorkspaceRowID: workspace.ID, Sequence: input.Sequence, InputName: input.Name, InputRole: input.Role, SourceKind: input.SourceKind, ArtifactRowID: input.ArtifactRowID, RetainedArtifactRowID: input.RetainedArtifact, SourceClosureRowID: input.SourceClosureID, ArtifactSha256: input.ArtifactSHA256, SourceReference: input.SourceReference})
		return err
	})
	return admitted, updated, err
}

type AddDestinationInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Sequence        int64
	Kind            string
	Key             string
	RepoTarget      sql.NullString
	SourceClosureID sql.NullInt64
}

func (s *Service) AddDestination(ctx context.Context, input AddDestinationInput) (workflowstore.FeatureWorkspaceDestination, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || input.Sequence < 1 || !oneOf(input.Kind, "destination", "fog") || strings.TrimSpace(input.Key) == "" || len(input.Key) > 512 {
		return workflowstore.FeatureWorkspaceDestination{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var destination workflowstore.FeatureWorkspaceDestination
	updated, err := s.mutateWorkspace(ctx, input.WorkspaceID, input.ExpectedVersion, func(tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace) error {
		var err error
		destination, err = tx.CreateFeatureWorkspaceDestination(ctx, workflowstore.CreateFeatureWorkspaceDestinationParams{DestinationID: s.ids.DestinationID(), WorkspaceRowID: workspace.ID, Sequence: input.Sequence, DestinationKind: input.Kind, DestinationKey: input.Key, RepoTarget: input.RepoTarget, SourceClosureRowID: input.SourceClosureID})
		return err
	})
	return destination, updated, err
}

type CreateDiscoveryTicketInput struct {
	WorkspaceID        string
	ExpectedVersion    int64
	TicketKey          string
	Subject            string
	DependsOnTicketIDs []string
	DependencyKind     string
}

func (s *Service) CreateDiscoveryTicket(ctx context.Context, input CreateDiscoveryTicketInput) (workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || strings.TrimSpace(input.TicketKey) == "" || len(input.TicketKey) > 128 || strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 1024 || (len(input.DependsOnTicketIDs) > 0 && !oneOf(input.DependencyKind, "blocks", "informs")) {
		return workflowstore.FeatureWorkspaceDiscoveryTicket{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	updated, err := s.mutateWorkspace(ctx, input.WorkspaceID, input.ExpectedVersion, func(tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace) error {
		var err error
		ticket, err = tx.CreateFeatureWorkspaceDiscoveryTicket(ctx, workflowstore.CreateFeatureWorkspaceDiscoveryTicketParams{DiscoveryTicketID: s.ids.DiscoveryTicketID(), WorkspaceRowID: workspace.ID, TicketKey: input.TicketKey, Subject: input.Subject})
		if err != nil {
			return err
		}
		for _, dependencyID := range input.DependsOnTicketIDs {
			dependency, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, strings.TrimSpace(dependencyID))
			if errors.Is(err, sql.ErrNoRows) || dependency.WorkspaceRowID != workspace.ID {
				return ErrDiscoveryTicketNotFound
			}
			if err != nil {
				return err
			}
			if err := tx.CreateFeatureWorkspaceTicketDependency(ctx, workflowstore.CreateFeatureWorkspaceTicketDependencyParams{TicketRowID: ticket.ID, DependsOnTicketRowID: dependency.ID, DependencyKind: input.DependencyKind}); err != nil {
				return err
			}
		}
		return nil
	})
	return ticket, updated, err
}

type ResolveDiscoveryTicketInput struct {
	WorkspaceID        string
	ExpectedVersion    int64
	TicketID           string
	ExpectedTicketVer  int64
	ResolutionSequence int64
	ResolutionKind     string
	ArtifactRowID      sql.NullInt64
	RetainedArtifact   sql.NullInt64
	ArtifactSHA256     string
	SourceClosureID    sql.NullInt64
}

func (s *Service) ResolveDiscoveryTicket(ctx context.Context, input ResolveDiscoveryTicketInput) (workflowstore.FeatureWorkspaceTicketResolution, workflowstore.FeatureWorkspaceDiscoveryTicket, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || input.ExpectedTicketVer < 1 || input.ResolutionSequence < 1 || !oneOf(input.ResolutionKind, "resolved", "rejected", "deferred") || !validSHA256(input.ArtifactSHA256) || input.ArtifactRowID.Valid == input.RetainedArtifact.Valid {
		return workflowstore.FeatureWorkspaceTicketResolution{}, workflowstore.FeatureWorkspaceDiscoveryTicket{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var resolution workflowstore.FeatureWorkspaceTicketResolution
	var ticket workflowstore.FeatureWorkspaceDiscoveryTicket
	updated, err := s.mutateWorkspace(ctx, input.WorkspaceID, input.ExpectedVersion, func(tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace) error {
		var err error
		ticket, err = tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, strings.TrimSpace(input.TicketID))
		if errors.Is(err, sql.ErrNoRows) || ticket.WorkspaceRowID != workspace.ID {
			return ErrDiscoveryTicketNotFound
		}
		if err != nil {
			return err
		}
		resolution, err = tx.CreateFeatureWorkspaceTicketResolution(ctx, workflowstore.CreateFeatureWorkspaceTicketResolutionParams{ResolutionID: s.ids.ResolutionID(), TicketRowID: ticket.ID, Sequence: input.ResolutionSequence, ResolutionKind: input.ResolutionKind, ArtifactRowID: input.ArtifactRowID, RetainedArtifactRowID: input.RetainedArtifact, ArtifactSha256: input.ArtifactSHA256, SourceClosureRowID: input.SourceClosureID})
		if err != nil {
			return err
		}
		ticket, err = tx.TransitionFeatureWorkspaceDiscoveryTicket(ctx, ticket.DiscoveryTicketID, "open", ticketStateForResolution(input.ResolutionKind), input.ExpectedTicketVer)
		return err
	})
	return resolution, ticket, updated, err
}

type AttachInvestigationInput struct {
	WorkspaceID      string
	ExpectedVersion  int64
	TicketID         string
	Sequence         int64
	Kind             string
	ArtifactRowID    sql.NullInt64
	RetainedArtifact sql.NullInt64
	ArtifactSHA256   string
	SourceClosureID  sql.NullInt64
}

func (s *Service) AttachInvestigation(ctx context.Context, input AttachInvestigationInput) (workflowstore.FeatureWorkspaceInvestigation, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || input.Sequence < 1 || !oneOf(input.Kind, "source", "artifact", "dependency") || !validSHA256(input.ArtifactSHA256) || input.ArtifactRowID.Valid == input.RetainedArtifact.Valid {
		return workflowstore.FeatureWorkspaceInvestigation{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var investigation workflowstore.FeatureWorkspaceInvestigation
	updated, err := s.mutateWorkspace(ctx, input.WorkspaceID, input.ExpectedVersion, func(tx *workflowstore.Tx, workspace workflowstore.FeatureWorkspace) error {
		var ticketRowID sql.NullInt64
		if strings.TrimSpace(input.TicketID) != "" {
			ticket, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, strings.TrimSpace(input.TicketID))
			if errors.Is(err, sql.ErrNoRows) || ticket.WorkspaceRowID != workspace.ID {
				return ErrDiscoveryTicketNotFound
			}
			if err != nil {
				return err
			}
			ticketRowID = sql.NullInt64{Int64: ticket.ID, Valid: true}
		}
		var err error
		investigation, err = tx.CreateFeatureWorkspaceInvestigation(ctx, workflowstore.CreateFeatureWorkspaceInvestigationParams{InvestigationID: s.ids.InvestigationID(), WorkspaceRowID: workspace.ID, TicketRowID: ticketRowID, Sequence: input.Sequence, InvestigationKind: input.Kind, ArtifactRowID: input.ArtifactRowID, RetainedArtifactRowID: input.RetainedArtifact, ArtifactSHA256: input.ArtifactSHA256, SourceClosureRowID: input.SourceClosureID})
		return err
	})
	return investigation, updated, err
}

type RouteWorkspaceInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Sequence        int64
	State           string
	TicketID        string
}

func (s *Service) RouteWorkspace(ctx context.Context, input RouteWorkspaceInput) (workflowstore.FeatureWorkspaceRouteState, workflowstore.FeatureWorkspace, error) {
	if !validWorkspaceMutation(input.WorkspaceID, input.ExpectedVersion) || input.Sequence < 1 || !oneOf(input.State, "discovery", "ready", "blocked", "resolved", "closed") {
		return workflowstore.FeatureWorkspaceRouteState{}, workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	var route workflowstore.FeatureWorkspaceRouteState
	var updated workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(input.WorkspaceID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		var ticketRowID sql.NullInt64
		if strings.TrimSpace(input.TicketID) != "" {
			ticket, err := tx.GetFeatureWorkspaceDiscoveryTicketByID(ctx, strings.TrimSpace(input.TicketID))
			if errors.Is(err, sql.ErrNoRows) || ticket.WorkspaceRowID != workspace.ID {
				return ErrDiscoveryTicketNotFound
			}
			if err != nil {
				return err
			}
			ticketRowID = sql.NullInt64{Int64: ticket.ID, Valid: true}
		}
		route, err = tx.CreateFeatureWorkspaceRouteState(ctx, workflowstore.CreateFeatureWorkspaceRouteStateParams{RouteStateID: s.ids.RouteStateID(), WorkspaceRowID: workspace.ID, Sequence: input.Sequence, WorkspaceVersion: workspace.Version + 1, State: input.State, TicketRowID: ticketRowID})
		if err != nil {
			return err
		}
		workspaceState := "open"
		if input.State == "closed" {
			workspaceState = "closed"
		}
		updated, err = tx.AdvanceFeatureWorkspaceRouteState(ctx, route.ID, workspaceState, workspace.WorkspaceID, workspace.Version)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrVersionConflict
	}
	return route, updated, err
}

type ReopenStaleSourceInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	Sequence        int64
	Source          RetainedClosureIdentity
}

func (s *Service) ReopenStaleSource(ctx context.Context, input ReopenStaleSourceInput) (workflowstore.FeatureWorkspaceRouteState, workflowstore.FeatureWorkspace, error) {
	if s.sourceAuthority == nil {
		return workflowstore.FeatureWorkspaceRouteState{}, workflowstore.FeatureWorkspace{}, ErrSourceAuthorityMissing
	}
	if _, err := s.sourceAuthority.ReadInvestigationClosure(ctx, input.Source); err == nil {
		return workflowstore.FeatureWorkspaceRouteState{}, workflowstore.FeatureWorkspace{}, ErrSourceIsCurrent
	} else if !errors.Is(err, ErrStaleSourceBase) && !errors.Is(err, ErrRetainedClosureUnavailable) {
		return workflowstore.FeatureWorkspaceRouteState{}, workflowstore.FeatureWorkspace{}, err
	}
	return s.RouteWorkspace(ctx, RouteWorkspaceInput{WorkspaceID: input.WorkspaceID, ExpectedVersion: input.ExpectedVersion, Sequence: input.Sequence, State: "discovery"})
}

func (s *Service) workspace(ctx context.Context, workspaceID string) (workflowstore.FeatureWorkspace, error) {
	if workspaceID == "" {
		return workflowstore.FeatureWorkspace{}, ErrInvalidWorkspaceRequest
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.FeatureWorkspace{}, ErrWorkspaceNotFound
	}
	return workspace, err
}

func (s *Service) mutateWorkspace(ctx context.Context, workspaceID string, expectedVersion int64, mutation func(*workflowstore.Tx, workflowstore.FeatureWorkspace) error) (workflowstore.FeatureWorkspace, error) {
	var updated workflowstore.FeatureWorkspace
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, strings.TrimSpace(workspaceID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Version != expectedVersion {
			return ErrVersionConflict
		}
		if err := mutation(tx, workspace); err != nil {
			return err
		}
		updated, err = tx.BumpFeatureWorkspaceVersion(ctx, workspace.WorkspaceID, expectedVersion)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return workflowstore.FeatureWorkspace{}, ErrVersionConflict
	}
	return updated, err
}

func validWorkspaceMutation(workspaceID string, expectedVersion int64) bool {
	return strings.TrimSpace(workspaceID) != "" && expectedVersion > 0
}
func oneOf(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}
func ticketStateForResolution(kind string) string {
	if kind == "resolved" {
		return "resolved"
	}
	if kind == "deferred" {
		return "blocked"
	}
	return "cancelled"
}
func validSHA256(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}
