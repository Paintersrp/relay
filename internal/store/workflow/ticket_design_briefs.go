package workflowstore

import (
	"context"
	"fmt"

	workflowartifacts "relay/internal/artifacts/workflow"
	workflowgenerated "relay/internal/store/workflowgenerated"
)

// Generated persistence types remain behind the workflow store boundary.
type (
	TicketDesignBrief         = workflowgenerated.TicketDesignBrief
	TicketDesignBriefApproval = workflowgenerated.TicketDesignBriefApproval

	CreateTicketDesignBriefParams         = workflowgenerated.CreateTicketDesignBriefParams
	CreateTicketDesignBriefApprovalParams = workflowgenerated.CreateTicketDesignBriefApprovalParams
	SetCurrentTicketDesignBriefParams     = workflowgenerated.SetCurrentTicketDesignBriefParams
)

func (s *Store) CreateTicketDesignBrief(ctx context.Context, params CreateTicketDesignBriefParams) (TicketDesignBrief, error) {
	return workflowgenerated.New(s.db).CreateTicketDesignBrief(ctx, params)
}

func (s *Store) GetTicketDesignBriefByBriefID(ctx context.Context, briefID string) (TicketDesignBrief, error) {
	return workflowgenerated.New(s.db).GetTicketDesignBriefByBriefID(ctx, briefID)
}

func (s *Store) GetTicketDesignBriefByRowID(ctx context.Context, rowID int64) (TicketDesignBrief, error) {
	return workflowgenerated.New(s.db).GetTicketDesignBriefByRowID(ctx, rowID)
}

func (s *Store) GetCurrentTicketDesignBriefBySelectionRowID(ctx context.Context, selectionRowID int64) (TicketDesignBrief, error) {
	return workflowgenerated.New(s.db).GetCurrentTicketDesignBriefBySelectionRowID(ctx, selectionRowID)
}

func (s *Store) ListTicketDesignBriefsByWorkspace(ctx context.Context, workspaceRowID int64) ([]TicketDesignBrief, error) {
	return workflowgenerated.New(s.db).ListTicketDesignBriefsByWorkspace(ctx, workspaceRowID)
}

func (tx *Tx) CreateTicketDesignBrief(ctx context.Context, params CreateTicketDesignBriefParams) (TicketDesignBrief, error) {
	return workflowgenerated.New(tx.tx).CreateTicketDesignBrief(ctx, params)
}

func (tx *Tx) GetTicketDesignBriefByBriefID(ctx context.Context, briefID string) (TicketDesignBrief, error) {
	return workflowgenerated.New(tx.tx).GetTicketDesignBriefByBriefID(ctx, briefID)
}

func (tx *Tx) GetTicketDesignBriefByRowID(ctx context.Context, rowID int64) (TicketDesignBrief, error) {
	return workflowgenerated.New(tx.tx).GetTicketDesignBriefByRowID(ctx, rowID)
}

func (tx *Tx) GetCurrentTicketDesignBriefBySelectionRowID(ctx context.Context, selectionRowID int64) (TicketDesignBrief, error) {
	return workflowgenerated.New(tx.tx).GetCurrentTicketDesignBriefBySelectionRowID(ctx, selectionRowID)
}

func (tx *Tx) SetCurrentTicketDesignBrief(ctx context.Context, params SetCurrentTicketDesignBriefParams) (DeliveryTicketSelection, error) {
	return workflowgenerated.New(tx.tx).SetCurrentTicketDesignBrief(ctx, params)
}

func (tx *Tx) ListTicketDesignBriefsByWorkspace(ctx context.Context, workspaceRowID int64) ([]TicketDesignBrief, error) {
	return workflowgenerated.New(tx.tx).ListTicketDesignBriefsByWorkspace(ctx, workspaceRowID)
}

func (s *Store) CreateTicketDesignBriefApproval(ctx context.Context, params CreateTicketDesignBriefApprovalParams) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(s.db).CreateTicketDesignBriefApproval(ctx, params)
}

