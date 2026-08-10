package packages

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourcevault"
	workflowstore "relay/internal/store/workflow"
)

func createApprovedRun(t *testing.T, reader SourceVaultReader) (*packageServiceFixture, string) {
	t.Helper()
	fixture := newPackageServiceFixture(t)
	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reader != nil {
		// Swap the reader before the approved-authority load so the load
		// re-resolves the selected Ticket source through the supplied reader.
		fixture.service.setSourceVaults(reader)
	}
	return fixture, approved.Run.RunID
}

func TestLoadApprovedAuthorityCanonicalDeliveryTicketSucceeds(t *testing.T) {
	ctx := context.Background()
	fixture, runID := createApprovedRun(t, nil)
	authority, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		t.Fatalf("LoadApprovedAuthorityForRun failed: %v", err)
	}
	doc := authority.DeliveryTicket
	if doc.DisplayName != "checkout.ticket-P2-T2.r1.delivery-ticket.json" {
		t.Errorf("DisplayName = %q, want %q", doc.DisplayName, "checkout.ticket-P2-T2.r1.delivery-ticket.json")
	}
	if doc.RelativePath != fixture.sourcePath {
		t.Errorf("RelativePath = %q, want %q", doc.RelativePath, fixture.sourcePath)
	}
	if doc.SHA256 != fixture.ticketSHA256 || !bytes.Equal(doc.Bytes, fixture.ticketDocument) {
		t.Errorf("exact Ticket bytes or digest were not preserved")
	}
	if len(authority.TicketProjection.ValidationCommands) != 1 || authority.TicketProjection.ValidationCommands[0].Command != "go test ./internal/app/packages" {
		t.Errorf("Ticket projection = %#v", authority.TicketProjection)
	}
	if len(authority.CompletedDependencies) != 0 {
		t.Errorf("ticket-only authority completed dependencies = %#v, want none", authority.CompletedDependencies)
	}
}

