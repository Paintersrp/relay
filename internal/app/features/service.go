package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidAuthorityRequest       = errors.New("invalid feature authority request")
	ErrWorkspaceNotFound             = errors.New("feature workspace not found")
	ErrVersionConflict               = errors.New("feature workspace version conflict")
	ErrFeatureCompletionConfirmation = errors.New("feature completion confirmation is required")
	ErrFeatureCompletionNotReady     = errors.New("feature workspace completion gates are not satisfied")
	ErrFeatureCompletionRecorded     = errors.New("feature workspace completion is already current")
)

type IDGenerator interface {
	AuthorityRevisionID() string
	CompletionDecisionID() string
}
type defaultIDGenerator struct{}

func (defaultIDGenerator) AuthorityRevisionID() string {
	return workflowstore.NewFeatureWorkspaceAuthorityRevisionID()
}
func (defaultIDGenerator) CompletionDecisionID() string {
	return workflowstore.NewFeatureWorkspaceCompletionDecisionID()
}

type Service struct {
	store *workflowstore.Store
	ids   IDGenerator
}

func NewService(store *workflowstore.Store) (*Service, error) {
	return NewServiceWithIDs(store, defaultIDGenerator{})
}
func NewServiceWithIDs(store *workflowstore.Store, ids IDGenerator) (*Service, error) {
	if store == nil || ids == nil {
		return nil, ErrInvalidAuthorityRequest
	}
	return &Service{store: store, ids: ids}, nil
}

type AuthorityLayerInput struct {
	Kind             string
	ArtifactRowID    sql.NullInt64
	RetainedArtifact sql.NullInt64
	ArtifactSHA256   string
	SourceClosureID  sql.NullInt64
}

type PublishAuthorityInput struct {
	WorkspaceID     string
	ExpectedVersion int64
	SourceClosureID sql.NullInt64
	Layers          []AuthorityLayerInput
}

type AuthorityRevisionDetail struct {
	Revision workflowstore.FeatureWorkspaceAuthorityRevision
	Layers   []workflowstore.FeatureWorkspaceAuthorityLayer
}

// PublishAuthority creates immutable replacement history. A workspace may
// deliberately have no authority revision; publication itself requires the
// selected governing layers to be exact, distinct artifacts.
func (s *Service) PublishAuthority(ctx context.Context, input PublishAuthorityInput) (AuthorityRevisionDetail, workflowstore.FeatureWorkspace, error) {
	if strings.TrimSpace(input.WorkspaceID) == "" || input.ExpectedVersion < 1 || len(input.Layers) == 0 || len(input.Layers) > 3 {
		return AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, ErrInvalidAuthorityRequest
	}
	seen := map[string]bool{}
	for _, layer := range input.Layers {
		if !oneOf(layer.Kind, "requirements", "design", "transition_plan") || seen[layer.Kind] || layer.ArtifactRowID.Valid == layer.RetainedArtifact.Valid || !validSHA256(layer.ArtifactSHA256) {
			return AuthorityRevisionDetail{}, workflowstore.FeatureWorkspace{}, ErrInvalidAuthorityRequest
		}
		seen[layer.Kind] = true
	}
	var detail AuthorityRevisionDetail
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
		prior, err := tx.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
		if err != nil {
			return err
		}
		detail.Revision, err = tx.CreateFeatureWorkspaceAuthorityRevision(ctx, workflowstore.CreateFeatureWorkspaceAuthorityRevisionParams{AuthorityRevisionID: s.ids.AuthorityRevisionID(), WorkspaceRowID: workspace.ID, RevisionNumber: int64(len(prior) + 1), SourceClosureRowID: input.SourceClosureID})
		if err != nil {
			return err
		}
		detail.Layers = make([]workflowstore.FeatureWorkspaceAuthorityLayer, 0, len(input.Layers))
		for sequence, layer := range input.Layers {
			created, err := tx.CreateFeatureWorkspaceAuthorityLayer(ctx, workflowstore.CreateFeatureWorkspaceAuthorityLayerParams{AuthorityRevisionRowID: detail.Revision.ID, LayerKind: storageLayerKind(layer.Kind), Sequence: int64(sequence + 1), ArtifactRowID: layer.ArtifactRowID, RetainedArtifactRowID: layer.RetainedArtifact, ArtifactSha256: layer.ArtifactSHA256, SourceClosureRowID: layer.SourceClosureID})
			if err != nil {
				return err
			}
			detail.Layers = append(detail.Layers, applicationLayerKind(created))
		}
		updated, err = tx.SetFeatureWorkspaceAuthorityRevision(ctx, detail.Revision.ID, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return err
		}
		return reopenCurrentFeatureCompletionForAuthority(ctx, tx, updated, detail.Revision)
	})
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrVersionConflict
	}
	return detail, updated, err
}

