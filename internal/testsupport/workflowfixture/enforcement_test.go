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
	source := `package sample
import (
  "path/filepath"
  workflowstore "relay/internal/store/workflow"
)
func fixture(t interface{ TempDir() string }) { _, _ = workflowstore.Open(filepath.Join(t.TempDir(), "workflow.db"), t.TempDir()) }
`
	file, err := parser.ParseFile(token.NewFileSet(), "fixture_test.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := prohibitedCalls(token.NewFileSet(), file, "sample", "fixture_test.go", map[string]bool{"workflowstore": true}); len(got) != 1 {
		t.Fatalf("prohibited calls = %v, want one", got)
	}
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
		relative, _ := filepath.Rel(root, path)
		if filepath.ToSlash(relative) == "internal/mcp/wayfinder_cold_start_test.go" {
			return nil // Explicit migration-upgrade fixture.
		}
		packagePath := filepath.ToSlash(filepath.Dir(relative))
		violations = append(violations, prohibitedCalls(set, file, packagePath, relative, aliases)...)
		return nil
	})
	return violations, err
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
			direct = packagePath == "internal/store/workflow" && fun.Name == "Open"
		}
		if !direct || !createsPathInline(call.Args[0]) {
			return true
		}
		position := set.Position(call.Pos())
		violations = append(violations, fmt.Sprintf("package=%s file=%s line=%d prohibited=workflowstore.Open exception=none", packagePath, filepath.ToSlash(path), position.Line))
		return true
	})
	return violations
}

func createsPathInline(expression ast.Expr) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	return ok && (selector.Sel.Name == "Join" || selector.Sel.Name == "TempDir")
}
