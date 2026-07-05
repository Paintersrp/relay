package speccompiler

import (
	_ "embed"
	"fmt"
	"sort"
	"strings"
)

const (
	SchemaVersion       = "1.0"
	RelaySpecsRepo      = "Paintersrp/relay-specs"
	RelaySpecsCommit    = "1d6afbea47a246b3b473176c760aed53457774d6"
	planSuffix          = ".plan.json"
	executionSpecSuffix = ".execution-spec.json"
)

type ArtifactKind string

const (
	ArtifactPlan          ArtifactKind = "plan"
	ArtifactExecutionSpec ArtifactKind = "execution_spec"
)

type Diagnostic struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type Result struct {
	OutputFilename *string      `json:"output_filename"`
	Markdown       *string      `json:"markdown"`
	Errors         []Diagnostic `json:"errors"`
	Notices        []Diagnostic `json:"notices"`
}

type Provenance struct {
	Repository          string
	Commit              string
	PlanSchemaPath      string
	ExecutionSchemaPath string
	CompilerContract    string
}

func SourceProvenance() Provenance {
	return Provenance{
		Repository:          RelaySpecsRepo,
		Commit:              RelaySpecsCommit,
		PlanSchemaPath:      "schemas/plan.schema.json",
		ExecutionSchemaPath: "schemas/execution-spec.schema.json",
		CompilerContract:    "contracts/compiler.md",
	}
}

func Compile(filenameBasename string, rawJSON []byte) Result {
	kind, slug, filenameErrors := classifyFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil)
	}

	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil)
	}

	notices := schemaVersionNotices(root)
	schemaValid, schemaErr := validateEmbeddedSchema(kind, rawJSON)

	var errors []Diagnostic
	switch kind {
	case ArtifactExecutionSpec:
		errors = validateExecutionSpec(root, slug)
	case ArtifactPlan:
		errors = validatePlan(root, slug)
	default:
		errors = append(errors, Diagnostic{Code: "unsupported_artifact_filename", Path: "", Message: "Unsupported artifact kind."})
	}

	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded schema validation failed: %v", schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: "Artifact does not satisfy the embedded v1.0 JSON Schema."})
	}

	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices)
	}

	var markdown string
	var output string
	var err error
	switch kind {
	case ArtifactExecutionSpec:
		markdown, err = renderExecutionSpec(rawJSON)
		output = slug + ".executor-brief.md"
	case ArtifactPlan:
		markdown, err = renderPlan(rawJSON)
		output = slug + ".plan.md"
	}
	if err != nil {
		return failed([]Diagnostic{
			{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated artifact for rendering: %v", err)}}, notices)
	}

	return Result{
		OutputFilename: &output,
		Markdown:       &markdown,
		Errors:         []Diagnostic{},
		Notices:        notices,
	}
}

func classifyFilename(filename string) (ArtifactKind, string, []Diagnostic) {
	if strings.ContainsAny(filename, `/\\`) {
		return "", "", []Diagnostic{
			{Code: "invalid_filename_basename", Path: "", Message: "Filename must be a basename without path separators."}}
	}
	var kind ArtifactKind
	var suffix string
	switch {
	case strings.HasSuffix(filename, executionSpecSuffix):
		kind, suffix = ArtifactExecutionSpec, executionSpecSuffix
	case strings.HasSuffix(filename, planSuffix):
		kind, suffix = ArtifactPlan, planSuffix
	default:
		return "", "", []Diagnostic{
			{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must end with .execution-spec.json or .plan.json."}}
	}
	slug := strings.TrimSuffix(filename, suffix)
	if !validFeatureSlug(slug) {
		return "", "", []Diagnostic{
			{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
	}
	return kind, slug, nil
}

func schemaVersionNotices(root *jsonNode) []Diagnostic {
	if root == nil || root.kind != nodeObject {
		return nil
	}
	member, ok := root.objectMember("schema_version")
	if ok && member.value.kind == nodeString && member.value.text == SchemaVersion {
		return nil
	}
	supplied := "absent"
	path := "/schema_version"
	if ok {
		supplied = member.value.describe()
	}
	return []Diagnostic{
		{
			Code:    "schema_version_fallback",
			Path:    path,
			Message: fmt.Sprintf("schema_version is %s; using latest supported version %s.", supplied, SchemaVersion),
		}}
}

func failed(errors, notices []Diagnostic) Result {
	if errors == nil {
		errors = []Diagnostic{}
	}
	if notices == nil {
		notices = []Diagnostic{}
	}
	return Result{Errors: errors, Notices: notices}
}

func normalizeDiagnostics(in []Diagnostic) []Diagnostic {
	seen := make(map[string]struct{}, len(in))
	out := make([]Diagnostic, 0, len(in))
	for _, d := range in {
		key := d.Path + "\x00" + d.Code + "\x00" + d.Message
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return comparePointers(out[i].Path, out[j].Path) < 0
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return out
}

func comparePointers(a, b string) int {
	as, bs := strings.Split(strings.TrimPrefix(a, "/"), "/"), strings.Split(strings.TrimPrefix(b, "/"), "/")
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] == bs[i] {
			continue
		}
		ai, aok := parseIndex(as[i])
		bi, bok := parseIndex(bs[i])
		if aok && bok {
			if ai < bi {
				return -1
			}
			return 1
		}
		if as[i] < bs[i] {
			return -1
		}
		return 1
	}
	if len(as) < len(bs) {
		return -1
	}
	if len(as) > len(bs) {
		return 1
	}
	return 0
}

func parseIndex(value string) (int, bool) {
	if value == "" {
		return 0, false
	}
	var n int
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
