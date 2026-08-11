package packages

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

// instructionTestFiles is the canonical instruction fixture set: files at
// several repository-relative paths including one non-applicable nested file
// and the standing execution-role procedure, which must never bind.
var instructionTestFiles = map[string]string{
	"AGENTS.md":                "# root repository instructions\n",
	"internal/AGENTS.md":       "# internal repository instructions\n",
	"internal/app/AGENTS.md":   "# app repository instructions\n",
	"tickets/AGENTS.md":        "# ticket repository instructions\n",
	"internal/other/AGENTS.md": "# non-applicable repository instructions\n",
	"agents/orchestrator.md":   "# standing execution-role procedure\n",
}

// expectedInstructionBasis returns the ordered instruction identities for the
// fixture's inspected source basis (the Ticket source path plus the obligation
// source area internal/app/packages) resolved against files.
func expectedInstructionBasis(t *testing.T, files map[string]string) []ApprovedRepositoryInstruction {
	t.Helper()
	sourcePaths := inspectedSourcePaths("tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json", []string{"internal/app/packages"})
	candidates := applicableInstructionPaths(sourcePaths)
	var instructions []ApprovedRepositoryInstruction
	for _, candidate := range candidates {
		content, ok := files[candidate]
		if !ok {
			continue
		}
		instructions = append(instructions, ApprovedRepositoryInstruction{
			RelativePath: candidate,
			SHA256:       sha256Hex([]byte(content)),
			SizeBytes:    int64(len(content)),
			ObjectOID:    strings.Repeat("e", 40),
		})
	}
	return instructions
}

// preparePackageWithInstructions prepares a package on a fixture whose
// source-vault reader serves the given instruction files.
func preparePackageWithInstructions(t *testing.T, files map[string]string) (*packageServiceFixture, PrepareResult) {
	t.Helper()
	fixture := newPackageServiceFixture(t)
	fixture.reader.paths = make(map[string][]byte, len(files))
	for path, content := range files {
		fixture.reader.paths[path] = []byte(content)
	}
	return fixture, preparePackage(t, fixture, false)
}

