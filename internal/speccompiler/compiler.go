package speccompiler

import (
	_ "embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	SchemaVersion = "1.0"

	RelaySpecsRepo   = "Paintersrp/relay-specs"
	RelaySpecsCommit = "cc4cd6d8fc5a3cd4a3b14b0366033e187afa2d77"

	RelayRepo                  = "Paintersrp/relay"
	RelayExecutionSchemaCommit = "040df75c2ff49306262f3069ac5bba39ee4ec36c"
	planSuffix                 = ".plan.json"
	executionSpecSuffix        = ".execution-spec.json"
)

type ArtifactKind string

const (
	ArtifactPlan          ArtifactKind = "plan"
	ArtifactExecutionSpec ArtifactKind = "execution_spec"
)

type FilenameInfo struct {
	Kind             ArtifactKind
	FeatureSlug      string
	PassNumber       int64
	HasPassQualifier bool
	OutputStem       string
}

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
	Repository                string
	Commit                    string
	PlanSchemaPath            string
	ExecutionSchemaRepository string
	ExecutionSchemaCommit     string
	ExecutionSchemaPath       string
	CompilerContract          string
}

func SourceProvenance() Provenance {
	return Provenance{
		Repository:                RelaySpecsRepo,
		Commit:                    RelaySpecsCommit,
		PlanSchemaPath:            "schemas/plan.schema.json",
		ExecutionSchemaRepository: RelayRepo,
		ExecutionSchemaCommit:     RelayExecutionSchemaCommit,
		ExecutionSchemaPath:       "internal/speccompiler/schemas/execution-spec.schema.json",
		CompilerContract:          "contracts/compiler.md",
	}
}

func Compile(filenameBasename string, rawJSON []byte) Result {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil)
	}
	kind := filename.Kind
	slug := filename.FeatureSlug

	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil)
	}

	notices := schemaVersionNotices(root)
	schemaValid, schemaErr := validateEmbeddedSchema(kind, rawJSON)

	var errors []Diagnostic
	switch kind {
	case ArtifactExecutionSpec:
		errors = validateExecutionSpec(root, slug, rawJSON)
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
		output = filename.OutputStem + ".executor-brief.md"
	case ArtifactPlan:
		markdown, err = renderPlan(rawJSON)
		output = filename.OutputStem + ".plan.md"
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

func ParseFilename(filename string) (FilenameInfo, []Diagnostic) {
	if strings.ContainsAny(filename, `/\\`) {
		return FilenameInfo{}, []Diagnostic{
			{Code: "invalid_filename_basename", Path: "", Message: "Filename must be a basename without path separators."}}
	}

	switch {
	case strings.HasSuffix(filename, executionSpecSuffix):
		stem := strings.TrimSuffix(filename, executionSpecSuffix)
		info := FilenameInfo{
			Kind:        ArtifactExecutionSpec,
			FeatureSlug: stem,
			OutputStem:  stem,
		}
		if qualifierIndex := strings.LastIndex(stem, ".pass-"); qualifierIndex >= 0 {
			featureSlug := stem[:qualifierIndex]
			qualifier := stem[qualifierIndex+len(".pass-"):]
			passNumber, ok := parsePassQualifier(qualifier)
			if !ok || featureSlug == "" || strings.Contains(featureSlug, ".pass-") {
				return FilenameInfo{}, []Diagnostic{{
					Code:    "invalid_pass_qualifier",
					Path:    "",
					Message: "Execution Spec pass qualifier must be one terminal .pass-<number> segment using a positive decimal number without leading zeros.",
				}}
			}
			info.FeatureSlug = featureSlug
			info.PassNumber = passNumber
			info.HasPassQualifier = true
		}
		if !validFeatureSlug(info.FeatureSlug) {
			return FilenameInfo{}, []Diagnostic{
				{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
		}
		return info, nil

	case strings.HasSuffix(filename, planSuffix):
		slug := strings.TrimSuffix(filename, planSuffix)
		if !validFeatureSlug(slug) {
			return FilenameInfo{}, []Diagnostic{
				{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
		}
		return FilenameInfo{
			Kind:        ArtifactPlan,
			FeatureSlug: slug,
			OutputStem:  slug,
		}, nil

	default:
		return FilenameInfo{}, []Diagnostic{
			{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must end with .execution-spec.json or .plan.json."}}
	}
}

func parsePassQualifier(value string) (int64, bool) {
	if value == "" || value[0] < '1' || value[0] > '9' {
		return 0, false
	}
	for index := 1; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return 0, false
		}
	}
	number, err := strconv.ParseInt(value, 10, 64)
	return number, err == nil
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
