package workflowfixture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepositoryTestsDoNotOpenEmptyWorkflowDatabasesDirectly(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	violations, err := directEmptyWorkflowOpens(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) != 0 {
		t.Fatalf("ordinary tests must use workflowfixture:\n%s", strings.Join(violations, "\n"))
	}
}

func TestDirectEmptyWorkflowOpenDetection(t *testing.T) {
	tests := []struct {
		name, packagePath, source string
		want                        int
	}{
		{"inline filepath.Join", "sample", `package sample; import ("path/filepath"; workflowstore "relay/internal/store/workflow"); func f() { workflowstore.Open(filepath.Join("a", "b"), "c") }`, 1},
		{"assigned path", "sample", `package sample; import workflowstore "relay/internal/store/workflow"; func f() { path := "workflow.db"; workflowstore.Open(path, "c") }`, 1},
		{"helper path", "sample", `package sample; import workflowstore "relay/internal/store/workflow"; func path() string { return "workflow.db" }; func f() { workflowstore.Open(path(), "c") }`, 1},
		{"aliased import", "sample", `package sample; import ws "relay/internal/store/workflow"; func f() { ws.Open("a", "b") }`, 1},
		{"dot import", "sample", `package sample; import . "relay/internal/store/workflow"; func f() { Open("a", "b") }`, 1},
		{"local package", "internal/store/workflow", `package workflow; func f() { Open("a", "b") }`, 1},
		{"allowed migration", "internal/mcp/wayfinder_cold_start_test.go", `package mcp; import workflowstore "relay/internal/store/workflow"; func f() { workflowstore.Open("a", "b") }`, 0},
		{"ordinary reopen", "sample", `package sample; import workflowstore "relay/internal/store/workflow"; func f() { workflowstore.Open("a", "b") }`, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, "fixture_test.go", test.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			aliases := workflowStoreAliases(file)
			got := 0
			if _, allowed := workflowStoreOpenAllowlist[test.packagePath]; !allowed {
				got = len(prohibitedCalls(set, file, test.packagePath, test.packagePath, aliases))
			}
			if got != test.want {
				t.Fatalf("prohibited calls = %d, want %d", got, test.want)
			}
		})
	}
}

var workflowStoreOpenAllowlist = map[string]string{
	"internal/mcp/wayfinder_cold_start_test.go": "exercises workflow schema migration upgrade from a legacy database",
}

func directEmptyWorkflowOpens(root string) ([]string, error) {
	set := token.NewFileSet()
	var violations []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		aliases := workflowStoreAliases(file)
		relative, _ := filepath.Rel(root, path)
		relative = filepath.ToSlash(relative)
		if _, allowed := workflowStoreOpenAllowlist[relative]; allowed {
			return nil
		}
		packagePath := filepath.ToSlash(filepath.Dir(relative))
		violations = append(violations, prohibitedCalls(set, file, packagePath, relative, aliases)...)
		return nil
	})
	return violations, err
}

func workflowStoreAliases(file *ast.File) map[string]bool {
	aliases := map[string]bool{}
	for _, spec := range file.Imports {
		if strings.Trim(spec.Path.Value, `"`) != "relay/internal/store/workflow" {
			continue
		}
		name := "workflowstore"
		if spec.Name != nil {
			name = spec.Name.Name
		}
		aliases[name] = true
	}
	return aliases
}

func prohibitedCalls(set *token.FileSet, file *ast.File, packagePath, path string, aliases map[string]bool) []string {
	var violations []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		direct := false
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			name, ok := fun.X.(*ast.Ident)
			direct = ok && aliases[name.Name] && fun.Sel.Name == "Open"
		case *ast.Ident:
			direct = (packagePath == "internal/store/workflow" || aliases["."]) && fun.Name == "Open"
		}
		if !direct {
			return true
		}
		position := set.Position(call.Pos())
		violations = append(violations, fmt.Sprintf("package=%s file=%s line=%d prohibited=workflowstore.Open exception=none", packagePath, filepath.ToSlash(path), position.Line))
		return true
	})
	return violations
}
