package pathsafety

import "testing"

func TestNormalizeRepoRelativePathRejectsUnsafePathsHostIndependent(t *testing.T) {
	unsafe := []string{
		`A:`,
		`C:reviewed.md`,
		`C:folder\reviewed.md`,
		`C:\folder\reviewed.md`,
		`C:/folder/reviewed.md`,
		`z:folder/file.md`,
		`/absolute/path.md`,
		`\rooted\path.md`,
		`\\server\share\file.md`,
		`//server/share/file.md`,
		`../file.md`,
		`..\file.md`,
		`nested/../../file.md`,
		`nested\..\..\file.md`,
		"nested/bad\x00file.md",
	}
	for _, value := range unsafe {
		t.Run(value, func(t *testing.T) {
			if got, ok := NormalizeRepoRelativePath(value, false); ok {
				t.Fatalf("NormalizeRepoRelativePath(%q) = %q, true; want rejection", value, got)
			}
		})
	}

	for _, value := range []string{`handoffs\planner\reviewed.md`, "handoffs/planner/reviewed.md"} {
		t.Run(value, func(t *testing.T) {
			got, ok := NormalizeRepoRelativePath(value, false)
			if !ok || got != "handoffs/planner/reviewed.md" {
				t.Fatalf("NormalizeRepoRelativePath(%q) = %q, %v; want normalized safe path", value, got, ok)
			}
		})
	}
}

func TestLooksLikePathRejectsDriveRelativeHostIndependent(t *testing.T) {
	for _, value := range []string{
		`A:`,
		`C:reviewed.md`,
		`C:folder\reviewed.md`,
		`C:\folder\reviewed.md`,
		`C:/folder/reviewed.md`,
		`z:folder/file.md`,
		`\\server\share\file.md`,
		`/absolute/path.md`,
		`\rooted\path.md`,
		`nested/file.md`,
	} {
		t.Run(value, func(t *testing.T) {
			if !LooksLikePath(value) {
				t.Fatalf("LooksLikePath(%q) = false, want true", value)
			}
		})
	}
}

func TestSafeDisplayBaseNameRejectsDriveQualifiedIdentity(t *testing.T) {
	for _, value := range []string{
		`A:`,
		`C:reviewed.md`,
		`C:folder\reviewed.md`,
		`C:\folder\reviewed.md`,
		`C:/folder/reviewed.md`,
		`z:folder/file.md`,
	} {
		t.Run(value, func(t *testing.T) {
			if got := SafeDisplayBaseName(value, "fallback.md"); got != "fallback.md" {
				t.Fatalf("SafeDisplayBaseName(%q) = %q, want fallback.md", value, got)
			}
		})
	}
	if got := SafeDisplayBaseName(`nested\reviewed.md`, "fallback.md"); got != "reviewed.md" {
		t.Fatalf("safe basename = %q, want reviewed.md", got)
	}
}