func (s *Store) GetTicketDesignBriefApprovalByApprovalID(ctx context.Context, approvalID string) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(s.db).GetTicketDesignBriefApprovalByApprovalID(ctx, approvalID)
}

func (s *Store) GetTicketDesignBriefApprovalByRowID(ctx context.Context, rowID int64) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(s.db).GetTicketDesignBriefApprovalByRowID(ctx, rowID)
}

func (s *Store) GetTicketDesignBriefApprovalByBriefRowID(ctx context.Context, briefRowID int64) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(s.db).GetTicketDesignBriefApprovalByBriefRowID(ctx, briefRowID)
}

func (tx *Tx) CreateTicketDesignBriefApproval(ctx context.Context, params CreateTicketDesignBriefApprovalParams) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(tx.tx).CreateTicketDesignBriefApproval(ctx, params)
}

func (tx *Tx) GetTicketDesignBriefApprovalByApprovalID(ctx context.Context, approvalID string) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(tx.tx).GetTicketDesignBriefApprovalByApprovalID(ctx, approvalID)
}

func (tx *Tx) GetTicketDesignBriefApprovalByRowID(ctx context.Context, rowID int64) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(tx.tx).GetTicketDesignBriefApprovalByRowID(ctx, rowID)
}

func (tx *Tx) GetTicketDesignBriefApprovalByBriefRowID(ctx context.Context, briefRowID int64) (TicketDesignBriefApproval, error) {
	return workflowgenerated.New(tx.tx).GetTicketDesignBriefApprovalByBriefRowID(ctx, briefRowID)
}

// ReadTicketDesignBriefBytes verifies the durable artifact path, digest, and
// size before returning the exact authored brief bytes. The caller supplies a
// bound maximum to avoid unbounded artifact reads.
func (s *Store) ReadTicketDesignBriefBytes(ctx context.Context, briefID string, maxBytes int) ([]byte, error) {
	brief, err := s.GetTicketDesignBriefByBriefID(ctx, briefID)
	if err != nil {
		return nil, err
	}
	artifact, err := getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, s.db, brief.ArtifactRowID)
	if err != nil {
		return nil, err
	}
	return readTicketDesignBriefArtifact(ctx, brief, artifact, s.artifacts, maxBytes)
}

func (tx *Tx) ReadTicketDesignBriefBytes(ctx context.Context, briefID string, maxBytes int) ([]byte, error) {
	brief, err := tx.GetTicketDesignBriefByBriefID(ctx, briefID)
	if err != nil {
		return nil, err
	}
	artifact, err := getFeatureWorkspaceDiscoveryArtifactByRowID(ctx, tx.tx, brief.ArtifactRowID)
	if err != nil {
		return nil, err
	}
	return readTicketDesignBriefArtifact(ctx, brief, artifact, tx.artifacts, maxBytes)
}

func readTicketDesignBriefArtifact(_ context.Context, brief TicketDesignBrief, artifact FeatureWorkspaceDiscoveryArtifact, artifactStore *workflowartifacts.Store, maxBytes int) ([]byte, error) {
	if artifactStore == nil {
		return nil, fmt.Errorf("workflow artifact store is required")
	}
	if artifact.ID != brief.ArtifactRowID || artifact.WorkspaceRowID != brief.WorkspaceRowID || artifact.Sha256 != brief.ArtifactSha256 || artifact.SizeBytes != brief.ArtifactSizeBytes {
		return nil, fmt.Errorf("ticket design brief artifact metadata does not match")
	}
	_, data, err := artifactStore.ReadVerifiedFile(workflowartifacts.File{
		Kind:         "ticket_design_brief",
		RelativePath: artifact.RelativePath,
		SHA256:       brief.ArtifactSha256,
		SizeBytes:    brief.ArtifactSizeBytes,
	}, maxBytes)
	return data, err
}
