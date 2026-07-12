package speccompiler

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	RelaySpecsRepo   = "Paintersrp/relay-specs"
	RelaySpecsCommit = "4f34d2163f703b64a92f60a0295573d15fcbb68e"

	planSuffix          = ".plan.json"
	executionSpecSuffix = ".execution-spec.json"
)

type ArtifactKind string

const (
	ArtifactPlan          ArtifactKind = "plan"
	ArtifactExecutionSpec ArtifactKind = "execution_spec"
)

type ReplaySemantics string

const (
	ReplayImmutableBase     ReplaySemantics = "immutable_base"
	ReplayEvolvingPathChain ReplaySemantics = "evolving_path_chain"
)

type validationProfile string

type renderingProfile string

const (
	validatePlanV1      validationProfile = "plan_v1"
	validateExecutionV1 validationProfile = "execution_v1"
	validateExecutionV2 validationProfile = "execution_v2"

	renderPlanV1      renderingProfile = "plan_v1"
	renderExecutionV1 renderingProfile = "execution_v1"
	renderExecutionV2 renderingProfile = "execution_v2"
)

type versionRegistration struct {
	Kind             ArtifactKind
	Version          string
	SchemaPath       string
	CanonicalSubstep []string
	Validation       validationProfile
	Rendering        renderingProfile
	Replay           ReplaySemantics
}

var versionRegistry = []versionRegistration{
	{
		Kind:       ArtifactPlan,
		Version:    "1.0",
		SchemaPath: "schemas/plan.schema.json",
		Validation: validatePlanV1,
		Rendering:  renderPlanV1,
	},
	{
		Kind:             ArtifactExecutionSpec,
		Version:          "1.0",
		SchemaPath:       "schemas/execution-spec-v1.0.schema.json",
		CanonicalSubstep: []string{"number", "instruction", "files", "completion_criteria"},
		Validation:       validateExecutionV1,
		Rendering:        renderExecutionV1,
		Replay:           ReplayImmutableBase,
	},
	{
		Kind:             ArtifactExecutionSpec,
		Version:          "2.0",
		SchemaPath:       "schemas/execution-spec.schema.json",
		CanonicalSubstep: []string{"number", "instruction", "depends_on", "atomic", "files", "completion_criteria"},
		Validation:       validateExecutionV2,
		Rendering:        renderExecutionV2,
		Replay:           ReplayEvolvingPathChain,
	},
}

var latestVersionByKind = map[ArtifactKind]string{
	ArtifactPlan:          "1.0",
	ArtifactExecutionSpec: "2.0",
}

var schemaVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

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

type SchemaProvenance struct {
	ArtifactKind ArtifactKind
	Version      string
	Path         string
}

type Provenance struct {
	Repository       string
	Commit           string
	CompilerContract string
	Schemas          []SchemaProvenance
}

func SourceProvenance() Provenance {
	schemas := make([]SchemaProvenance, 0, len(versionRegistry))
	for _, registration := range versionRegistry {
		schemas = append(schemas, SchemaProvenance{
			ArtifactKind: registration.Kind,
			Version:      registration.Version,
			Path:         registration.SchemaPath,
		})
	}
	return Provenance{
		Repository:       RelaySpecsRepo,
		Commit:           RelaySpecsCommit,
		CompilerContract: "contracts/compiler.md",
		Schemas:          schemas,
	}
}

func Compile(filenameBasename string, rawJSON []byte) Result {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil)
	}
	if filename.Kind == ArtifactExecutionSpec {
		result, _ := CompileExecutionSpec(filenameBasename, rawJSON)
		return result
	}
	return compilePlan(filename, rawJSON)
}

func CompileExecutionSpec(filenameBasename string, rawJSON []byte) (Result, *ExecutionDocument) {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil), nil
	}
	if filename.Kind != ArtifactExecutionSpec {
		return failed([]Diagnostic{Diagnostic{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must identify an Execution Spec."}}, nil), nil
	}

	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil), nil
	}
	return compileExecutionDocument(filename, root, rawJSON)
}

