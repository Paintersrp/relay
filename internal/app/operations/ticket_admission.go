package operations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"relay/internal/app/tickets"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

var ErrTicketAdmission = errors.New("invalid ticket packet admission")

// TicketSelectionMember identifies the exact current ticket revision observed
// by a selection caller. It is intentionally the same identity boundary as
// tickets.SelectionMemberInput without making this packet-admission package a
// second selection owner.
type TicketSelectionMember struct {
	TicketID      string
	RevisionRowID int64
}

// TicketOperationRequest is the packet-bound business identity passed to the
// ticket route. It carries no package or Run fields: those are deliberately
// outside the Pass 4 route surface.
type TicketOperationRequest struct {
	PacketID               string
	OperationID            registry.OperationID
	Action                 registry.AllowedAction
	WorkspaceID            string
	TicketID               string
	RevisionRowID          int64
	ExpectedRevisionNumber int64
	AuthorityRevisionID    string
	SourceClosureRowID     int64
	ExternalPriority       int64
	PayloadSHA256          string
	SelectionMembers       []TicketSelectionMember
	RequiredDependencies   []DependencyRequirement
}

// TicketAdmissionService is the narrow packet boundary used by the ticket
// route. It verifies the one action-to-surface mapping before a shared ticket
// owner can be called.
type TicketAdmissionService struct{ packets PacketMutationAuthorizer }

func NewTicketAdmissionService(packets PacketMutationAuthorizer) (*TicketAdmissionService, error) {
	if packets == nil {
		return nil, ErrTicketAdmission
	}
	return &TicketAdmissionService{packets: packets}, nil
}

// ValidateTicketOperationRequest validates the transport-independent route
// identity. The operation and action must be the exact registered pair, so a
// valid Planner frontier packet can never authorize a local mutation.
func ValidateTicketOperationRequest(request TicketOperationRequest) error {
	if strings.TrimSpace(request.PacketID) != request.PacketID || request.PacketID == "" ||
		strings.TrimSpace(request.WorkspaceID) != request.WorkspaceID || request.WorkspaceID == "" {
		return ErrTicketAdmission
	}
	operation, ok := registry.TicketOperationForAction(request.Action)
	if !ok || operation.OperationID != request.OperationID {
		return ErrTicketAdmission
	}
	if err := validateTicketDependencies(request.RequiredDependencies); err != nil {
		return err
	}

	switch request.Action {
	case registry.TicketActionReadFrontier:
		if request.TicketID != "" || request.RevisionRowID != 0 || request.ExpectedRevisionNumber != 0 ||
			request.AuthorityRevisionID != "" || request.SourceClosureRowID != 0 || request.ExternalPriority != 0 || request.PayloadSHA256 != "" || len(request.SelectionMembers) != 0 {
			return ErrTicketAdmission
		}
	case registry.TicketActionPublish:
		if !exactNonBlank(request.TicketID) || request.RevisionRowID != 0 || request.ExpectedRevisionNumber < 0 ||
			request.AuthorityRevisionID != "" || request.SourceClosureRowID < 1 || request.ExternalPriority < 0 || !validTicketSHA256(request.PayloadSHA256) || len(request.SelectionMembers) != 0 {
			return ErrTicketAdmission
		}
	case registry.TicketActionReplaceDependencies:
		if !exactNonBlank(request.TicketID) || request.RevisionRowID != 0 || request.ExpectedRevisionNumber < 1 ||
			request.AuthorityRevisionID != "" || request.SourceClosureRowID < 1 || request.ExternalPriority < 0 || !validTicketSHA256(request.PayloadSHA256) || len(request.SelectionMembers) != 0 {
			return ErrTicketAdmission
		}
	case registry.TicketActionApprove:
		if !exactNonBlank(request.TicketID) || request.RevisionRowID < 1 || request.ExpectedRevisionNumber != 0 ||
			!exactNonBlank(request.AuthorityRevisionID) || request.SourceClosureRowID < 1 || request.ExternalPriority != 0 || !validTicketSHA256(request.PayloadSHA256) || len(request.SelectionMembers) != 0 {
			return ErrTicketAdmission
		}
	case registry.TicketActionUpdatePriority:
		if !exactNonBlank(request.TicketID) || request.RevisionRowID != 0 || request.ExpectedRevisionNumber != 0 ||
			request.AuthorityRevisionID != "" || request.SourceClosureRowID != 0 || request.ExternalPriority < 0 || !validTicketSHA256(request.PayloadSHA256) || len(request.SelectionMembers) != 0 {
			return ErrTicketAdmission
		}
	case registry.TicketActionSelect:
		if request.TicketID != "" || request.RevisionRowID != 0 || request.ExpectedRevisionNumber != 0 ||
			request.AuthorityRevisionID != "" || request.SourceClosureRowID != 0 || request.ExternalPriority != 0 ||
			!validTicketSHA256(request.PayloadSHA256) || validateTicketSelectionMembers(request.SelectionMembers) != nil {
			return ErrTicketAdmission
		}
	default:
		return ErrTicketAdmission
	}
	return nil
}

