package testfixtures

// DeliveryTicket is the smallest complete current Delivery Ticket v2.0 source
// document fixture used by package, ticket, and execution tests. The selected
// approved Delivery Ticket is the sole ticket semantic authority; no Ticket
// Design Brief fixture exists anymore.
const DeliveryTicket = `{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","goal":"Deliver the package ticket.","context":"Carried ticket context.","scope":{"in_scope":["Package the selected ticket."],"out_of_scope":["Unrelated work."]},"depends_on":[],"required_invariants":["Packages must bind the exact approved Ticket."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/packages","obligation":"Preserve the selected package basis.","prerequisites":[]}],"proof_obligations":["Prove package preparation binds the approved Ticket."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/packages","expected":"all tests pass"}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["All tests pass."]}`
