package operations

import (
	"context"
	"strconv"
	"strings"

	"relay/internal/app/tickets"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

// TicketFrontierReadRequest is the only packet-authorized Ticket request.
// Ticket mutations are direct domain operations and never pass through this
// boundary. FeatureSlug is required; RequestedUnitID narrows the projection
// and is empty when no filter is supplied.
type TicketFrontierReadRequest struct {
	PacketID        string
	FeatureSlug     string
	RequestedUnitID string
}

// TicketFrontierAdmissionService verifies the exact active Planner frontier
// packet. It intentionally cannot encode Ticket, package, lease, or completion
// mutations.
type TicketFrontierAdmissionService struct{ packets PacketMutationAuthorizer }

func NewTicketFrontierAdmissionService(packets PacketMutationAuthorizer) (*TicketFrontierAdmissionService, error) {
	if packets == nil {
		return nil, ErrTicketAdmission
	}
	return &TicketFrontierAdmissionService{packets: packets}, nil
}

func ValidateTicketFrontierReadRequest(request TicketFrontierReadRequest) error {
	if !exactNonBlank(request.PacketID) || !tickets.ValidFrontierFeatureSlug(request.FeatureSlug) {
		return ErrTicketAdmission
	}
	if request.RequestedUnitID != "" && !tickets.ValidFrontierUnitTicketID(request.RequestedUnitID) {
		return ErrTicketAdmission
	}
	operation, ok := registry.TicketOperationForAction(registry.TicketActionReadFrontier)
	if !ok || operation.OperationID != registry.PlannerTicketFrontierOperationID ||
		operation.SurfaceContract != registry.PlannerTicketFrontierSurface {
		return ErrTicketAdmission
	}
	return nil
}

func (s *TicketFrontierAdmissionService) Admit(ctx context.Context, request TicketFrontierReadRequest) (MutationAuthorization, error) {
	if s == nil || s.packets == nil || ValidateTicketFrontierReadRequest(request) != nil {
		return MutationAuthorization{}, ErrTicketAdmission
	}
	operation, _ := registry.TicketOperationForAction(registry.TicketActionReadFrontier)
	return s.packets.AuthorizeMutation(ctx, MutationRequest{
		PacketID: request.PacketID, SurfaceContract: operation.SurfaceContract, OperationID: operation.OperationID,
		Action: registry.TicketActionReadFrontier,
	})
}

type TicketWorkflowOwner interface {
	Publish(context.Context, tickets.PublishInput) (tickets.PublishedRevision, error)
	UpdateExternalPriority(context.Context, string, int64) (workflowstore.DeliveryTicket, error)
	Approve(context.Context, tickets.ApproveInput) (workflowstore.DeliveryTicketRevisionApproval, error)
	Read(context.Context, string) (tickets.TicketDetail, error)
	ListFrontier(context.Context, string) (tickets.Frontier, error)
	Select(context.Context, tickets.SelectInput) (tickets.SelectionResult, error)
}

// PacketReader is deliberately read-only. It exists solely to verify the
// Planner remediation authoring packet before remediation publication.
type PacketReader interface {
	Get(context.Context, string) (PacketView, error)
}

// TicketWorkflowService projects direct Delivery Ticket domain commands. It
// has no local-operator mutation admission path. A supplied packet reader is
// used only for remediation authoring verification.
type TicketWorkflowService struct {
	owner   TicketWorkflowOwner
	packets PacketReader
}

func NewTicketWorkflowService(packets PacketReader, owner TicketWorkflowOwner) (*TicketWorkflowService, error) {
	if owner == nil {
		return nil, ErrTicketAdmission
	}
	return &TicketWorkflowService{owner: owner, packets: packets}, nil
}

// RemediationAuthoringReference binds a remediation publication to the exact
// active Planner packet that supplied its retained authoring context. It is
// authoring authority only, never mutation authorization.
type RemediationAuthoringReference struct {
	PacketID             string `json:"packet_id"`
	ExpectedPacketSHA256 string `json:"expected_packet_sha256"`
}

// Publish delegates ordinary publication directly to the Ticket owner. A
// remediation publication is admitted only after its Planner authoring packet
// has been read and verified exactly.
func (s *TicketWorkflowService) Publish(ctx context.Context, input tickets.PublishInput, reference *RemediationAuthoringReference) (tickets.PublishedRevision, error) {
	if s == nil || s.owner == nil {
		return tickets.PublishedRevision{}, ErrTicketAdmission
	}
	if err := ValidateTicketPublicationInput(input, reference); err != nil {
		return tickets.PublishedRevision{}, err
	}
	if input.RemediationSeedID != "" {
		if err := s.verifyRemediationAuthoring(ctx, input, *reference); err != nil {
			return tickets.PublishedRevision{}, err
		}
	}
	return s.owner.Publish(ctx, input)
}

func (s *TicketWorkflowService) ReplaceDependencies(ctx context.Context, input tickets.PublishInput) (tickets.PublishedRevision, error) {
	if s == nil || s.owner == nil || input.RemediationSeedID != "" {
		return tickets.PublishedRevision{}, ErrTicketAdmission
	}
	return s.owner.Publish(ctx, input)
}

// Approve verifies the caller's revision and source-closure binding against
// the current Ticket before delegating the authority-bound approval to the
// Ticket owner.
func (s *TicketWorkflowService) Approve(ctx context.Context, input tickets.ApproveInput, sourceClosureRowID int64) (workflowstore.DeliveryTicketRevisionApproval, error) {
	if s == nil || s.owner == nil || sourceClosureRowID < 1 {
		return workflowstore.DeliveryTicketRevisionApproval{}, ErrTicketAdmission
	}
	detail, err := s.owner.Read(ctx, input.TicketID)
	if err != nil {
		return workflowstore.DeliveryTicketRevisionApproval{}, err
	}
	if !detail.Ticket.CurrentRevisionRowID.Valid || detail.Ticket.CurrentRevisionRowID.Int64 != input.RevisionRowID ||
		detail.Revision.ID != input.RevisionRowID || detail.Revision.SourceClosureRowID != sourceClosureRowID {
		return workflowstore.DeliveryTicketRevisionApproval{}, ErrTicketAdmission
	}
	return s.owner.Approve(ctx, input)
}

func (s *TicketWorkflowService) UpdatePriority(ctx context.Context, ticketID string, externalPriority int64) (workflowstore.DeliveryTicket, error) {
	if s == nil || s.owner == nil {
		return workflowstore.DeliveryTicket{}, ErrTicketAdmission
	}
	return s.owner.UpdateExternalPriority(ctx, ticketID, externalPriority)
}

// ListFrontier is a direct HTTP/domain read. The separately mounted MCP
// frontier route applies TicketFrontierAdmissionService before calling the
// same owner read.
func (s *TicketWorkflowService) ListFrontier(ctx context.Context, workspaceID string) (tickets.Frontier, error) {
	if s == nil || s.owner == nil {
		return tickets.Frontier{}, ErrTicketAdmission
	}
	return s.owner.ListFrontier(ctx, workspaceID)
}

func (s *TicketWorkflowService) Select(ctx context.Context, input tickets.SelectInput) (tickets.SelectionResult, error) {
	if s == nil || s.owner == nil {
		return tickets.SelectionResult{}, ErrTicketAdmission
	}
	return s.owner.Select(ctx, input)
}

func ValidateTicketPublicationInput(input tickets.PublishInput, reference *RemediationAuthoringReference) error {
	if input.RemediationSeedID == "" {
		if reference != nil {
			return ErrTicketAdmission
		}
		return nil
	}
	if reference == nil || !exactNonBlank(input.RemediationSeedID) || !exactNonBlank(reference.PacketID) ||
		!exactNonBlank(reference.ExpectedPacketSHA256) || !validTicketSHA256(reference.ExpectedPacketSHA256) {
		return ErrTicketAdmission
	}
	return nil
}

func exactNonBlank(value string) bool { return strings.TrimSpace(value) == value && value != "" }

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

func stringRevisionID(value int64) string { return strconv.FormatInt(value, 10) }
