package applier

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"relay/internal/speccompiler"
)

type virtualFile struct {
	exists  bool
	content string
	mode    os.FileMode
}

type mutationActionKind string

const (
	mutationWrite  mutationActionKind = "write"
	mutationRemove mutationActionKind = "remove"
	mutationRename mutationActionKind = "rename"
)

type mutationAction struct {
	kind                mutationActionKind
	path                string
	absolute            string
	destination         string
	destinationAbsolute string
	content             string
	mode                os.FileMode
}

func (a mutationAction) paths() []string {
	if a.kind == mutationRename {
		return []string{a.path, a.destination}
	}
	return []string{a.path}
}

func workspaceRoot(value string) (string, string) {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return "", "workspace root is required without outer whitespace"
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Sprintf("resolve workspace root: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", "workspace root is unavailable or not a directory"
	}
	return filepath.Clean(root), ""
}

func safePath(root, rel string) (string, string, error) {
	if strings.TrimSpace(rel) == "" || strings.TrimSpace(rel) != rel {
		return "", "", fmt.Errorf("path must be nonblank without outer whitespace")
	}
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "//") || strings.Contains(rel, "\\") || strings.Contains(rel, ":") {
		return "", "", fmt.Errorf("unsafe repository path: %s", rel)
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", fmt.Errorf("unsafe repository path: %s", rel)
		}
	}
	if parts[0] == ".git" {
		return "", "", fmt.Errorf("unsafe repository path targets git metadata: %s", rel)
	}
	absolute := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
	prefix := root + string(os.PathSeparator)
	if absolute != root && !strings.HasPrefix(absolute, prefix) {
		return "", "", fmt.Errorf("unsafe repository path escapes workspace: %s", rel)
	}
	return filepath.ToSlash(rel), absolute, nil
}

func preflightResidualPathChain(root string, replay speccompiler.ReplaySemantics, works []speccompiler.ProjectedFileWork) error {
	paths := make([]string, 0)
	for _, work := range works {
		paths = appendUnique(paths, work.Path)
		if work.Operation == "rename" {
			paths = appendUnique(paths, work.DestinationPath)
		}
	}
	state := make(map[string]virtualFile, len(paths))
	for _, path := range paths {
		clean, absolute, err := safePath(root, path)
		if err != nil {
			return err
		}
		file, err := inspectPath(absolute)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", clean, err)
		}
		state[clean] = file
	}
	base := cloneVirtualState(state)
	for _, work := range works {
		path := filepath.ToSlash(work.Path)
		current := state[path]
		switch work.Operation {
		case "create":
			if current.exists {
				return fmt.Errorf("create destination already exists: %s", path)
			}
			state[path] = virtualFile{exists: true, content: work.Content, mode: 0o644}
		case "modify":
			if !current.exists {
				return fmt.Errorf("modify source is missing: %s", path)
			}
			updated, err := replayDirectives(replay, path, current.content, base[path], work.Directives)
			if err != nil {
				return err
			}
			current.content = updated
			state[path] = current
		case "delete":
			if !current.exists {
				return fmt.Errorf("delete source is missing: %s", path)
			}
			state[path] = virtualFile{}
		case "rename":
			destination := filepath.ToSlash(work.DestinationPath)
			if !current.exists {
				return fmt.Errorf("rename source is missing: %s", path)
			}
			if state[destination].exists {
				return fmt.Errorf("rename destination already exists: %s", destination)
			}
			if work.PreserveContent {
				state[destination] = current
			} else {
				if work.Content == "" {
					return fmt.Errorf("rename replacement content is empty: %s", work.Ref)
				}
				current.content = work.Content
				state[destination] = current
			}
			state[path] = virtualFile{}
		default:
			return fmt.Errorf("unsupported projected file operation: %s", work.Operation)
		}
	}
	return nil
}

func preflightPathChain(root string, replay speccompiler.ReplaySemantics, works []speccompiler.ProjectedFileWork) ([]mutationAction, error) {
	paths := make([]string, 0)
	for _, work := range works {
		paths = appendUnique(paths, work.Path)
		if work.Operation == "rename" {
			paths = appendUnique(paths, work.DestinationPath)
		}
	}
	state := make(map[string]virtualFile, len(paths))
	absoluteByPath := make(map[string]string, len(paths))
	for _, path := range paths {
		clean, absolute, err := safePath(root, path)
		if err != nil {
			return nil, err
		}
		file, err := inspectPath(absolute)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", clean, err)
		}
		state[clean] = file
		absoluteByPath[clean] = absolute
	}
	base := cloneVirtualState(state)
	actions := make([]mutationAction, 0, len(works))
	for _, work := range works {
		path := filepath.ToSlash(work.Path)
		current := state[path]
		switch work.Operation {
		case "create":
			if current.exists {
				return nil, fmt.Errorf("create destination already exists: %s", path)
			}
			current = virtualFile{exists: true, content: work.Content, mode: 0o644}
			state[path] = current
			actions = append(actions, mutationAction{kind: mutationWrite, path: path, absolute: absoluteByPath[path], content: work.Content, mode: current.mode})
		case "modify":
			if !current.exists {
				return nil, fmt.Errorf("modify source is missing: %s", path)
			}
			updated, err := replayDirectives(replay, path, current.content, base[path], work.Directives)
			if err != nil {
				return nil, err
			}
			current.content = updated
			state[path] = current
			actions = append(actions, mutationAction{kind: mutationWrite, path: path, absolute: absoluteByPath[path], content: updated, mode: current.mode})
		case "delete":
			if !current.exists {
				return nil, fmt.Errorf("delete source is missing: %s", path)
			}
			state[path] = virtualFile{}
			actions = append(actions, mutationAction{kind: mutationRemove, path: path, absolute: absoluteByPath[path]})
		case "rename":
			destination := filepath.ToSlash(work.DestinationPath)
			if !current.exists {
				return nil, fmt.Errorf("rename source is missing: %s", path)
			}
			if state[destination].exists {
				return nil, fmt.Errorf("rename destination already exists: %s", destination)
			}
			state[destination] = current
			state[path] = virtualFile{}
			actions = append(actions, mutationAction{
				kind:                mutationRename,
				path:                path,
				absolute:            absoluteByPath[path],
				destination:         destination,
				destinationAbsolute: absoluteByPath[destination],
			})
		default:
			return nil, fmt.Errorf("unsupported projected file operation: %s", work.Operation)
		}
	}
	return actions, nil
}