func TestLoadApprovedAuthorityCarriesCompletedDependencies(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	depDocument := packageTicketDocumentWithDependency(fixture.baseCommit)
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: depDocument}
	fixture.service.setSourceVaults(reader)

	db := fixture.store.DB()
	var workspaceRowID, closureRowID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM feature_workspaces WHERE workspace_id = ?`, fixture.workspaceID).Scan(&workspaceRowID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM source_vault_closures WHERE closure_id = ?`, fixture.closureID).Scan(&closureRowID); err != nil {
		t.Fatal(err)
	}
	var dependencyTicketID, dependencyRevisionID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P2-T1', ?, 5) RETURNING id`, workspaceRowID).Scan(&dependencyTicketID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, ?, 'Dependency goal.', 'Dependency context.', 'not_required') RETURNING id`, dependencyTicketID, fixture.baseCommit, closureRowID, "tickets/checkout.ticket-P2-T1.r1.delivery-ticket.json").Scan(&dependencyRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE delivery_tickets SET current_revision_row_id = ? WHERE id = ?`, dependencyRevisionID, dependencyTicketID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_ticket_revision_dependencies (revision_row_id, sequence, depends_on_revision_row_id, outcome) VALUES (?, 1, ?, 'satisfied')`, fixture.revisionID, dependencyRevisionID); err != nil {
		t.Fatal(err)
	}

	prepared, err := fixture.service.Prepare(ctx, PrepareInput{SelectionID: fixture.selectionID})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := fixture.service.Approve(ctx, ApproveInput{PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package"})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := fixture.service.LoadApprovedAuthorityForRun(ctx, approved.Run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	want := []ApprovedCompletedDependency{{Sequence: 1, TicketID: "P2-T1", Revision: 1, Outcome: "satisfied"}}
	if !reflect.DeepEqual(authority.CompletedDependencies, want) {
		t.Fatalf("completed dependencies = %#v, want %#v", authority.CompletedDependencies, want)
	}
	if len(authority.TicketDependencies) != 1 || authority.TicketDependencies[0].DependsOnRevisionRowID != dependencyRevisionID || authority.TicketDependencies[0].Outcome != "satisfied" {
		t.Fatalf("raw TicketDependencies = %#v, want unchanged raw rows", authority.TicketDependencies)
	}
}

func TestLoadApprovedAuthorityCompilerRejectsNoncanonicalPropertyOrder(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `{"schema_version":"2.0","feature_slug":"checkout"`, `{"feature_slug":"checkout","schema_version":"2.0"`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityCompilerRejectsMissingRequiredFields(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"explicit_deferrals":[],`, ``, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityCompilerRejectsEmptyRequiredCollections(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"validation_commands":[{"working_directory":"","command":"go test ./internal/app/packages","expected":"all tests pass"}]`, `"validation_commands":[]`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityCompilerRejectsUnsafeObligationPaths(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"source_area":"internal/app/packages"`, `"source_area":"../unsafe"`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityCanonicalFilenameMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := append([]byte(nil), fixture.ticketDocument...)
	reader := &customSourceVaultReader{path: "tickets/wrong.json", bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityGoalMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"goal":"Package the selected ticket."`, `"goal":"Different goal."`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityContextMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"context":"Package basis context."`, `"context":"Different context."`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityTransitionApplicabilityMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"transition_applicability":"not_required"`, `"transition_applicability":"required"`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityDependencyMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"depends_on":[]`, `"depends_on":[{"ticket_id":"P2-T1","revision":1}]`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityImplementationObligationPathMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"source_area":"internal/app/packages"`, `"source_area":"internal/other"`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityImplementationObligationTextMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"obligation":"Preserve the selected package basis."`, `"obligation":"Different text."`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityMemberOrDependencyOrderingMismatchIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(strings.Replace(string(fixture.ticketDocument), `"implementation_obligations":[{"source_area":"internal/app/packages","obligation":"Preserve the selected package basis.","prerequisites":[]}]`, `"implementation_obligations":[]`, 1))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestResolveApprovedCompletedDependenciesRequiresCompletedOutcome(t *testing.T) {
	ctx := context.Background()
	fixture := newPackageServiceFixture(t)
	db := fixture.store.DB()
	var workspaceRowID, closureRowID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM feature_workspaces WHERE workspace_id = ?`, fixture.workspaceID).Scan(&workspaceRowID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id FROM source_vault_closures WHERE closure_id = ?`, fixture.closureID).Scan(&closureRowID); err != nil {
		t.Fatal(err)
	}
	var dependencyTicketID, dependencyRevisionID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_tickets (ticket_id, workspace_row_id, external_priority) VALUES ('P2-T1', ?, 5) RETURNING id`, workspaceRowID).Scan(&dependencyTicketID); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `INSERT INTO delivery_ticket_revisions (delivery_ticket_row_id, revision_number, repo_target, branch, base_commit, source_closure_row_id, source_path, goal, context, transition_applicability) VALUES (?, 1, 'relay', 'main', ?, ?, ?, 'Dependency goal.', 'Dependency context.', 'not_required') RETURNING id`, dependencyTicketID, fixture.baseCommit, closureRowID, "tickets/checkout.ticket-P2-T1.r1.delivery-ticket.json").Scan(&dependencyRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE delivery_tickets SET current_revision_row_id = ? WHERE id = ?`, dependencyRevisionID, dependencyTicketID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO delivery_ticket_revision_dependencies (revision_row_id, sequence, depends_on_revision_row_id, outcome) VALUES (?, 1, ?, 'blocked')`, fixture.revisionID, dependencyRevisionID); err != nil {
		t.Fatal(err)
	}
	err := fixture.store.WithTx(ctx, func(tx *workflowstore.Tx) error {
		dependencies, err := tx.ListDeliveryTicketRevisionDependencies(ctx, fixture.revisionID)
		if err != nil {
			return err
		}
		_, err = resolveApprovedCompletedDependencies(ctx, tx, fixture.revisionID, dependencies)
		return err
	})
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("resolveApprovedCompletedDependencies error = %v, want ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityMalformedObjectOIDIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	reader := &customSourceVaultReader{path: fixture.sourcePath, objectOID: "INVALID-OID", bytes: append([]byte(nil), fixture.ticketDocument...)}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthoritySourceVaultErrorCodeDiscoverable(t *testing.T) {
	reader := &customSourceVaultReader{err: &sourcevault.Error{Code: sourcevault.CodeVaultUnavailable}}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if sourcevault.ErrorCode(err) != sourcevault.CodeVaultUnavailable {
		t.Fatalf("ErrorCode = %q, want %q", sourcevault.ErrorCode(err), sourcevault.CodeVaultUnavailable)
	}
}

func TestLoadApprovedAuthorityErrApprovedAuthorityInvalidDiscoverable(t *testing.T) {
	reader := &customSourceVaultReader{err: &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestServiceLoadApprovedAuthoritySourceAwareConstructorRejectsNilReader(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	_, err := NewServiceWithSourceVaults(fixture.store, nil)
	if err == nil {
		t.Fatal("NewServiceWithSourceVaults(store, nil) succeeded, want error")
	}
}

func TestServiceLoadApprovedAuthorityNoExportedTestSetterRemains(t *testing.T) {
	serviceType := reflect.TypeOf(&Service{})
	for i := 0; i < serviceType.NumMethod(); i++ {
		method := serviceType.Method(i)
		if strings.Contains(method.Name, "SetSourceVaults") {
			t.Fatalf("found exported test setter method %q", method.Name)
		}
	}
}

func TestLoadApprovedAuthorityRepeatedAndDefensiveCopy(t *testing.T) {
	fixture, runID := createApprovedRun(t, nil)
	ctx := context.Background()
	first, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(first.DeliveryTicket, second.DeliveryTicket) {
		t.Fatalf("repeated load returned different DeliveryTicket documents:\nfirst: %#v\nsecond: %#v", first.DeliveryTicket, second.DeliveryTicket)
	}

	first.DeliveryTicket.Bytes[0] = 'X'

	third, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first.DeliveryTicket.Bytes, third.DeliveryTicket.Bytes) {
		t.Fatal("mutation of returned Bytes affected subsequent load")
	}
}

func TestLoadApprovedAuthorityReadOnlyGuarantee(t *testing.T) {
	fixture, runID := createApprovedRun(t, nil)
	ctx := context.Background()
	tables := []string{
		"artifacts", "source_vault_retentions", "execution_packages",
		"delivery_ticket_selections", "execution_package_approvals", "runs",
	}
	countsBefore := make(map[string]int64)
	for _, tbl := range tables {
		var cnt int64
		if err := fixture.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		countsBefore[tbl] = cnt
	}

	if _, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID); err != nil {
		t.Fatal(err)
	}

	for _, tbl := range tables {
		var cnt int64
		if err := fixture.store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tbl).Scan(&cnt); err != nil {
			t.Fatal(err)
		}
		if cnt != countsBefore[tbl] {
			t.Errorf("table %s count changed from %d to %d", tbl, countsBefore[tbl], cnt)
		}
	}
}

func TestValidateDeliveryTicketSourcePath(t *testing.T) {
	invalidPaths := []string{
		"",
		" tickets/file.json",
		"tickets/file.json ",
		"/tickets/file.json",
		"\\tickets/file.json",
		"C:/tickets/file.json",
		"tickets\\file.json",
		"tickets//file.json",
		"./tickets/file.json",
		"tickets/../file.json",
		"tickets/file\x01.json",
	}

	for _, path := range invalidPaths {
		if err := validateDeliveryTicketSourcePath(path); err == nil {
			t.Errorf("validateDeliveryTicketSourcePath(%q) accepted invalid path", path)
		}
	}

	validPaths := []string{
		"tickets/file.json",
		"tickets/checkout/checkout.ticket-P2-T2.r1.delivery-ticket.json",
	}
	for _, path := range validPaths {
		if err := validateDeliveryTicketSourcePath(path); err != nil {
			t.Errorf("validateDeliveryTicketSourcePath(%q) rejected valid path: %v", path, err)
		}
	}
}

type customSourceVaultReader struct {
	path      string
	objectOID string
	bytes     []byte
	err       error
}

func (c *customSourceVaultReader) ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if c.err != nil {
		return sourcevault.ReadPathResult{}, c.err
	}
	if c.path != "" && request.Path != c.path {
		return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
	}
	oid := c.objectOID
	if oid == "" {
		oid = strings.Repeat("d", 40)
	}
	return sourcevault.ReadPathResult{
		ObjectOID: oid,
		Bytes:     append([]byte(nil), c.bytes...),
	}, nil
}
