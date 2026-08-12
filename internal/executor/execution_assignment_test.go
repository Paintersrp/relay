package executor

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	executionpackages "relay/internal/app/packages"
	"relay/internal/speccompiler"
	workflowstore "relay/internal/store/workflow"
)

func TestBuildExecutionAssignmentTicketOnlyCanonicalContract(t *testing.T) {
	authority := executionAssignmentAuthority(nil)
	_, content, filename, err := buildExecutionAssignment(authority)
	if err != nil {
		t.Fatal(err)
	}
	if filename != "checkout.ticket-P2-T2.r1.execution-assignment.json" {
		t.Fatalf("filename = %q", filename)
	}
	if len(content) == 0 || content[len(content)-1] != '\n' {
		t.Fatal("assignment does not have one trailing newline")
	}
	if strings.Count(string(content), "\n") != 1 {
		t.Fatal("assignment contains more than its trailing newline")
	}
	keys := []string{"schema_version", "run", "package", "package_approval", "ticket", "dependencies", "repository", "source", "authority", "authority_layers", "repository_instructions", "delivery_ticket", "deterministic_operations", "validation_commands", "standing_role"}
	last := -1
	for _, key := range keys {
		position := strings.Index(string(content), `"`+key+`"`)
		if position <= last {
			t.Fatalf("JSON key %q is out of canonical order", key)
		}
		last = position
	}
	var decoded map[string]any
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded["deterministic_operations"]; !reflect.DeepEqual(got, map[string]any{"presence": "absent"}) {
		t.Fatalf("operations = %#v, want explicit absence", got)
	}
	if got := decoded["dependencies"]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("dependencies = %#v, want explicit empty completed dependencies", got)
	}
	if got := decoded["repository_instructions"]; !reflect.DeepEqual(got, []any{}) {
		t.Fatalf("repository_instructions = %#v, want explicit empty basis", got)
	}
	source, ok := decoded["source"].(map[string]any)
	if !ok || source["commit_oid"] != strings.Repeat("a", 40) || source["tree_oid"] != strings.Repeat("b", 40) || source["generation"] != float64(1) || source["ref_name"] != "refs/relay/closures/closure-1" || source["state"] != "ready" {
		t.Fatalf("source = %#v", source)
	}
	commands := decoded["validation_commands"].([]any)
	if len(commands) != 2 || commands[0].(map[string]any)["command"] != "go test ./first" || commands[1].(map[string]any)["expected"] != "second exact result" {
		t.Fatalf("validation commands lost order or text: %#v", commands)
	}
	role, ok := decoded["standing_role"].(map[string]any)
	if !ok || role["authority_repository"] != "Paintersrp/relay-specs" || role["authority_commit"] != "9ea40ac112d0683affc10ba6bad2d15efe9e59f4" || role["source_path"] != "agents/orchestrator.md" {
		t.Fatalf("standing role = %#v", role)
	}
}

func TestBuildExecutionAssignmentPreservesPartialOperationsAndLayerOrder(t *testing.T) {
	authority := executionAssignmentAuthority(&executionpackages.ApprovedDeterministicOperations{
		ApprovedDocument: executionpackages.ApprovedDocument{
			DisplayName:  "checkout.ticket-P2-T2.r1.deterministic-operations.json",
			RelativePath: "packages/package-1/checkout.ticket-P2-T2.r1.deterministic-operations.json",
			MediaType:    "application/json",
			SHA256:       strings.Repeat("d", 64),
		},
		Coverage: "partial",
	})
	authority.AuthorityLayers = append(authority.AuthorityLayers, executionpackages.ApprovedAuthorityLayer{
		Kind: "shared_design", Sequence: 2, RelativePath: "plans/checkout/design.json", MediaType: "application/json", SHA256: strings.Repeat("e", 64),
	})
	assignment, content, _, err := buildExecutionAssignment(authority)
	if err != nil {
		t.Fatal(err)
	}
	if assignment.DeterministicOperations.Presence != "present" || assignment.DeterministicOperations.Coverage != "partial" || assignment.DeterministicOperations.SHA256 != strings.Repeat("d", 64) {
		t.Fatalf("operations assignment = %#v", assignment.DeterministicOperations)
	}
	if got := []int64{assignment.AuthorityLayers[0].Sequence, assignment.AuthorityLayers[1].Sequence}; !reflect.DeepEqual(got, []int64{1, 2}) {
		t.Fatalf("layer order = %#v", got)
	}
	if !strings.Contains(string(content), `"coverage":"partial"`) {
		t.Fatal("partial coverage was not serialized")
	}
}

