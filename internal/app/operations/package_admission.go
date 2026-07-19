package operations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"

	"relay/internal/app/packages"
	"relay/internal/executor"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

var ErrPackageAdmission = errors.New("invalid execution package packet admission")

func IsPackageWorkflowNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows) || errors.Is(err, packages.ErrPackageNotFound)
}

const (
	packageSelectionDependencyClass = "execution_package_selection"
	packageSpecDependencyClass      = "execution_package_execution_spec"
	packageBriefDependencyClass     = "execution_package_ticket_design_brief"
	packageBasisDependencyClass     = "execution_package_basis"
	runDependencyClass              = "workflow_run"
	mutationLeaseDependencyClass    = "repository_branch_mutation_lease"
)

// PackageOperationRequest binds an operator mutation to one retained packet.
// The fields are intentionally limited to immutable package or lease identity;
// repository paths, process identities, and other local runtime values are not
// admitted or returned through this boundary.
type PackageOperationRequest struct {
	PacketID                     string
	OperationID                  registry.OperationID
	Action                       registry.AllowedAction
	SelectionID                  string
	PackageID                    string
	RunID                        string
	LeaseID                      string
	PayloadSHA256                string
	ExpectedPackageSha256        string
	OperatorConfirmationEvidence string
	RequiredDependencies         []DependencyRequirement
}

// PackageAdmissionService verifies the one registered local-operator action
// before the existing package or executor owner is called.
type PackageAdmissionService struct{ packets PacketMutationAuthorizer }

func NewPackageAdmissionService(packets PacketMutationAuthorizer) (*PackageAdmissionService, error) {
	if packets == nil {
		return nil, ErrPackageAdmission
	}
	return &PackageAdmissionService{packets: packets}, nil
}

func ValidatePackageOperationRequest(request PackageOperationRequest) error {
	if !exactNonBlank(request.PacketID) || !validTicketSHA256(request.PayloadSHA256) || validatePackageDependencies(request.RequiredDependencies) != nil {
		return ErrPackageAdmission
	}
	operation, ok := registry.PackageOperationForAction(request.Action)
	if !ok || operation.OperationID != request.OperationID {
		return ErrPackageAdmission
	}
	switch request.Action {
	case registry.PackageActionPrepare:
		if !exactNonBlank(request.SelectionID) || request.PackageID != "" || request.RunID != "" || request.LeaseID != "" {
			return ErrPackageAdmission
		}
	case registry.PackageActionApprove:
		if request.SelectionID != "" || !exactNonBlank(request.PackageID) || request.RunID != "" || request.LeaseID != "" {
			return ErrPackageAdmission
		}
		if !exactNonBlank(request.ExpectedPackageSha256) || !exactNonBlank(request.OperatorConfirmationEvidence) {
			return ErrPackageAdmission
		}
	case registry.MutationLeaseActionReconcile:
		if request.SelectionID != "" || request.PackageID != "" || !exactNonBlank(request.RunID) || !exactNonBlank(request.LeaseID) {
			return ErrPackageAdmission
		}
	default:
		return ErrPackageAdmission
	}
	return nil
}

func (s *PackageAdmissionService) Admit(ctx context.Context, request PackageOperationRequest) (MutationAuthorization, error) {
	if s == nil || s.packets == nil || ValidatePackageOperationRequest(request) != nil {
		return MutationAuthorization{}, ErrPackageAdmission
	}
	operation, _ := registry.PackageOperationForAction(request.Action)
	return s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: request.PacketID, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Action: request.Action, RequiredDependencies: append([]DependencyRequirement(nil), request.RequiredDependencies...),
	})
}

type PackageWorkflowOwner interface {
	Prepare(context.Context, packages.PrepareInput) (packages.PrepareResult, error)
	Approve(context.Context, packages.ApproveInput) (packages.ApproveResult, error)
	Get(context.Context, string) (packages.Detail, error)
}

type MutationLeaseReconciler interface {
	ReconcileMutationLease(context.Context, string) (executor.WorkflowMutationLeaseReconcileResult, error)
}

// MutationLeaseStatus is the safe projection used by API, MCP, and UI. It
// deliberately omits local paths, process metadata, and reconciliation notes.
type MutationLeaseStatus struct {
	Run   workflowstore.Run
	Lease *workflowstore.RepositoryBranchMutationLease
}

// PackageWorkflowService is a packet gate around the established package and
// executor owners. It does not create a second Run or lease lifecycle.
type PackageWorkflowService struct {
	admission  *PackageAdmissionService
	packages   PackageWorkflowOwner
	reconciler MutationLeaseReconciler
	store      *workflowstore.Store
}