func (s *Service) ReadAuthority(ctx context.Context, workspaceID string) ([]AuthorityRevisionDetail, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, ErrInvalidAuthorityRequest
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWorkspaceNotFound
	}
	if err != nil {
		return nil, err
	}
	revisions, err := s.store.ListFeatureWorkspaceAuthorityRevisions(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	result := make([]AuthorityRevisionDetail, len(revisions))
	for index, revision := range revisions {
		result[index].Revision = revision
		result[index].Layers, err = s.store.ListFeatureWorkspaceAuthorityLayers(ctx, revision.ID)
		if err != nil {
			return nil, err
		}
		for layerIndex := range result[index].Layers {
			result[index].Layers[layerIndex] = applicationLayerKind(result[index].Layers[layerIndex])
		}
	}
	return result, nil
}

func oneOf(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}

// P3-T1's durable schema predates the route vocabulary and encodes this one
// layer as "plan". Keep that compatibility detail below the application
// boundary; callers only observe the governing transition_plan authority.
func storageLayerKind(kind string) string {
	if kind == "transition_plan" {
		return "plan"
	}
	return kind
}

func applicationLayerKind(layer workflowstore.FeatureWorkspaceAuthorityLayer) workflowstore.FeatureWorkspaceAuthorityLayer {
	if layer.LayerKind == "plan" {
		layer.LayerKind = "transition_plan"
	}
	return layer
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

type CompletionInput struct {
	WorkspaceID       string
	ExpectedVersion   int64
	OperatorConfirmed bool
}

type CompletionGate struct {
	Name  string
	Ready bool
}

type CompletionStatus struct {
	Workspace       workflowstore.FeatureWorkspace
	Gates           []CompletionGate
	CurrentDecision *workflowstore.FeatureWorkspaceCompletionDecision
}

type CompletionResult struct {
	Decision  workflowstore.FeatureWorkspaceCompletionDecision
	Workspace workflowstore.FeatureWorkspace
}

// EvaluateCompletion exposes the current gate matrix without creating a
// completion record. Completion itself remains an explicit confirmed action.
func (s *Service) EvaluateCompletion(ctx context.Context, workspaceID string) (CompletionStatus, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return CompletionStatus{}, ErrInvalidAuthorityRequest
	}
	workspace, err := s.store.GetFeatureWorkspaceByWorkspaceID(ctx, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return CompletionStatus{}, ErrWorkspaceNotFound
	}
	if err != nil {
		return CompletionStatus{}, err
	}
	gates, err := featureCompletionGates(ctx, s.store, workspace)
	if err != nil {
		return CompletionStatus{}, err
	}
	status := CompletionStatus{Workspace: workspace, Gates: gates}
	if decision, err := s.store.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); err == nil {
		status.CurrentDecision = &decision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return CompletionStatus{}, err
	}
	return status, nil
}

func (s *Service) Complete(ctx context.Context, input CompletionInput) (CompletionResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	if input.WorkspaceID == "" || input.ExpectedVersion < 1 {
		return CompletionResult{}, ErrInvalidAuthorityRequest
	}
	if !input.OperatorConfirmed {
		return CompletionResult{}, ErrFeatureCompletionConfirmation
	}
	result := CompletionResult{}
	err := s.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		workspace, err := tx.GetFeatureWorkspaceByWorkspaceID(ctx, input.WorkspaceID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrWorkspaceNotFound
		}
		if err != nil {
			return err
		}
		if workspace.Version != input.ExpectedVersion {
			return ErrVersionConflict
		}
		if _, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID); err == nil {
			return ErrFeatureCompletionRecorded
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		gates, err := featureCompletionGates(ctx, tx, workspace)
		if err != nil {
			return err
		}
		if !completionGatesReady(gates) {
			return ErrFeatureCompletionNotReady
		}
		authority, err := tx.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil || !authority.SourceClosureRowID.Valid {
			return ErrFeatureCompletionNotReady
		}
		decision, err := tx.CreateFeatureWorkspaceCompletionDecision(ctx, workflowstore.CreateFeatureWorkspaceCompletionDecisionParams{
			CompletionDecisionID:   s.ids.CompletionDecisionID(),
			WorkspaceRowID:         workspace.ID,
			AuthorityRevisionRowID: authority.ID,
			SourceClosureRowID:     authority.SourceClosureRowID.Int64,
			Decision:               "completed",
		})
		if err != nil {
			return err
		}
		updated, err := tx.BumpFeatureWorkspaceVersion(ctx, workspace.WorkspaceID, workspace.Version)
		if err != nil {
			return ErrVersionConflict
		}
		result = CompletionResult{Decision: decision, Workspace: updated}
		return nil
	})
	return result, err
}

