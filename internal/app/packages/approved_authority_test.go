package packages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"relay/internal/sourcevault"
)

func createApprovedRun(t *testing.T, reader SourceVaultReader) (*packageServiceFixture, string) {
	t.Helper()
	fixture := newPackageServiceFixture(t)
	if reader != nil {
		svc, err := NewServiceWithSourceVaults(fixture.store, reader)
		if err != nil {
			t.Fatal(err)
		}
		fixture.service = svc
	}
	prepared := preparePackage(t, fixture, false)
	approved, err := fixture.service.Approve(context.Background(), ApproveInput{
		PackageID: prepared.Package.PackageID, ExpectedPackageSha256: prepared.Package.PackageSha256, OperatorConfirmationEvidence: "approve package",
	})
	if err != nil {
		t.Fatal(err)
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
}

func TestLoadApprovedAuthorityInsignificantSourceWhitespacePreserved(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(fmt.Sprintf(" {\n  \"schema_version\": \"1.0\",\n  \"feature_slug\": \"checkout\",\n  \"ticket_id\": \"P2-T2\",\n  \"revision\": 1,\n  \"replaces_revision\": null,\n  \"repo_target\": \"relay\",\n  \"branch\": \"main\",\n  \"base_commit\": %q,\n  \"goal\": \"Package the selected ticket.\",\n  \"context\": \"Package basis context.\",\n  \"scope\": {\"in_scope\":[\"Package service.\"],\"out_of_scope\":[\"Unrelated work.\"]},\n  \"depends_on\": [],\n  \"implementation_obligations\": [{\"path\":\"internal/app/packages\",\"obligation\":\"Preserve the selected package basis.\"}],\n  \"validation_intent\": [\"Validate package creation.\"],\n  \"transition_applicability\": \"not_required\",\n  \"completion_criteria\": [\"All tests pass.\"]\n} ", fixture.baseCommit))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	authority, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(authority.DeliveryTicket.Bytes, rawBytes) {
		t.Fatalf("whitespace bytes changed: got %q, want %q", authority.DeliveryTicket.Bytes, rawBytes)
	}
}

func TestLoadApprovedAuthorityCompilerRejectsNoncanonicalPropertyOrder(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := []byte(fmt.Sprintf(`{"feature_slug":"checkout","schema_version":"1.0","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":[],"out_of_scope":[]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":[],"transition_applicability":"not_required","completion_criteria":[]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"../unsafe","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := packageDeliveryTicketBytes(fixture.baseCommit)
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Different goal.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Different context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[{"ticket_id":"P2-T1","revision":1}],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/other","obligation":"Preserve the selected package basis."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[{"path":"internal/app/packages","obligation":"Different text."}],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
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
	rawBytes := []byte(fmt.Sprintf(`{"schema_version":"1.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":%q,"goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"implementation_obligations":[],"validation_intent":["Validate package creation."],"transition_applicability":"not_required","completion_criteria":["All tests pass."]}`, fixture.baseCommit))
	reader := &customSourceVaultReader{path: fixture.sourcePath, bytes: rawBytes}
	fixture, runID := createApprovedRun(t, reader)
	ctx := context.Background()
	_, err := fixture.service.LoadApprovedAuthorityForRun(ctx, runID)
	if err == nil || !errors.Is(err, ErrApprovedAuthorityInvalid) {
		t.Fatalf("error = %v, want wrapping ErrApprovedAuthorityInvalid", err)
	}
}

func TestLoadApprovedAuthorityMalformedObjectOIDIsRejected(t *testing.T) {
	fixture := newPackageServiceFixture(t)
	rawBytes := packageDeliveryTicketBytes(fixture.baseCommit)
	reader := &customSourceVaultReader{path: fixture.sourcePath, objectOID: "INVALID-OID", bytes: rawBytes}
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