func NewPackageWorkflowService(packets PacketMutationAuthorizer, owner PackageWorkflowOwner, reconciler MutationLeaseReconciler, store *workflowstore.Store) (*PackageWorkflowService, error) {
	admission, err := NewPackageAdmissionService(packets)
	if err != nil || owner == nil || reconciler == nil || store == nil {
		return nil, ErrPackageAdmission
	}
	return &PackageWorkflowService{admission: admission, packages: owner, reconciler: reconciler, store: store}, nil
}

type PackagePrepareOperationInput struct {
	Admission PackageOperationRequest
	Prepare   packages.PrepareInput
}

func (s *PackageWorkflowService) Prepare(ctx context.Context, input PackagePrepareOperationInput) (PackageDetailView, error) {
	payload, err := PackagePreparePayloadSHA256(input.Prepare)
	if err != nil || input.Admission.Action != registry.PackageActionPrepare || input.Admission.SelectionID != input.Prepare.SelectionID || input.Admission.PayloadSHA256 != payload || !sameDependencies(input.Admission.RequiredDependencies, packagePrepareDependencies(input.Prepare)) {
		return PackageDetailView{}, ErrPackageAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.PackageActionPrepare); err != nil {
		return PackageDetailView{}, err
	}
	result, err := s.packages.Prepare(ctx, input.Prepare)
	if err != nil {
		return PackageDetailView{}, err
	}
	return packageDetailView(packages.Detail{Package: result.Package, Members: result.Members, Briefs: result.Briefs, ExecutionSpec: result.ExecutionSpec}), nil
}

type PackageApproveOperationInput struct {
	Admission PackageOperationRequest
}

func (s *PackageWorkflowService) Approve(ctx context.Context, input PackageApproveOperationInput) (PackageApprovalView, error) {
	payload, err := PackageApprovePayloadSHA256(input.Admission.PackageID, input.Admission.ExpectedPackageSha256, input.Admission.OperatorConfirmationEvidence)
	if err != nil || input.Admission.Action != registry.PackageActionApprove || input.Admission.PayloadSHA256 != payload {
		return PackageApprovalView{}, ErrPackageAdmission
	}
	detail, err := s.packages.Get(ctx, input.Admission.PackageID)
	if err != nil {
		return PackageApprovalView{}, err
	}
	if !sameDependencies(input.Admission.RequiredDependencies, packageApproveDependencies(detail.Package)) {
		return PackageApprovalView{}, ErrPackageAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.PackageActionApprove); err != nil {
		return PackageApprovalView{}, err
	}
	result, err := s.packages.Approve(ctx, packages.ApproveInput{
		PackageID:                    input.Admission.PackageID,
		ExpectedPackageSha256:        input.Admission.ExpectedPackageSha256,
		OperatorConfirmationEvidence: input.Admission.OperatorConfirmationEvidence,
	})
	if err != nil {
		return PackageApprovalView{}, err
	}
	return PackageApprovalView{Package: packageIdentityView(result.Package), Run: runView(result.Run), PackageApprovalID: result.PackageApproval.ApprovalID}, nil
}

func (s *PackageWorkflowService) Get(ctx context.Context, packageID string) (PackageDetailView, error) {
	if s == nil || s.packages == nil {
		return PackageDetailView{}, ErrPackageAdmission
	}
	detail, err := s.packages.Get(ctx, packageID)
	if err != nil {
		return PackageDetailView{}, err
	}
	return packageDetailView(detail), nil
}

func (s *PackageWorkflowService) GetMutationLease(ctx context.Context, runID string) (*MutationLeaseView, error) {
	status, err := s.getMutationLease(ctx, runID)
	if err != nil {
		return nil, err
	}
	return mutationLeaseView(status), nil
}

func (s *PackageWorkflowService) getMutationLease(ctx context.Context, runID string) (MutationLeaseStatus, error) {
	if s == nil || s.store == nil || !exactNonBlank(runID) {
		return MutationLeaseStatus{}, ErrPackageAdmission
	}
	run, err := s.store.GetRunByRunID(ctx, runID)
	if err != nil {
		return MutationLeaseStatus{}, err
	}
	status := MutationLeaseStatus{Run: run}
	lease, err := s.store.GetActiveRepositoryBranchMutationLease(ctx, run.RepoTarget, run.Branch)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return MutationLeaseStatus{}, err
	}
	status.Lease = &lease
	return status, nil
}

type MutationLeaseReconcileOperationInput struct{ Admission PackageOperationRequest }

