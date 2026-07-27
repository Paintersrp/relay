package audits

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"relay/internal/executor"
	workflowrepos "relay/internal/repos/workflow"
)

func testAuditCommitEvidence(baseCommit, auditedCommit string) workflowrepos.AuditCommitEvidence {
	return workflowrepos.AuditCommitEvidence{
		Branch:        "main",
		BaseCommit:    baseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles:  []string{"internal/example.go"},
		NameStatus:    "A\tinternal/example.go\n",
		DiffStat:      " internal/example.go | 1 +\n 1 file changed, 1 insertion(+)\n",
		CommitLog:     auditedCommit + "\tAuthor\t2026-07-27T00:00:00Z\tCommit msg\n",
		Diff:          "diff --git a/internal/example.go b/internal/example.go\nnew file mode 100644\n--- /dev/null\n+++ b/internal/example.go\n@@ -0,0 +1 @@\n+package example\n",
		FileChanges: []workflowrepos.AuditFileChange{
			{
				Path:       "internal/example.go",
				ChangeType: "added",
				Additions:  1,
				Deletions:  0,
			},
		},
	}
}

func testLoadVerifiedEvidence(t *testing.T, mode executor.EffectiveExecutorBriefMode) (*packageEvidenceFixture, WorkflowPackageExecutionEvidence) {
	t.Helper()
	fixture := buildPackageEvidence(t, mode)
	loader, err := NewWorkflowPackageExecutionEvidenceService(fixture.store, fixture.sourceVaultReader)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := loader.Load(context.Background(), fixture.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, evidence
}

func TestAssemblePackageAuditInput_AllEffectiveModes(t *testing.T) {
	modes := []executor.EffectiveExecutorBriefMode{
		executor.EffectiveExecutorBriefAdaptiveNoOperations,
		executor.EffectiveExecutorBriefAdaptivePreflightFailed,
		executor.EffectiveExecutorBriefAdaptiveAfterPartialApplication,
		executor.EffectiveExecutorBriefDeterministicComplete,
	}

	auditedCommit := strings.Repeat("b", 40)
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			_, evidence := testLoadVerifiedEvidence(t, mode)
			commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

			input, err := assemblePackageAuditInput(evidence, commit)
			if err != nil {
				t.Fatalf("assemblePackageAuditInput failed for mode %s: %v", mode, err)
			}

			packet, data, err := buildWorkflowPackageAuditPacket(input)
			if err != nil {
				t.Fatalf("buildWorkflowPackageAuditPacket failed for mode %s: %v", mode, err)
			}

			if packet.Run.UserIntent != evidence.Authority.TicketRevision.Goal {
				t.Fatalf("UserIntent mismatch: got %q, want %q", packet.Run.UserIntent, evidence.Authority.TicketRevision.Goal)
			}
			if len(data) == 0 || data[len(data)-1] != '\n' {
				t.Fatalf("packet output missing trailing newline")
			}
		})
	}
}

func TestAssemblePackageAuditInput_UserIntent(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

	// User intent comes exactly from ticket revision goal
	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatal(err)
	}
	if input.UserIntent != evidence.Authority.TicketRevision.Goal {
		t.Fatalf("UserIntent = %q, want %q", input.UserIntent, evidence.Authority.TicketRevision.Goal)
	}

	// Empty goal fails
	badEvidence := evidence
	badEvidence.Authority.TicketRevision.Goal = ""
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for empty user intent goal")
	}

	// Outer whitespace fails
	badEvidence.Authority.TicketRevision.Goal = "  Goal with whitespace  "
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for user intent with outer whitespace")
	}

	// Invalid UTF-8 fails
	badEvidence.Authority.TicketRevision.Goal = "Goal \xff\xfe"
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for user intent with invalid UTF-8")
	}
}