// Admit validates the business identity, the retained packet dependencies,
// and the active packet before a ticket route operation can continue. Frontier
// reads use the same packet proof but are still read-only at the shared owner.
func (s *TicketAdmissionService) Admit(ctx context.Context, request TicketOperationRequest) (MutationAuthorization, error) {
	if s == nil || s.packets == nil {
		return MutationAuthorization{}, ErrTicketAdmission
	}
	if err := ValidateTicketOperationRequest(request); err != nil {
		return MutationAuthorization{}, err
	}
	operation, _ := registry.TicketOperationForAction(request.Action)
	return s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: request.PacketID, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Action: request.Action, RequiredDependencies: append([]DependencyRequirement(nil), request.RequiredDependencies...),
	})
}

// TicketWorkflowOwner is the one shared owner interface exposed through the
// packet gateway. The gateway has no direct store access and cannot create a
// package or Run.
type TicketWorkflowOwner interface {
	Publish(context.Context, tickets.PublishInput) (tickets.PublishedRevision, error)
	UpdateExternalPriority(context.Context, string, int64) (workflowstore.DeliveryTicket, error)
	Approve(context.Context, tickets.ApproveInput) (workflowstore.DeliveryTicketRevisionApproval, error)
	Read(context.Context, string) (tickets.TicketDetail, error)
	ListFrontier(context.Context, string) (tickets.Frontier, error)
	Select(context.Context, tickets.SelectInput) (tickets.SelectionResult, error)
}

// TicketWorkflowService runs every ticket operation through packet admission
// before it reaches tickets.Service. A dependency replacement is a full ticket
// publication under a distinct packet action; it never mutates dependencies in
// place.
type TicketWorkflowService struct {
	admission *TicketAdmissionService
	owner     TicketWorkflowOwner
}

func NewTicketWorkflowService(packets PacketMutationAuthorizer, owner TicketWorkflowOwner) (*TicketWorkflowService, error) {
	admission, err := NewTicketAdmissionService(packets)
	if err != nil || owner == nil {
		return nil, ErrTicketAdmission
	}
	return &TicketWorkflowService{admission: admission, owner: owner}, nil
}

type TicketPublishOperationInput struct {
	Admission TicketOperationRequest
	Publish   tickets.PublishInput
}

func (s *TicketWorkflowService) Publish(ctx context.Context, input TicketPublishOperationInput) (tickets.PublishedRevision, error) {
	payload, err := TicketPublishPayloadSHA256(input.Publish)
	if err != nil || !matchesPublishRequest(input.Admission, registry.TicketActionPublish, input.Publish) || input.Admission.PayloadSHA256 != payload {
		return tickets.PublishedRevision{}, ErrTicketAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.TicketActionPublish); err != nil {
		return tickets.PublishedRevision{}, err
	}
	return s.owner.Publish(ctx, input.Publish)
}

