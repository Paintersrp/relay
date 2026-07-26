package executor

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	executionpackages "relay/internal/app/packages"
	"relay/internal/planningartifacts"
	workflowstore "relay/internal/store/workflow"
)

func TestBuildExecutionAssignmentBriefOnlyCanonicalContract(t *testing.T) {
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
	keys := []string{"schema_version", "run", "package", "package_approval", "ticket", "repository", "source", "authority", "authority_layers", "ticket_design_brief", "deterministic_operations", "validation_commands", "executor_instructions"}
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
	commands := decoded["validation_commands"].([]any)
	if len(commands) != 2 || commands[0].(map[string]any)["command"] != "go test ./first" || commands[1].(map[string]any)["expected"] != "second exact result" {
		t.Fatalf("validation commands lost order or text: %#v", commands)
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
	noCommands.BriefProjection.ValidationCommands = nil
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

func executionAssignmentAuthority(operations *executionpackages.ApprovedDeterministicOperations) executionpackages.ApprovedAuthority {
	return executionpackages.ApprovedAuthority{
		Run:                     workflowstore.Run{ID: 11, RunID: "run-1", Status: workflowstore.RunStatusSetupReady, FeatureSlug: "checkout", RepoTarget: "relay", Branch: "main", BaseCommit: strings.Repeat("a", 40)},
		Package:                 workflowstore.ExecutionPackage{ID: 21, PackageID: "package-1", PackageSha256: strings.Repeat("b", 64), AuthoritySha256: strings.Repeat("c", 64), SourceSha256: strings.Repeat("d", 64)},
		PackageApproval:         workflowstore.ExecutionPackageApproval{ID: 31, ApprovalID: "pkg-approval-1", PackageSha256: strings.Repeat("b", 64)},
		Workspace:               workflowstore.FeatureWorkspace{ID: 41, FeatureSlug: "checkout"},
		Authority:               workflowstore.FeatureWorkspaceAuthorityRevision{ID: 51, AuthorityRevisionID: "authority-1", RevisionNumber: 3},
		Source:                  workflowstore.SourceVaultClosure{ID: 61, ClosureID: "closure-1"},
		Ticket:                  workflowstore.DeliveryTicket{ID: 71, TicketID: "P2-T2"},
		TicketRevision:          workflowstore.DeliveryTicketRevision{ID: 81, RevisionNumber: 1},
		TicketApproval:          workflowstore.DeliveryTicketRevisionApproval{ID: 91, ApprovalID: "approval-1"},
		AuthorityLayers:         []executionpackages.ApprovedAuthorityLayer{{Kind: "requirements", Sequence: 1, RelativePath: "plans/checkout/requirements.json", MediaType: "application/json", SHA256: strings.Repeat("e", 64)}},
		TicketDesignBrief:       executionpackages.ApprovedDocument{DisplayName: "checkout.ticket-P2-T2.r1.design-brief.md", RelativePath: "packages/package-1/checkout.ticket-P2-T2.r1.design-brief.md", MediaType: "text/markdown", SHA256: strings.Repeat("f", 64)},
		BriefProjection:         planningartifacts.TicketDesignBriefProjection{ValidationCommands: []planningartifacts.ValidationCommand{{WorkingDirectory: ".", Command: "go test ./first", Expected: "first exact result"}, {WorkingDirectory: "internal", Command: "go test ./second", Expected: "second exact result"}}},
		DeterministicOperations: operations,
	}
}