// storedInstructionRows reads the stored instruction rows for a package.
func storedInstructionRows(t *testing.T, fixture *packageServiceFixture, packageRowID int64) []workflowstore.ExecutionPackageRepositoryInstruction {
	t.Helper()
	rows, err := fixture.store.ListExecutionPackageRepositoryInstructions(context.Background(), packageRowID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRepositoryInstructionsRootAgentsApplies(t *testing.T) {
	files := map[string]string{"AGENTS.md": "# root repository instructions\n"}
	fixture, prepared := preparePackageWithInstructions(t, files)
	want := expectedInstructionBasis(t, files)
	if len(want) != 1 || want[0].RelativePath != "AGENTS.md" {
		t.Fatalf("expected basis = %#v", want)
	}
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 1 || rows[0].Path != "AGENTS.md" || rows[0].Sha256 != want[0].SHA256 || rows[0].Sequence != 1 {
		t.Fatalf("stored instruction rows = %#v", rows)
	}
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(want) {
		t.Fatalf("package instruction digest = %q, want %q", prepared.Package.RepositoryInstructionsSha256, repositoryInstructionsBasisSHA256(want))
	}

	approved, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.RepositoryInstructions, want) {
		t.Fatalf("approved authority repository instructions = %#v, want %#v", loaded.RepositoryInstructions, want)
	}
}

func TestRepositoryInstructionsNestedApplicableAgentsApplies(t *testing.T) {
	files := map[string]string{"internal/AGENTS.md": "# internal repository instructions\n"}
	fixture, prepared := preparePackageWithInstructions(t, files)
	want := expectedInstructionBasis(t, files)
	if len(want) != 1 || want[0].RelativePath != "internal/AGENTS.md" {
		t.Fatalf("expected basis = %#v", want)
	}
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 1 || rows[0].Path != "internal/AGENTS.md" || rows[0].Sha256 != want[0].SHA256 {
		t.Fatalf("stored instruction rows = %#v", rows)
	}
}

func TestRepositoryInstructionsNestedNonApplicableAgentsIsExcluded(t *testing.T) {
	// The nested file is rooted at internal/other, which contains no inspected
	// source path; it must not bind even though it exists in the closure.
	files := map[string]string{"internal/other/AGENTS.md": "# non-applicable\n"}
	fixture, prepared := preparePackageWithInstructions(t, files)
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 0 {
		t.Fatalf("non-applicable nested instruction bound: %#v", rows)
	}
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(nil) {
		t.Fatalf("package instruction digest = %q, want empty-basis digest", prepared.Package.RepositoryInstructionsSha256)
	}
}

func TestRepositoryInstructionsMultipleSourceSubtreesApply(t *testing.T) {
	// The fixture inspected source basis spans the tickets subtree (Ticket
	// source path) and the internal subtree (obligation source area), so
	// AGENTS.md files rooted at each of those subtrees apply together.
	files := map[string]string{
		"AGENTS.md":              "# root\n",
		"tickets/AGENTS.md":      "# tickets\n",
		"internal/AGENTS.md":     "# internal\n",
		"internal/app/AGENTS.md": "# app\n",
	}
	fixture, prepared := preparePackageWithInstructions(t, files)
	want := expectedInstructionBasis(t, files)
	if len(want) != 4 {
		t.Fatalf("expected basis = %#v", want)
	}
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 4 {
		t.Fatalf("stored instruction rows = %#v", rows)
	}
	for index, instruction := range want {
		if rows[index].Path != instruction.RelativePath || rows[index].Sha256 != instruction.SHA256 {
			t.Fatalf("stored instruction row %d = %#v, want %#v", index, rows[index], instruction)
		}
	}
}

func TestRepositoryInstructionsDeduplicatedAcrossSourcePaths(t *testing.T) {
	// The repository-root AGENTS.md applies to every inspected source path
	// (the Ticket source path and the obligation source area) and must bind
	// exactly once.
	files := map[string]string{"AGENTS.md": "# root repository instructions\n"}
	fixture, prepared := preparePackageWithInstructions(t, files)
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 1 || rows[0].Path != "AGENTS.md" {
		t.Fatalf("root instruction bound %d times: %#v", len(rows), rows)
	}
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(expectedInstructionBasis(t, files)) {
		t.Fatal("deduplicated basis digest mismatch")
	}
}

func TestRepositoryInstructionsDeterministicPathOrdering(t *testing.T) {
	files := map[string]string{
		"tickets/AGENTS.md":      "# tickets\n",
		"internal/app/AGENTS.md": "# app\n",
		"AGENTS.md":              "# root\n",
		"internal/AGENTS.md":     "# internal\n",
	}
	fixture, prepared := preparePackageWithInstructions(t, files)
	want := []string{"AGENTS.md", "internal/AGENTS.md", "internal/app/AGENTS.md", "tickets/AGENTS.md"}
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	got := make([]string, len(rows))
	for index, row := range rows {
		got[index] = row.Path
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored instruction order = %#v, want %#v", got, want)
	}
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(expectedInstructionBasis(t, files)) {
		t.Fatal("ordered basis digest mismatch")
	}
}

func TestRepositoryInstructionsBindExactSourceVaultBytes(t *testing.T) {
	files := map[string]string{
		"AGENTS.md":          "# root repository instructions\n",
		"internal/AGENTS.md": "# internal repository instructions\n",
	}
	fixture, prepared := preparePackageWithInstructions(t, files)
	want := expectedInstructionBasis(t, files)
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(want) {
		t.Fatal("package digest does not bind the exact served bytes")
	}
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.RepositoryInstructions, want) {
		t.Fatalf("approved authority instructions = %#v, want exact byte-bound identities %#v", loaded.RepositoryInstructions, want)
	}
	for _, instruction := range loaded.RepositoryInstructions {
		if instruction.SHA256 != sha256Hex([]byte(files[instruction.RelativePath])) {
			t.Fatalf("instruction %s digest does not bind the exact closure bytes", instruction.RelativePath)
		}
	}
}