// ReplaceDependencies publishes a complete replacement revision carrying the
// new dependency outcomes. It cannot create a mutable dependency side route.
func (s *TicketWorkflowService) ReplaceDependencies(ctx context.Context, input TicketPublishOperationInput) (tickets.PublishedRevision, error) {
	payload, err := TicketPublishPayloadSHA256(input.Publish)
	if err != nil || !matchesPublishRequest(input.Admission, registry.TicketActionReplaceDependencies, input.Publish) || input.Admission.PayloadSHA256 != payload {
		return tickets.PublishedRevision{}, ErrTicketAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.TicketActionReplaceDependencies); err != nil {
		return tickets.PublishedRevision{}, err
	}
	return s.owner.Publish(ctx, input.Publish)
}

type TicketApprovalOperationInput struct {
	Admission TicketOperationRequest
	Approve   tickets.ApproveInput
}

func (s *TicketWorkflowService) Approve(ctx context.Context, input TicketApprovalOperationInput) (workflowstore.DeliveryTicketRevisionApproval, error) {
	request := input.Admission
	payload, err := TicketApprovalPayloadSHA256(input.Approve)
	if request.Action != registry.TicketActionApprove || request.TicketID != input.Approve.TicketID ||
		request.RevisionRowID != input.Approve.RevisionRowID || request.AuthorityRevisionID != input.Approve.AuthorityRevisionID || err != nil || request.PayloadSHA256 != payload {
		return workflowstore.DeliveryTicketRevisionApproval{}, ErrTicketAdmission
	}
	if _, err := s.admit(ctx, request, registry.TicketActionApprove); err != nil {
		return workflowstore.DeliveryTicketRevisionApproval{}, err
	}
	detail, err := s.owner.Read(ctx, request.TicketID)
	if err != nil {
		return workflowstore.DeliveryTicketRevisionApproval{}, err
	}
	if detail.Revision.ID != request.RevisionRowID || detail.Revision.SourceClosureRowID != request.SourceClosureRowID {
		return workflowstore.DeliveryTicketRevisionApproval{}, ErrTicketAdmission
	}
	return s.owner.Approve(ctx, input.Approve)
}

func (s *TicketWorkflowService) UpdatePriority(ctx context.Context, request TicketOperationRequest) (workflowstore.DeliveryTicket, error) {
	payload, err := TicketPriorityPayloadSHA256(request.TicketID, request.ExternalPriority)
	if err != nil || request.PayloadSHA256 != payload {
		return workflowstore.DeliveryTicket{}, ErrTicketAdmission
	}
	if _, err := s.admit(ctx, request, registry.TicketActionUpdatePriority); err != nil {
		return workflowstore.DeliveryTicket{}, err
	}
	return s.owner.UpdateExternalPriority(ctx, request.TicketID, request.ExternalPriority)
}

func (s *TicketWorkflowService) ListFrontier(ctx context.Context, request TicketOperationRequest) (tickets.Frontier, error) {
	if _, err := s.admit(ctx, request, registry.TicketActionReadFrontier); err != nil {
		return tickets.Frontier{}, err
	}
	return s.owner.ListFrontier(ctx, request.WorkspaceID)
}

type TicketSelectionOperationInput struct {
	Admission TicketOperationRequest
	Select    tickets.SelectInput
}

func (s *TicketWorkflowService) Select(ctx context.Context, input TicketSelectionOperationInput) (tickets.SelectionResult, error) {
	payload, err := TicketSelectionPayloadSHA256(input.Select)
	if input.Admission.Action != registry.TicketActionSelect || input.Admission.WorkspaceID != input.Select.WorkspaceID ||
		!sameSelectionMembers(input.Admission.SelectionMembers, input.Select.Members) || err != nil || input.Admission.PayloadSHA256 != payload {
		return tickets.SelectionResult{}, ErrTicketAdmission
	}
	if _, err := s.admit(ctx, input.Admission, registry.TicketActionSelect); err != nil {
		return tickets.SelectionResult{}, err
	}
	return s.owner.Select(ctx, input.Select)
}

