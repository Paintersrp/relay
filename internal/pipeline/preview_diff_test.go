package pipeline

import (
	"testing"
)

func TestBuildPreviewDiffAddsAndRemovesLines(t *testing.T) {
	original := "a\nb\nc\n"
	transformed := "a\nB\nc\nd\n"

	diff := BuildPreviewDiff(original, transformed, 300)

	lines := diffLines(diff)
	expected := []string{
		"same: a",
		"remove: b",
		"add: B",
		"same: c",
		"add: d",
	}

	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d:\n%#v", len(expected), len(lines), lines)
	}
	for i := range expected {
		if lines[i] != expected[i] {
			t.Errorf("line %d: expected %q, got %q", i, expected[i], lines[i])
		}
	}
	if diff.Truncated {
		t.Error("expected Truncated=false")
	}
}

func TestBuildPreviewDiffTruncates(t *testing.T) {
	var original, transformed string
	for i := 0; i < 10; i++ {
		original += "a\n"
		transformed += "b\n"
	}

	diff := BuildPreviewDiff(original, transformed, 3)

	if !diff.Truncated {
		t.Error("expected Truncated=true")
	}
	if len(diff.Lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(diff.Lines))
	}
}

func TestBuildPreviewDiffEmptyInputs(t *testing.T) {
	diff := BuildPreviewDiff("", "", 300)
	if diff.Truncated {
		t.Error("expected Truncated=false for empty inputs")
	}
	if len(diff.Lines) != 0 {
		t.Errorf("expected 0 lines, got %d", len(diff.Lines))
	}
}

func TestBuildPreviewDiffEmptyVsContent(t *testing.T) {
	diff := BuildPreviewDiff("", "hello\nworld", 300)
	if len(diff.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(diff.Lines))
	}
	if diff.Lines[0].Kind != "add" || diff.Lines[0].Text != "hello" {
		t.Errorf("expected add:hello, got %s:%s", diff.Lines[0].Kind, diff.Lines[0].Text)
	}
	if diff.Lines[1].Kind != "add" || diff.Lines[1].Text != "world" {
		t.Errorf("expected add:world, got %s:%s", diff.Lines[1].Kind, diff.Lines[1].Text)
	}
}

func TestBuildPreviewDiffContentVsEmpty(t *testing.T) {
	diff := BuildPreviewDiff("hello\nworld", "", 300)
	if len(diff.Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(diff.Lines))
	}
	if diff.Lines[0].Kind != "remove" || diff.Lines[0].Text != "hello" {
		t.Errorf("expected remove:hello, got %s:%s", diff.Lines[0].Kind, diff.Lines[0].Text)
	}
	if diff.Lines[1].Kind != "remove" || diff.Lines[1].Text != "world" {
		t.Errorf("expected remove:world, got %s:%s", diff.Lines[1].Kind, diff.Lines[1].Text)
	}
}

func TestBuildPreviewDiffDefaultMaxLines(t *testing.T) {
	diff := BuildPreviewDiff("", "", 0)
	if len(diff.Lines) != 0 {
		t.Errorf("expected 0 lines for zero maxLines with empty input")
	}
}

func diffLines(diff PreviewDiff) []string {
	var out []string
	for _, l := range diff.Lines {
		out = append(out, l.Kind+": "+l.Text)
	}
	return out
}