func TestRepositoryInstructionsMembershipChangeInvalidatesPackage(t *testing.T) {
	ctx := context.Background()

	// Membership growth: a newly applicable AGENTS.md must invalidate approval.
	fixture, prepared := preparePackageWithInstructions(t, map[string]string{"AGENTS.md": "# root\n"})
	fixture.reader.paths["internal/AGENTS.md"] = []byte("# internal\n")
	if _, err := fixture.service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve stale basis"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approval after instruction membership growth error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture.store.DB(), "runs", 0)

	// Membership shrink: a bound AGENTS.md disappearing must invalidate too.
	shrink, shrinkPrepared := preparePackageWithInstructions(t, map[string]string{
		"AGENTS.md":          "# root\n",
		"internal/AGENTS.md": "# internal\n",
	})
	delete(shrink.reader.paths, "internal/AGENTS.md")
	if _, err := shrink.service.Approve(ctx, ApproveInput{PackageID: shrinkPrepared.Package.PackageID, ExpectedPackageSha256: shrinkPrepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve stale basis"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approval after instruction membership shrink error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, shrink.store.DB(), "execution_package_approvals", 0)
	assertCount(t, shrink.store.DB(), "runs", 0)
}

func TestRepositoryInstructionsByteChangeInvalidatesPackage(t *testing.T) {
	ctx := context.Background()
	files := map[string]string{
		"AGENTS.md":          "# root repository instructions\n",
		"internal/AGENTS.md": "# internal repository instructions\n",
	}

	// Approval preflight revalidation: change the exact bytes of a bound
	// instruction before approval and the immutable package basis rejects it.
	fixture, prepared := preparePackageWithInstructions(t, files)
	fixture.reader.paths["internal/AGENTS.md"] = []byte("# changed repository instructions\n")
	if _, err := fixture.service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve stale basis"}); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("approval after instruction byte change error = %v, want ErrPackageBasisChanged", err)
	}
	assertCount(t, fixture.store.DB(), "execution_package_approvals", 0)
	assertCount(t, fixture.store.DB(), "runs", 0)

	// Get readback and approved-authority load after approval must also reject
	// the same drift.
	approvedFixture, approved := preparePackageWithInstructions(t, files)
	result, err := approvedFixture.service.Approve(ctx, ApproveInput{PackageID: approved.Package.PackageID, ExpectedPackageSha256: approved.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	approvedFixture.reader.paths["internal/AGENTS.md"] = []byte("# changed repository instructions\n")
	if _, err := approvedFixture.service.Get(ctx, approved.Package.PackageID); !errors.Is(err, ErrPackageBasisChanged) {
		t.Fatalf("read-back after instruction byte change error = %v, want ErrPackageBasisChanged", err)
	}
	if _, err := approvedFixture.service.LoadApprovedAuthorityForRun(ctx, result.Run.RunID); err == nil {
		t.Fatal("approved-authority load accepted changed instruction bytes")
	}
}

func TestRepositoryInstructionsStoredRowTamperInvalidatesAuthority(t *testing.T) {
	fixture, prepared := preparePackageWithInstructions(t, map[string]string{
		"AGENTS.md":          "# root\n",
		"internal/AGENTS.md": "# internal\n",
	})
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	// Stored rows are immutable history, so a divergent row can only be added;
	// the approved-authority load must reject the extra unbound instruction.
	if _, err := fixture.store.DB().Exec(`INSERT INTO execution_package_repository_instructions (package_row_id, sequence, path, sha256) VALUES (?, 99, 'unbound/AGENTS.md', ?)`, prepared.Package.ID, strings.Repeat("9", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID); err == nil {
		t.Fatal("approved-authority load accepted an unbound instruction row")
	}
}

func TestRepositoryInstructionsEmptyBasisWhenNoneApplicable(t *testing.T) {
	fixture, prepared := preparePackageWithInstructions(t, map[string]string{
		// No AGENTS.md exists anywhere; only a non-instruction file is present.
		"README.md": "# readme\n",
	})
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 0 {
		t.Fatalf("empty basis stored rows = %#v", rows)
	}
	if prepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(nil) {
		t.Fatal("empty basis digest mismatch")
	}
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := fixture.service.LoadApprovedAuthorityForRun(context.Background(), approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RepositoryInstructions == nil || len(loaded.RepositoryInstructions) != 0 {
		t.Fatalf("approved authority empty basis = %#v, want a concrete empty list", loaded.RepositoryInstructions)
	}
}

func TestRepositoryInstructionsExcludeStandingRoleAndUnrelatedFiles(t *testing.T) {
	files := map[string]string{
		"agents/orchestrator.md": "# standing execution-role procedure\n",
		"README.md":              "# readme\n",
		"go.mod":                 "module example\n",
	}
	fixture, prepared := preparePackageWithInstructions(t, files)
	rows := storedInstructionRows(t, fixture, prepared.Package.ID)
	if len(rows) != 0 {
		t.Fatalf("standing-role or unrelated files bound as instructions: %#v", rows)
	}
	candidates := applicableInstructionPaths(inspectedSourcePaths("tickets/checkout.ticket-P2-T2.r1.delivery-ticket.json", []string{"internal/app/packages"}))
	for _, candidate := range candidates {
		if candidate == "agents/orchestrator.md" {
			t.Fatalf("agents/orchestrator.md is an instruction candidate")
		}
		if strings.HasSuffix(candidate, "README.md") || strings.HasSuffix(candidate, "go.mod") {
			t.Fatalf("unrelated file %q is an instruction candidate", candidate)
		}
	}
}

func TestRepositoryInstructionsBasisDeterminism(t *testing.T) {
	// The same closure content always produces the same digest regardless of
	// resolution order or duplicate candidates.
	files := map[string]string{
		"AGENTS.md":              "# root\n",
		"internal/AGENTS.md":     "# internal\n",
		"internal/app/AGENTS.md": "# app\n",
	}
	firstFixture, firstPrepared := preparePackageWithInstructions(t, files)
	_ = firstFixture
	want := expectedInstructionBasis(t, files)
	_, secondPrepared := preparePackageWithInstructions(t, files)
	if firstPrepared.Package.PackageSha256 != secondPrepared.Package.PackageSha256 {
		t.Fatal("identical instruction content produced different package digests")
	}
	if secondPrepared.Package.RepositoryInstructionsSha256 != repositoryInstructionsBasisSHA256(want) {
		t.Fatal("recomputed instruction digest differs")
	}
}