type featureCompletionReader interface {
	GetFeatureWorkspaceAuthorityRevisionByRowID(context.Context, int64) (workflowstore.FeatureWorkspaceAuthorityRevision, error)
	GetSourceVaultClosureByRowID(context.Context, int64) (workflowstore.SourceVaultClosure, error)
	ListFeatureWorkspaceAuthorityLayers(context.Context, int64) ([]workflowstore.FeatureWorkspaceAuthorityLayer, error)
	ListDeliveryTicketsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicket, error)
	GetDeliveryTicketRevisionByRowID(context.Context, int64) (workflowstore.DeliveryTicketRevision, error)
	GetDeliveryTicketRevisionSatisfaction(context.Context, int64) (workflowstore.DeliveryTicketRevisionSatisfaction, error)
	ListDeliveryTicketSelectionsByWorkspace(context.Context, int64) ([]workflowstore.DeliveryTicketSelection, error)
	ListAuditRemediationSeedsByWorkspace(context.Context, int64) ([]workflowstore.AuditRemediationSeed, error)
	GetAuditRemediationSeedReopening(context.Context, int64) (workflowstore.AuditRemediationSeedReopening, error)
}

func featureCompletionGates(ctx context.Context, reader featureCompletionReader, workspace workflowstore.FeatureWorkspace) ([]CompletionGate, error) {
	authorityReady := workspace.CurrentAuthorityRevisionRowID.Valid
	var authority workflowstore.FeatureWorkspaceAuthorityRevision
	if authorityReady {
		value, err := reader.GetFeatureWorkspaceAuthorityRevisionByRowID(ctx, workspace.CurrentAuthorityRevisionRowID.Int64)
		if err != nil {
			return nil, err
		}
		authority = value
		if !authority.SourceClosureRowID.Valid {
			authorityReady = false
		} else {
			closure, err := reader.GetSourceVaultClosureByRowID(ctx, authority.SourceClosureRowID.Int64)
			if err != nil {
				return nil, err
			}
			authorityReady = authority.WorkspaceRowID == workspace.ID && closure.State == workflowstore.SourceVaultClosureStateReady
		}
	}

	tickets, err := reader.ListDeliveryTicketsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	ticketsReady, auditReady, transitionsReady := true, true, true
	requiresTransition := false
	for _, ticket := range tickets {
		if !ticket.CurrentRevisionRowID.Valid {
			continue
		}
		revision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, ticket.CurrentRevisionRowID.Int64)
		if err != nil {
			return nil, err
		}
		if revision.CancellationReason.Valid {
			continue
		}
		if _, err := reader.GetDeliveryTicketRevisionSatisfaction(ctx, revision.ID); errors.Is(err, sql.ErrNoRows) {
			ticketsReady, auditReady = false, false
		} else if err != nil {
			return nil, err
		}
		if revision.TransitionApplicability == "required" {
			requiresTransition = true
		}
	}
	if requiresTransition && authorityReady {
		layers, err := reader.ListFeatureWorkspaceAuthorityLayers(ctx, authority.ID)
		if err != nil {
			return nil, err
		}
		transitionsReady = false
		for _, layer := range layers {
			if layer.LayerKind == "plan" || layer.LayerKind == "transition_plan" {
				transitionsReady = true
				break
			}
		}
	}
	if requiresTransition && !authorityReady {
		transitionsReady = false
	}

	selections, err := reader.ListDeliveryTicketSelectionsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	integrationReady := true
	for _, selection := range selections {
		if selection.State == "active" {
			integrationReady = false
			break
		}
	}

	remediationReady := true
	seeds, err := reader.ListAuditRemediationSeedsByWorkspace(ctx, workspace.ID)
	if err != nil {
		return nil, err
	}
	for _, seed := range seeds {
		reopening, err := reader.GetAuditRemediationSeedReopening(ctx, seed.ID)
		if errors.Is(err, sql.ErrNoRows) {
			remediationReady = false
			break
		}
		if err != nil || reopening.ReopeningRevisionRowID < 1 {
			if err != nil {
				return nil, err
			}
			remediationReady = false
			break
		}
		remediationRevision, err := reader.GetDeliveryTicketRevisionByRowID(ctx, reopening.ReopeningRevisionRowID)
		if err != nil {
			return nil, err
		}
		if remediationRevision.CancellationReason.Valid {
			remediationReady = false
			break
		}
	}
	return []CompletionGate{
		{Name: "authority", Ready: authorityReady},
		{Name: "tickets", Ready: ticketsReady},
		{Name: "integration", Ready: integrationReady},
		{Name: "transitions", Ready: transitionsReady},
		{Name: "remediation", Ready: remediationReady},
		{Name: "audit", Ready: auditReady},
	}, nil
}

func completionGatesReady(gates []CompletionGate) bool {
	for _, gate := range gates {
		if !gate.Ready {
			return false
		}
	}
	return true
}

func reopenCurrentFeatureCompletionForAuthority(
	ctx context.Context,
	tx *workflowstore.Tx,
	workspace workflowstore.FeatureWorkspace,
	authority workflowstore.FeatureWorkspaceAuthorityRevision,
) error {
	completion, err := tx.GetCurrentFeatureWorkspaceCompletionDecision(ctx, workspace.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = tx.CreateFeatureWorkspaceCompletionReopening(ctx, workflowstore.CreateFeatureWorkspaceCompletionReopeningParams{
		CompletionDecisionRowID:         completion.ID,
		ReopeningKind:                   "authority_revision",
		ReopeningAuthorityRevisionRowID: sql.NullInt64{Int64: authority.ID, Valid: true},
	})
	return err
}
