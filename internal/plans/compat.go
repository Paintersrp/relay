// Package plans is a compatibility shim that re-exports types and constructors
// from relay/internal/app/plans. All business implementation has moved there.
// This package exists only to avoid breaking imports in packages that have not
// yet been updated (e.g. test files compiled against this package).
package plans

import (
	appplans "relay/internal/app/plans"
	"relay/internal/store"
)

// Type aliases — allow existing callers to reference these types without change.
type Service = appplans.Service
type RunLifecycleService = appplans.RunLifecycleService
type OrchestratorWorkService = appplans.OrchestratorWorkService

// Plan v2 types.
type PlannerPassPlan = appplans.PlannerPassPlan
type PlanMeta = appplans.PlanMeta
type RefactorPlanMetadata = appplans.RefactorPlanMetadata
type RefactorCandidateMetadata = appplans.RefactorCandidateMetadata
type SourceIntent = appplans.SourceIntent
type ProjectContext = appplans.ProjectContext
type MCPCapabilityProfile = appplans.MCPCapabilityProfile
type GlobalContextRules = appplans.GlobalContextRules
type PlanPassInput = appplans.PlanPassInput
type ContextPlan = appplans.ContextPlan
type ContextSearchTerm = appplans.ContextSearchTerm
type ContextFileRead = appplans.ContextFileRead
type SourceSnapshotRequirements = appplans.SourceSnapshotRequirements
type ContextBudget = appplans.ContextBudget
type SubmitPlanRequest = appplans.SubmitPlanRequest
type SubmitPlanResult = appplans.SubmitPlanResult
type PlanValidationReport = appplans.PlanValidationReport
type PlanValidationIssue = appplans.PlanValidationIssue

// Work-packet types.
type WorkBlocker = appplans.WorkBlocker
type NextPassWorkResponse = appplans.NextPassWorkResponse
type WorkProjectSummary = appplans.WorkProjectSummary
type WorkPlanSummary = appplans.WorkPlanSummary
type WorkPassSummary = appplans.WorkPassSummary
type WorkRefactorCandidateMetadata = appplans.WorkRefactorCandidateMetadata
type WorkDependencyStatus = appplans.WorkDependencyStatus
type WorkRunSummary = appplans.WorkRunSummary
type WorkContextSummary = appplans.WorkContextSummary
type SuggestedRunSubmission = appplans.SuggestedRunSubmission
type SuggestedRunArguments = appplans.SuggestedRunArguments
type NextPassWorkRequest = appplans.NextPassWorkRequest
type NextAuditWorkRequest = appplans.NextAuditWorkRequest
type NextAuditWorkResponse = appplans.NextAuditWorkResponse
type AuditWorkRunSummary = appplans.AuditWorkRunSummary
type WorkArtifactReference = appplans.WorkArtifactReference
type AuditPriorPassContext = appplans.AuditPriorPassContext
type AuditDecisionPayloadGuidance = appplans.AuditDecisionPayloadGuidance
type AuditDecisionRoute = appplans.AuditDecisionRoute

// Constructors.
func NewService(s *store.Store) *Service {
	return appplans.NewService(s)
}

func NewServiceWithSchemaPath(s *store.Store, schemaPath string) *Service {
	return appplans.NewServiceWithSchemaPath(s, schemaPath)
}

func NewRunLifecycleService(s *store.Store) *RunLifecycleService {
	return appplans.NewRunLifecycleService(s)
}

func NewOrchestratorWorkService(s *store.Store) *OrchestratorWorkService {
	return appplans.NewOrchestratorWorkService(s)
}

