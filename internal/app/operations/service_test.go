package operations

import (
	"context"
	"path/filepath"
	"testing"

	workflowstore "relay/internal/store/workflow"
)

func TestServiceRejectsMissingStoreAndInvalidPacketDocument(t *testing.T) {
	if service, err := NewService(nil); err == nil || service != nil {
		t.Fatal("nil store was accepted")
	}
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Create(context.Background(), CreateInput{}); ErrorCode(err) != CodeInvalidPacketDocument {
		t.Fatalf("invalid packet error = %v", err)
	}
}
