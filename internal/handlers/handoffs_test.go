package handlers

import "testing"

func TestNormalizeManualRepoInputRejectsBlankPath(t *testing.T) {
	_, _, err := normalizeManualRepoInput("relay", "")
	if err == nil {
		t.Fatal("expected error for blank path")
	}
}

func TestNormalizeManualRepoInputRejectsWhitespacePath(t *testing.T) {
	_, _, err := normalizeManualRepoInput("relay", "  ")
	if err == nil {
		t.Fatal("expected error for whitespace path")
	}
}

func TestNormalizeManualRepoInputDerivesNameFromPath(t *testing.T) {
	name, path, err := normalizeManualRepoInput("", "D:/Code/relay")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "relay" {
		t.Fatalf("expected name 'relay', got %q", name)
	}
	if path != "D:/Code/relay" {
		t.Fatalf("expected path 'D:/Code/relay', got %q", path)
	}
}

func TestNormalizeManualRepoInputUsesGivenName(t *testing.T) {
	name, path, err := normalizeManualRepoInput("my-project", "D:/Code/relay")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "my-project" {
		t.Fatalf("expected name 'my-project', got %q", name)
	}
	if path != "D:/Code/relay" {
		t.Fatalf("expected path 'D:/Code/relay', got %q", path)
	}
}

func TestNormalizeManualRepoInputRejectsDotPath(t *testing.T) {
	_, _, err := normalizeManualRepoInput("relay", ".")
	if err == nil {
		t.Fatal("expected error for dot path")
	}
}


