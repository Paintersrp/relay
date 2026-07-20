package operations

import (
	"database/sql"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestSourceReadDependencyKeySelectsPrimaryOrExactNamedAnchor(t *testing.T) {
	if got := sourceReadDependencyKey("relay", ""); got != "repository:relay:primary" {
		t.Fatalf("primary key = %q", got)
	}
	if got := sourceReadDependencyKey("relay", "baseline"); got != "repository:relay:anchor:baseline" {
		t.Fatalf("anchor key = %q", got)
	}
}

func TestNamedAnchorResolutionRequiresOneMatchingRelationshipAndRetainedDependency(t *testing.T) {
	dependencyKey := "repository:relay:anchor:baseline"
	relationship := workflowstore.OperationPacketVaultRelationship{ID: 1, DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: dependencyKey, OwnerIdentity: "packet-anchor-owner"}
	resolved, ok := oneSourceRelationship([]workflowstore.OperationPacketVaultRelationship{relationship}, dependencyKey)
	if !ok || resolved.ID != relationship.ID {
		t.Fatalf("relationship = %#v ok=%v", resolved, ok)
	}
	if _, ok := oneSourceRelationship([]workflowstore.OperationPacketVaultRelationship{relationship, relationship}, dependencyKey); ok {
		t.Fatal("duplicate anchor relationship was accepted")
	}
	dependencies := []workflowstore.OperationPacketRetentionDependency{{DependencyClass: workflowstore.OperationPacketDependencyRepositoryVault, DependencyKey: dependencyKey, Required: true, Attached: true, Retained: true, OwnerIdentity: sql.NullString{String: relationship.OwnerIdentity, Valid: true}}}
	if !matchingRetainedDependency(dependencies, dependencyKey, relationship.OwnerIdentity) {
		t.Fatal("matching retained dependency was rejected")
	}
	dependencies[0].Retained = false
	if matchingRetainedDependency(dependencies, dependencyKey, relationship.OwnerIdentity) {
		t.Fatal("unretained dependency was accepted")
	}
}
