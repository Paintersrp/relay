package agentrefs

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type BackendInventory struct {
	Packages    []BackendPackage
	ImportEdges []BackendImportEdge
}

type BackendPackage struct {
	Dir                 string
	PackageName         string
	SourceFiles         []string
	TestFiles           []string
	ExportedSymbols     []BackendSymbol
	ServiceLikeSymbols  []BackendSymbol
	HandlerLikeSymbols  []BackendSymbol
	RouteMounterSymbols []BackendSymbol
	TestFunctions       []BackendSymbol
}

type BackendSymbol struct {
	Name string
	Kind string
	File string
}

type BackendImportEdge struct {
	FromDir      string
	ToImportPath string
	File         string
}

var scannerRoots = []string{
	"internal/app",
	"internal/api",
	"internal/store",
	"internal/mcp",
}

func isExcluded(path string) bool {
	clean := filepath.ToSlash(path)

	if strings.HasPrefix(clean, "internal/store/generated/") {
		return true
	}
	if strings.HasSuffix(clean, "_templ.go") {
		return true
	}
	if strings.HasPrefix(clean, "docs/generated/") {
		return true
	}

	parts := strings.Split(clean, "/")
	for _, p := range parts {
		if p == ".git" || p == "node_modules" || p == "tmp" || p == "bin" || p == "dist" {
			return true
		}
	}

	return false
}

