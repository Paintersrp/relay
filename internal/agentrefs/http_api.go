package agentrefs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type HTTPAPIRouteEntry struct {
	Method    string
	Path      string
	Handler   string
	SourceFile string
	Group     string
}

type HTTPAPIInventory struct {
	Routes []HTTPAPIRouteEntry
}

var httpAPIRouteSourceFiles = []string{
	"internal/api/plans/routes.go",
	"internal/api/runs/routes.go",
	"internal/api/artifacts/routes.go",
	"internal/api/intake/routes.go",
	"internal/api/projects/routes.go",
	"internal/api/audits/routes.go",
	"internal/server/routes.go",
}

func ScanHTTPAPISurface(repoRoot string) (*HTTPAPIInventory, error) {
	inv := &HTTPAPIInventory{}

	for _, relPath := range httpAPIRouteSourceFiles {
		absPath := filepath.Join(repoRoot, relPath)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			continue
		}

		routes, err := scanRouteFile(absPath, relPath)
		if err != nil {
			continue
		}
		inv.Routes = append(inv.Routes, routes...)
	}

	if serverFile := filepath.Join(repoRoot, "internal/server/routes.go"); fileExists(serverFile) {
		serverRoutes, err := scanServerRoutes(serverFile, inv.Routes)
		if err == nil {
			inv.Routes = serverRoutes
		}
	}

	sort.Slice(inv.Routes, func(i, j int) bool {
		if inv.Routes[i].Path != inv.Routes[j].Path {
			return inv.Routes[i].Path < inv.Routes[j].Path
		}
		return inv.Routes[i].Method < inv.Routes[j].Method
	})

	return inv, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func scanRouteFile(absPath, relPath string) ([]HTTPAPIRouteEntry, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	var routes []HTTPAPIRouteEntry

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name
		if !isHTTPMethod(method) {
			return true
		}

		if len(call.Args) < 2 {
			return true
		}

		path := extractStringLiteral(call.Args[0])
		if path == "" {
			return true
		}

		handler := extractHandlerName(call.Args[1])

		routes = append(routes, HTTPAPIRouteEntry{
			Method:     chiMethodToHTTP(method),
			Path:       path,
			Handler:    handler,
			SourceFile: relPath,
		})

		return true
	})

	return routes, nil
}

func scanServerRoutes(absPath string, featureRoutes []HTTPAPIRouteEntry) ([]HTTPAPIRouteEntry, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, absPath, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	relPath := "internal/server/routes.go"

	var apiRoutes []HTTPAPIRouteEntry
	for _, r := range featureRoutes {
		if r.Group == "" {
			r.Group = "/api"
		}
		apiRoutes = append(apiRoutes, r)
	}

	var serverRoutes []HTTPAPIRouteEntry

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		method := sel.Sel.Name

		switch method {
		case "Handle":
			if len(call.Args) >= 2 {
				path := extractStringLiteral(call.Args[0])
				handler := extractHandlerName(call.Args[1])
				if path != "" {
					serverRoutes = append(serverRoutes, HTTPAPIRouteEntry{
						Method:     "ANY",
						Path:       path,
						Handler:    handler,
						SourceFile: relPath,
						Group:      "",
					})
				}
			}
			return true
		case "HandleFunc":
			if len(call.Args) >= 2 {
				path := extractStringLiteral(call.Args[0])
				if path != "" {
					serverRoutes = append(serverRoutes, HTTPAPIRouteEntry{
						Method:     "ANY",
						Path:       path,
						Handler:    "inline_handler",
						SourceFile: relPath,
						Group:      "",
					})
				}
			}
			return true
		}

		if isHTTPMethod(method) && len(call.Args) >= 2 {
			path := extractStringLiteral(call.Args[0])
			handler := extractHandlerName(call.Args[1])
			if path != "" {
				serverRoutes = append(serverRoutes, HTTPAPIRouteEntry{
					Method:     chiMethodToHTTP(method),
					Path:       path,
					Handler:    handler,
					SourceFile: relPath,
					Group:      "",
				})
			}
		}

		return true
	})

	for _, r := range apiRoutes {
		dup := false
		for _, sr := range serverRoutes {
			if sr.Path == r.Path && sr.Method == r.Method {
				dup = true
				break
			}
		}
		if !dup {
			serverRoutes = append(serverRoutes, r)
		}
	}

	serverRoutes = append(serverRoutes, HTTPAPIRouteEntry{
		Method:     "ANY",
		Path:       "/static/*",
		Handler:    "http.FileServer",
		SourceFile: relPath,
		Group:      "",
	})

	sort.Slice(serverRoutes, func(i, j int) bool {
		if serverRoutes[i].Path != serverRoutes[j].Path {
			return serverRoutes[i].Path < serverRoutes[j].Path
		}
		return serverRoutes[i].Method < serverRoutes[j].Method
	})

	return serverRoutes, nil
}