func (s *TicketWorkflowService) admit(ctx context.Context, request TicketOperationRequest, action registry.AllowedAction) (MutationAuthorization, error) {
	if s == nil || s.admission == nil || s.owner == nil || request.Action != action {
		return MutationAuthorization{}, ErrTicketAdmission
	}
	return s.admission.Admit(ctx, request)
}

func matchesPublishRequest(request TicketOperationRequest, action registry.AllowedAction, input tickets.PublishInput) bool {
	return request.Action == action && request.WorkspaceID == input.WorkspaceID && request.TicketID == input.TicketID &&
		request.ExpectedRevisionNumber == input.ExpectedRevisionNumber && request.ExternalPriority == input.ExternalPriority &&
		request.SourceClosureRowID == input.Revision.SourceClosureRowID
}

func sameSelectionMembers(left []TicketSelectionMember, right []tickets.SelectionMemberInput) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]struct{}, len(left))
	for _, member := range left {
		seen[member.TicketID+"\x00"+stringRevisionID(member.RevisionRowID)] = struct{}{}
	}
	for _, member := range right {
		key := member.TicketID + "\x00" + stringRevisionID(member.RevisionRowID)
		if _, ok := seen[key]; !ok {
			return false
		}
	}
	return true
}

func validateTicketDependencies(values []DependencyRequirement) error {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !exactNonBlank(value.Class) || !exactNonBlank(value.Key) {
			return ErrTicketAdmission
		}
		key := value.Class + "\x00" + value.Key
		if _, duplicate := seen[key]; duplicate {
			return ErrTicketAdmission
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateTicketSelectionMembers(values []TicketSelectionMember) error {
	if len(values) == 0 {
		return ErrTicketAdmission
	}
	seenTickets := make(map[string]struct{}, len(values))
	seenRevisions := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if !exactNonBlank(value.TicketID) || value.RevisionRowID < 1 {
			return ErrTicketAdmission
		}
		if _, duplicate := seenTickets[value.TicketID]; duplicate {
			return ErrTicketAdmission
		}
		if _, duplicate := seenRevisions[value.RevisionRowID]; duplicate {
			return ErrTicketAdmission
		}
		seenTickets[value.TicketID] = struct{}{}
		seenRevisions[value.RevisionRowID] = struct{}{}
	}
	return nil
}

func exactNonBlank(value string) bool { return strings.TrimSpace(value) == value && value != "" }

func TicketPublishPayloadSHA256(input tickets.PublishInput) (string, error) {
	return ticketPayloadSHA256(input)
}

func TicketApprovalPayloadSHA256(input tickets.ApproveInput) (string, error) {
	return ticketPayloadSHA256(input)
}

func TicketPriorityPayloadSHA256(ticketID string, externalPriority int64) (string, error) {
	return ticketPayloadSHA256(struct {
		TicketID         string `json:"ticket_id"`
		ExternalPriority int64  `json:"external_priority"`
	}{TicketID: ticketID, ExternalPriority: externalPriority})
}

func TicketSelectionPayloadSHA256(input tickets.SelectInput) (string, error) {
	canonical := input
	canonical.Members = append([]tickets.SelectionMemberInput(nil), input.Members...)
	sort.Slice(canonical.Members, func(left, right int) bool {
		if canonical.Members[left].TicketID != canonical.Members[right].TicketID {
			return canonical.Members[left].TicketID < canonical.Members[right].TicketID
		}
		return canonical.Members[left].RevisionRowID < canonical.Members[right].RevisionRowID
	})
	return ticketPayloadSHA256(canonical)
}

func ticketPayloadSHA256(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func validTicketSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func stringRevisionID(value int64) string {
	return strconv.FormatInt(value, 10)
}
