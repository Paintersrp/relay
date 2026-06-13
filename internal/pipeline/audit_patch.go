package pipeline

import (
	"fmt"
	"strings"
)

type AuditPatchLine struct {
	Kind string
	Text string
}

type AuditPatchHunk struct {
	Header string
	Lines  []AuditPatchLine
}

type AuditPatchFile struct {
	Header     string
	OldPath    string
	NewPath    string
	Path       string
	ChangeType string
	Binary     bool
	Created    bool
	Deleted    bool
	Renamed    bool
	MetaLines  []string
	Hunks      []AuditPatchHunk
}

func ParseUnifiedDiffPatch(patch string) []AuditPatchFile {
	patch = strings.TrimSpace(patch)
	if patch == "" {
		return nil
	}

	lines := strings.Split(patch, "\n")
	files := make([]AuditPatchFile, 0)
	var current *AuditPatchFile
	var currentHunk *AuditPatchHunk

	flushHunk := func() {
		if current != nil && currentHunk != nil {
			current.Hunks = append(current.Hunks, *currentHunk)
			currentHunk = nil
		}
	}

	flushFile := func() {
		flushHunk()
		if current == nil {
			return
		}
		current.finalize()
		files = append(files, *current)
		current = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			flushFile()
			current = &AuditPatchFile{Header: line}
			current.parseDiffHeader(line)
			continue
		}

		if current == nil {
			continue
		}

		if strings.HasPrefix(line, "@@ ") {
			flushHunk()
			currentHunk = &AuditPatchHunk{Header: line}
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.Created = true
			current.MetaLines = append(current.MetaLines, line)
		case strings.HasPrefix(line, "deleted file mode "):
			current.Deleted = true
			current.MetaLines = append(current.MetaLines, line)
		case strings.HasPrefix(line, "rename from "):
			current.Renamed = true
			current.MetaLines = append(current.MetaLines, line)
			current.OldPath = normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "rename from ")))
		case strings.HasPrefix(line, "rename to "):
			current.Renamed = true
			current.MetaLines = append(current.MetaLines, line)
			current.NewPath = normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "rename to ")))
		case strings.HasPrefix(line, "copy from "):
			current.MetaLines = append(current.MetaLines, line)
		case strings.HasPrefix(line, "copy to "):
			current.MetaLines = append(current.MetaLines, line)
		case strings.HasPrefix(line, "--- "):
			current.MetaLines = append(current.MetaLines, line)
			path := normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "--- ")))
			if path == "/dev/null" {
				current.Created = true
			} else {
				current.OldPath = path
			}
		case strings.HasPrefix(line, "+++ "):
			current.MetaLines = append(current.MetaLines, line)
			path := normalizeDiffPath(strings.TrimSpace(strings.TrimPrefix(line, "+++ ")))
			if path == "/dev/null" {
				current.Deleted = true
			} else {
				current.NewPath = path
			}
		case strings.HasPrefix(line, "Binary files ") || strings.HasPrefix(line, "GIT binary patch"):
			current.Binary = true
			current.MetaLines = append(current.MetaLines, line)
		default:
			if currentHunk != nil {
				currentHunk.Lines = append(currentHunk.Lines, AuditPatchLine{
					Kind: classifyDiffLine(line),
					Text: line,
				})
			} else {
				current.MetaLines = append(current.MetaLines, line)
			}
		}
	}

	flushFile()
	return files
}

func (f *AuditPatchFile) parseDiffHeader(line string) {
	rest := strings.TrimSpace(strings.TrimPrefix(line, "diff --git "))
	fields := strings.Fields(rest)
	if len(fields) < 2 {
		return
	}
	f.OldPath = normalizeDiffPath(fields[0])
	f.NewPath = normalizeDiffPath(fields[1])
}

func (f *AuditPatchFile) finalize() {
	if f.NewPath != "" && f.NewPath != "/dev/null" {
		f.Path = f.NewPath
	} else if f.OldPath != "" && f.OldPath != "/dev/null" {
		f.Path = f.OldPath
	}

	switch {
	case f.Binary:
		f.ChangeType = "binary"
	case f.Renamed:
		f.ChangeType = "renamed"
	case f.Created || f.OldPath == "/dev/null":
		f.ChangeType = "added"
	case f.Deleted || f.NewPath == "/dev/null":
		f.ChangeType = "deleted"
	default:
		f.ChangeType = "modified"
	}
}

func normalizeDiffPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "/dev/null" {
		return path
	}
	path = strings.TrimPrefix(path, "a/")
	path = strings.TrimPrefix(path, "b/")
	return path
}

func classifyDiffLine(line string) string {
	switch {
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		return "add"
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		return "delete"
	case strings.HasPrefix(line, " "):
		return "context"
	default:
		return "meta"
	}
}

func renderAuditPatchFileExcerpt(file AuditPatchFile, maxChars int) (string, bool) {
	if maxChars <= 0 {
		maxChars = 20000
	}

	var b strings.Builder
	currentLen := 0
	truncated := false

	appendLine := func(line string) bool {
		segment := line + "\n"
		if currentLen+len(segment) > maxChars {
			truncated = true
			return false
		}
		b.WriteString(segment)
		currentLen += len(segment)
		return true
	}

	if file.Header != "" && !appendLine(file.Header) {
		return strings.TrimSpace(b.String()), true
	}

	for _, meta := range file.MetaLines {
		if !appendLine(meta) {
			break
		}
	}

	for _, hunk := range file.Hunks {
		var hunkBuilder strings.Builder
		hunkBuilder.WriteString(hunk.Header)
		hunkBuilder.WriteByte('\n')
		for _, line := range hunk.Lines {
			hunkBuilder.WriteString(line.Text)
			hunkBuilder.WriteByte('\n')
		}
		hunkText := hunkBuilder.String()
		if currentLen+len(hunkText) > maxChars {
			truncated = true
			break
		}
		b.WriteString(hunkText)
		currentLen += len(hunkText)
	}

	return strings.TrimSpace(b.String()), truncated
}

func countAuditPatchLineKinds(file AuditPatchFile) (added, deleted, context int) {
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case "add":
				added++
			case "delete":
				deleted++
			case "context":
				context++
			}
		}
	}
	return added, deleted, context
}

func auditPatchFilePath(file AuditPatchFile) string {
	if file.Path != "" {
		return file.Path
	}
	if file.NewPath != "" && file.NewPath != "/dev/null" {
		return file.NewPath
	}
	if file.OldPath != "" && file.OldPath != "/dev/null" {
		return file.OldPath
	}
	return "unknown"
}

func auditPatchFileSummary(file AuditPatchFile) string {
	added, deleted, context := countAuditPatchLineKinds(file)
	parts := []string{
		fmt.Sprintf("path=%s", auditPatchFilePath(file)),
		fmt.Sprintf("change=%s", file.ChangeType),
		fmt.Sprintf("added=%d", added),
		fmt.Sprintf("deleted=%d", deleted),
		fmt.Sprintf("context=%d", context),
	}
	if file.Binary {
		parts = append(parts, "binary=true")
	}
	if file.Renamed {
		parts = append(parts, "renamed=true")
	}
	if file.Created {
		parts = append(parts, "created=true")
	}
	if file.Deleted {
		parts = append(parts, "deleted=true")
	}
	return strings.Join(parts, ", ")
}