func compileExecutionDocument(filename FilenameInfo, root *jsonNode, rawJSON []byte) (Result, *ExecutionDocument) {
	registration, notices, versionErrors := selectVersion(filename.Kind, root)
	if len(versionErrors) != 0 {
		return failed(normalizeDiagnostics(versionErrors), normalizeDiagnostics(notices)), nil
	}

	schemaValid, schemaErr := validateEmbeddedSchema(registration, rawJSON)
	errors := validateExecutionSpec(root, filename.FeatureSlug, registration)
	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded %s %s schema validation failed: %v", registration.Kind, registration.Version, schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: fmt.Sprintf("Artifact does not satisfy the embedded %s %s JSON Schema.", registration.Kind, registration.Version)})
	}
	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices), nil
	}

	document, err := decodeExecutionDocument(rawJSON, registration)
	if err != nil {
		return failed([]Diagnostic{Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Execution Spec: %v", err)}}, notices), nil
	}
	markdown, err := renderExecutionSpec(document, registration.Rendering)
	if err != nil {
		return failed([]Diagnostic{Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Render validated Execution Spec: %v", err)}}, notices), nil
	}
	output := filename.OutputStem + ".executor-brief.md"
	return Result{
		OutputFilename: &output,
		Markdown:       &markdown,
		Errors:         []Diagnostic{},
		Notices:        notices,
	}, document
}

func compilePlan(filename FilenameInfo, rawJSON []byte) Result {
	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil)
	}
	registration, notices, versionErrors := selectVersion(filename.Kind, root)
	if len(versionErrors) != 0 {
		return failed(normalizeDiagnostics(versionErrors), normalizeDiagnostics(notices))
	}
	schemaValid, schemaErr := validateEmbeddedSchema(registration, rawJSON)
	errors := validatePlan(root, filename.FeatureSlug)
	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded %s %s schema validation failed: %v", registration.Kind, registration.Version, schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: fmt.Sprintf("Artifact does not satisfy the embedded %s %s JSON Schema.", registration.Kind, registration.Version)})
	}
	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices)
	}
	markdown, err := renderPlan(rawJSON)
	if err != nil {
		return failed([]Diagnostic{Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Plan for rendering: %v", err)}}, notices)
	}
	output := filename.OutputStem + ".plan.md"
	return Result{OutputFilename: &output, Markdown: &markdown, Errors: []Diagnostic{}, Notices: notices}
}

func selectVersion(kind ArtifactKind, root *jsonNode) (versionRegistration, []Diagnostic, []Diagnostic) {
	latest, ok := latestVersion(kind)
	if !ok {
		return versionRegistration{}, nil, []Diagnostic{Diagnostic{Code: "unsupported_artifact_filename", Path: "", Message: "Unsupported artifact kind."}}
	}
	supplied := "absent"
	if root != nil && root.kind == nodeObject {
		if member, present := root.objectMember("schema_version"); present {
			supplied = member.value.describe()
			if member.value.kind == nodeString {
				if registration, supported := registeredVersion(kind, member.value.text); supported {
					return registration, nil, nil
				}
				if schemaVersionPattern.MatchString(member.value.text) {
					return versionRegistration{}, nil, []Diagnostic{Diagnostic{
						Code:    "unsupported_schema_version",
						Path:    "/schema_version",
						Message: fmt.Sprintf("schema_version %q is not supported for %s artifacts.", member.value.text, kind),
					}}
				}
			}
		}
	}
	return latest, []Diagnostic{Diagnostic{
		Code:    "schema_version_fallback",
		Path:    "/schema_version",
		Message: fmt.Sprintf("schema_version is %s; using latest supported %s version %s.", supplied, kind, latest.Version),
	}}, nil
}

func registeredVersion(kind ArtifactKind, version string) (versionRegistration, bool) {
	for _, registration := range versionRegistry {
		if registration.Kind == kind && registration.Version == version {
			return registration, true
		}
	}
	return versionRegistration{}, false
}

func latestVersion(kind ArtifactKind) (versionRegistration, bool) {
	version, ok := latestVersionByKind[kind]
	if !ok {
		return versionRegistration{}, false
	}
	return registeredVersion(kind, version)
}

func ParseFilename(filename string) (FilenameInfo, []Diagnostic) {
	if strings.ContainsAny(filename, `/\\`) {
		return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_filename_basename", Path: "", Message: "Filename must be a basename without path separators."}}
	}

	switch {
	case strings.HasSuffix(filename, executionSpecSuffix):
		stem := strings.TrimSuffix(filename, executionSpecSuffix)
		info := FilenameInfo{Kind: ArtifactExecutionSpec, FeatureSlug: stem, OutputStem: stem}
		if qualifierIndex := strings.LastIndex(stem, ".pass-"); qualifierIndex >= 0 {
			featureSlug := stem[:qualifierIndex]
			qualifier := stem[qualifierIndex+len(".pass-"):]
			passNumber, ok := parsePassQualifier(qualifier)
			if !ok || featureSlug == "" || strings.Contains(featureSlug, ".pass-") {
				return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_pass_qualifier", Path: "", Message: "Execution Spec pass qualifier must be one terminal .pass-<number> segment using a positive decimal number without leading zeros."}}
			}
			info.FeatureSlug = featureSlug
			info.PassNumber = passNumber
			info.HasPassQualifier = true
		}
		if !validFeatureSlug(info.FeatureSlug) {
			return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
		}
		return info, nil
	case strings.HasSuffix(filename, planSuffix):
		slug := strings.TrimSuffix(filename, planSuffix)
		if !validFeatureSlug(slug) {
			return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
		}
		return FilenameInfo{Kind: ArtifactPlan, FeatureSlug: slug, OutputStem: slug}, nil
	default:
		return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "unsupported_artifact_filename", Path: "", Message: "Filename must end with .execution-spec.json or .plan.json."}}
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
