package agentrefs

import (
	"bufio"
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

// StorageInventory holds deterministic, source-backed storage surface inputs.
type StorageInventory struct {
	SQLCConfig      *SQLCConfig
	QueryFiles      []SQLQueryFile
	MigrationFiles  []MigrationFile
	StoreWrappers   []StoreWrapper
	GeneratedFiles  []string
	UnresolvedNotes []string
}

type SQLCConfig struct {
	Version      string
	Engine       string
	QueriesDir   string
	SchemaDir    string
	GeneratedOut string
	Package      string
}

type SQLQueryFile struct {
	Path     string
	Domain   string
	Queries  []SQLQueryDecl
	ParseErr string
}

type SQLQueryDecl struct {
	Name string
	Kind string
	Line int
}

type MigrationFile struct {
	Path         string
	CreatedTable string
	ParseErr     string
}

type StoreWrapper struct {
	File    string
	Name    string
	Kind    string
	Recv    string
}

var sqlcQueryDeclRegex = regexp.MustCompile(`^\s*--\s*name:\s*(\w+)\s*:(\w+)\s*`)
var createTableRegex = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"?\w+"?\.)?"?(\w+)"?`)

func ScanStorageSurface(repoRoot string) (*StorageInventory, error) {
	inv := &StorageInventory{}

	inv.SQLCConfig = scanSQLCConfig(repoRoot)
	inv.QueryFiles = scanQueryFiles(repoRoot)
	inv.MigrationFiles = scanMigrationFiles(repoRoot)
	inv.StoreWrappers = scanStoreWrappers(repoRoot)
	inv.GeneratedFiles = scanGeneratedBoundary(repoRoot)

	return inv, nil
}

func scanSQLCConfig(repoRoot string) *SQLCConfig {
	path := filepath.Join(repoRoot, "sqlc.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return &SQLCConfig{
			QueriesDir: "internal/db/queries",
			SchemaDir:  "internal/db/migrations",
		}
	}

	cfg := &SQLCConfig{
		QueriesDir: "internal/db/queries",
		SchemaDir:  "internal/db/migrations",
	}

	sc := bufio.NewScanner(strings.NewReader(string(data)))
	var inGen, inGo bool
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case strings.HasPrefix(trimmed, "version:"):
			cfg.Version = strings.Trim(strings.TrimPrefix(trimmed, "version:"), "\"'")
		case strings.HasPrefix(trimmed, "- engine:"):
			cfg.Engine = strings.Trim(strings.TrimPrefix(trimmed, "- engine:"), "\"'")
		case strings.HasPrefix(trimmed, "queries:"):
			cfg.QueriesDir = strings.Trim(strings.TrimPrefix(trimmed, "queries:"), "\"'")
		case strings.HasPrefix(trimmed, "schema:"):
			cfg.SchemaDir = strings.Trim(strings.TrimPrefix(trimmed, "schema:"), "\"'")
		case strings.HasPrefix(trimmed, "gen:"):
			inGen = true
		case inGen && trimmed == "go:":
			inGo = true
		case inGo && strings.HasPrefix(trimmed, "package:"):
			cfg.Package = strings.Trim(strings.TrimPrefix(trimmed, "package:"), "\"'")
		case inGo && strings.HasPrefix(trimmed, "out:"):
			cfg.GeneratedOut = strings.Trim(strings.TrimPrefix(trimmed, "out:"), "\"'")
		case trimmed == "sql:" || trimmed == "- engine:" || trimmed == "sqlite":
			// no-op
		}

		if !inGen && strings.HasPrefix(trimmed, "gen:") {
			inGen = true
		}
	}

	return cfg
}

func scanQueryFiles(repoRoot string) []SQLQueryFile {
	dir := filepath.Join(repoRoot, "internal/db/queries")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []SQLQueryFile{}
	}

	var files []SQLQueryFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.ToSlash(filepath.Join("internal/db/queries", entry.Name()))
		qf := SQLQueryFile{
			Path:   path,
			Domain: queryDomainFromPath(path),
		}

		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			qf.ParseErr = err.Error()
			files = append(files, qf)
			continue
		}

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			m := sqlcQueryDeclRegex.FindStringSubmatch(line)
			if len(m) == 3 {
				qf.Queries = append(qf.Queries, SQLQueryDecl{
					Name: m[1],
					Kind: m[2],
					Line: lineNo,
				})
			}
		}

		files = append(files, qf)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

func queryDomainFromPath(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	return name
}

func scanMigrationFiles(repoRoot string) []MigrationFile {
	dir := filepath.Join(repoRoot, "internal/db/migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []MigrationFile{}
	}

	var files []MigrationFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		path := filepath.ToSlash(filepath.Join("internal/db/migrations", entry.Name()))
		mf := MigrationFile{Path: path}

		data, err := os.ReadFile(filepath.Join(repoRoot, path))
		if err != nil {
			mf.ParseErr = err.Error()
			files = append(files, mf)
			continue
		}

		content := string(data)
		m := createTableRegex.FindStringSubmatch(content)
		if len(m) == 2 {
			mf.CreatedTable = m[1]
		}

		files = append(files, mf)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files
}

func scanStoreWrappers(repoRoot string) []StoreWrapper {
	path := filepath.Join(repoRoot, "internal/store/db.go")
	data, err := os.ReadFile(path)
	if err != nil {
		return []StoreWrapper{}
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, data, parser.ParseComments)
	if err != nil {
		return []StoreWrapper{}
	}

	var wrappers []StoreWrapper
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			alias, ok := ts.Type.(*ast.Ident)
			if !ok {
				continue
			}
			if alias.Obj != nil && alias.Name != "" {
				wrappers = append(wrappers, StoreWrapper{
					File: "internal/store/db.go",
					Name: ts.Name.Name,
					Kind: "generated_model_alias",
				})
			}
		}
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv == nil || len(fn.Recv.List) == 0 {
			continue
		}
		recvType := "unknown"
		switch t := fn.Recv.List[0].Type.(type) {
		case *ast.StarExpr:
			if ident, ok := t.X.(*ast.Ident); ok {
				recvType = ident.Name
			}
		case *ast.Ident:
			recvType = t.Name
		}
		if fn.Name.IsExported() {
			wrappers = append(wrappers, StoreWrapper{
				File: "internal/store/db.go",
				Name: fn.Name.Name,
				Kind: "store_wrapper_method",
				Recv: recvType,
			})
		}
	}

	sort.Slice(wrappers, func(i, j int) bool {
		if wrappers[i].Kind != wrappers[j].Kind {
			return wrappers[i].Kind < wrappers[j].Kind
		}
		return wrappers[i].Name < wrappers[j].Name
	})
	return wrappers
}

func scanGeneratedBoundary(repoRoot string) []string {
	dir := filepath.Join(repoRoot, "internal/store/generated")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{}
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join("internal/store/generated", entry.Name())
		files = append(files, filepath.ToSlash(path))
	}
	sort.Strings(files)
	return files
}

func safeEvidence(value string) Evidence {
	if err := ValidateRepoRelativePath(value); err == nil {
		return Evidence{Kind: "source", Value: value}
	}
	return Evidence{Kind: "source", Value: strings.ReplaceAll(value, "\\", "/")}
}

func BuildStorageSurfaceDoc(repoRoot string) (*ReferenceDocument, error) {
	inv, err := ScanStorageSurface(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("scan storage surface: %w", err)
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

	addSourceInput("sqlc.yaml", "sqlc config")
	for _, qf := range inv.QueryFiles {
		addSourceInput(qf.Path, "sqlc query file")
	}
	for _, mf := range inv.MigrationFiles {
		addSourceInput(mf.Path, "migration")
	}
	addSourceInput("internal/store/db.go", "store wrapper")
	for _, f := range inv.GeneratedFiles {
		addSourceInput(f, "generated sqlc boundary")
	}

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

	var facts []Fact

	if inv.SQLCConfig != nil {
		cfg := inv.SQLCConfig
		statement := fmt.Sprintf("sqlc configuration uses sqlite engine with query directory %q, schema directory %q, generated package %q, and output %q.",
			cfg.QueriesDir, cfg.SchemaDir, cfg.Package, cfg.GeneratedOut)
		if cfg.Version != "" {
			statement = fmt.Sprintf("sqlc configuration version %s uses sqlite engine with query directory %q, schema directory %q, generated package %q, and output %q.",
				cfg.Version, cfg.QueriesDir, cfg.SchemaDir, cfg.Package, cfg.GeneratedOut)
		}
		evidence := []Evidence{}
		for _, v := range []string{"sqlc.yaml", cfg.QueriesDir, cfg.SchemaDir, cfg.GeneratedOut} {
			if v != "" {
				evidence = append(evidence, safeEvidence(v))
			}
		}
		facts = append(facts, Fact{
			ID:        uniqueID("storage", "sqlc", "config"),
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  evidence,
		})
	}

	for _, qf := range inv.QueryFiles {
		fileFactID := uniqueID("storage", "query-file", qf.Path)
		fileEvidence := []Evidence{safeEvidence(qf.Path)}
		if qf.ParseErr != "" {
			facts = append(facts, Fact{
				ID:        fileFactID,
				Label:     FactLabelUnresolved,
				Statement: fmt.Sprintf("Query file %q could not be parsed: %s", qf.Path, qf.ParseErr),
				Evidence:  fileEvidence,
			})
			continue
		}
		facts = append(facts, Fact{
			ID:        fileFactID,
			Label:     FactLabelProven,
			Statement: fmt.Sprintf("SQL query file %q defines %d sqlc query declaration(s).", qf.Path, len(qf.Queries)),
			Evidence:  fileEvidence,
		})

		for _, q := range qf.Queries {
			qID := uniqueID("storage", "query", qf.Domain, q.Name)
			val := fmt.Sprintf("%s#L%d", qf.Path, q.Line)
			evidence := []Evidence{safeEvidence(val)}
			facts = append(facts, Fact{
				ID:        qID,
				Label:     FactLabelProven,
				Statement: fmt.Sprintf("sqlc query %q (%s) declared in %q.", q.Name, q.Kind, qf.Path),
				Evidence:  evidence,
			})

			derivedID := uniqueID("storage", "query-domain", qf.Domain, q.Name)
			facts = append(facts, Fact{
				ID:        derivedID,
				Label:     FactLabelDerived,
				Statement: fmt.Sprintf("sqlc query %q belongs to SQL domain %q by file-path convention.", q.Name, qf.Domain),
				Evidence: []Evidence{
					safeEvidence(qf.Path),
				},
			})
		}
	}

	for _, mf := range inv.MigrationFiles {
		mfID := uniqueID("storage", "migration", mf.Path)
		if mf.ParseErr != "" {
			facts = append(facts, Fact{
				ID:        mfID,
				Label:     FactLabelUnresolved,
				Statement: fmt.Sprintf("Migration file %q could not be read: %s", mf.Path, mf.ParseErr),
				Evidence:  []Evidence{safeEvidence(mf.Path)},
			})
			continue
		}

		statement := fmt.Sprintf("Migration file %q is present in the migration surface.", mf.Path)
		label := FactLabelProven
		if mf.CreatedTable != "" {
			statement = fmt.Sprintf("Migration file %q contains a CREATE TABLE statement for table %q.", mf.Path, mf.CreatedTable)
		} else {
			statement = fmt.Sprintf("Migration file %q is present; no simple CREATE TABLE statement was detected.", mf.Path)
			label = FactLabelConvention
		}
		facts = append(facts, Fact{
			ID:        mfID,
			Label:     label,
			Statement: statement,
			Evidence:  []Evidence{safeEvidence(mf.Path)},
		})
	}

	for _, w := range inv.StoreWrappers {
		wID := uniqueID("storage", "store-wrapper", w.Kind, w.Name)
		statement := fmt.Sprintf("Store wrapper %s %q in %q.", w.Kind, w.Name, w.File)
		if w.Recv != "" {
			statement = fmt.Sprintf("Store wrapper %s %q on receiver %q in %q.", w.Kind, w.Name, w.Recv, w.File)
		}
		facts = append(facts, Fact{
			ID:        wID,
			Label:     FactLabelProven,
			Statement: statement,
			Evidence:  []Evidence{safeEvidence(w.File + "#" + w.Name)},
		})
	}

	if len(inv.GeneratedFiles) > 0 {
		genID := uniqueID("storage", "generated", "boundary")
		facts = append(facts, Fact{
			ID:    genID,
			Label: FactLabelConvention,
			Statement: fmt.Sprintf("Generated sqlc boundary contains %d file(s) under internal/store/generated; these are build outputs and must not be hand-edited.",
				len(inv.GeneratedFiles)),
			Evidence: []Evidence{safeEvidence("internal/store/generated")},
		})
	} else {
		facts = append(facts, Fact{
			ID:        uniqueID("storage", "generated", "boundary", "missing"),
			Label:     FactLabelUnresolved,
			Statement: "Generated sqlc boundary directory internal/store/generated was not found or is empty.",
			Evidence:  []Evidence{safeEvidence("internal/store/generated")},
		})
	}

	expectedQueries := []string{"internal/db/queries", "internal/db/migrations", "internal/store/db.go", "sqlc.yaml"}
	for _, p := range expectedQueries {
		abs := filepath.Join(repoRoot, p)
		_, err := os.Stat(abs)
		if err != nil {
			facts = append(facts, Fact{
				ID:        uniqueID("storage", "gap", "missing", p),
				Label:     FactLabelUnresolved,
				Statement: fmt.Sprintf("Expected storage surface input %q is missing.", p),
				Evidence:  []Evidence{safeEvidence(p)},
			})
		}
	}

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
		ReferenceID:   "storage-surface",
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
