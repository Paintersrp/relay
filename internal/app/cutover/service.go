package cutover

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	workflowstore "relay/internal/store/workflow"
)

type Service struct {
	store *workflowstore.Store
}

func NewService(store *workflowstore.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("workflow store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) Prepare(ctx context.Context, request PrepareRequest) (*State, error) {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" {
		return nil, ErrCutoverConfigurationInvalid
	}
	configuration, err := normalizeGatewayConfiguration(request.GatewayConfiguration)
	if err != nil || configuration.TopologyVersion != appSurfaceTopologyVersion {
		return nil, ErrCutoverConfigurationInvalid
	}
	if len(request.Prerequisites) == 0 || len(request.ActivationEvidence) == 0 ||
		len(request.RollForwardCriteria) == 0 ||
		(request.RollbackEligibility == "eligible" && len(request.RollbackEvidence) == 0) {
		return nil, ErrCutoverConfigurationInvalid
	}
	request.GatewayConfiguration = configuration
	var activation workflowstore.CutoverActivation
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		var createErr error
		activation, createErr = tx.CreateCutoverActivation(
			ctx,
			request.ActivationID,
			request.WorkspaceRowID,
			request.TransitionPlanTicketRevisionRowID,
			request.TransitionPlanTicketID,
			request.TransitionPlanTicketRevision,
			request.TransitionPlanAuthorityLayerRowID,
			request.TransitionPlanSHA256,
			request.AuthorityRevisionRowID,
			request.AuthorityRevisionID,
			request.AuthorityRevisionNumber,
			request.AuthoritySHA256,
			request.RollbackEligibility,
		)
		if createErr != nil {
			return createErr
		}
		if createErr = tx.CreateCutoverGatewayConfiguration(ctx, activation.ID, toStoreGatewayConfiguration(configuration)); createErr != nil {
			return createErr
		}
		for index, prerequisite := range request.Prerequisites {
			if _, createErr = tx.CreateCutoverPrerequisite(ctx, activation.ID, int64(index+1), prerequisite.Prerequisite, prerequisite.Evidence); createErr != nil {
				return createErr
			}
		}
		for index, evidence := range request.ActivationEvidence {
			if _, createErr = tx.CreateCutoverObligation(ctx, activation.ID, "activation", int64(index+1), evidence.Obligation, evidence.Evidence); createErr != nil {
				return createErr
			}
		}
		for index, evidence := range request.RollbackEvidence {
			if _, createErr = tx.CreateCutoverObligation(ctx, activation.ID, "rollback", int64(index+1), evidence.Obligation, evidence.Evidence); createErr != nil {
				return createErr
			}
		}
		for index, criterion := range request.RollForwardCriteria {
			if _, createErr = tx.CreateCutoverRollForwardCriterion(ctx, activation.ID, int64(index+1), criterion); createErr != nil {
				return createErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	state := stateFrom(activation)
	state.GatewayConfiguration = &configuration
	return &state, nil
}

func (s *Service) State(ctx context.Context) (*State, bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return nil, found, err
	}
	state := stateFrom(activation)
	configuration, err := s.loadVerifiedConfiguration(ctx, activation.ID)
	if err != nil {
		return nil, false, err
	}
	state.GatewayConfiguration = &configuration
	return &state, true, nil
}

func (s *Service) Readiness(ctx context.Context, activationID string) (*Readiness, error) {
	activation, err := s.store.GetCutoverActivationByID(ctx, activationID)
	if err != nil {
		return nil, err
	}
	prereqs, err := s.store.ListCutoverActivationPrerequisites(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	obligations, err := s.store.ListCutoverActivationObligations(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	criteria, err := s.store.ListCutoverRollForwardCriteria(ctx, activation.ID)
	if err != nil {
		return nil, err
	}
	result := &Readiness{
		Prepared:                 activation.ActivationStatus == "prepared",
		Active:                   activation.ActivationStatus == "active",
		BoundaryCrossed:          activation.ExecutionBoundaryStatus == "crossed",
		AggregateAdmissionClosed: activation.ActivationStatus == "active",
		Prerequisites:            make([]string, 0, len(prereqs)),
		Obligations:              make([]string, 0, len(obligations)),
		RollForwardCriteria:      make([]string, 0, len(criteria)),
		Evidence:                 make([]PrerequisiteEvidence, 0, len(prereqs)),
		ActivationEvidence:       make([]ObligationEvidence, 0, len(obligations)),
		ConfigurationErrors:      []string{},
	}
	for _, p := range prereqs {
		result.Prerequisites = append(result.Prerequisites, p.Prerequisite)
		result.Evidence = append(result.Evidence, PrerequisiteEvidence{Prerequisite: p.Prerequisite, Evidence: p.Evidence})
	}
	for _, o := range obligations {
		result.Obligations = append(result.Obligations, o.Obligation)
		result.ActivationEvidence = append(result.ActivationEvidence, ObligationEvidence{Kind: o.ObligationKind, Obligation: o.Obligation, Evidence: o.Evidence})
	}
	for _, c := range criteria {
		result.RollForwardCriteria = append(result.RollForwardCriteria, c.CompletionCriterion)
	}
	configuration, configErr := s.loadVerifiedConfiguration(ctx, activation.ID)
	if configErr == nil && configuration.TopologyVersion != appSurfaceTopologyVersion {
		configErr = ErrCutoverConfigurationInvalid
	}
	if configErr != nil {
		result.ConfigurationErrors = append(result.ConfigurationErrors, configErr.Error())
	} else {
		result.GatewayConfiguration = &configuration
	}
	hasActivationEvidence := false
	hasRollbackEvidence := activation.RollbackEligibility != "eligible"
	for _, evidence := range result.ActivationEvidence {
		switch evidence.Kind {
		case "activation":
			hasActivationEvidence = true
		case "rollback":
			hasRollbackEvidence = true
		}
	}
	_, currentFound, currentErr := s.store.GetCurrentCutoverActivation(ctx)
	if currentErr != nil {
		return nil, currentErr
	}
	result.Ready = result.Prepared &&
		!currentFound &&
		configErr == nil &&
		len(prereqs) > 0 &&
		hasActivationEvidence &&
		hasRollbackEvidence &&
		len(criteria) > 0
	return result, nil
}

func (s *Service) History(ctx context.Context) ([]State, error) {
	values, err := s.store.ListAllCutoverActivations(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]State, 0, len(values))
	for _, value := range values {
		state := stateFrom(value)
		configuration, configErr := s.loadVerifiedConfiguration(ctx, value.ID)
		if configErr == nil {
			state.GatewayConfiguration = &configuration
		} else if !errors.Is(configErr, sql.ErrNoRows) {
			return nil, configErr
		}
		result = append(result, state)
	}
	return result, nil
}

func (s *Service) Activate(ctx context.Context, request ActivationRequest) (*State, error) {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" {
		return nil, ErrCutoverNotFound
	}
	if request.ActivatedAt == "" {
		request.ActivatedAt = canonicalTime(time.Now())
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return nil, err
	}
	if activation.ActivationStatus != "prepared" {
		return nil, ErrCutoverNotReady
	}
	readiness, err := s.Readiness(ctx, request.ActivationID)
	if err != nil || !readiness.Ready {
		if err != nil {
			return nil, err
		}
		return nil, ErrCutoverNotReady
	}
	_, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil {
		return nil, err
	}
	if found {
		return nil, ErrCutoverAlreadyActive
	}
	var updated workflowstore.CutoverActivation
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		persisted, verifyErr := tx.LoadCutoverGatewayConfiguration(ctx, activation.ID)
		if verifyErr != nil {
			return verifyErr
		}
		configuration, verifyErr := normalizeGatewayConfiguration(fromStoreGatewayConfiguration(persisted))
		if verifyErr != nil {
			return verifyErr
		}
		if configuration.ConfigurationSHA256 != persisted.ConfigurationSHA256 {
			return ErrCutoverConfigurationMismatch
		}
		if err := tx.SetCutoverCurrentState(ctx, activation.ID); err != nil {
			return err
		}
		updated, err = tx.ActivateCutover(ctx, request.ActivationID, request.ActivatedAt, activation.RollbackEligibility)
		return err
	})
	if err != nil {
		return nil, err
	}
	state := stateFrom(updated)
	state.GatewayConfiguration = readiness.GatewayConfiguration
	return &state, nil
}

