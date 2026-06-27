package agentrefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanBackendSurface_ExtractsPackagesSymbolsTestsAndImportEdges(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "internal/app/example/service.go", `package example

type Service struct {}

func NewService() *Service { return &Service{} }

func (s *Service) DoSomething() {}
`)

	createFile(t, dir, "internal/api/example/routes.go", `package example

import "relay/internal/app/example"

type Handler struct {}

func NewHandler() *Handler { return &Handler{} }

func MountRoutes() {}
`)

	createFile(t, dir, "internal/api/example/routes_test.go", `package example

import "testing"

func TestMountRoutes(t *testing.T) {}
func TestHandlerWorks(t *testing.T) {}
`)

	createFile(t, dir, "internal/store/example/store.go", `package example

const ExportedKey = "value"
`)

	inv, err := ScanBackendSurface(dir)
	if err != nil {
		t.Fatalf("ScanBackendSurface: %v", err)
	}

	if len(inv.Packages) == 0 {
		t.Fatal("expected at least one package")
	}

	foundServicePkg := false
	foundRoutesPkg := false
	foundStorePkg := false
	for _, pkg := range inv.Packages {
		if pkg.Dir == "internal/app/example" {
			foundServicePkg = true
			if pkg.PackageName != "example" {
				t.Errorf("expected package name 'example', got %q", pkg.PackageName)
			}
			if len(pkg.SourceFiles) != 1 {
				t.Errorf("expected 1 source file, got %d", len(pkg.SourceFiles))
			}
			if len(pkg.ExportedSymbols) == 0 {
				t.Error("expected exported symbols in service package")
			}
			foundService := false
			foundNewService := false
			for _, s := range pkg.ServiceLikeSymbols {
				if s.Name == "Service" {
					foundService = true
				}
				if s.Name == "NewService" {
					foundNewService = true
				}
			}
			if !foundService {
				t.Error("expected Service type in service-like symbols")
			}
			if !foundNewService {
				t.Error("expected NewService in service-like symbols")
			}
		}
		if pkg.Dir == "internal/api/example" {
			foundRoutesPkg = true
			if len(pkg.TestFiles) != 1 {
				t.Errorf("expected 1 test file, got %d", len(pkg.TestFiles))
			}
			if len(pkg.TestFunctions) != 2 {
				t.Errorf("expected 2 test functions, got %d", len(pkg.TestFunctions))
			}
			if len(pkg.RouteMounterSymbols) != 1 {
				t.Errorf("expected 1 route mounter symbol, got %d", len(pkg.RouteMounterSymbols))
			}
			if len(pkg.HandlerLikeSymbols) == 0 {
				t.Error("expected handler-like symbols")
			}
		}
		if pkg.Dir == "internal/store/example" {
			foundStorePkg = true
		}
	}

	if !foundServicePkg {
		t.Error("did not find internal/app/example package")
	}
	if !foundRoutesPkg {
		t.Error("did not find internal/api/example package")
	}
	if !foundStorePkg {
		t.Error("did not find internal/store/example package")
	}

	foundImportEdge := false
	for _, edge := range inv.ImportEdges {
		if edge.FromDir == "internal/api/example" && strings.Contains(edge.ToImportPath, "internal/app/example") {
			foundImportEdge = true
		}
	}
	if !foundImportEdge {
		t.Error("expected import edge from internal/api/example to internal/app/example")
	}
}

func TestScanBackendSurface_ExcludesGeneratedFiles(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "internal/store/generated/models.go", `package generated
type Model struct { ID int }
`)
	createFile(t, dir, "internal/store/generated/queries.go", `package generated
func Query() {}
`)
	createFile(t, dir, "internal/store/generated/some_test.go", `package generated
import "testing"
func TestGenerated(t *testing.T) {}
`)
	createFile(t, dir, "internal/api/example/view_templ.go", `package example
type View struct {}
`)
	createFile(t, dir, "internal/api/example/real.go", `package example
type RealService struct {}
`)

	inv, err := ScanBackendSurface(dir)
	if err != nil {
		t.Fatalf("ScanBackendSurface: %v", err)
	}

	for _, pkg := range inv.Packages {
		if pkg.Dir == "internal/store/generated" {
			t.Error("generated directory should not appear in packages")
		}
		for _, f := range pkg.SourceFiles {
			if strings.Contains(f, "internal/store/generated/") {
				t.Errorf("generated file should not appear in source files: %s", f)
			}
			if strings.HasSuffix(f, "_templ.go") {
				t.Errorf("_templ.go file should not appear in source files: %s", f)
			}
		}
		for _, f := range pkg.TestFiles {
			if strings.Contains(f, "internal/store/generated/") {
				t.Errorf("generated test file should not appear: %s", f)
			}
		}
	}

	foundReal := false
	for _, pkg := range inv.Packages {
		if pkg.Dir == "internal/api/example" {
			for _, f := range pkg.SourceFiles {
				if f == "internal/api/example/real.go" {
					foundReal = true
				}
				if f == "internal/api/example/view_templ.go" {
					t.Error("view_templ.go should be excluded")
				}
			}
		}
	}
	if !foundReal {
		t.Error("real.go should still be scanned")
	}
}

