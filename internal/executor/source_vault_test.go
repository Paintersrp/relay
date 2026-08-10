package executor

import (
	"context"
	"fmt"
	"strings"

	executionpackages "relay/internal/app/packages"
	"relay/internal/sourcevault"
)

// stubSourceVaultReader is a narrow package-private test implementation of
// executionpackages.SourceVaultReader. By default it returns
// sourcevault.CodeObjectUnavailable so tests that do not exercise retained
// authority fail closed. When configured with a matching path and canonical
// Delivery Ticket bytes it returns those bytes for source-vault reads.
type stubSourceVaultReader struct {
	path  string
	bytes []byte
	err   error
}

func (r *stubSourceVaultReader) ReadPath(ctx context.Context, request sourcevault.ReadPathRequest) (sourcevault.ReadPathResult, error) {
	if r.err != nil {
		return sourcevault.ReadPathResult{}, r.err
	}
	if r.path != "" && request.Path == r.path {
		return sourcevault.ReadPathResult{
			ObjectOID: strings.Repeat("d", 40),
			Bytes:     append([]byte(nil), r.bytes...),
		}, nil
	}
	return sourcevault.ReadPathResult{}, &sourcevault.Error{Code: sourcevault.CodeObjectUnavailable}
}

func (r *stubSourceVaultReader) WithErr(err error) *stubSourceVaultReader {
	return &stubSourceVaultReader{path: r.path, bytes: r.bytes, err: err}
}

func newUnavailableSourceVaultReader() *stubSourceVaultReader {
	return &stubSourceVaultReader{}
}

func newPackageSourceVaultReader(path string, baseCommit string) *stubSourceVaultReader {
	return &stubSourceVaultReader{path: path, bytes: packageDeliveryTicketBytes(baseCommit)}
}

func packageDeliveryTicketBytes(baseCommit string) []byte {
	return []byte(fmt.Sprintf(`{"schema_version":"2.0","feature_slug":"checkout","ticket_id":"P2-T2","revision":1,"replaces_revision":null,"repo_target":"relay","branch":"main","base_commit":"%s","goal":"Package the selected ticket.","context":"Package basis context.","scope":{"in_scope":["Package service."],"out_of_scope":["Unrelated work."]},"depends_on":[],"required_invariants":["Packages must bind the exact approved Ticket."],"forbidden_behaviors":[],"implementation_obligations":[{"source_area":"internal/app/packages","obligation":"Preserve the selected package basis.","prerequisites":[]}],"proof_obligations":["Prove package preparation binds the approved Ticket."],"validation_commands":[{"working_directory":"","command":"go test ./internal/app/packages","expected":"all tests pass"}],"transition_applicability":"not_required","explicit_deferrals":[],"completion_criteria":["All tests pass."]}`, baseCommit))
}

// packageDeliveryTicketBytesWithDependency returns the canonical approved
// Delivery Ticket bytes with one completed depends-on entry (P2-T1 revision 1),
// matching the dependency seeded by the assignment integration fixture.
func packageDeliveryTicketBytesWithDependency(baseCommit string) []byte {
	return []byte(strings.Replace(string(packageDeliveryTicketBytes(baseCommit)), `"depends_on":[]`, `"depends_on":[{"ticket_id":"P2-T1","revision":1}]`, 1))
}

var _ executionpackages.SourceVaultReader = (*stubSourceVaultReader)(nil)