func (s *Service) Rollback(ctx context.Context, request RollbackRequest) (*State, error) {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" {
		return nil, ErrCutoverNotFound
	}
	if request.RolledBackAt == "" {
		request.RolledBackAt = canonicalTime(time.Now())
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return nil, err
	}
	if activation.ExecutionBoundaryStatus == "crossed" || activation.RollbackEligibility != "eligible" {
		return nil, ErrCutoverRollbackBlocked
	}
	var updated workflowstore.CutoverActivation
	err = s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		updated, err = tx.RollbackCutover(ctx, request.ActivationID, request.RolledBackAt)
		if err != nil {
			return err
		}
		return tx.ClearCutoverCurrentState(ctx)
	})
	if err != nil {
		return nil, err
	}
	state := stateFrom(updated)
	return &state, nil
}

func (s *Service) CrossExecutionBoundary(ctx context.Context, request BoundaryRequest) error {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" || request.RunRowID < 1 {
		return fmt.Errorf("invalid boundary crossing request")
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return err
	}
	if activation.ActivationStatus != "active" {
		return ErrCutoverNotActive
	}
	if activation.ExecutionBoundaryStatus == "crossed" {
		return ErrCutoverBoundaryCrossed
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		_, err := tx.ConditionalCrossCutoverExecutionBoundary(ctx, request.ActivationID, request.RunRowID)
		return err
	})
}