func replayDirectives(replay speccompiler.ReplaySemantics, path, content string, base virtualFile, directives []speccompiler.ProjectedDirective) (string, error) {
	current := content
	for _, directive := range directives {
		switch directive.Kind {
		case "replace":
			if count := strings.Count(current, directive.OldText); count != directive.ExpectedOccurrences {
				return "", fmt.Errorf("replace for %s expected %d occurrence(s), found %d: %s", path, directive.ExpectedOccurrences, count, directive.Ref)
			}
			current = strings.Replace(current, directive.OldText, directive.NewText, directive.ExpectedOccurrences)
		case "insert_before":
			if count := strings.Count(current, directive.Anchor); count != directive.ExpectedOccurrences {
				return "", fmt.Errorf("insert_before for %s expected %d occurrence(s), found %d: %s", path, directive.ExpectedOccurrences, count, directive.Ref)
			}
			current = strings.Replace(current, directive.Anchor, directive.Content+directive.Anchor, directive.ExpectedOccurrences)
		case "insert_after":
			if count := strings.Count(current, directive.Anchor); count != directive.ExpectedOccurrences {
				return "", fmt.Errorf("insert_after for %s expected %d occurrence(s), found %d: %s", path, directive.ExpectedOccurrences, count, directive.Ref)
			}
			current = strings.Replace(current, directive.Anchor, directive.Anchor+directive.Content, directive.ExpectedOccurrences)
		case "remove":
			if count := strings.Count(current, directive.OldText); count != directive.ExpectedOccurrences {
				return "", fmt.Errorf("remove for %s expected %d occurrence(s), found %d: %s", path, directive.ExpectedOccurrences, count, directive.Ref)
			}
			current = strings.Replace(current, directive.OldText, "", directive.ExpectedOccurrences)
		case "replace_file":
			current = directive.Content
		default:
			return "", fmt.Errorf("unsupported deterministic directive: %s", directive.Ref)
		}
	}
	return current, nil
}

func directiveSelector(directive speccompiler.ProjectedDirective) string {
	switch directive.Kind {
	case "replace", "remove":
		return directive.OldText
	case "insert_before", "insert_after":
		return directive.Anchor
	default:
		return ""
	}
}

func inspectPath(absolute string) (virtualFile, error) {
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return virtualFile{}, nil
	}
	if err != nil {
		return virtualFile{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return virtualFile{}, fmt.Errorf("symbolic links are not supported")
	}
	if !info.Mode().IsRegular() {
		return virtualFile{}, fmt.Errorf("path is not a regular file")
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return virtualFile{}, err
	}
	return virtualFile{exists: true, content: string(content), mode: info.Mode().Perm()}, nil
}

func cloneVirtualState(source map[string]virtualFile) map[string]virtualFile {
	copyState := make(map[string]virtualFile, len(source))
	for path, file := range source {
		copyState[path] = file
	}
	return copyState
}

func applyMutationActions(actions []mutationAction) ([]string, error) {
	changed := make([]string, 0)
	for _, action := range actions {
		switch action.kind {
		case mutationWrite:
			if err := os.MkdirAll(filepath.Dir(action.absolute), 0o755); err != nil {
				return sortedUnique(changed), err
			}
			mode := action.mode
			if mode == 0 {
				mode = 0o644
			}
			if err := os.WriteFile(action.absolute, []byte(action.content), mode); err != nil {
				return sortedUnique(append(changed, action.path)), err
			}
			changed = append(changed, action.path)
		case mutationRemove:
			if err := os.Remove(action.absolute); err != nil {
				return sortedUnique(append(changed, action.path)), err
			}
			changed = append(changed, action.path)
		case mutationRename:
			if err := os.MkdirAll(filepath.Dir(action.destinationAbsolute), 0o755); err != nil {
				return sortedUnique(changed), err
			}
			if err := os.Rename(action.absolute, action.destinationAbsolute); err != nil {
				return sortedUnique(append(changed, action.path, action.destination)), err
			}
			changed = append(changed, action.path, action.destination)
		default:
			return sortedUnique(changed), fmt.Errorf("unsupported mutation action %q", action.kind)
		}
	}
	return sortedUnique(changed), nil
}

func sortedUnique(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		seen[value] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