func isHTTPMethod(name string) bool {
	switch name {
	case "Get", "Post", "Put", "Delete", "Patch", "Head", "Options", "Connect", "Trace":
		return true
	}
	return false
}

func chiMethodToHTTP(chiMethod string) string {
	return strings.ToUpper(chiMethod)
}

func extractStringLiteral(expr ast.Expr) string {
	if bl, ok := expr.(*ast.BasicLit); ok && bl.Kind == token.STRING {
		return strings.Trim(bl.Value, `"`)
	}
	return ""
}

func extractHandlerName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.SelectorExpr:
		return extractHandlerName(e.X) + "." + e.Sel.Name
	case *ast.Ident:
		return e.Name
	case *ast.FuncLit:
		return "inline_handler"
	case *ast.CallExpr:
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			return sel.Sel.Name
		}
		if ident, ok := e.Fun.(*ast.Ident); ok {
			return ident.Name
		}
		return "constructed_handler"
	default:
		return "unknown_handler"
	}
}

func BuildHTTPAPISurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	inv, err := ScanHTTPAPISurface(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan HTTP API surface: %w", err)
	}

	var sourceInputs []SourceInput
	seenPaths := make(map[string]bool)

	addSourceInput := func(path string, role string) {
		if seenPaths[path] {
			return
		}
		seenPaths[path] = true
		hash, err := ComputeSHA256(filepath.Join(repoRoot, path))
		if err != nil {
			hash = "unavailable"
		}
		sourceInputs = append(sourceInputs, SourceInput{
			Path:   path,
			SHA256: hash,
			Role:   role,
		})
	}

	for _, rf := range httpAPIRouteSourceFiles {
		if fileExists(filepath.Join(repoRoot, rf)) {
			addSourceInput(rf, "route source")
		}
	}

	var facts []Fact
	factOrdinal := 0
	slugCounters := make(map[string]int)

	makeSlug := func(s string) string {
		slug := strings.ToLower(s)
		slug = strings.ReplaceAll(slug, "/", "-")
		slug = strings.ReplaceAll(slug, "{", "")
		slug = strings.ReplaceAll(slug, "}", "")
		slug = strings.Trim(slug, "-")
		if slug == "" {
			return "root"
		}
		return slug
	}

	for _, route := range inv.Routes {
		factOrdinal++
		slug := makeSlug(route.Path + "-" + route.Method)
		if c, ok := slugCounters[slug]; ok {
			slugCounters[slug] = c + 1
			slug = fmt.Sprintf("%s-%d", slug, c+1)
		} else {
			slugCounters[slug] = 0
		}
		factID := fmt.Sprintf("http-api-route-%s", slug)

		fullPath := route.Path
		if route.Group != "" {
			fullPath = route.Group + route.Path
		}

		statement := fmt.Sprintf("Route %s %s handled by %s", route.Method, fullPath, route.Handler)

		var evidence []Evidence
		if err := ValidateRepoRelativePath(route.SourceFile); err == nil {
			evidence = append(evidence, Evidence{Kind: "source", Value: route.SourceFile})
		}

		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	if factOrdinal == 0 {
		facts = append(facts, Fact{
			ID:        "http-api-noroutes-unresolved",
			Label:     FactLabelUnresolved,
			Statement: "No HTTP API routes were detected from source files. Either route source files are missing or the scanner could not parse them.",
		})
	}

	labels := []FactLabel{
		FactLabelProven,
		FactLabelDerived,
		FactLabelConvention,
		FactLabelUnresolved,
		FactLabelConflict,
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "http-api-surface",
		Repo: RepoIdentity{
			ProjectID: "relay",
			RepoID:    "Paintersrp/relay",
			Branch:    "main",
		},
		GeneratedBy: GeneratorIdentity{
			Name:    "relay-agentrefs",
			Version: "0.1.0",
		},
		Rendering: RenderingContract{
			JSONPrimary:       true,
			MarkdownFromJSON:  true,
			DeterministicSort: true,
			NoTimestamps:      true,
			RelativePathsOnly: true,
		},
		SourceInputs: sourceInputs,
		FactLabels:   labels,
		Facts:        facts,
		References:   []ReferenceEntry{},
	}

	return doc, nil
}
