package pipeline

import (
	"strings"
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

func TestBuildPreviewDiffOmitsDistantUnchangedLines(t *testing.T) {
	original := strings.Join([]string{
		"same-1",
		"same-2",
		"same-3",
		"same-4",
		"old-line",
		"same-5",
		"same-6",
		"same-7",
		"same-8",
	}, "\n")

	transformed := strings.Join([]string{
		"same-1",
		"same-2",
		"same-3",
		"same-4",
		"new-line",
		"same-5",
		"same-6",
		"same-7",
		"same-8",
	}, "\n")

	diff := BuildPreviewDiff(original, transformed, 300)

	var kinds []string
	for _, l := range diff.Lines {
		kinds = append(kinds, l.Kind)
	}

	hasRemove := false
	hasAdd := false
	for _, l := range diff.Lines {
		if l.Kind == "remove" && l.Text == "old-line" {
			hasRemove = true
		}
		if l.Kind == "add" && l.Text == "new-line" {
			hasAdd = true
		}
	}
	if !hasRemove {
		t.Error("expected remove line old-line")
	}
	if !hasAdd {
		t.Error("expected add line new-line")
	}

	// Distant unchanged lines like same-1 and same-8 should be outside context window (3 lines around change at index 4)
	// Change is at index 4, so context covers indices 1-7. same-1 is at index 0, outside context.
	for _, l := range diff.Lines {
		if l.Kind == "same" && l.Text == "same-1" {
			t.Error("did not expect same-1 (too far from change)")
		}
	}
}

func TestBuildPreviewDiffInsertsSkipBetweenDistantHunks(t *testing.T) {
	original := strings.Join([]string{
		"a",
		"old-1",
		"b",
		"c",
		"d",
		"e",
		"f",
		"g",
		"h",
		"old-2",
		"i",
	}, "\n")

	transformed := strings.Join([]string{
		"a",
		"new-1",
		"b",
		"c",
		"d",
		"e",
		"f",
		"g",
		"h",
		"new-2",
		"i",
	}, "\n")

	diff := BuildPreviewDiff(original, transformed, 300)

	hasSkip := false
	for _, l := range diff.Lines {
		if l.Kind == "skip" {
			hasSkip = true
			break
		}
	}
	if !hasSkip {
		t.Error("expected at least one skip line between distant hunks")
	}
}

func TestBuildPreviewDiffNoChangesReturnsEmpty(t *testing.T) {
	diff := BuildPreviewDiff("line one\nline two\nline three", "line one\nline two\nline three", 300)
	if len(diff.Lines) != 0 {
		t.Errorf("expected 0 lines for identical inputs, got %d", len(diff.Lines))
	}
	if diff.Truncated {
		t.Error("expected Truncated=false")
	}
}

func TestBuildPreviewDiffTruncatesCompactHunks(t *testing.T) {
	var original, transformed strings.Builder
	for i := 0; i < 50; i++ {
		original.WriteString("same\n")
		transformed.WriteString("same\n")
	}
	// Insert many changes in the middle
	for i := 0; i < 50; i++ {
		original.WriteString("old-line\n")
		transformed.WriteString("new-line\n")
	}

	diff := BuildPreviewDiff(original.String(), transformed.String(), 10)

	if !diff.Truncated {
		t.Error("expected Truncated=true for large compacted output")
	}
	if len(diff.Lines) > 10 {
		t.Errorf("expected at most 10 lines, got %d", len(diff.Lines))
	}
}

func diffLines(diff PreviewDiff) []string {
	var out []string
	for _, l := range diff.Lines {
		out = append(out, l.Kind+": "+l.Text)
	}
	return out
}
