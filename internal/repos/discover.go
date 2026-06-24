package repos

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

type DiscoveredRepo struct {
	Name string
	Path string
}

type ScanResult struct {
	Repos    []DiscoveredRepo
	Skipped  []string
	Warnings []string
}

var ignoredDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"data":         true,
	"tmp":          true,
	"bin":          true,
	".cache":       true,
	".next":        true,
	"dist":         true,
	"coverage":     true,
}

func Discover(root string, maxDepth int) ScanResult {
	result := ScanResult{}

	root = NormalizePath(root)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		result.Warnings = append(result.Warnings, "root does not exist: "+root)
		return result
	}

	// check if root itself is a repo
	if isRepo(root) {
		result.Repos = append(result.Repos, DiscoveredRepo{
			Name: filepath.Base(root),
			Path: root,
		})
		return result
	}

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			result.Warnings = append(result.Warnings, "walk error at "+path+": "+err.Error())
			return nil
		}

		if path == root {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		depth := len(strings.Split(filepath.ToSlash(rel), "/"))
		if depth > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		if ignoredDirs[name] {
			return filepath.SkipDir
		}

		if isRepo(path) {
			normPath := NormalizePath(path)
			result.Repos = append(result.Repos, DiscoveredRepo{
				Name: filepath.Base(path),
				Path: normPath,
			})
			return filepath.SkipDir
		}

		return nil
	})

	return result
}

func isRepo(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}
	return info.IsDir() || info.Mode().IsRegular()
}

func NormalizePath(p string) string {
	// Normalize to forward-slash form regardless of host OS so Windows-style
	// inputs (e.g. "D:\Code\relay") are handled consistently on every platform.
	// filepath.ToSlash only rewrites the host separator, which leaves
	// backslashes intact on non-Windows hosts, so convert them explicitly and
	// clean with the slash-based path package.
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	return path.Clean(p)
}