func (s *Service) RecordRollForwardEvidence(ctx context.Context, request RollForwardEvidenceRequest) error {
	request.ActivationID = strings.TrimSpace(request.ActivationID)
	if request.ActivationID == "" || request.CriterionSequence < 1 || strings.TrimSpace(request.Evidence) == "" {
		return fmt.Errorf("invalid roll-forward evidence request")
	}
	activation, err := s.store.GetCutoverActivationByID(ctx, request.ActivationID)
	if err != nil {
		return err
	}
	if activation.RollForwardStatus != "required" {
		return ErrCutoverNotActive
	}
	criteria, err := s.store.ListCutoverRollForwardCriteria(ctx, activation.ID)
	if err != nil {
		return err
	}
	var criterionRowID int64
	for _, criterion := range criteria {
		if criterion.Sequence == request.CriterionSequence {
			criterionRowID = criterion.ID
			break
		}
	}
	if criterionRowID == 0 {
		return fmt.Errorf("roll-forward criterion sequence %d not found", request.CriterionSequence)
	}
	return s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		if _, err := tx.CreateCutoverRollForwardEvidence(ctx, activation.ID, criterionRowID, request.Evidence); err != nil {
			return err
		}
		evidence, err := s.store.ListCutoverRollForwardEvidence(ctx, activation.ID)
		if err != nil {
			return err
		}
		if len(evidence) == len(criteria) {
			_, err = tx.CompleteCutoverRollForward(ctx, request.ActivationID)
		}
		return err
	})
}

func (s *Service) IsLegacyAdmissionClosed(ctx context.Context) (bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return false, err
	}
	return activation.ActivationStatus == "active", nil
}

func (s *Service) IsBoundaryCrossed(ctx context.Context) (bool, error) {
	activation, found, err := s.store.GetCurrentCutoverActivation(ctx)
	if err != nil || !found {
		return false, err
	}
	return activation.ExecutionBoundaryStatus == "crossed", nil
}

func ErrLegacyAdmission() error {
	return ErrLegacyAdmissionClosed
}

func (s *Service) loadVerifiedConfiguration(ctx context.Context, activationRowID int64) (GatewayConfigurationIdentity, error) {
	persisted, err := s.store.LoadCutoverGatewayConfiguration(ctx, activationRowID)
	if err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	configuration := fromStoreGatewayConfiguration(persisted)
	normalized, err := normalizeGatewayConfiguration(configuration)
	if err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	if normalized.ConfigurationSHA256 != persisted.ConfigurationSHA256 {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationMismatch
	}
	return normalized, nil
}