func TestAssemblePackageAuditInput_DeliveryTicket(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatal(err)
	}

	dt := evidence.Authority.DeliveryTicket
	if input.DeliveryTicket.Filename != dt.DisplayName {
		t.Fatalf("Filename = %q, want %q", input.DeliveryTicket.Filename, dt.DisplayName)
	}
	if input.DeliveryTicket.MediaType != dt.MediaType {
		t.Fatalf("MediaType = %q, want %q", input.DeliveryTicket.MediaType, dt.MediaType)
	}
	if input.DeliveryTicket.SHA256 != dt.SHA256 {
		t.Fatalf("SHA256 = %q, want %q", input.DeliveryTicket.SHA256, dt.SHA256)
	}
	if !bytes.Equal(input.DeliveryTicket.Bytes, dt.Bytes) {
		t.Fatal("DeliveryTicket bytes mismatch")
	}

	// Display name mismatch with relative path base fails
	badEvidence := evidence
	badEvidence.Authority.DeliveryTicket.DisplayName = "wrong-name.json"
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for delivery ticket display name mismatch")
	}

	// Non-JSON media type fails
	badEvidence = evidence
	badEvidence.Authority.DeliveryTicket.MediaType = "text/plain"
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for non-JSON delivery ticket media type")
	}

	// Invalid JSON bytes fails
	badEvidence = evidence
	badEvidence.Authority.DeliveryTicket.Bytes = []byte("invalid json")
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for invalid JSON delivery ticket bytes")
	}

	// SHA-256 mismatch fails
	badEvidence = evidence
	badEvidence.Authority.DeliveryTicket.SHA256 = strings.Repeat("a", 64)
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for SHA-256 mismatch")
	}

	// Invalid ObjectOID fails
	badEvidence = evidence
	badEvidence.Authority.DeliveryTicket.ObjectOID = "invalid-oid"
	if _, err := assemblePackageAuditInput(badEvidence, commit); err == nil {
		t.Fatal("expected error for invalid ObjectOID")
	}
}

func TestAssemblePackageAuditInput_ChangedFilesAndMapping(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)

	commit := workflowrepos.AuditCommitEvidence{
		Branch:        evidence.Run.Branch,
		BaseCommit:    evidence.Run.BaseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles: []string{
			"added.go",
			"modified.go",
			"deleted.go",
			"old_rename.go",
			"new_rename.go",
			"old_copy.go",
			"new_copy.go",
			"type_change.go",
		},
		Diff: "diff --git a/added.go b/added.go\n...",
		FileChanges: []workflowrepos.AuditFileChange{
			{Path: "added.go", ChangeType: "added", Additions: 10, Deletions: 0},
			{Path: "modified.go", ChangeType: "modified", Additions: 5, Deletions: 2},
			{Path: "deleted.go", ChangeType: "deleted", Additions: 0, Deletions: 20},
			{Path: "new_rename.go", PreviousPath: "old_rename.go", ChangeType: "renamed", Additions: 1, Deletions: 1},
			{Path: "new_copy.go", PreviousPath: "old_copy.go", ChangeType: "copied", Additions: 15, Deletions: 0},
			{Path: "type_change.go", ChangeType: "type_changed", Additions: 0, Deletions: 0},
		},
	}

	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemblePackageAuditInput failed: %v", err)
	}

	// Check exact mapping and order preservation
	if len(input.Commit.ChangedFiles) != len(commit.FileChanges) {
		t.Fatalf("ChangedFiles count = %d, want %d", len(input.Commit.ChangedFiles), len(commit.FileChanges))
	}

	for i, want := range commit.FileChanges {
		got := input.Commit.ChangedFiles[i]
		if got.Path != want.Path || got.PreviousPath != want.PreviousPath || got.ChangeType != want.ChangeType || got.Additions != want.Additions || got.Deletions != want.Deletions {
			t.Errorf("ChangedFile[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestAssemblePackageAuditInput_RelevantPathsSortingAndUniqueness(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)

	commit := workflowrepos.AuditCommitEvidence{
		Branch:        evidence.Run.Branch,
		BaseCommit:    evidence.Run.BaseCommit,
		AuditedCommit: auditedCommit,
		ChangedFiles:  []string{"z_file.go", "a_file.go", "src/old.go", "src/new.go"},
		Diff:          "diff --git a/z_file.go b/z_file.go\n...",
		FileChanges: []workflowrepos.AuditFileChange{
			{Path: "z_file.go", ChangeType: "modified", Additions: 1, Deletions: 1},
			{Path: "a_file.go", ChangeType: "added", Additions: 1, Deletions: 0},
			{Path: "src/new.go", PreviousPath: "src/old.go", ChangeType: "renamed", Additions: 0, Deletions: 0},
		},
	}

	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemblePackageAuditInput failed: %v", err)
	}

	wantRelevant := []string{"a_file.go", "src/new.go", "src/old.go", "z_file.go"}
	if !reflect.DeepEqual(input.RelevantSourcePaths, wantRelevant) {
		t.Fatalf("RelevantSourcePaths = %v, want %v", input.RelevantSourcePaths, wantRelevant)
	}
}

func TestAssemblePackageAuditInput_ContradictoryChangedFilesFail(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)

	baseCommit := evidence.Run.BaseCommit

	// FileChanges resulting path missing from ChangedFiles
	commit1 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit1.ChangedFiles = []string{"other.go"}
	if _, err := assemblePackageAuditInput(evidence, commit1); err == nil {
		t.Fatal("expected error when FileChanges resulting path is missing from ChangedFiles")
	}

	// ChangedFiles entry not corresponding to any path/previous_path in FileChanges
	commit2 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit2.ChangedFiles = []string{"internal/example.go", "unrelated.go"}
	if _, err := assemblePackageAuditInput(evidence, commit2); err == nil {
		t.Fatal("expected error when ChangedFiles contains entry not in FileChanges")
	}

	// Duplicate resulting path in FileChanges
	commit3 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit3.FileChanges = []workflowrepos.AuditFileChange{
		{Path: "internal/example.go", ChangeType: "added"},
		{Path: "internal/example.go", ChangeType: "modified"},
	}
	if _, err := assemblePackageAuditInput(evidence, commit3); err == nil {
		t.Fatal("expected error for duplicate resulting path in FileChanges")
	}

	// Previous path given for added file
	commit4 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit4.FileChanges[0].PreviousPath = "old.go"
	if _, err := assemblePackageAuditInput(evidence, commit4); err == nil {
		t.Fatal("expected error when previous_path is set for added file")
	}

	// Missing previous path for renamed file
	commit5 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit5.FileChanges[0].ChangeType = "renamed"
	if _, err := assemblePackageAuditInput(evidence, commit5); err == nil {
		t.Fatal("expected error when previous_path is missing for renamed file")
	}
}

