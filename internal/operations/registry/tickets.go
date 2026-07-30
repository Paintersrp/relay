package registry

const (
	PlannerTicketFrontierOperationID OperationID       = "planner.ticket_frontier"
	PlannerTicketFrontierSurface     SurfaceContractID = "planner-ticket-frontier.v1"

	TicketActionReadFrontier              AllowedAction = "read_ticket_frontier"
	TicketActionPublish                   AllowedAction = "publish_ticket"
	TicketActionApprove                   AllowedAction = "approve_ticket"
	TicketActionUpdatePriority            AllowedAction = "update_ticket_priority"
	TicketActionReplaceDependencies       AllowedAction = "replace_ticket_dependencies"
	TicketActionSelect                    AllowedAction = "select_tickets"
	PackageActionPrepare                  AllowedAction = "prepare_execution_package"
	PackageActionApprove                  AllowedAction = "approve_execution_package"
	MutationLeaseActionReconcile          AllowedAction = "reconcile_mutation_lease"
	FeatureCompletionActionComplete       AllowedAction = "complete_feature_workspace"
	FeatureAuthorityActionRecordApproval  AllowedAction = "record_authority_approval"
	FeatureAuthorityActionPublishApproved AllowedAction = "publish_approved_authority"
)

type TicketRoleProfile struct {
	Role            Role
	SurfaceContract SurfaceContractID
	Operations      []OperationID
	ManifestSHA256  string
}

func TicketOperations() []OperationDefinition {
	operations := make([]OperationDefinition, 0, 1)
	if publishedOperation, ok := LookupPublishedOperation(PlannerTicketFrontierOperationID); ok {
		operations = append(operations, publishedOperationAsLegacy(publishedOperation))
	}
	return operations
}

func TicketOperationForAction(action AllowedAction) (OperationDefinition, bool) {
	if action == TicketActionReadFrontier {
		for _, op := range TicketOperations() {
			for _, candidate := range op.AllowedNonSourceActions {
				if candidate == action {
					return cloneOperation(op), true
				}
			}
		}
	}
	return OperationDefinition{}, false
}

func TicketRoleProfiles() []TicketRoleProfile {
	operations := TicketOperations()
	roleProfiles := make([]TicketRoleProfile, len(operations))
	for i, op := range operations {
		roleProfiles[i] = TicketRoleProfile{Role: op.Role, SurfaceContract: op.SurfaceContract, Operations: []OperationID{op.OperationID}, ManifestSHA256: roleProfileSHA256(op.Role, op.SurfaceContract, op.OperationID)}
	}
	return roleProfiles
}
