package packages

import (
	"testing"

	"relay/internal/testfixtures"
)

const packageBrief = testfixtures.TicketDesignBrief

func TestValidateInputAcceptsBriefOnlyAndDoesNotInventOperations(t *testing.T) {
	brief := ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.design-brief.md", Bytes: []byte(packageBrief)}
	brief.ExpectedSHA256 = sha256Hex(brief.Bytes)
	validated, err := validateInput(PrepareInput{SelectionID: "selection-1", TicketDesignBrief: brief})
	if err != nil {
		t.Fatal(err)
	}
	if validated.operations != nil || validated.brief.identity.TicketID != "P2-T2" || validated.brief.identity.Revision != 1 {
		t.Fatalf("validated=%+v", validated)
	}
}

func TestValidateInputAcceptsOperationsOnlyWhenExactAndCanonical(t *testing.T) {
	brief := ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.design-brief.md", Bytes: []byte(packageBrief)}
	brief.ExpectedSHA256 = sha256Hex(brief.Bytes)
	operations := ArtifactInput{
		DisplayName: "checkout.ticket-P2-T2.r1.deterministic-operations.json",
		Bytes:       []byte("{\"schema_version\":\"1.0\",\"feature_slug\":\"checkout\",\"repo_target\":\"relay\",\"branch\":\"main\",\"base_commit\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"coverage\":\"complete\",\"operations\":[{\"path\":\"internal/example.go\",\"operation\":\"create\",\"implementation\":{\"content\":\"package example\\n\"}}]}"),
	}
	operations.ExpectedSHA256 = sha256Hex(operations.Bytes)
	validated, err := validateInput(PrepareInput{SelectionID: "selection-1", TicketDesignBrief: brief, DeterministicOperations: &operations})
	if err != nil {
		t.Fatal(err)
	}
	if validated.operations == nil || validated.operations.document.Coverage != "complete" {
		t.Fatalf("validated operations=%+v", validated.operations)
	}
}

func TestValidateInputRejectsEmptyOperationsArtifact(t *testing.T) {
	brief := ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.design-brief.md", Bytes: []byte(packageBrief)}
	brief.ExpectedSHA256 = sha256Hex(brief.Bytes)
	operations := ArtifactInput{DisplayName: "checkout.ticket-P2-T2.r1.deterministic-operations.json", Bytes: []byte{}}
	operations.ExpectedSHA256 = sha256Hex(operations.Bytes)
	if _, err := validateInput(PrepareInput{SelectionID: "selection-1", TicketDesignBrief: brief, DeterministicOperations: &operations}); err == nil {
		t.Fatal("empty operations artifact was accepted")
	}
}