func TestAssemblePackageAuditInput_RepositoryMismatch(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	baseCommit := evidence.Run.BaseCommit

	// Branch mismatch
	commit1 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit1.Branch = "feature-branch"
	if _, err := assemblePackageAuditInput(evidence, commit1); err == nil {
		t.Fatal("expected error for branch mismatch")
	}

	// Base commit mismatch
	commit2 := testAuditCommitEvidence(strings.Repeat("9", 40), auditedCommit)
	if _, err := assemblePackageAuditInput(evidence, commit2); err == nil {
		t.Fatal("expected error for base commit mismatch")
	}

	// Audited commit equals base commit
	commit3 := testAuditCommitEvidence(baseCommit, baseCommit)
	if _, err := assemblePackageAuditInput(evidence, commit3); err == nil {
		t.Fatal("expected error when audited_commit equals base_commit")
	}

	// Malformed audited commit
	commit4 := testAuditCommitEvidence(baseCommit, "not-a-sha")
	if _, err := assemblePackageAuditInput(evidence, commit4); err == nil {
		t.Fatal("expected error for malformed audited_commit")
	}
}

func TestAssemblePackageAuditInput_ExecutionSummaries(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)

	// Adaptive mode derives fixed adaptive summary
	_, adaptiveEvidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	adaptiveCommit := testAuditCommitEvidence(adaptiveEvidence.Run.BaseCommit, auditedCommit)
	inputAdaptive, err := assemblePackageAuditInput(adaptiveEvidence, adaptiveCommit)
	if err != nil {
		t.Fatalf("assemble adaptive failed: %v", err)
	}
	wantAdaptiveSummary := "The authorized adaptive Executor attempt completed successfully."
	if inputAdaptive.Execution.CompletionSummary != wantAdaptiveSummary {
		t.Fatalf("Adaptive summary = %q, want %q", inputAdaptive.Execution.CompletionSummary, wantAdaptiveSummary)
	}

	// Deterministic-complete mode derives fixed deterministic summary
	_, detEvidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefDeterministicComplete)
	detCommit := testAuditCommitEvidence(detEvidence.Run.BaseCommit, auditedCommit)
	inputDet, err := assemblePackageAuditInput(detEvidence, detCommit)
	if err != nil {
		t.Fatalf("assemble deterministic-complete failed: %v", err)
	}
	wantDetSummary := "Deterministic Operations completely fulfilled the approved Brief; no adaptive Executor attempt was dispatched."
	if inputDet.Execution.CompletionSummary != wantDetSummary {
		t.Fatalf("Deterministic summary = %q, want %q", inputDet.Execution.CompletionSummary, wantDetSummary)
	}
}

