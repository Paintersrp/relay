package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "relay"

type goImport struct {
	file string
	path string
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repo root from %s", dir)
		}
		dir = parent
	}
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()

	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "build", ".tanstack", ".vite":
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk go files: %v", err)
	}
	return files
}

func importsForFile(t *testing.T, path string) []goImport {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	imports := make([]goImport, 0, len(file.Imports))
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import in %s: %v", path, err)
		}
		imports = append(imports, goImport{file: path, path: value})
	}
	return imports
}

func rel(t *testing.T, root, path string) string {
	t.Helper()

	value, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("rel %s: %v", path, err)
	}
	return filepath.ToSlash(value)
}

func isUnder(relPath, prefix string) bool {
	return relPath == prefix || strings.HasPrefix(relPath, prefix+"/")
}

func isAPIImport(path string) bool {
	return path == modulePath+"/internal/api" || strings.HasPrefix(path, modulePath+"/internal/api/")
}

func isAppImport(path string) bool {
	return path == modulePath+"/internal/app" || strings.HasPrefix(path, modulePath+"/internal/app/")
}

func isStoreImport(path string) bool {
	return path == modulePath+"/internal/store" || strings.HasPrefix(path, modulePath+"/internal/store/")
}

func isFeatureAPIFile(relPath string) bool {
	const prefix = "internal/api/"
	if !strings.HasPrefix(relPath, prefix) {
		return false
	}
	return strings.Contains(strings.TrimPrefix(relPath, prefix), "/")
}

func TestAppPackagesDoNotImportAPI(t *testing.T) {
	root := repoRoot(t)
	for _, file := range goFiles(t, filepath.Join(root, "internal", "app")) {
		for _, imp := range importsForFile(t, file) {
			if isAPIImport(imp.path) {
				t.Fatalf("%s imports API package %q; app packages must not import API packages", rel(t, root, file), imp.path)
			}
		}
	}
}

func TestStoreDoesNotImportAppOrAPI(t *testing.T) {
	root := repoRoot(t)
	for _, file := range goFiles(t, filepath.Join(root, "internal", "store")) {
		for _, imp := range importsForFile(t, file) {
			if isAPIImport(imp.path) || isAppImport(imp.path) {
				t.Fatalf("%s imports %q; store must not import API or app packages", rel(t, root, file), imp.path)
			}
		}
	}
}

func TestFeatureAPIPackagesDoNotImportLegacyRootAPI(t *testing.T) {
	root := repoRoot(t)
	apiRoot := filepath.Join(root, "internal", "api")

	for _, file := range goFiles(t, apiRoot) {
		relPath := rel(t, root, file)
		if !isFeatureAPIFile(relPath) {
			continue // root internal/api files are the legacy adapter package, not feature subpackages.
		}

		for _, imp := range importsForFile(t, file) {
			if imp.path == modulePath+"/internal/api" {
				t.Fatalf("%s imports legacy root API package; feature API packages must use feature-local handlers", relPath)
			}
		}
	}
}

func TestFeatureAPIPackagesDoNotImportStoreDirectly(t *testing.T) {
	root := repoRoot(t)
	apiRoot := filepath.Join(root, "internal", "api")

	for _, file := range goFiles(t, apiRoot) {
		relPath := rel(t, root, file)
		if !isFeatureAPIFile(relPath) {
			continue // root legacy API adapter may still use store for refactor backlog/dev smoke.
		}
		if isUnder(relPath, "internal/api/shared") {
			continue
		}

		for _, imp := range importsForFile(t, file) {
			if isStoreImport(imp.path) {
				t.Fatalf("%s imports store package %q directly; feature API packages must delegate persistence to app services", relPath, imp.path)
			}
		}
	}
}

func TestOperationRegistryDoesNotImportRuntimeLayers(t *testing.T) {
	root := repoRoot(t)
	registryRoot := filepath.Join(root, "internal", "operations", "registry")
	for _, file := range goFiles(t, registryRoot) {
		for _, imp := range importsForFile(t, file) {
			if strings.HasPrefix(imp.path, modulePath+"/internal/") {
				t.Fatalf("%s imports runtime package %q; operation registry must remain runtime-independent", rel(t, root, file), imp.path)
			}
		}
	}
}

func TestSurfaceContractsImportOnlyOperationRegistryInternally(t *testing.T) {
	root := repoRoot(t)
	surfaceRoot := filepath.Join(root, "internal", "mcp", "surfacecontracts")
	const allowed = modulePath + "/internal/operations/registry"
	for _, file := range goFiles(t, surfaceRoot) {
		for _, imp := range importsForFile(t, file) {
			if strings.HasPrefix(imp.path, modulePath+"/internal/") && imp.path != allowed {
				t.Fatalf("%s imports runtime package %q; surface contracts may import only the operation registry", rel(t, root, file), imp.path)
			}
		}
	}
}