func slugify(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]+`)
	slug := re.ReplaceAllString(s, "-")
	slug = strings.ToLower(slug)
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "unknown"
	}
	return slug
}

func slugifyFile(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return slugify(dir + "-" + name)
}

var importPathRegex = regexp.MustCompile(`^relay/internal/`)

func isInternalImport(p string) bool {
	return importPathRegex.MatchString(p)
}

func ScanBackendSurface(repoRoot string) (*BackendInventory, error) {
	inv := &BackendInventory{}
	pkgMap := make(map[string]*BackendPackage)
	var importEdges []BackendImportEdge

	for _, root := range scannerRoots {
		absRoot := filepath.Join(repoRoot, root)
		if _, err := os.Stat(absRoot); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(absRoot, func(fpath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			rel, err := filepath.Rel(repoRoot, fpath)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)

			if info.IsDir() {
				if isExcluded(rel + "/") {
					return filepath.SkipDir
				}
				return nil
			}

			if filepath.Ext(info.Name()) != ".go" {
				return nil
			}

			if isExcluded(rel) {
				return nil
			}

			dir := filepath.ToSlash(filepath.Dir(rel))

			if strings.HasSuffix(info.Name(), "_test.go") {
				pkg := getOrCreatePackage(pkgMap, dir)
				pkg.TestFiles = append(pkg.TestFiles, rel)
				fset := token.NewFileSet()
				f, err := parser.ParseFile(fset, fpath, nil, 0)
				if err != nil {
					return nil
				}
				for _, decl := range f.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok {
						continue
					}
					if strings.HasPrefix(fn.Name.Name, "Test") {
						pkg.TestFunctions = append(pkg.TestFunctions, BackendSymbol{
							Name: fn.Name.Name,
							Kind: "test_function",
							File: rel,
						})
					}
				}
				return nil
			}

			pkg := getOrCreatePackage(pkgMap, dir)
			pkg.SourceFiles = append(pkg.SourceFiles, rel)

			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, fpath, nil, parser.ParseComments)
			if err != nil {
				return nil
			}

			if pkg.PackageName == "" {
				pkg.PackageName = f.Name.Name
			}

			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Name.IsExported() && !strings.HasPrefix(d.Name.Name, "Test") {
						kind := "func"
						sym := BackendSymbol{Name: d.Name.Name, Kind: kind, File: rel}
						pkg.ExportedSymbols = append(pkg.ExportedSymbols, sym)

						if d.Name.Name == "MountRoutes" {
							pkg.RouteMounterSymbols = append(pkg.RouteMounterSymbols, sym)
						} else if d.Name.Name == "NewService" || d.Name.Name == "NewHandler" {
							pkg.ServiceLikeSymbols = append(pkg.ServiceLikeSymbols, BackendSymbol{
								Name: d.Name.Name, Kind: "constructor", File: rel,
							})
							if d.Name.Name == "NewHandler" {
								pkg.HandlerLikeSymbols = append(pkg.HandlerLikeSymbols, BackendSymbol{
									Name: d.Name.Name, Kind: "constructor", File: rel,
								})
							}
						} else if strings.HasPrefix(d.Name.Name, "New") && strings.HasSuffix(d.Name.Name, "Service") {
							pkg.ServiceLikeSymbols = append(pkg.ServiceLikeSymbols, BackendSymbol{
								Name: d.Name.Name, Kind: "constructor", File: rel,
							})
						} else if strings.HasPrefix(d.Name.Name, "New") && strings.HasSuffix(d.Name.Name, "Handler") {
							pkg.HandlerLikeSymbols = append(pkg.HandlerLikeSymbols, BackendSymbol{
								Name: d.Name.Name, Kind: "constructor", File: rel,
							})
						}
					}

				case *ast.GenDecl:
					if d.Tok != token.TYPE && d.Tok != token.CONST && d.Tok != token.VAR {
						continue
					}
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								sym := BackendSymbol{Name: s.Name.Name, Kind: "type", File: rel}
								pkg.ExportedSymbols = append(pkg.ExportedSymbols, sym)

								if s.Name.Name == "Service" {
									pkg.ServiceLikeSymbols = append(pkg.ServiceLikeSymbols, BackendSymbol{
										Name: s.Name.Name, Kind: "type", File: rel,
									})
								} else if strings.HasSuffix(s.Name.Name, "Service") {
									pkg.ServiceLikeSymbols = append(pkg.ServiceLikeSymbols, BackendSymbol{
										Name: s.Name.Name, Kind: "type", File: rel,
									})
								} else if s.Name.Name == "Handler" {
									pkg.HandlerLikeSymbols = append(pkg.HandlerLikeSymbols, BackendSymbol{
										Name: s.Name.Name, Kind: "type", File: rel,
									})
								} else if strings.HasSuffix(s.Name.Name, "Handler") {
									pkg.HandlerLikeSymbols = append(pkg.HandlerLikeSymbols, BackendSymbol{
										Name: s.Name.Name, Kind: "type", File: rel,
									})
								}
							}
						case *ast.ValueSpec:
							for _, name := range s.Names {
								if name.IsExported() {
									pkg.ExportedSymbols = append(pkg.ExportedSymbols, BackendSymbol{
										Name: name.Name,
										Kind: map[token.Token]string{token.CONST: "const", token.VAR: "var"}[d.Tok],
										File: rel,
									})
								}
							}
						}
					}
				}
			}

			for _, imp := range f.Imports {
				if imp.Path == nil {
					continue
				}
				impPath := strings.Trim(imp.Path.Value, "\"")
				if !isInternalImport(impPath) {
					continue
				}
				importEdges = append(importEdges, BackendImportEdge{
					FromDir:      dir,
					ToImportPath: impPath,
					File:         rel,
				})
			}

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", root, err)
		}
	}

	for _, pkg := range pkgMap {
		inv.Packages = append(inv.Packages, *pkg)
	}

	sort.Slice(inv.Packages, func(i, j int) bool {
		return inv.Packages[i].Dir < inv.Packages[j].Dir
	})

	inv.ImportEdges = importEdges
	sort.Slice(inv.ImportEdges, func(i, j int) bool {
		if inv.ImportEdges[i].FromDir != inv.ImportEdges[j].FromDir {
			return inv.ImportEdges[i].FromDir < inv.ImportEdges[j].FromDir
		}
		return inv.ImportEdges[i].ToImportPath < inv.ImportEdges[j].ToImportPath
	})

	return inv, nil
}

func getOrCreatePackage(pkgMap map[string]*BackendPackage, dir string) *BackendPackage {
	if p, ok := pkgMap[dir]; ok {
		return p
	}
	p := &BackendPackage{Dir: dir}
	pkgMap[dir] = p
	return p
}

type manualMapEntry struct {
	path  string
	owner string
}

func parseManualMapForGoPaths(repoRoot string) ([]manualMapEntry, error) {
	mapPath := filepath.Join(repoRoot, "docs/backend-code-surface-map.md")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	content := string(data)
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(content, -1)

	var entries []manualMapEntry
	seen := make(map[string]bool)

	for _, m := range matches {
		val := m[1]
		if !strings.HasSuffix(val, ".go") {
			continue
		}
		if !strings.HasPrefix(val, "internal/") {
			continue
		}
		if seen[val] {
			continue
		}
		seen[val] = true
		isBackendRoot := false
		for _, root := range scannerRoots {
			if strings.HasPrefix(val, root) {
				isBackendRoot = true
				break
			}
		}
		if !isBackendRoot {
			continue
		}
		entries = append(entries, manualMapEntry{path: val})
	}

	return entries, nil
}

func compareManualMap(repoRoot string, inv *BackendInventory) []Fact {
	var facts []Fact

	mapPath := filepath.Join(repoRoot, "docs/backend-code-surface-map.md")
	if _, err := os.Stat(mapPath); os.IsNotExist(err) {
		facts = append(facts, Fact{
			ID:        "backend-map-gap-unavailable",
			Label:     FactLabelUnresolved,
			Statement: "Manual orientation map docs/backend-code-surface-map.md is missing and was unavailable for comparison.",
			Evidence: []Evidence{
				{Kind: "map", Value: "docs/backend-code-surface-map.md"},
			},
		})
		return facts
	}

	entries, err := parseManualMapForGoPaths(repoRoot)
	if err != nil {
		facts = append(facts, Fact{
			ID:        "backend-map-gap-parse-error",
			Label:     FactLabelUnresolved,
			Statement: fmt.Sprintf("Could not parse manual map: %v", err),
			Evidence: []Evidence{
				{Kind: "map", Value: "docs/backend-code-surface-map.md"},
			},
		})
		return facts
	}

	existingPaths := make(map[string]bool)
	for _, pkg := range inv.Packages {
		for _, f := range pkg.SourceFiles {
			existingPaths[f] = true
		}
		for _, f := range pkg.TestFiles {
			existingPaths[f] = true
		}
	}

	ordinal := 0
	for _, entry := range entries {
		fullPath := filepath.Join(repoRoot, entry.path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			facts = append(facts, Fact{
				ID:        fmt.Sprintf("backend-map-gap-%d", ordinal),
				Label:     FactLabelUnresolved,
				Statement: fmt.Sprintf("Manual map cites %s but it does not exist in the local checkout.", entry.path),
				Evidence: []Evidence{
					{Kind: "map", Value: "docs/backend-code-surface-map.md"},
					{Kind: "source", Value: entry.path},
				},
			})
			ordinal++
			continue
		}

		if !existingPaths[entry.path] {
			exists, _ := filepath.Glob(fullPath)
			if len(exists) == 0 {
				facts = append(facts, Fact{
					ID:        fmt.Sprintf("backend-map-gap-%d", ordinal),
					Label:     FactLabelUnresolved,
					Statement: fmt.Sprintf("Manual map cites %s which exists on disk but was not scanned by the backend scanner.", entry.path),
					Evidence: []Evidence{
						{Kind: "map", Value: "docs/backend-code-surface-map.md"},
						{Kind: "source", Value: entry.path},
					},
				})
				ordinal++
			}
		}
	}

	return facts
}

func BuildBackendSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	inv, err := ScanBackendSurface(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan backend surface: %w", err)
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

	for _, pkg := range inv.Packages {
		for _, f := range pkg.SourceFiles {
			addSourceInput(f, "source")
		}
		for _, f := range pkg.TestFiles {
			addSourceInput(f, "test")
		}
	}

	if _, err := os.Stat(filepath.Join(repoRoot, "docs/backend-code-surface-map.md")); err == nil {
		addSourceInput("docs/backend-code-surface-map.md", "manual map")
	}

	var facts []Fact
	slugCounters := make(map[string]int)

	makeID := func(prefix string, parts ...string) string {
		raw := prefix
		for _, p := range parts {
			raw = raw + "-" + slugify(p)
		}
		return raw
	}

	uniqueID := func(prefix string, parts ...string) string {
		base := makeID(prefix, parts...)
		if _, ok := slugCounters[base]; ok {
			slugCounters[base]++
			return fmt.Sprintf("%s-%d", base, slugCounters[base])
		}
		slugCounters[base] = 1
		return base
	}

	for _, pkg := range inv.Packages {
		factID := uniqueID("backend-package", pkg.Dir)
		var evidence []Evidence
		for _, f := range pkg.SourceFiles {
			if err := ValidateRepoRelativePath(f); err == nil {
				evidence = append(evidence, Evidence{Kind: "source", Value: f})
			}
		}
		for _, f := range pkg.TestFiles {
			if err := ValidateRepoRelativePath(f); err == nil {
				evidence = append(evidence, Evidence{Kind: "test", Value: f})
			}
		}
		facts = append(facts, Fact{
			ID:        factID,
			Label:     FactLabelProven,
			Statement: fmt.Sprintf("Backend package %s (%s)", pkg.Dir, pkg.PackageName),
			Evidence:  evidence,
		})

		for _, sym := range pkg.ExportedSymbols {
			symID := uniqueID("backend-symbol", pkg.Dir, sym.Kind, sym.Name)
			val := sym.File
			if sym.Kind == "method" {
				val = sym.File + "#" + sym.Name
			}
			symEvidence := []Evidence{}
			if err := ValidateRepoRelativePath(val); err == nil {
				symEvidence = append(symEvidence, Evidence{Kind: "source", Value: val})
			} else {
				symEvidence = append(symEvidence, Evidence{Kind: "source", Value: sym.File})
			}
			facts = append(facts, Fact{
				ID:        symID,
				Label:     FactLabelProven,
				Statement: fmt.Sprintf("Exported %s %s in %s", sym.Kind, sym.Name, sym.File),
				Evidence:  symEvidence,
			})
		}

		for _, sym := range pkg.ServiceLikeSymbols {
			symID := uniqueID("backend-service-like", pkg.Dir, sym.Name)
			val := sym.File
			if err := ValidateRepoRelativePath(val); err == nil {
				facts = append(facts, Fact{
					ID:    symID,
					Label: FactLabelConvention,
					Statement: fmt.Sprintf("Service-like symbol %s (%s) in %s — identified by naming convention (named Service, ending in Service, or NewService/New*Service constructor).",
						sym.Name, sym.Kind, sym.File),
					Evidence: []Evidence{
						{Kind: "source", Value: val},
					},
				})
			}
		}

		for _, sym := range pkg.HandlerLikeSymbols {
			symID := uniqueID("backend-handler-like", pkg.Dir, sym.Name)
			val := sym.File
			if err := ValidateRepoRelativePath(val); err == nil {
				facts = append(facts, Fact{
					ID:    symID,
					Label: FactLabelConvention,
					Statement: fmt.Sprintf("Handler-like symbol %s (%s) in %s — identified by naming convention (named Handler, ending in Handler, or NewHandler/New*Handler constructor).",
						sym.Name, sym.Kind, sym.File),
					Evidence: []Evidence{
						{Kind: "source", Value: val},
					},
				})
			}
		}

		for _, sym := range pkg.RouteMounterSymbols {
			symID := uniqueID("backend-route-mounter-like", pkg.Dir, sym.Name)
			val := sym.File + "#" + sym.Name
			if err := ValidateRepoRelativePath(val); err == nil {
				facts = append(facts, Fact{
					ID:    symID,
					Label: FactLabelConvention,
					Statement: fmt.Sprintf("Route-mounter function %s in %s — this is router-surface evidence only, not a complete HTTP route/API reference.",
						sym.Name, sym.File),
					Evidence: []Evidence{
						{Kind: "source", Value: val},
					},
				})
			}
		}

		for _, fn := range pkg.TestFunctions {
			testID := uniqueID("backend-test", pkg.Dir, fn.Name)
			val := fn.File
			symEvidence := []Evidence{}
			if err := ValidateRepoRelativePath(val); err == nil {
				symEvidence = append(symEvidence, Evidence{Kind: "test", Value: val})
			}
			facts = append(facts, Fact{
				ID:        testID,
				Label:     FactLabelProven,
				Statement: fmt.Sprintf("Test function %s in %s", fn.Name, fn.File),
				Evidence:  symEvidence,
			})
		}
	}

	for _, edge := range inv.ImportEdges {
		edgeID := uniqueID("backend-import-edge", edge.FromDir, edge.ToImportPath)
		val := edge.File
		if err := ValidateRepoRelativePath(val); err == nil {
			facts = append(facts, Fact{
				ID:    edgeID,
				Label: FactLabelDerived,
				Statement: fmt.Sprintf("Import edge: %s imports %s",
					edge.FromDir, edge.ToImportPath),
				Evidence: []Evidence{
					{Kind: "source", Value: val},
				},
			})
		}
	}

	mapFacts := compareManualMap(repoRoot, inv)
	facts = append(facts, mapFacts...)

	sort.Slice(facts, func(i, j int) bool {
		return facts[i].ID < facts[j].ID
	})

	labels := []FactLabel{
		FactLabelProven,
		FactLabelDerived,
		FactLabelConvention,
		FactLabelUnresolved,
		FactLabelConflict,
	}

	doc := &ReferenceDocument{
		SchemaVersion: "1.0.0",
		ReferenceID:   "backend-surface",
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