func (s *PackageWorkflowService) ReconcileMutationLease(ctx context.Context, input MutationLeaseReconcileOperationInput) (MutationLeaseReconcileView, error) {
	payload, err := MutationLeaseReconcilePayloadSHA256(input.Admission.RunID, input.Admission.LeaseID)
	if err != nil || input.Admission.Action != registry.MutationLeaseActionReconcile || input.Admission.PayloadSHA256 != payload {
		return MutationLeaseReconcileView{}, ErrPackageAdmission
	}
	status, err := s.getMutationLease(ctx, input.Admission.RunID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	if status.Lease == nil || status.Lease.LeaseID != input.Admission.LeaseID || !sameDependencies(input.Admission.RequiredDependencies, mutationLeaseDependencies(status)) {
		return MutationLeaseReconcileView{}, ErrPackageAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.MutationLeaseActionReconcile); err != nil {
		return MutationLeaseReconcileView{}, err
	}
	result, err := s.reconciler.ReconcileMutationLease(ctx, status.Run.RunID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	if result.Released {
		return MutationLeaseReconcileView{Released: true}, nil
	}
	refreshed, err := s.getMutationLease(ctx, status.Run.RunID)
	if err != nil {
		return MutationLeaseReconcileView{}, err
	}
	return MutationLeaseReconcileView{Lease: mutationLeaseView(refreshed)}, nil
}

func (s *PackageWorkflowService) admit(ctx context.Context, request PackageOperationRequest, action registry.AllowedAction) (MutationAuthorization, error) {
	if s == nil || s.admission == nil || request.Action != action {
		return MutationAuthorization{}, ErrPackageAdmission
	}
	return s.admission.Admit(ctx, request)
}

func PackagePreparePayloadSHA256(input packages.PrepareInput) (string, error) {
	return packagePayloadSHA256(input)
}

func PackageApprovePayloadSHA256(packageID, expectedPackageSha256, evidence string) (string, error) {
	return packagePayloadSHA256(struct {
		PackageID                   string `json:"package_id"`
		ExpectedPackageSha256       string `json:"expected_package_sha256"`
		OperatorConfirmationEvidence string `json:"operator_confirmation_evidence"`
	}{PackageID: packageID, ExpectedPackageSha256: expectedPackageSha256, OperatorConfirmationEvidence: evidence})
}

func MutationLeaseReconcilePayloadSHA256(runID, leaseID string) (string, error) {
	return packagePayloadSHA256(struct {
		RunID   string `json:"run_id"`
		LeaseID string `json:"lease_id"`
	}{RunID: runID, LeaseID: leaseID})
}

func packagePayloadSHA256(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func packagePrepareDependencies(input packages.PrepareInput) []DependencyRequirement {
	values := make([]DependencyRequirement, 0, len(input.TicketDesignBriefs)+2)
	values = append(values, DependencyRequirement{Class: packageSelectionDependencyClass, Key: "selection:" + input.SelectionID})
	values = append(values, DependencyRequirement{Class: packageSpecDependencyClass, Key: packageArtifactDependencyKey(input.ExecutionSpec)})
	for _, brief := range input.TicketDesignBriefs {
		values = append(values, DependencyRequirement{Class: packageBriefDependencyClass, Key: packageArtifactDependencyKey(brief)})
	}
	return sortedDependencies(values)
}

func packageApproveDependencies(value workflowstore.ExecutionPackage) []DependencyRequirement {
	return []DependencyRequirement{{Class: packageBasisDependencyClass, Key: "package:" + value.PackageID + ":" + value.PackageSha256}}
}

func mutationLeaseDependencies(status MutationLeaseStatus) []DependencyRequirement {
	if status.Lease == nil {
		return nil
	}
	return []DependencyRequirement{
		{Class: runDependencyClass, Key: "run:" + status.Run.RunID},
		{Class: mutationLeaseDependencyClass, Key: "lease:" + status.Lease.LeaseID},
	}
}

func packageArtifactDependencyKey(value packages.ArtifactInput) string {
	return value.DisplayName + ":" + value.ExpectedSHA256
}

func validatePackageDependencies(values []DependencyRequirement) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !exactNonBlank(value.Class) || !exactNonBlank(value.Key) {
			return ErrPackageAdmission
		}
		key := value.Class + "\x00" + value.Key
		if _, duplicate := seen[key]; duplicate {
			return ErrPackageAdmission
		}
		seen[key] = struct{}{}
	}
	return nil
}

func sameDependencies(left, right []DependencyRequirement) bool {
	if len(left) != len(right) {
		return false
	}
	left = sortedDependencies(left)
	right = sortedDependencies(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sortedDependencies(values []DependencyRequirement) []DependencyRequirement {
	result := append([]DependencyRequirement(nil), values...)
	sort.Slice(result, func(left, right int) bool {
		if result[left].Class != result[right].Class {
			return result[left].Class < result[right].Class
		}
		return result[left].Key < result[right].Key
	})
	return result
}
