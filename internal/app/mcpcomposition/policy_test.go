package mcpcomposition

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"relay/internal/mcp/fileacquisition"
	"relay/internal/sourcegateway"
	workflowstore "relay/internal/store/workflow"
)

func TestOpenAndNewForwardSourceGatewayOptions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := workflowstore.Open(filepath.Join(root, "workflow.db"), filepath.Join(root, "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	fetcher := fileacquisition.FetchFunc(func(context.Context, fileacquisition.FileParameter) (fileacquisition.FetchedFile, error) {
		return fileacquisition.FetchedFile{}, errors.New("test fetcher must not run")
	})
	key := []byte("mcpcomposition-test-cursor-key-000")

	opened, err := Open(ctx, filepath.Join(root, "open-vault"), store, key, fetcher)
	if err != nil {
		t.Fatalf("Open without options: %v", err)
	}
	if _, err := New(store, opened.Vaults, opened.Publications, key, fetcher); err != nil {
		t.Fatalf("New without options: %v", err)
	}

	var optionCalls int
	option := sourcegateway.Option(func(service *sourcegateway.Service) error {
		if service == nil {
			t.Fatal("sourcegateway option received nil service")
		}
		optionCalls++
		return nil
	})
	if _, err := New(store, opened.Vaults, opened.Publications, key, fetcher, option); err != nil {
		t.Fatalf("New with option: %v", err)
	}
	if _, err := Open(ctx, filepath.Join(root, "option-vault"), store, key, fetcher, option); err != nil {
		t.Fatalf("Open with option: %v", err)
	}
	if optionCalls != 2 {
		t.Fatalf("option calls = %d, want 2", optionCalls)
	}

	invalid := sourcegateway.Option(func(*sourcegateway.Service) error {
		return errors.New("invalid test option")
	})
	if _, err := New(store, opened.Vaults, opened.Publications, key, fetcher, invalid); err == nil {
		t.Fatal("New with invalid option succeeded")
	}
	if _, err := Open(ctx, filepath.Join(root, "invalid-vault"), store, key, fetcher, invalid); err == nil {
		t.Fatal("Open with invalid option succeeded")
	}
}