func TestBuildExecutionAssignmentRejectsIncompleteProjection(t *testing.T) {
	noCommands := executionAssignmentAuthority(nil)
	noCommands.TicketProjection.ValidationCommands = nil
	if _, _, _, err := buildExecutionAssignment(noCommands); err == nil {
		t.Fatal("empty validation commands succeeded")
	}

	badOperations := executionAssignmentAuthority(&executionpackages.ApprovedDeterministicOperations{
		ApprovedDocument: executionpackages.ApprovedDocument{
			DisplayName:  "checkout.ticket-P2-T2.r1.deterministic-operations.json",
			RelativePath: "packages/package-1/checkout.ticket-P2-T2.r1.deterministic-operations.json",
			MediaType:    "application/json",
			SHA256:       strings.Repeat("f", 64),
		},
		Coverage: "unknown",
	})
	if _, _, _, err := buildExecutionAssignment(badOperations); err == nil {
		t.Fatal("invalid operations coverage succeeded")
	}
}

func TestBuildExecutionAssignmentCarriesCompletedDependenciesAndSourceIdentity(t *testing.T) {
	authority := executionAssignmentAuthority(nil)
	authority.CompletedDependencies = []executionpackages.ApprovedCompletedDependency{
		{Sequence: 1, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"},
		{Sequence: 2, TicketID: "P1-T7", Revision: 3, Outcome: "satisfied"},
	}
	assignment, content, _, err := buildExecutionAssignment(authority)
	if err != nil {
		t.Fatal(err)
	}
	wantDeps := []ExecutionAssignmentDependency{
		{Sequence: 1, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"},
		{Sequence: 2, TicketID: "P1-T7", Revision: 3, Outcome: "satisfied"},
	}
	if !reflect.DeepEqual(assignment.Dependencies, wantDeps) {
		t.Fatalf("assignment dependencies = %#v, want %#v", assignment.Dependencies, wantDeps)
	}
	if assignment.Source.CommitOID != strings.Repeat("a", 40) || assignment.Source.TreeOID != strings.Repeat("b", 40) || assignment.Source.Generation != 1 || assignment.Source.RefName != "refs/relay/closures/closure-1" || assignment.Source.State != "ready" {
		t.Fatalf("assignment source identity = %#v", assignment.Source)
	}
	if !strings.Contains(string(content), `"ticket_id":"P2-T1"`) || !strings.Contains(string(content), `"outcome":"satisfied"`) || !strings.Contains(string(content), `"commit_oid":"`+strings.Repeat("a", 40)+`"`) {
		t.Fatal("completed dependency or source identity was not serialized")
	}
}

func TestBuildExecutionAssignmentTransportsRepositoryInstructions(t *testing.T) {
	authority := executionAssignmentAuthority(nil)
	authority.RepositoryInstructions = []executionpackages.ApprovedRepositoryInstruction{
		{RelativePath: "AGENTS.md", SHA256: strings.Repeat("a", 64), SizeBytes: 5, ObjectOID: strings.Repeat("b", 40)},
		{RelativePath: "internal/AGENTS.md", SHA256: strings.Repeat("c", 64), SizeBytes: 7, ObjectOID: strings.Repeat("d", 40)},
		{RelativePath: "internal/app/AGENTS.md", SHA256: strings.Repeat("e", 64), SizeBytes: 9, ObjectOID: strings.Repeat("f", 40)},
	}
	assignment, content, _, err := buildExecutionAssignment(authority)
	if err != nil {
		t.Fatal(err)
	}
	want := []ExecutionAssignmentRepositoryInstruction{
		{Path: "AGENTS.md", SHA256: strings.Repeat("a", 64)},
		{Path: "internal/AGENTS.md", SHA256: strings.Repeat("c", 64)},
		{Path: "internal/app/AGENTS.md", SHA256: strings.Repeat("e", 64)},
	}
	if !reflect.DeepEqual(assignment.RepositoryInstructions, want) {
		t.Fatalf("assignment repository instructions = %#v, want %#v", assignment.RepositoryInstructions, want)
	}
	if !strings.Contains(string(content), `"repository_instructions"`) || !strings.Contains(string(content), `"path":"internal/AGENTS.md"`) || !strings.Contains(string(content), `"sha256":"`+strings.Repeat("e", 64)+`"`) {
		t.Fatal("repository instruction identities were not serialized")
	}
}

func TestBuildExecutionAssignmentRejectsInconsistentRepositoryInstructions(t *testing.T) {
	reordered := executionAssignmentAuthority(nil)
	reordered.RepositoryInstructions = []executionpackages.ApprovedRepositoryInstruction{
		{RelativePath: "internal/AGENTS.md", SHA256: strings.Repeat("a", 64)},
		{RelativePath: "AGENTS.md", SHA256: strings.Repeat("b", 64)},
	}
	if _, _, _, err := buildExecutionAssignment(reordered); err == nil {
		t.Fatal("out-of-order repository instruction paths succeeded")
	}

	duplicate := executionAssignmentAuthority(nil)
	duplicate.RepositoryInstructions = []executionpackages.ApprovedRepositoryInstruction{
		{RelativePath: "AGENTS.md", SHA256: strings.Repeat("a", 64)},
		{RelativePath: "AGENTS.md", SHA256: strings.Repeat("b", 64)},
	}
	if _, _, _, err := buildExecutionAssignment(duplicate); err == nil {
		t.Fatal("duplicate repository instruction path succeeded")
	}

	badPath := executionAssignmentAuthority(nil)
	badPath.RepositoryInstructions = []executionpackages.ApprovedRepositoryInstruction{
		{RelativePath: "agents/orchestrator.md", SHA256: strings.Repeat("a", 64)},
	}
	if _, _, _, err := buildExecutionAssignment(badPath); err == nil {
		t.Fatal("non-AGENTS.md repository instruction path succeeded")
	}

	badSHA := executionAssignmentAuthority(nil)
	badSHA.RepositoryInstructions = []executionpackages.ApprovedRepositoryInstruction{
		{RelativePath: "AGENTS.md", SHA256: "not-a-digest"},
	}
	if _, _, _, err := buildExecutionAssignment(badSHA); err == nil {
		t.Fatal("malformed repository instruction SHA-256 succeeded")
	}
}

func TestBuildExecutionAssignmentRejectsInconsistentCompletedDependencies(t *testing.T) {
	reorder := executionAssignmentAuthority(nil)
	reorder.CompletedDependencies = []executionpackages.ApprovedCompletedDependency{
		{Sequence: 2, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"},
		{Sequence: 1, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"},
	}
	if _, _, _, err := buildExecutionAssignment(reorder); err == nil {
		t.Fatal("out-of-order completed dependency sequence succeeded")
	}

	missingTicket := executionAssignmentAuthority(nil)
	missingTicket.CompletedDependencies = []executionpackages.ApprovedCompletedDependency{
		{Sequence: 1, TicketID: "", Revision: 1, Outcome: "satisfied"},
	}
	if _, _, _, err := buildExecutionAssignment(missingTicket); err == nil {
		t.Fatal("blank completed dependency Ticket ID succeeded")
	}
}

func TestBuildExecutionAssignmentRejectsInconsistentSourceIdentity(t *testing.T) {
	incomplete := executionAssignmentAuthority(nil)
	incomplete.Source = workflowstore.SourceVaultClosure{ID: 61, ClosureID: "closure-1"}
	if _, _, _, err := buildExecutionAssignment(incomplete); err == nil {
		t.Fatal("incomplete source closure identity succeeded")
	}

	badOID := executionAssignmentAuthority(nil)
	badOID.Source.CommitOID = "not-an-oid"
	if _, _, _, err := buildExecutionAssignment(badOID); err == nil {
		t.Fatal("invalid source commit OID succeeded")
	}
}

func executionAssignmentAuthority(operations *executionpackages.ApprovedDeterministicOperations) executionpackages.ApprovedAuthority {
	return executionpackages.ApprovedAuthority{
		Run:             workflowstore.Run{ID: 11, RunID: "run-1", Status: workflowstore.RunStatusSetupReady, FeatureSlug: "checkout", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40)},
		Package:         workflowstore.ExecutionPackage{ID: 21, PackageID: "package-1", PackageSha256: strings.Repeat("b", 64), AuthoritySha256: strings.Repeat("c", 64), SourceSha256: strings.Repeat("d", 64)},
		PackageApproval: workflowstore.ExecutionPackageApproval{ID: 31, ApprovalID: "pkg-approval-1", PackageSha256: strings.Repeat("b", 64)},
		Workspace:       workflowstore.FeatureWorkspace{ID: 41, FeatureSlug: "checkout"},
		Authority:       workflowstore.FeatureWorkspaceAuthorityRevision{ID: 51, AuthorityRevisionID: "authority-1", RevisionNumber: 3},
		Source:          workflowstore.SourceVaultClosure{ID: 61, ClosureID: "closure-1", CommitOID: strings.Repeat("a", 40), TreeOID: strings.Repeat("b", 40), Generation: 1, RefName: "refs/relay/closures/closure-1", State: "ready"},
		Ticket:          workflowstore.DeliveryTicket{ID: 71, TicketID: "P2-T2"},
		TicketRevision:  workflowstore.DeliveryTicketRevision{ID: 81, RevisionNumber: 1},
		TicketApproval:  workflowstore.DeliveryTicketRevisionApproval{ID: 91, ApprovalID: "approval-1"},
		AuthorityLayers: []executionpackages.ApprovedAuthorityLayer{{Kind: "requirements", Sequence: 1, RelativePath: "plans/checkout/requirements.json", MediaType: "application/json", SHA256: strings.Repeat("e", 64)}},
		DeliveryTicket:  executionpackages.ApprovedSourceDocument{DisplayName: "checkout.ticket-P2-T2.r1.delivery-ticket.json", RelativePath: "tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json", MediaType: "application/json", SHA256: strings.Repeat("f", 64)},
		TicketProjection: speccompiler.DeliveryTicketProjection{
			FeatureSlug: "checkout", TicketID: "P2-T2", Revision: 1,
			ValidationCommands: []speccompiler.DeliveryTicketValidationCommand{
				{WorkingDirectory: "", Command: "go test ./first", Expected: "first exact result"},
				{WorkingDirectory: "internal", Command: "go test ./second", Expected: "second exact result"},
			},
		},
		DeterministicOperations: operations,
	}
}