func normalizeLegacyGatewayConfiguration(input GatewayConfigurationIdentity) (GatewayConfigurationIdentity, error) {
	input.RelayRepository = strings.TrimSpace(input.RelayRepository)
	input.StandingRepository = strings.TrimSpace(input.StandingRepository)
	if input.RelayRepository == "" || input.StandingRepository == "" ||
		!validLowerHex(input.RelayCommitOID, 40) ||
		!validLowerHex(input.StandingCommitOID, 40) {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	if len(input.Routes) != 7 || len(input.Mappings) != 7 || len(input.StandingAuthorities) != 3 || len(input.DependencyOutcomes) != 3 {
		return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
	}
	sort.Slice(input.Routes, func(i, j int) bool { return input.Routes[i].Sequence < input.Routes[j].Sequence })
	sort.Slice(input.Mappings, func(i, j int) bool { return input.Mappings[i].Sequence < input.Mappings[j].Sequence })
	sort.Slice(input.StandingAuthorities, func(i, j int) bool { return input.StandingAuthorities[i].Role < input.StandingAuthorities[j].Role })
	sort.Slice(input.DependencyOutcomes, func(i, j int) bool {
		return input.DependencyOutcomes[i].Sequence < input.DependencyOutcomes[j].Sequence
	})

	expectedRoutes := map[string]struct {
		role    string
		surface string
	}{
		"/mcp/v1/wayfinder/workspace":     {"wayfinder", "wayfinder-workspace.v1"},
		"/mcp/v1/wayfinder/discovery":     {"wayfinder", "wayfinder-discovery.v1"},
		"/mcp/v1/wayfinder/investigation": {"wayfinder", "wayfinder-investigation.v1"},
		"/mcp/v1/planner/authoring":       {"planner", "planner-authoring.v1"},
		"/mcp/v1/planner/frontier":        {"planner", "planner-ticket-frontier.v1"},
		"/mcp/v1/auditor/review":          {"auditor", "auditor-review.v1"},
		"/mcp/v1/auditor/audit":           {"auditor", "auditor-audit.v1"},
	}
	expectedMappings := map[string]string{
		"wayfinder-workspace":     "/mcp/v1/wayfinder/workspace",
		"wayfinder-discovery":     "/mcp/v1/wayfinder/discovery",
		"wayfinder-investigation": "/mcp/v1/wayfinder/investigation",
		"planner-authoring":       "/mcp/v1/planner/authoring",
		"planner-frontier":        "/mcp/v1/planner/frontier",
		"auditor-review":          "/mcp/v1/auditor/review",
		"auditor-audit":           "/mcp/v1/auditor/audit",
	}
	expectedStandingPaths := map[string]string{
		"wayfinder": "agents/wayfinder.md",
		"planner":   "agents/planner.md",
		"auditor":   "agents/auditor.md",
	}
	seenRoutes := map[string]struct{}{}
	for index, route := range input.Routes {
		expected, ok := expectedRoutes[route.RoutePath]
		if !ok || route.Sequence != int64(index+1) || route.Role != expected.role || route.SurfaceContractID != expected.surface ||
			!validLowerHex(route.ManifestSHA256, 64) || !validLowerHex(route.AuthorityCommitOID, 40) || !validLowerHex(route.AuthorityBlobOID, 40) {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenRoutes[route.RoutePath]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenRoutes[route.RoutePath] = struct{}{}
	}
	seenMappings := map[string]struct{}{}
	for index, mapping := range input.Mappings {
		expectedRoute, ok := expectedMappings[mapping.MappingID]
		if !ok || mapping.Sequence != int64(index+1) || mapping.RoutePath != expectedRoute ||
			strings.TrimSpace(mapping.ListenerIdentity) == "" || strings.TrimSpace(mapping.UpstreamIdentity) == "" ||
			!validLowerHex(mapping.HealthEvidenceSHA256, 64) || !validLowerHex(mapping.TraceEvidenceSHA256, 64) {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenMappings[mapping.MappingID]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenMappings[mapping.MappingID] = struct{}{}
	}
	seenStanding := map[string]struct{}{}
	for _, authority := range input.StandingAuthorities {
		expectedPath, ok := expectedStandingPaths[authority.Role]
		if !ok || authority.Repository != input.StandingRepository || authority.CommitOID != input.StandingCommitOID ||
			authority.Path != expectedPath || !validLowerHex(authority.BlobOID, 40) || !validLowerHex(authority.ContentSHA256, 64) {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenStanding[authority.Role]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenStanding[authority.Role] = struct{}{}
	}
	expectedDependencies := map[string]int64{
		"CURRENT-MCP-SURFACES":    2,
		"PRIVATE-TRANSPORT-TRACE": 2,
		"STANDING-AUTHORITY":      2,
	}
	seenDependencies := map[string]struct{}{}
	for index, dependency := range input.DependencyOutcomes {
		expectedRevision, ok := expectedDependencies[dependency.TicketID]
		if !ok || dependency.Sequence != int64(index+1) ||
			dependency.TicketRevision != expectedRevision ||
			dependency.Outcome != "completed_accepted" ||
			!validLowerHex(dependency.EvidenceSHA256, 64) {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		if _, duplicate := seenDependencies[dependency.TicketID]; duplicate {
			return GatewayConfigurationIdentity{}, ErrCutoverConfigurationInvalid
		}
		seenDependencies[dependency.TicketID] = struct{}{}
	}
	input.ConfigurationSHA256 = ""
	raw, err := json.Marshal(input)
	if err != nil {
		return GatewayConfigurationIdentity{}, err
	}
	sum := sha256.Sum256(raw)
	input.ConfigurationSHA256 = hex.EncodeToString(sum[:])
	return input, nil
}

func toStoreGatewayConfiguration(value GatewayConfigurationIdentity) workflowstore.CutoverGatewayConfiguration {
	result := workflowstore.CutoverGatewayConfiguration{
		ConfigurationSHA256: value.ConfigurationSHA256,
		RelayRepository:     value.RelayRepository,
		RelayCommitOID:      value.RelayCommitOID,
		StandingRepository:  value.StandingRepository,
		StandingCommitOID:   value.StandingCommitOID,
	}
	for _, route := range value.Routes {
		result.Routes = append(result.Routes, workflowstore.CutoverGatewayRoute{
			Sequence: route.Sequence, RoutePath: route.RoutePath, Role: route.Role,
			SurfaceContractID: route.SurfaceContractID, ManifestSHA256: route.ManifestSHA256,
			AuthorityCommitOID: route.AuthorityCommitOID, AuthorityBlobOID: route.AuthorityBlobOID,
		})
	}
	for _, mapping := range value.Mappings {
		result.Mappings = append(result.Mappings, workflowstore.CutoverGatewayMapping{
			Sequence: mapping.Sequence, MappingID: mapping.MappingID, RoutePath: mapping.RoutePath,
			ListenerIdentity: mapping.ListenerIdentity, UpstreamIdentity: mapping.UpstreamIdentity,
			HealthEvidenceSHA256: mapping.HealthEvidenceSHA256, TraceEvidenceSHA256: mapping.TraceEvidenceSHA256,
		})
	}
	for _, surface := range value.AppSurfaces {
		result.AppSurfaces = append(result.AppSurfaces, workflowstore.CutoverGatewayAppSurface{
			Sequence: surface.Sequence, Surface: surface.Surface, PublicPath: surface.PublicPath, ManifestSHA256: surface.ManifestSHA256,
		})
	}
	for _, membership := range value.RouteMemberships {
		result.RouteMemberships = append(result.RouteMemberships, workflowstore.CutoverGatewayRouteMembership{
			RoutePath: membership.RoutePath, PublicSurface: membership.PublicSurface,
		})
	}
	for _, mapping := range value.AppSurfaceMappings {
		result.AppSurfaceMappings = append(result.AppSurfaceMappings, workflowstore.CutoverGatewayAppSurfaceMapping{
			Sequence: mapping.Sequence, MappingID: mapping.MappingID, PublicSurface: mapping.PublicSurface, PublicPath: mapping.PublicPath,
			ListenerIdentity: mapping.ListenerIdentity, UpstreamIdentity: mapping.UpstreamIdentity,
			HealthEvidenceSHA256: mapping.HealthEvidenceSHA256, TraceEvidenceSHA256: mapping.TraceEvidenceSHA256,
		})
	}
	for _, authority := range value.StandingAuthorities {
		result.StandingAuthorities = append(result.StandingAuthorities, workflowstore.CutoverGatewayStandingAuthority{
			Role: authority.Role, Repository: authority.Repository, CommitOID: authority.CommitOID,
			Path: authority.Path, BlobOID: authority.BlobOID, ContentSHA256: authority.ContentSHA256,
		})
	}
	for _, dependency := range value.DependencyOutcomes {
		result.DependencyOutcomes = append(result.DependencyOutcomes, workflowstore.CutoverGatewayDependencyOutcome{
			Sequence: dependency.Sequence, TicketID: dependency.TicketID, TicketRevision: dependency.TicketRevision,
			Outcome: dependency.Outcome, EvidenceSHA256: dependency.EvidenceSHA256,
		})
	}
	return result
}

func fromStoreGatewayConfiguration(value workflowstore.CutoverGatewayConfiguration) GatewayConfigurationIdentity {
	result := GatewayConfigurationIdentity{
		ConfigurationSHA256: value.ConfigurationSHA256,
		RelayRepository:     value.RelayRepository,
		RelayCommitOID:      value.RelayCommitOID,
		StandingRepository:  value.StandingRepository,
		StandingCommitOID:   value.StandingCommitOID,
	}
	for _, route := range value.Routes {
		result.Routes = append(result.Routes, RouteIdentity{
			Sequence: route.Sequence, RoutePath: route.RoutePath, Role: route.Role,
			SurfaceContractID: route.SurfaceContractID, ManifestSHA256: route.ManifestSHA256,
			AuthorityCommitOID: route.AuthorityCommitOID, AuthorityBlobOID: route.AuthorityBlobOID,
		})
	}
	for _, mapping := range value.Mappings {
		result.Mappings = append(result.Mappings, MappingIdentity{
			Sequence: mapping.Sequence, MappingID: mapping.MappingID, RoutePath: mapping.RoutePath,
			ListenerIdentity: mapping.ListenerIdentity, UpstreamIdentity: mapping.UpstreamIdentity,
			HealthEvidenceSHA256: mapping.HealthEvidenceSHA256, TraceEvidenceSHA256: mapping.TraceEvidenceSHA256,
		})
	}
	for _, surface := range value.AppSurfaces {
		result.AppSurfaces = append(result.AppSurfaces, AppSurfaceIdentity{
			Sequence: surface.Sequence, Surface: surface.Surface, PublicPath: surface.PublicPath, ManifestSHA256: surface.ManifestSHA256,
		})
	}
	for _, membership := range value.RouteMemberships {
		result.RouteMemberships = append(result.RouteMemberships, RouteMembershipIdentity{
			RoutePath: membership.RoutePath, PublicSurface: membership.PublicSurface,
		})
	}
	for _, mapping := range value.AppSurfaceMappings {
		result.AppSurfaceMappings = append(result.AppSurfaceMappings, AppSurfaceMappingIdentity{
			Sequence: mapping.Sequence, MappingID: mapping.MappingID, PublicSurface: mapping.PublicSurface, PublicPath: mapping.PublicPath,
			ListenerIdentity: mapping.ListenerIdentity, UpstreamIdentity: mapping.UpstreamIdentity,
			HealthEvidenceSHA256: mapping.HealthEvidenceSHA256, TraceEvidenceSHA256: mapping.TraceEvidenceSHA256,
		})
	}
	for _, authority := range value.StandingAuthorities {
		result.StandingAuthorities = append(result.StandingAuthorities, StandingAuthorityIdentity{
			Role: authority.Role, Repository: authority.Repository, CommitOID: authority.CommitOID,
			Path: authority.Path, BlobOID: authority.BlobOID, ContentSHA256: authority.ContentSHA256,
		})
	}
	for _, dependency := range value.DependencyOutcomes {
		result.DependencyOutcomes = append(result.DependencyOutcomes, DependencyOutcomeIdentity{
			Sequence: dependency.Sequence, TicketID: dependency.TicketID, TicketRevision: dependency.TicketRevision,
			Outcome: dependency.Outcome, EvidenceSHA256: dependency.EvidenceSHA256,
		})
	}
	if len(result.AppSurfaces) != 0 {
		result.TopologyVersion = appSurfaceTopologyVersion
	}
	return result
}

func validLowerHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func canonicalTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

var _ = errors.Is