// Re-exported issue constants.
const (
	IssuePlanJSONSyntax                 = appplans.IssuePlanJSONSyntax
	IssuePlanSchemaInvalid              = appplans.IssuePlanSchemaInvalid
	IssuePlanSecretDetected             = appplans.IssuePlanSecretDetected
	IssuePlanStatusInvalidForSubmission = appplans.IssuePlanStatusInvalidForSubmission
	IssuePlanPassStatusInvalid          = appplans.IssuePlanPassStatusInvalid
	IssuePlanDuplicatePlanID            = appplans.IssuePlanDuplicatePlanID
	IssuePlanDuplicatePassID            = appplans.IssuePlanDuplicatePassID
	IssuePlanDuplicateSequence          = appplans.IssuePlanDuplicateSequence
	IssuePlanDependencyUnknown          = appplans.IssuePlanDependencyUnknown
	IssuePlanDependencySelf             = appplans.IssuePlanDependencySelf
	IssuePlanDependencyDuplicate        = appplans.IssuePlanDependencyDuplicate
	IssuePlanEmptyRequiredValue         = appplans.IssuePlanEmptyRequiredValue
	IssuePlanEmptyRequiredArray         = appplans.IssuePlanEmptyRequiredArray
	IssuePlanStorageFailed              = appplans.IssuePlanStorageFailed
	IssuePlanProjectRequired            = appplans.IssuePlanProjectRequired
	IssuePlanProjectUnknown             = appplans.IssuePlanProjectUnknown
	IssuePlanPassStatusInvalidRuntime   = appplans.IssuePlanPassStatusInvalidRuntime
	IssuePlanRefactorMetadataInvalid    = appplans.IssuePlanRefactorMetadataInvalid
)

// Re-exported pass status constants.
const (
	StatusPassPlanned          = appplans.StatusPassPlanned
	StatusPassReadyForPlanner  = appplans.StatusPassReadyForPlanner
	StatusPassHandoffReady     = appplans.StatusPassHandoffReady
	StatusPassRunCreated       = appplans.StatusPassRunCreated
	StatusPassInProgress       = appplans.StatusPassInProgress
	StatusPassAuditReady       = appplans.StatusPassAuditReady
	StatusPassCompleted        = appplans.StatusPassCompleted
	StatusPassRevisionRequired = appplans.StatusPassRevisionRequired
	StatusPassBlocked          = appplans.StatusPassBlocked
	StatusPassSkipped          = appplans.StatusPassSkipped
)

// Re-exported blocker code constants.
const (
	BlockerUnknownProject               = appplans.BlockerUnknownProject
	BlockerUnknownPlan                  = appplans.BlockerUnknownPlan
	BlockerProjectPlanMismatch          = appplans.BlockerProjectPlanMismatch
	BlockerPlanNotActive                = appplans.BlockerPlanNotActive
	BlockerDependenciesIncomplete       = appplans.BlockerDependenciesIncomplete
	BlockerPriorPassAwaitsAudit         = appplans.BlockerPriorPassAwaitsAudit
	BlockerActiveRunExists              = appplans.BlockerActiveRunExists
	BlockerRequiredSourceContextMissing = appplans.BlockerRequiredSourceContextMissing
	BlockerRequiredContextPacketMissing = appplans.BlockerRequiredContextPacketMissing
	BlockerRevisionRequiredSamePass     = appplans.BlockerRevisionRequiredSamePass
	BlockerNoEligiblePass               = appplans.BlockerNoEligiblePass
	BlockerUnsafeRequest                = appplans.BlockerUnsafeRequest
	BlockerUnknownPass                  = appplans.BlockerUnknownPass
	BlockerUnknownRun                   = appplans.BlockerUnknownRun
	BlockerRunNotInProjectPlan          = appplans.BlockerRunNotInProjectPlan
	BlockerRunNotAuditReady             = appplans.BlockerRunNotAuditReady
	BlockerAuditEvidenceMissing         = appplans.BlockerAuditEvidenceMissing
	BlockerAuditAlreadyFinalized        = appplans.BlockerAuditAlreadyFinalized
	BlockerNoAuditWork                  = appplans.BlockerNoAuditWork
)

// Re-exported tool name constants.
const (
	NextPassWorkTool  = appplans.NextPassWorkTool
	NextAuditWorkTool = appplans.NextAuditWorkTool
)

// Re-exported helper functions.
var (
	ResolvePlanProjectID    = appplans.ResolvePlanProjectID
	IsInitialPlanPassStatus = appplans.IsInitialPlanPassStatus
)