func TestBuildBackendSurfaceDoc_DeterministicAndRelativePathOnly(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "internal/app/example/svc.go", `package example
type Service struct{}
func NewService() *Service { return &Service{} }
`)
	createFile(t, dir, "internal/app/example/svc_test.go", `package example
import "testing"
func TestService(t *testing.T) {}
`)

	doc1, err := BuildBackendSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildBackendSurfaceDoc (1): %v", err)
	}

	doc2, err := BuildBackendSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildBackendSurfaceDoc (2): %v", err)
	}

	json1, err := RenderJSON(doc1)
	if err != nil {
		t.Fatalf("RenderJSON (1): %v", err)
	}

	json2, err := RenderJSON(doc2)
	if err != nil {
		t.Fatalf("RenderJSON (2): %v", err)
	}

	if string(json1) != string(json2) {
		t.Error("BuildBackendSurfaceDoc is not deterministic: two runs produced different JSON")
	}

	for _, si := range doc1.SourceInputs {
		if strings.HasPrefix(si.Path, "/") {
			t.Errorf("source input path %q must not be absolute", si.Path)
		}
		if strings.Contains(si.Path, "..") {
			t.Errorf("source input path %q must not contain '..'", si.Path)
		}
		if strings.Contains(si.Path, "\\") {
			t.Errorf("source input path %q must not contain backslashes", si.Path)
		}
		if strings.Contains(si.Path, "\n") || strings.Contains(si.Path, "\r") {
			t.Errorf("source input path %q must not contain newlines", si.Path)
		}
	}

	for _, f := range doc1.Facts {
		for _, e := range f.Evidence {
			val := e.Value
			if strings.HasPrefix(val, "/") {
				t.Errorf("evidence value %q in fact %q must not be absolute", val, f.ID)
			}
			if strings.Contains(val, "..") {
				t.Errorf("evidence value %q in fact %q must not contain '..'", val, f.ID)
			}
			if strings.Contains(val, "\\") {
				t.Errorf("evidence value %q in fact %q must not contain backslashes", val, f.ID)
			}
			if strings.Contains(val, "\n") || strings.Contains(val, "\r") {
				t.Errorf("evidence value %q in fact %q must not contain newlines", val, f.ID)
			}
		}
	}
}

func TestManualMapComparison_EmitsUnresolvedForMissingBacktickedGoPath(t *testing.T) {
	dir := t.TempDir()

	createFile(t, dir, "internal/app/example/svc.go", `package example
type Service struct{}
func NewService() *Service { return &Service{} }
`)

	mapContent := "# Backend Code Surface Map\n\n| source_path | owner |\n| ----------- | ----- |\n| `internal/app/example/svc.go` | example |\n| `internal/app/ghost/missing.go` | ghost |\n"
	createFile(t, dir, "docs/backend-code-surface-map.md", mapContent)

	doc, err := BuildBackendSurfaceDoc(dir)
	if err != nil {
		t.Fatalf("BuildBackendSurfaceDoc: %v", err)
	}

	foundUnresolved := false
	mapEvidenceOnProven := false

	for _, f := range doc.Facts {
		if f.ID == "backend-map-gap-unavailable" || f.ID == "backend-map-gap-parse-error" {
			continue
		}
		if strings.HasPrefix(f.ID, "backend-map-gap-") && f.Label == FactLabelUnresolved {
			foundUnresolved = true
		}
		if f.Label == FactLabelProven {
			for _, e := range f.Evidence {
				if e.Kind == "map" {
					mapEvidenceOnProven = true
				}
			}
		}
	}

	if !foundUnresolved {
		t.Error("expected at least one unresolved fact for missing backticked Go path from manual map")
	}

	if mapEvidenceOnProven {
		t.Error("proven facts must not have map evidence; only unresolved/conflict facts may cite the manual map")
	}
}

func createFile(t *testing.T, baseDir, path, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
