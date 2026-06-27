package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"relay/internal/store"
	"relay/internal/store/generated"
)

func (svc *Service) CreatePlanAttemptWithIntent(ctx context.Context, req CreatePlanAttemptWithIntentRequest) (*PlanAttemptResult, error) {
	project, err := svc.lookupAttemptProject(req.ProjectID)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return blockAttempt(BlockerUnknownProject, "project is unknown")
	}
	policy, blocked, err := svc.ResolvePlanReviewPolicy(ctx, project.ProjectID, req.DriftReviewMode, req.ModelTier)
	if blocked != nil || err != nil {
		return blocked, err
	}
	tx, err := svc.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create plan attempt transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	result, err := svc.CreatePlanAttemptWithIntentInTxWithPolicy(ctx, tx, *project, req, policy)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create plan attempt: %w", err)
	}
	committed = true
	return result, nil
}

// CreatePlanAttemptWithIntentInTx creates the intent packet and draft plan
// attempt using the caller's transaction. Callers are responsible for committing
// or rolling back the transaction.
func (svc *Service) CreatePlanAttemptWithIntentInTx(ctx context.Context, tx *sql.Tx, project store.Project, req CreatePlanAttemptWithIntentRequest) (*PlanAttemptResult, error) {
	policy, blocked, err := svc.ResolvePlanReviewPolicy(ctx, project.ProjectID, req.DriftReviewMode, req.ModelTier)
	if blocked != nil || err != nil {
		return blocked, err
	}
	return svc.CreatePlanAttemptWithIntentInTxWithPolicy(ctx, tx, project, req, policy)
}

