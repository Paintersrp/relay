package packages

import (
	"testing"
)

func TestValidateInputAcceptsSelectionOnlyAndDoesNotInventOperations(t *testing.T) {
	validated, err := validateInput(PrepareInput{SelectionID: "selection-1"})
	if err != nil {
		t.Fatal(err)
	}
	if validated.operations != nil || validated.operationsSHA256 != "" || validated.operationsCoverage != "" {
		t.Fatalf("validated=%+v", validated)
	}
}

func TestValidateInputAcceptsOperationsOnlyWhenExactAndCanonical(t *testing.T) {
	operations := ArtifactInput{
		DisplayName: "checkout.ticket-P2-T2.r1.deterministic-operations.json",
		Bytes:       []byte("{\"schema_version\":\"1.0\",\"feature_slug\":\"checkout\",\"repo_target\":\"relay\",\"branch\":\"main\",\"base_commit\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"coverage\":\"complete\",\"operations\":[{\"path\":\"internal/example.go\",\"operation\":\"create\",\"implementation\":{\"content\":\"package example\\n\"}}]}"),
	}
	operations.ExpectedSHA256 = sha256Hex(operations.Bytes)
	validated, err := validateInput(PrepareInput{SelectionID: "selection-1", DeterministicOperations: &operations})
	if err != nil {
		t.Fatal(err)
	}
	if validated.operations == nil || validated.operations.document.Coverage != "complete" {
		t.Fatalf("validated operations=%+v", validated.operations)
	}
}

func TestValidateInputRejectsEmptyOperationsArtifact(t *testing.T) {
	operations := ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.deterministic-operations.json", Bytes: []byte{}}
	operations.ExpectedSHA256 = sha256Hex(operations.Bytes)
	if _, err := validateInput(PrepareInput{SelectionID: "selection-1", DeterministicOperations: &operations}); err == nil {
		t.Fatal("empty operations artifact was accepted")
	}
}

func TestValidateInputRejectsBlankSelectionID(t *testing.T) {
	if _, err := validateInput(PrepareInput{SelectionID: " selection-1"}); err == nil {
		t.Fatal("blank selection ID was accepted")
	}
}
