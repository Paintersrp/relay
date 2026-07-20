package sourcegateway

import (
	"strings"
	"testing"

	"relay/internal/app/operations"
	"relay/internal/operations/registry"
	workflowstore "relay/internal/store/workflow"
)

func testFidelityAuthority(anchor string) operations.SourceReadAuthority {
	dependency := "repository:relay:primary"
	if anchor != "" {
		dependency = "repository:relay:anchor:" + anchor
	}
	return operations.SourceReadAuthority{
		Summary:       operations.PacketSummary{PacketID: "packet", PacketSHA256: strings.Repeat("a", 64), SurfaceContract: registry.SurfaceContractID("surface"), OperationID: registry.OperationID("operation"), ProjectID: "project"},
		RepositoryKey: "relay", DependencyKey: dependency, AnchorName: anchor, PublicationID: "publication",
		Relationship: workflowstore.OperationPacketVaultRelationship{ID: 1, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: dependency, CommitOID: strings.Repeat("b", 40), TreeOID: strings.Repeat("c", 40)},
	}
}

func TestFidelityCursorBindsExactDependencyAndAnchor(t *testing.T) {
	authority := testFidelityAuthority("baseline")
	cursor := fidelityCursorBase(authority, "history", strings.Repeat("d", 64))
	if !fidelityCursorMatches(cursor, authority, "history", strings.Repeat("d", 64)) {
		t.Fatal("matching fidelity cursor was rejected")
	}
	changed := authority
	changed.DependencyKey = "repository:relay:primary"
	changed.AnchorName = ""
	if fidelityCursorMatches(cursor, changed, "history", strings.Repeat("d", 64)) {
		t.Fatal("cursor crossed dependency authority")
	}
}

func TestComparisonEntryIdentityIncludesExactPathBytesAndKind(t *testing.T) {
	before := comparisonEntryID(ChangeModification, []byte{0x80}, "100644", "blob", strings.Repeat("a", 40), nil, "", "", "")
	after := comparisonEntryID(ChangeModification, []byte{0x81}, "100644", "blob", strings.Repeat("a", 40), nil, "", "", "")
	if before == after || len(before) != 64 {
		t.Fatalf("comparison identities before=%q after=%q", before, after)
	}
}
