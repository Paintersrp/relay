package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	PlannerTicketFrontierOperationID       OperationID       = "planner.ticket_frontier"
	LocalOperatorTicketWorkflowOperationID OperationID       = "local_operator.ticket_workflow"
	PlannerTicketFrontierSurface           SurfaceContractID = "planner-ticket-frontier.v1"
	LocalOperatorTicketWorkflowSurface     SurfaceContractID = "local-operator-ticket-workflow.v1"

	TicketActionReadFrontier        AllowedAction = "read_ticket_frontier"
	TicketActionPublish             AllowedAction = "publish_ticket"
	TicketActionApprove             AllowedAction = "approve_ticket"
	TicketActionUpdatePriority      AllowedAction = "update_ticket_priority"
	TicketActionReplaceDependencies AllowedAction = "replace_ticket_dependencies"
	TicketActionSelect              AllowedAction = "select_tickets"
	PackageActionPrepare            AllowedAction = "prepare_execution_package"
	PackageActionApprove            AllowedAction = "approve_execution_package"
	MutationLeaseActionReconcile    AllowedAction = "reconcile_mutation_lease"
)

// TicketRoleProfile is the closed operation inventory for the ticket route.
// Planner is deliberately read-only; all route-state mutations belong to the
// local operator and require packet admission before a durable owner is used.
type TicketRoleProfile struct {
	Role            Role
	SurfaceContract SurfaceContractID
	Operations      []OperationID
	ManifestSHA256  string
}

var ticketOperations = []OperationDefinition{
	{
		OperationID:              PlannerTicketFrontierOperationID,
		Role:                     "planner",
		SurfaceContract:          PlannerTicketFrontierSurface,
		ManifestDomain:           "delivery_ticket_frontier",
		OutputKind:               "delivery_ticket_frontier",
		OutputPersistence:        "derived_read",
		SourcePolicy:             "current_workspace_route",
		HistoricalAuthority:      "none",
		AllowedNonSourceActions:  []AllowedAction{TicketActionReadFrontier},
		PacketSemanticProjection: "relay.semantic.ticket-frontier-read.v1",
	},
	{
		OperationID:              LocalOperatorTicketWorkflowOperationID,
		Role:                     "local_operator",
		SurfaceContract:          LocalOperatorTicketWorkflowSurface,
		ManifestDomain:           "delivery_ticket_workflow",
		OutputKind:               "delivery_ticket_route_mutation",
		OutputPersistence:        "durable_workspace",
		SourcePolicy:             "current_clean_project_required_source",
		HistoricalAuthority:      "none",
		AllowedNonSourceActions:  []AllowedAction{TicketActionPublish, TicketActionApprove, TicketActionUpdatePriority, TicketActionReplaceDependencies, TicketActionSelect, PackageActionPrepare, PackageActionApprove, MutationLeaseActionReconcile},
		PacketSemanticProjection: "relay.semantic.ticket-mutation.v1",
	},
}

// PackageOperationForAction returns the local-operator operation that owns
// package composition, package approval, and durable lease recovery. These
// actions intentionally reuse the one local-operator packet identity rather
// than creating an alternate package or Run lifecycle.
func PackageOperationForAction(action AllowedAction) (OperationDefinition, bool) {
	switch action {
	case PackageActionPrepare, PackageActionApprove, MutationLeaseActionReconcile:
		return TicketOperationForAction(action)
	default:
		return OperationDefinition{}, false
	}
}

// TicketOperations returns the stable packet-admitted ticket operation set.
func TicketOperations() []OperationDefinition {
	out := make([]OperationDefinition, len(ticketOperations))
	for index, operation := range ticketOperations {
		out[index] = cloneOperation(operation)
	}
	return out
}

// TicketOperationForAction returns the one closed ticket surface that owns an
// action. In particular, it prevents a Planner frontier packet from being
// reused for a local-operator mutation (or vice versa).
func TicketOperationForAction(action AllowedAction) (OperationDefinition, bool) {
	for _, operation := range ticketOperations {
		for _, candidate := range operation.AllowedNonSourceActions {
			if candidate == action {
				return cloneOperation(operation), true
			}
		}
	}
	return OperationDefinition{}, false
}

// TicketRoleProfiles returns the ordered Planner and local-operator surfaces.
func TicketRoleProfiles() []TicketRoleProfile {
	profiles := []TicketRoleProfile{
		{Role: "planner", SurfaceContract: PlannerTicketFrontierSurface, Operations: []OperationID{PlannerTicketFrontierOperationID}},
		{Role: "local_operator", SurfaceContract: LocalOperatorTicketWorkflowSurface, Operations: []OperationID{LocalOperatorTicketWorkflowOperationID}},
	}
	for index := range profiles {
		profiles[index].ManifestSHA256 = ticketRoleManifestSHA256(profiles[index])
	}
	return profiles
}

func ticketRoleManifestSHA256(profile TicketRoleProfile) string {
	parts := make([]string, 0, len(profile.Operations)+2)
	parts = append(parts, string(profile.Role), string(profile.SurfaceContract))
	for _, operationID := range profile.Operations {
		parts = append(parts, "operation:"+string(operationID))
		operation, ok := ticketOperation(operationID)
		if !ok {
			continue
		}
		parts = append(parts,
			"manifest_domain:"+string(operation.ManifestDomain),
			"output_kind:"+operation.OutputKind,
			"output_persistence:"+operation.OutputPersistence,
			"source_policy:"+string(operation.SourcePolicy),
			"historical_authority:"+string(operation.HistoricalAuthority),
			"semantic_projection:"+operation.PacketSemanticProjection,
		)
		for _, action := range operation.AllowedNonSourceActions {
			parts = append(parts, "action:"+string(action))
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func ticketOperation(want OperationID) (OperationDefinition, bool) {
	for _, operation := range ticketOperations {
		if operation.OperationID == want {
			return operation, true
		}
	}
	return OperationDefinition{}, false
}