// CreatePlanAttemptWithIntentInTxWithPolicy is the transaction helper variant
// for callers that resolved review policy before opening the outer transaction.
func (svc *Service) CreatePlanAttemptWithIntentInTxWithPolicy(ctx context.Context, tx *sql.Tx, project store.Project, req CreatePlanAttemptWithIntentRequest, policy *EffectivePlanReviewPolicy) (*PlanAttemptResult, error) {
	if policy == nil {
		return blockAttempt(BlockerDriftReviewBlocked, "plan review policy is unavailable")
	}
	canonical, rawHash, err := validateAttemptPlanInput(req.RawPlanJSON, req.PlanArtifactRef, req.OptionalMarkdownRef)
	if err != nil {
		return blockAttempt(BlockerMissingPlanArtifact, err.Error())
	}
	if err := checkIntentPacketInputForSecrets(req.IntentPacket); err != nil {
		return blockAttempt(BlockerUnsafeRetrieval, err.Error())
	}

	slug := generateSlug()
	intentPacketID := strings.TrimSpace(req.IntentPacketID)
	if intentPacketID == "" {
		intentPacketID = newIntentPacketID(slug)
	}
	intentThreadID := strings.TrimSpace(req.IntentThreadID)
	if intentThreadID == "" {
		intentThreadID = newIntentThreadID(slug)
	}
	planAttemptID := strings.TrimSpace(req.PlanAttemptID)
	if planAttemptID == "" {
		planAttemptID = newPlanAttemptID(slug)
	}
	constraints, err := constraintsJSON(req.IntentPacket.Constraints)
	if err != nil {
		return nil, err
	}
	contentHash := strings.TrimSpace(req.IntentPacket.ContentHash)
	if contentHash == "" {
		contentHash = sha256Bytes([]byte(req.IntentPacket.Summary + "\n" + req.IntentPacket.LiteralUserRequest + "\n" + constraints))
	}
	redactionStatus := normalizeRedactionStatus(req.IntentPacket.RedactionStatus)

	q := generated.New(tx)
	intent, err := q.CreateIntentPacket(ctx, generated.CreateIntentPacketParams{
		IntentPacketID:          intentPacketID,
		ProjectRowID:            project.ID,
		ProjectID:               project.ProjectID,
		IntentThreadID:          intentThreadID,
		RootIntentPacketID:      intentPacketID,
		ParentIntentPacketID:    sql.NullString{},
		RevisionOfPlanAttemptID: sql.NullString{},
		Kind:                    IntentKindOriginal,
		CapturedFrom:            normalizeCapturedFrom(req.IntentPacket.Source.CapturedFrom, CapturedFromPlannerChat),
		CapturedBy:              req.IntentPacket.Source.CapturedBy,
		SourceArtifactPath:      req.IntentPacket.Source.SourceArtifactPath,
		Summary:                 req.IntentPacket.Summary,
		LiteralUserRequest:      req.IntentPacket.LiteralUserRequest,
		ConstraintsJson:         constraints,
		RedactionStatus:         redactionStatus,
		ContentHash:             contentHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create intent packet: %w", err)
	}
	attempt, err := q.CreatePlanAttempt(ctx, generated.CreatePlanAttemptParams{
		PlanAttemptID:              planAttemptID,
		ProjectRowID:               project.ID,
		ProjectID:                  project.ProjectID,
		IntentThreadID:             intentThreadID,
		RootIntentPacketID:         intentPacketID,
		CurrentIntentPacketID:      intentPacketID,
		SupersedesPlanAttemptID:    sql.NullString{},
		ReplacementPlanAttemptID:   sql.NullString{},
		Status:                     PlanAttemptStatusDraft,
		ReviewState:                PlanAttemptReviewPacketReady,
		DriftReviewMode:            policy.DriftReviewMode,
		ModelTier:                  policy.ModelTier,
		PlanJsonArtifactPath:       req.PlanArtifactRef.Path,
		PlanJsonArtifactSha256:     req.PlanArtifactRef.SHA256,
		RawPlanJson:                string(canonical),
		RawPlanJsonHash:            rawHash,
		PlanMarkdownArtifactPath:   optionalArtifactPath(req.OptionalMarkdownRef),
		PlanMarkdownArtifactSha256: optionalArtifactHash(req.OptionalMarkdownRef),
	})
	if err != nil {
		return nil, fmt.Errorf("create plan attempt: %w", err)
	}
	return &PlanAttemptResult{
		OK:           true,
		IntentPacket: &intent,
		PlanAttempt:  &attempt,
		ReviewPolicy: policy,
		ReviewAction: initialReviewAction(policy.DriftReviewMode),
	}, nil
}

func (svc *Service) GetPlanIntentReviewPacket(ctx context.Context, req GetPlanIntentReviewPacketRequest) (*PlanAttemptResult, error) {
	project, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if attempt.Status != PlanAttemptStatusDraft {
		return blockAttempt(BlockerAttemptNotReviewable, "plan attempt is not a draft")
	}
	packet, blocked, err := svc.buildPlanIntentReviewPacket(ctx, *project, attempt)
	if blocked != nil || err != nil {
		return blocked, err
	}
	return &PlanAttemptResult{OK: true, PlanAttempt: &attempt, ReviewPacket: packet}, nil
}

func (svc *Service) buildPlanIntentReviewPacket(ctx context.Context, project store.Project, attempt store.PlanAttempt) (*PlanIntentReviewPacket, *PlanAttemptResult, error) {
	if _, _, err := verifyStoredAttemptPlan(attempt); err != nil {
		blocked, blockErr := blockAttempt(BlockerArtifactHashMismatch, err.Error())
		return nil, blocked, blockErr
	}
	q := generated.New(svc.store.DB())
	root, err := q.GetIntentPacketByIDAndProject(ctx, generated.GetIntentPacketByIDAndProjectParams{
		IntentPacketID: attempt.RootIntentPacketID,
		ProjectRowID:   project.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			blocked, blockErr := blockAttempt(BlockerMissingIntentPacket, "root intent packet is missing")
			return nil, blocked, blockErr
		}
		return nil, nil, err
	}
	reviewed, err := q.GetIntentPacketByIDAndProject(ctx, generated.GetIntentPacketByIDAndProjectParams{
		IntentPacketID: attempt.CurrentIntentPacketID,
		ProjectRowID:   project.ID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			blocked, blockErr := blockAttempt(BlockerMissingIntentPacket, "reviewed intent packet is missing")
			return nil, blocked, blockErr
		}
		return nil, nil, err
	}
	attempts, err := q.ListPlanAttemptsByThread(ctx, generated.ListPlanAttemptsByThreadParams{
		ProjectRowID:   project.ID,
		IntentThreadID: attempt.IntentThreadID,
	})
	if err != nil {
		return nil, nil, err
	}
	reviews, err := q.ListIntentDriftReviewsByThread(ctx, generated.ListIntentDriftReviewsByThreadParams{
		ProjectRowID:   project.ID,
		IntentThreadID: attempt.IntentThreadID,
	})
	if err != nil {
		return nil, nil, err
	}

	packet := PlanIntentReviewPacket{
		PacketID:              newReviewPacketID(sanitizeSlug(attempt.PlanAttemptID)),
		ProjectID:             project.ProjectID,
		PlanAttemptID:         attempt.PlanAttemptID,
		IntentThreadID:        attempt.IntentThreadID,
		RootIntentPacket:      intentPacketEvidence(root),
		ReviewedIntentPacket:  intentPacketEvidence(reviewed),
		PlanAttempt:           planAttemptEvidence(attempt),
		RawPlanJSON:           json.RawMessage(attempt.RawPlanJson),
		PlanArtifacts:         planArtifactsEvidence(attempt),
		PriorAttemptSummaries: priorAttemptSummaries(attempts, attempt.PlanAttemptID),
		PriorReviewSummaries:  priorReviewSummaries(reviews),
		RedactionStatus:       reviewed.RedactionStatus,
		RetrievalSemantics:    RetrievalSemantics{RetrievalOnly: true, ModelCallPerformed: false, StateMutated: false},
		GeneratedAt:           time.Now().UTC().Format(time.RFC3339),
	}
	packet.PacketHash = hashReviewPacket(packet)
	return &packet, nil, nil
}

func (svc *Service) SubmitIntentDriftReview(ctx context.Context, req SubmitIntentDriftReviewRequest) (*PlanAttemptResult, error) {
	project, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if attempt.Status != PlanAttemptStatusDraft {
		return blockAttempt(BlockerAttemptNotReviewable, "plan attempt is not a draft")
	}
	input := req.DriftReview
	if input.PlanAttemptID != attempt.PlanAttemptID ||
		input.IntentThreadID != attempt.IntentThreadID ||
		input.RootIntentPacketID != attempt.RootIntentPacketID ||
		input.ReviewedIntentPacketID != attempt.CurrentIntentPacketID {
		return blockAttempt(BlockerStaleAttempt, "drift review does not match current attempt lineage")
	}
	if err := validateDriftReviewInput(input); err != nil {
		return blockAttempt(BlockerDriftReviewBlocked, err.Error())
	}
	packet, blocked, err := svc.buildPlanIntentReviewPacket(ctx, *project, attempt)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if input.ReviewPacketHash != packet.PacketHash {
		return blockAttempt(BlockerStaleAttempt, "drift review packet hash does not match current plan intent review packet")
	}
	reviewID := strings.TrimSpace(input.IntentDriftReviewID)
	if reviewID == "" {
		reviewID = newIntentDriftReviewID(generateSlug())
	}
	reviewState := PlanAttemptReviewExternalSubmitted
	if input.ReviewSource == ReviewSourceInternal {
		reviewState = PlanAttemptReviewInternalGenerated
	}
	tx, err := svc.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin drift review transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := generated.New(tx)
	review, err := q.CreateIntentDriftReview(ctx, generated.CreateIntentDriftReviewParams{
		IntentDriftReviewID:    reviewID,
		ProjectRowID:           project.ID,
		ProjectID:              project.ProjectID,
		PlanAttemptRowID:       attempt.ID,
		PlanAttemptID:          attempt.PlanAttemptID,
		IntentThreadID:         attempt.IntentThreadID,
		RootIntentPacketID:     attempt.RootIntentPacketID,
		ReviewedIntentPacketID: attempt.CurrentIntentPacketID,
		ReviewPacketHash:       input.ReviewPacketHash,
		ReviewSource:           input.ReviewSource,
		SubmittedBy:            input.SubmittedBy,
		SourceArtifactPath:     input.SourceArtifactPath,
		OverallAlignment:       input.OverallAlignment,
		Confidence:             input.Confidence,
		FindingsJson:           string(input.FindingsJSON),
		RecommendedAction:      input.RecommendedAction,
		ApprovalGateStatus:     input.ApprovalGateStatus,
		ModelMetadataJson:      rawMessageNullString(input.ModelMetadataJSON),
		InputHash:              input.InputHash,
		OutputHash:             input.OutputHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create intent drift review: %w", err)
	}
	updated, err := q.UpdatePlanAttemptReviewState(ctx, generated.UpdatePlanAttemptReviewStateParams{
		ReviewState: reviewState,
		ID:          attempt.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("update plan attempt review state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit drift review: %w", err)
	}
	committed = true
	return &PlanAttemptResult{OK: true, PlanAttempt: &updated, DriftReview: &review}, nil
}

func (svc *Service) RevisePlanAttempt(ctx context.Context, req RevisePlanAttemptRequest) (*PlanAttemptResult, error) {
	project, oldAttempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if oldAttempt.Status != PlanAttemptStatusDraft {
		return blockAttempt(BlockerAttemptNotReviewable, "only draft attempts can be revised")
	}
	canonical, rawHash, err := validateAttemptPlanInput(req.RawPlanJSON, req.PlanArtifactRef, req.OptionalMarkdownRef)
	if err != nil {
		return blockAttempt(BlockerMissingPlanArtifact, err.Error())
	}
	if err := checkIntentPacketInputForSecrets(req.NewIntentPacket); err != nil {
		return blockAttempt(BlockerUnsafeRetrieval, err.Error())
	}
	slug := generateSlug()
	newAttemptID := strings.TrimSpace(req.NewPlanAttemptID)
	if newAttemptID == "" {
		newAttemptID = newPlanAttemptID(slug)
	}
	revisionIntentPacketID := strings.TrimSpace(req.NewIntentPacketID)
	if revisionIntentPacketID == "" {
		revisionIntentPacketID = newIntentPacketID(slug)
	}
	constraints, err := constraintsJSON(req.NewIntentPacket.Constraints)
	if err != nil {
		return nil, err
	}
	contentHash := strings.TrimSpace(req.NewIntentPacket.ContentHash)
	if contentHash == "" {
		contentHash = sha256Bytes([]byte(req.NewIntentPacket.Summary + "\n" + req.NewIntentPacket.LiteralUserRequest + "\n" + constraints))
	}
	tx, err := svc.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin revise plan attempt transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	q := generated.New(tx)
	intent, err := q.CreateIntentPacket(ctx, generated.CreateIntentPacketParams{
		IntentPacketID:          revisionIntentPacketID,
		ProjectRowID:            project.ID,
		ProjectID:               project.ProjectID,
		IntentThreadID:          oldAttempt.IntentThreadID,
		RootIntentPacketID:      oldAttempt.RootIntentPacketID,
		ParentIntentPacketID:    sql.NullString{String: oldAttempt.CurrentIntentPacketID, Valid: true},
		RevisionOfPlanAttemptID: sql.NullString{String: oldAttempt.PlanAttemptID, Valid: true},
		Kind:                    IntentKindRevision,
		CapturedFrom:            normalizeCapturedFrom(req.NewIntentPacket.Source.CapturedFrom, CapturedFromRevisionNotes),
		CapturedBy:              req.NewIntentPacket.Source.CapturedBy,
		SourceArtifactPath:      req.NewIntentPacket.Source.SourceArtifactPath,
		Summary:                 req.NewIntentPacket.Summary,
		LiteralUserRequest:      req.NewIntentPacket.LiteralUserRequest,
		ConstraintsJson:         constraints,
		RedactionStatus:         normalizeRedactionStatus(req.NewIntentPacket.RedactionStatus),
		ContentHash:             contentHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create revision intent packet: %w", err)
	}
	newAttempt, err := q.CreatePlanAttempt(ctx, generated.CreatePlanAttemptParams{
		PlanAttemptID:              newAttemptID,
		ProjectRowID:               project.ID,
		ProjectID:                  project.ProjectID,
		IntentThreadID:             oldAttempt.IntentThreadID,
		RootIntentPacketID:         oldAttempt.RootIntentPacketID,
		CurrentIntentPacketID:      intent.IntentPacketID,
		SupersedesPlanAttemptID:    sql.NullString{String: oldAttempt.PlanAttemptID, Valid: true},
		ReplacementPlanAttemptID:   sql.NullString{},
		Status:                     PlanAttemptStatusDraft,
		ReviewState:                PlanAttemptReviewPacketReady,
		DriftReviewMode:            oldAttempt.DriftReviewMode,
		ModelTier:                  oldAttempt.ModelTier,
		PlanJsonArtifactPath:       req.PlanArtifactRef.Path,
		PlanJsonArtifactSha256:     req.PlanArtifactRef.SHA256,
		RawPlanJson:                string(canonical),
		RawPlanJsonHash:            rawHash,
		PlanMarkdownArtifactPath:   optionalArtifactPath(req.OptionalMarkdownRef),
		PlanMarkdownArtifactSha256: optionalArtifactHash(req.OptionalMarkdownRef),
	})
	if err != nil {
		return nil, fmt.Errorf("create replacement plan attempt: %w", err)
	}
	if _, err := q.MarkPlanAttemptSuperseded(ctx, generated.MarkPlanAttemptSupersededParams{
		ReplacementPlanAttemptID: sql.NullString{String: newAttempt.PlanAttemptID, Valid: true},
		ID:                       oldAttempt.ID,
	}); err != nil {
		return nil, fmt.Errorf("mark plan attempt superseded: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit revise plan attempt: %w", err)
	}
	committed = true
	return &PlanAttemptResult{OK: true, IntentPacket: &intent, PlanAttempt: &newAttempt}, nil
}

func (svc *Service) VoidPlanAttempt(ctx context.Context, req VoidPlanAttemptRequest) (*PlanAttemptResult, error) {
	_, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if attempt.Status != PlanAttemptStatusDraft {
		return blockAttempt(BlockerAttemptNotReviewable, "only draft attempts can be voided")
	}
	updated, err := generated.New(svc.store.DB()).VoidPlanAttempt(ctx, attempt.ID)
	if err != nil {
		return nil, fmt.Errorf("void plan attempt: %w", err)
	}
	return &PlanAttemptResult{OK: true, PlanAttempt: &updated}, nil
}

func (svc *Service) ApprovePlanAttempt(ctx context.Context, req ApprovePlanAttemptRequest) (*PlanAttemptResult, error) {
	if !req.Approved {
		return blockAttempt(BlockerApprovalRequired, "explicit approval is required")
	}
	project, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if attempt.Status != PlanAttemptStatusDraft {
		return blockAttempt(BlockerApprovalRequired, "only draft attempts can be approved")
	}
	if _, _, err := verifyStoredAttemptPlan(attempt); err != nil {
		return blockAttempt(BlockerArtifactHashMismatch, err.Error())
	}
	q := generated.New(svc.store.DB())
	review, hasReview, blocked, err := svc.resolveApprovalReview(ctx, q, attempt, req.AcceptedDriftReviewID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if blocked, err := svc.approvalGateBlocker(ctx, *project, attempt, review, hasReview, req); blocked != nil || err != nil {
		return blocked, err
	}
	acceptedID := sql.NullString{}
	if hasReview {
		acceptedID = sql.NullString{String: review.IntentDriftReviewID, Valid: true}
	}
	updated, err := q.ApprovePlanAttempt(ctx, generated.ApprovePlanAttemptParams{
		AcceptedDriftReviewID: acceptedID,
		ID:                    attempt.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("approve plan attempt: %w", err)
	}
	return &PlanAttemptResult{OK: true, PlanAttempt: &updated}, nil
}

func (svc *Service) SubmitPlanAttempt(ctx context.Context, req SubmitPlanAttemptRequest) (*PlanAttemptResult, error) {
	_, attempt, blocked, err := svc.loadProjectAttempt(ctx, req.ProjectID, req.PlanAttemptID)
	if blocked != nil || err != nil {
		return blocked, err
	}
	if attempt.Status != PlanAttemptStatusApproved {
		return blockAttempt(BlockerApprovalRequired, "plan attempt must be approved before submission")
	}
	canonical, _, err := verifyStoredAttemptPlan(attempt)
	if err != nil {
		return blockAttempt(BlockerArtifactHashMismatch, err.Error())
	}

	// Contract-required submit gates (C4 / PASS-003A)
	if !req.SubmissionConfirmed {
		return blockAttempt(BlockerApprovalRequired, "explicit submission confirmation is required")
	}
	if strings.TrimSpace(req.ReviewedPlanJSONArtifactSHA256) == "" {
		return blockAttempt(BlockerMissingPlanArtifact, "reviewed_plan_json_artifact_sha256 is required")
	}
	if req.ReviewedPlanJSONArtifactSHA256 != attempt.PlanJsonArtifactSha256 {
		return blockAttempt(BlockerArtifactHashMismatch, "reviewed_plan_json_artifact_sha256 does not match approved attempt artifact hash")
	}
	if strings.TrimSpace(req.AcceptedDriftReviewID) != "" {
		if !attempt.AcceptedDriftReviewID.Valid || req.AcceptedDriftReviewID != attempt.AcceptedDriftReviewID.String {
			return blockAttempt(BlockerStaleAttempt, "accepted_drift_review_id does not match approved attempt")
		}
	}

	lineage := planSubmissionLineage{
		SubmittedPlanAttemptID:  sql.NullString{String: attempt.PlanAttemptID, Valid: true},
		IntentThreadID:          sql.NullString{String: attempt.IntentThreadID, Valid: true},
		RootIntentPacketID:      sql.NullString{String: attempt.RootIntentPacketID, Valid: true},
		SubmittedIntentPacketID: sql.NullString{String: attempt.CurrentIntentPacketID, Valid: true},
		AcceptedDriftReviewID:   attempt.AcceptedDriftReviewID,
	}
	var submitted store.PlanAttempt
	result, err := svc.submitPlan(ctx, canonical, attempt.PlanJsonArtifactPath, req.ProjectID, lineage, func(q *generated.Queries, plan store.Plan) (*store.PlanAttempt, error) {
		updated, err := q.MarkPlanAttemptSubmitted(ctx, generated.MarkPlanAttemptSubmittedParams{
			SubmittedPlanRowID: sql.NullInt64{Int64: plan.ID, Valid: true},
			SubmittedPlanID:    sql.NullString{String: plan.PlanID, Valid: true},
			ID:                 attempt.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("mark plan attempt submitted: %w", err)
		}
		submitted = updated
		return &updated, nil
	})
	if err != nil {
		return nil, err
	}
	if !result.Report.Valid {
		return &PlanAttemptResult{OK: false, BlockerCode: BlockerApprovalRequired, Message: "stored plan JSON is not valid for submission"}, nil
	}
	return &PlanAttemptResult{OK: true, PlanAttempt: &submitted, Plan: &result.Plan, Passes: result.Passes}, nil
}

func (svc *Service) lookupAttemptProject(projectID string) (*store.Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, nil
	}
	project, err := svc.store.GetProjectByProjectID(projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup project %q: %w", projectID, err)
	}
	return project, nil
}

func (svc *Service) loadProjectAttempt(ctx context.Context, projectID string, planAttemptID string) (*store.Project, store.PlanAttempt, *PlanAttemptResult, error) {
	project, err := svc.lookupAttemptProject(projectID)
	if err != nil {
		return nil, store.PlanAttempt{}, nil, err
	}
	if project == nil {
		blocked, err := blockAttempt(BlockerUnknownProject, "project is unknown")
		return nil, store.PlanAttempt{}, blocked, err
	}
	attempt, err := generated.New(svc.store.DB()).GetPlanAttemptForProject(ctx, generated.GetPlanAttemptForProjectParams{
		ProjectRowID:  project.ID,
		PlanAttemptID: strings.TrimSpace(planAttemptID),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			blocked, err := blockAttempt(BlockerUnknownAttempt, "plan attempt is unknown for project")
			return project, store.PlanAttempt{}, blocked, err
		}
		return nil, store.PlanAttempt{}, nil, fmt.Errorf("lookup plan attempt: %w", err)
	}
	return project, attempt, nil, nil
}

func validateAttemptPlanInput(raw json.RawMessage, jsonRef PlanArtifactRef, markdownRef *PlanArtifactRef) ([]byte, string, error) {
	if len(raw) == 0 {
		return nil, "", fmt.Errorf("raw plan JSON is required")
	}
	if len(raw) > MaxRawPlanJSONSize {
		return nil, "", fmt.Errorf("raw plan JSON exceeds %d bytes", MaxRawPlanJSONSize)
	}
	canonical, hash, err := canonicalRawPlanJSON(raw)
	if err != nil {
		return nil, "", err
	}
	if err := validatePlanJSONArtifact(jsonRef, hash); err != nil {
		return nil, "", err
	}
	if err := validateOptionalMarkdownArtifact(markdownRef); err != nil {
		return nil, "", err
	}
	return canonical, hash, nil
}

func verifyStoredAttemptPlan(attempt store.PlanAttempt) ([]byte, string, error) {
	canonical, hash, err := canonicalRawPlanJSON(json.RawMessage(attempt.RawPlanJson))
	if err != nil {
		return nil, "", err
	}
	if hash != attempt.RawPlanJsonHash {
		return nil, "", fmt.Errorf("stored raw plan hash mismatch: expected %s, got %s", attempt.RawPlanJsonHash, hash)
	}
	if hash != attempt.PlanJsonArtifactSha256 {
		return nil, "", fmt.Errorf("plan artifact hash mismatch: expected %s, got %s", attempt.PlanJsonArtifactSha256, hash)
	}
	return canonical, hash, nil
}

func optionalArtifactPath(ref *PlanArtifactRef) sql.NullString {
	if ref == nil || ref.Path == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: ref.Path, Valid: true}
}

func optionalArtifactHash(ref *PlanArtifactRef) sql.NullString {
	if ref == nil || ref.SHA256 == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: ref.SHA256, Valid: true}
}

func normalizeRedactionStatus(status string) string {
	switch status {
	case RedactionStatusRedacted, RedactionStatusVerifiedNoSecrets, RedactionStatusBlockedSensitive:
		return status
	default:
		return RedactionStatusNotRequired
	}
}

func normalizeCapturedFrom(value string, fallback string) string {
	switch value {
	case CapturedFromPlannerChat, CapturedFromRevisionNotes, CapturedFromImportedReq:
		return value
	default:
		return fallback
	}
}

func intentPacketEvidence(packet store.IntentPacket) IntentPacketEvidence {
	return IntentPacketEvidence{
		IntentPacketID:     packet.IntentPacketID,
		Kind:               packet.Kind,
		Summary:            packet.Summary,
		LiteralUserRequest: packet.LiteralUserRequest,
		Constraints:        packet.ConstraintsJson,
		ContentHash:        packet.ContentHash,
		RedactionStatus:    packet.RedactionStatus,
		SourceArtifactPath: packet.SourceArtifactPath,
		CreatedAt:          packet.CreatedAt,
	}
}

func planAttemptEvidence(attempt store.PlanAttempt) PlanAttemptEvidence {
	return PlanAttemptEvidence{
		PlanAttemptID:            attempt.PlanAttemptID,
		Status:                   attempt.Status,
		ReviewState:              attempt.ReviewState,
		DriftReviewMode:          attempt.DriftReviewMode,
		ModelTier:                attempt.ModelTier,
		CurrentIntentPacketID:    attempt.CurrentIntentPacketID,
		RootIntentPacketID:       attempt.RootIntentPacketID,
		SupersedesPlanAttemptID:  attempt.SupersedesPlanAttemptID.String,
		ReplacementPlanAttemptID: attempt.ReplacementPlanAttemptID.String,
		AcceptedDriftReviewID:    attempt.AcceptedDriftReviewID.String,
		SubmittedPlanID:          attempt.SubmittedPlanID.String,
		CreatedAt:                attempt.CreatedAt,
		UpdatedAt:                attempt.UpdatedAt,
	}
}

func planArtifactsEvidence(attempt store.PlanAttempt) PlanArtifactsEvidence {
	return PlanArtifactsEvidence{
		JSONArtifactPath:       attempt.PlanJsonArtifactPath,
		JSONArtifactSHA256:     attempt.PlanJsonArtifactSha256,
		MarkdownArtifactPath:   attempt.PlanMarkdownArtifactPath.String,
		MarkdownArtifactSHA256: attempt.PlanMarkdownArtifactSha256.String,
		RawPlanJSONHash:        attempt.RawPlanJsonHash,
	}
}

func priorAttemptSummaries(attempts []store.PlanAttempt, currentID string) []PriorAttemptInfo {
	summaries := make([]PriorAttemptInfo, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.PlanAttemptID == currentID {
			continue
		}
		summaries = append(summaries, PriorAttemptInfo{
			PlanAttemptID:         attempt.PlanAttemptID,
			Status:                attempt.Status,
			ReviewState:           attempt.ReviewState,
			CurrentIntentPacketID: attempt.CurrentIntentPacketID,
			SupersedesID:          attempt.SupersedesPlanAttemptID.String,
			ReplacementID:         attempt.ReplacementPlanAttemptID.String,
			SubmittedPlanID:       attempt.SubmittedPlanID.String,
			CreatedAt:             attempt.CreatedAt,
			UpdatedAt:             attempt.UpdatedAt,
		})
	}
	return summaries
}

func priorReviewSummaries(reviews []store.IntentDriftReview) []PriorReviewInfo {
	summaries := make([]PriorReviewInfo, 0, len(reviews))
	for _, review := range reviews {
		summaries = append(summaries, PriorReviewInfo{
			IntentDriftReviewID: review.IntentDriftReviewID,
			ReviewSource:        review.ReviewSource,
			OverallAlignment:    review.OverallAlignment,
			RecommendedAction:   review.RecommendedAction,
			ApprovalGateStatus:  review.ApprovalGateStatus,
			ReviewPacketHash:    review.ReviewPacketHash,
			CreatedAt:           review.CreatedAt,
		})
	}
	return summaries
}

func hashReviewPacket(packet PlanIntentReviewPacket) string {
	packet.PacketHash = ""
	packet.GeneratedAt = ""
	packet.PacketID = ""
	b, err := json.Marshal(packet)
	if err != nil {
		return ""
	}
	return sha256Bytes(b)
}

func validateDriftReviewInput(input DriftReviewInput) error {
	if input.ReviewSource != ReviewSourceExternal && input.ReviewSource != ReviewSourceInternal {
		return fmt.Errorf("review_source must be external or internal")
	}
	if !knownOverallAlignment(input.OverallAlignment) {
		return fmt.Errorf("unknown overall_alignment %q", input.OverallAlignment)
	}
	if !knownRecommendedAction(input.RecommendedAction) {
		return fmt.Errorf("unknown recommended_action %q", input.RecommendedAction)
	}
	if !knownApprovalGateStatus(input.ApprovalGateStatus) {
		return fmt.Errorf("unknown approval_gate_status %q", input.ApprovalGateStatus)
	}
	if input.Confidence < 0 || input.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1")
	}
	if !validateSHA256(input.ReviewPacketHash) || !validateSHA256(input.InputHash) || !validateSHA256(input.OutputHash) {
		return fmt.Errorf("review_packet_hash, input_hash, and output_hash must be sha256 values")
	}
	if len(input.FindingsJSON) == 0 {
		return fmt.Errorf("findings_json is required")
	}
	var findings any
	if err := json.Unmarshal(input.FindingsJSON, &findings); err != nil {
		return fmt.Errorf("findings_json must be valid JSON: %w", err)
	}
	switch findings.(type) {
	case []any, map[string]any:
	default:
		return fmt.Errorf("findings_json must be an array or object")
	}
	return nil
}

func rawMessageNullString(raw json.RawMessage) sql.NullString {
	if len(raw) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(raw), Valid: true}
}

func knownOverallAlignment(value string) bool {
	switch value {
	case OverallAlignmentAligned, OverallAlignmentMinorDrift, OverallAlignmentMajorDrift, OverallAlignmentUnclear:
		return true
	default:
		return false
	}
}

func knownRecommendedAction(value string) bool {
	switch value {
	case RecommendedActionApprove, RecommendedActionApproveWithAck, RecommendedActionRevise, RecommendedActionVoid, RecommendedActionManualReview:
		return true
	default:
		return false
	}
}

func knownApprovalGateStatus(value string) bool {
	switch value {
	case ApprovalGateStatusNotRequired, ApprovalGateStatusReady, ApprovalGateStatusAckRequired, ApprovalGateStatusRevisionRequired, ApprovalGateStatusBlocked:
		return true
	default:
		return false
	}
}

func initialReviewAction(mode string) *PlanAttemptReviewAction {
	switch mode {
	case DriftReviewModeDisabled:
		return &PlanAttemptReviewAction{Action: "drift_review_disabled", OK: true, Message: "drift review is disabled"}
	case DriftReviewModeManual:
		return &PlanAttemptReviewAction{Action: "manual_review_available", OK: true, Message: "manual drift review is available"}
	case DriftReviewModeExternal:
		return &PlanAttemptReviewAction{Action: "external_review_required", OK: true, Message: "external drift review submission is required"}
	case DriftReviewModeAutomatic:
		return &PlanAttemptReviewAction{Action: "run_drift_review", OK: false, Message: "automatic drift review has not run yet"}
	default:
		return nil
	}
}

func (svc *Service) resolveApprovalReview(ctx context.Context, q *generated.Queries, attempt store.PlanAttempt, reviewID string) (store.IntentDriftReview, bool, *PlanAttemptResult, error) {
	reviewID = strings.TrimSpace(reviewID)
	if reviewID != "" {
		review, err := q.GetIntentDriftReviewByIDAndProject(ctx, generated.GetIntentDriftReviewByIDAndProjectParams{
			IntentDriftReviewID: reviewID,
			ProjectRowID:        attempt.ProjectRowID,
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				blocked, err := blockAttempt(BlockerDriftReviewRequired, "accepted drift review is unknown")
				return store.IntentDriftReview{}, false, blocked, err
			}
			return store.IntentDriftReview{}, false, nil, err
		}
		if review.PlanAttemptID != attempt.PlanAttemptID {
			blocked, err := blockAttempt(BlockerStaleAttempt, "accepted drift review belongs to a different attempt")
			return store.IntentDriftReview{}, false, blocked, err
		}
		return review, true, nil, nil
	}
	reviews, err := q.ListIntentDriftReviewsByAttempt(ctx, attempt.ID)
	if err != nil {
		return store.IntentDriftReview{}, false, nil, err
	}
	if len(reviews) == 0 {
		return store.IntentDriftReview{}, false, nil, nil
	}
	if len(reviews) > 1 {
		blocked, err := blockAttempt(BlockerDriftReviewRequired, "accepted drift review must be explicit when multiple reviews exist")
		return store.IntentDriftReview{}, false, blocked, err
	}
	return reviews[0], true, nil, nil
}
