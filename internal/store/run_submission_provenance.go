package store

import (
	"context"
	"database/sql"

	"relay/internal/store/generated"
)

type RunSubmissionProvenance = generated.RunSubmissionProvenance

type CreateRunSubmissionProvenanceParams struct {
	RunID                int64
	PlannerHandoffSha256 string
	PlannerHandoffBytes  int64
	Source               string
	ClientTraceID        string
	SourceArtifactPath   string
	RepoTarget           string
	BranchContext        string
	PlanID               string
	PassID               string
	PlanRowID            sql.NullInt64
	PlanPassRowID        sql.NullInt64
	ManagedPlanPass      string
	ManagedPlanPassName  string
	ContextPacketID      string
	SourceSnapshotID     string
	HandoffMetadataJSON  string
	SubmissionArgsJSON   string
}

func (s *Store) CreateRunSubmissionProvenance(params CreateRunSubmissionProvenanceParams) (*RunSubmissionProvenance, error) {
	row, err := s.queries.CreateRunSubmissionProvenance(context.Background(), generated.CreateRunSubmissionProvenanceParams{
		RunID:                params.RunID,
		PlannerHandoffSha256: params.PlannerHandoffSha256,
		PlannerHandoffBytes:  params.PlannerHandoffBytes,
		Source:               params.Source,
		ClientTraceID:        params.ClientTraceID,
		SourceArtifactPath:   params.SourceArtifactPath,
		RepoTarget:           params.RepoTarget,
		BranchContext:        params.BranchContext,
		PlanID:               params.PlanID,
		PassID:               params.PassID,
		PlanRowID:            params.PlanRowID,
		PlanPassRowID:        params.PlanPassRowID,
		ManagedPlanPass:      params.ManagedPlanPass,
		ManagedPlanPassName:  params.ManagedPlanPassName,
		ContextPacketID:      params.ContextPacketID,
		SourceSnapshotID:     params.SourceSnapshotID,
		HandoffMetadataJson:  params.HandoffMetadataJSON,
		SubmissionArgsJson:   params.SubmissionArgsJSON,
	})
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) GetRunSubmissionProvenanceByRun(runID int64) (*RunSubmissionProvenance, error) {
	row, err := s.queries.GetRunSubmissionProvenanceByRun(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListRunSubmissionProvenanceByPlanPass(planID, passID string) ([]RunSubmissionProvenance, error) {
	return s.queries.ListRunSubmissionProvenanceByPlanPass(context.Background(), generated.ListRunSubmissionProvenanceByPlanPassParams{
		PlanID: planID,
		PassID: passID,
	})
}

func (s *Store) ListRunSubmissionProvenanceByPlan(planID string) ([]RunSubmissionProvenance, error) {
	return s.queries.ListRunSubmissionProvenanceByPlan(context.Background(), planID)
}
