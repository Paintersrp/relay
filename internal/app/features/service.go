package features

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	workflowstore "relay/internal/store/workflow"
)

var (
	ErrInvalidAuthorityRequest = errors.New("invalid feature authority request")
	ErrWorkspaceNotFound       = errors.New("feature workspace not found")
	ErrVersionConflict         = errors.New("feature workspace version conflict")
)

type IDGenerator interface{ AuthorityRevisionID() string }
type defaultIDGenerator struct{}

func (defaultIDGenerator) AuthorityRevisionID() string {
	return workflowstore.NewFeatureWorkspaceAuthorityRevisionID()
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
		return err
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