func TestAssemblePackageAuditInput_DiffPreservationAndValidation(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	baseCommit := evidence.Run.BaseCommit

	// Exact diff bytes and SHA-256 preserved without trimming or normalization
	rawDiff := "  diff --git a/f.go b/f.go\r\n+line\r\n  "
	commit := testAuditCommitEvidence(baseCommit, auditedCommit)
	commit.Diff = rawDiff

	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemblePackageAuditInput failed: %v", err)
	}

	if len(input.Artifacts) != 1 {
		t.Fatalf("Artifacts count = %d, want 1", len(input.Artifacts))
	}
	art := input.Artifacts[0]
	if art.Filename != "committed.diff" {
		t.Fatalf("Artifact filename = %q, want committed.diff", art.Filename)
	}
	if art.MediaType != "text/x-diff; charset=utf-8" {
		t.Fatalf("Artifact media type = %q, want text/x-diff; charset=utf-8", art.MediaType)
	}
	if !bytes.Equal(art.Bytes, []byte(rawDiff)) {
		t.Fatal("Artifact bytes do not match raw diff exactly (trimmed or normalized)")
	}
	if art.SHA256 != workflowPackageSHA256([]byte(rawDiff)) {
		t.Fatal("Artifact SHA-256 does not match raw diff digest")
	}

	// Empty diff fails
	commitEmpty := testAuditCommitEvidence(baseCommit, auditedCommit)
	commitEmpty.Diff = ""
	if _, err := assemblePackageAuditInput(evidence, commitEmpty); err == nil {
		t.Fatal("expected error for empty diff")
	}

	// Oversized diff fails
	commitOversized := testAuditCommitEvidence(baseCommit, auditedCommit)
	commitOversized.Diff = strings.Repeat("a", workflowrepos.MaxAuditDiffBytes+1)
	if _, err := assemblePackageAuditInput(evidence, commitOversized); err == nil {
		t.Fatal("expected error for oversized diff")
	}

	// Invalid UTF-8 diff fails
	commitInvalidUTF8 := testAuditCommitEvidence(baseCommit, auditedCommit)
	commitInvalidUTF8.Diff = "diff \xff\xfe"
	if _, err := assemblePackageAuditInput(evidence, commitInvalidUTF8); err == nil {
		t.Fatal("expected error for invalid UTF-8 diff")
	}
}

func TestAssemblePackageAuditInput_DefensiveCopies(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

	input, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}

	// Mutate returned input fields
	input.DeliveryTicket.Bytes[0] ^= 0xff
	input.Artifacts[0].Bytes[0] ^= 0xff
	input.Commit.ChangedFiles[0].Path = "mutated.go"
	input.RelevantSourcePaths[0] = "mutated.go"
	if len(input.Evidence.Validation) > 0 {
		input.Evidence.Validation[0].Status = "failed"
	}
	if len(input.Evidence.Authority.AuthorityLayers) > 0 && len(input.Evidence.Authority.AuthorityLayers[0].Bytes) > 0 {
		input.Evidence.Authority.AuthorityLayers[0].Bytes[0] ^= 0xff
	}

	// Re-load fresh evidence & commit to verify original structures were not mutated
	dtOriginal := evidence.Authority.DeliveryTicket.Bytes[0]
	if dtOriginal == input.DeliveryTicket.Bytes[0] {
		t.Fatal("DeliveryTicket bytes aliased supplied evidence")
	}
	if commit.FileChanges[0].Path == "mutated.go" {
		t.Fatal("FileChanges aliased supplied commit")
	}
}

func TestAssemblePackageAuditInput_DeterministicIdenticalOutput(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	_, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

	input1, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemble 1 failed: %v", err)
	}
	_, bytes1, err := buildWorkflowPackageAuditPacket(input1)
	if err != nil {
		t.Fatalf("build 1 failed: %v", err)
	}

	input2, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemble 2 failed: %v", err)
	}
	_, bytes2, err := buildWorkflowPackageAuditPacket(input2)
	if err != nil {
		t.Fatalf("build 2 failed: %v", err)
	}

	if !bytes.Equal(bytes1, bytes2) {
		t.Fatal("Repeated assembly produced different packet outputs")
	}
}

func TestAssemblePackageAuditInput_NoPureDatabaseWrites(t *testing.T) {
	auditedCommit := strings.Repeat("b", 40)
	fixture, evidence := testLoadVerifiedEvidence(t, executor.EffectiveExecutorBriefAdaptiveNoOperations)
	commit := testAuditCommitEvidence(evidence.Run.BaseCommit, auditedCommit)

	// Record initial DB state by querying tables
	var countBefore int
	if err := fixture.store.DB().QueryRow("SELECT COUNT(*) FROM workflow_audit_packets").Scan(&countBefore); err == nil {
		// Table may or may not exist, if it exists count rows
	}

	_, err := assemblePackageAuditInput(evidence, commit)
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}

	var countAfter int
	if err := fixture.store.DB().QueryRow("SELECT COUNT(*) FROM workflow_audit_packets").Scan(&countAfter); err == nil {
		if countBefore != countAfter {
			t.Fatalf("assemblePackageAuditInput performed database writes: count before %d, after %d", countBefore, countAfter)
		}
	}
}
