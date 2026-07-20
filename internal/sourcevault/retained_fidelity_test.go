package sourcevault

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommitNodeParserPreservesOrderedHeadersParentsAndMessageCoordinates(t *testing.T) {
	commitOID := strings.Repeat("a", 40)
	treeOID := strings.Repeat("b", 40)
	parentOID := strings.Repeat("c", 40)
	raw := []byte("tree " + treeOID + "\nparent " + parentOID + "\nauthor A <a@example.com> 1 +0000\ncommitter A <a@example.com> 2 +0000\ngpgsig -----BEGIN\n continuation\n\nmessage\x00bytes\n")
	node, err := parseCommitNode(commitOID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if node.CommitOID != commitOID || node.TreeOID != treeOID || len(node.ParentOIDs) != 1 || node.ParentOIDs[0] != parentOID {
		t.Fatalf("node identity = %#v", node)
	}
	if node.RawSize != int64(len(raw)) || node.MessageOffset+node.MessageSize != node.RawSize || !bytes.Equal(raw[node.MessageOffset:], []byte("message\x00bytes\n")) {
		t.Fatalf("message coordinates = %#v", node)
	}
	if len(node.Headers) != 5 || string(node.Headers[4].Name) != "gpgsig" || !bytes.Contains(node.Headers[4].Raw, []byte(" continuation\n")) {
		t.Fatalf("headers = %#v", node.Headers)
	}
}

func TestCommitNodeParserRejectsMissingTree(t *testing.T) {
	_, err := parseCommitNode(strings.Repeat("a", 40), []byte("author A <a@example.com> 1 +0000\n\nmessage\n"))
	if ErrorCode(err) != CodeObjectMismatch {
		t.Fatalf("missing tree error = %v", err)
	}
}
