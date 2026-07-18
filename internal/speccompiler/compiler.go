package speccompiler

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"relay/internal/artifactschema"
)

const (
	RelaySpecsRepo   = artifactschema.AuthorityRepository
	RelaySpecsCommit = artifactschema.AuthorityCommit

	planSuffix                   = ".plan.json"
	executionSpecSuffix          = ".execution-spec.json"
	deliveryTicketJSONSuffix     = ".delivery-ticket.json"
	deliveryTicketMarkdownSuffix = ".delivery-ticket.md"
	transitionPlanJSONSuffix     = ".transition-plan.json"
	transitionPlanMarkdownSuffix = ".transition-plan.md"
	requirementsSuffix           = ".requirements.md"
	sharedDesignSuffix           = ".design.md"
	ticketDesignBriefSuffix      = ".design-brief.md"
)

type ArtifactKind string

const (
	ArtifactPlan              ArtifactKind = "plan"
	ArtifactExecutionSpec     ArtifactKind = "execution_spec"
	ArtifactDeliveryTicket    ArtifactKind = "delivery_ticket"
	ArtifactTransitionPlan    ArtifactKind = "transition_plan"
	ArtifactRequirements      ArtifactKind = "requirements"
	ArtifactSharedDesign      ArtifactKind = "shared_design"
	ArtifactTicketDesignBrief ArtifactKind = "ticket_design_brief"
)

type filenameRule struct {
	suffix             string
	kind               ArtifactKind
	allowPassQualifier bool
	ticketQualified    bool
}

var canonicalFilenameRules = []filenameRule{
	{suffix: executionSpecSuffix, kind: ArtifactExecutionSpec, allowPassQualifier: true},
	{suffix: planSuffix, kind: ArtifactPlan},
	{suffix: deliveryTicketJSONSuffix, kind: ArtifactDeliveryTicket, ticketQualified: true},
	{suffix: deliveryTicketMarkdownSuffix, kind: ArtifactDeliveryTicket, ticketQualified: true},
	{suffix: transitionPlanJSONSuffix, kind: ArtifactTransitionPlan, ticketQualified: true},
	{suffix: transitionPlanMarkdownSuffix, kind: ArtifactTransitionPlan, ticketQualified: true},
	{suffix: requirementsSuffix, kind: ArtifactRequirements},
	{suffix: sharedDesignSuffix, kind: ArtifactSharedDesign},
	{suffix: ticketDesignBriefSuffix, kind: ArtifactTicketDesignBrief, ticketQualified: true},
}

var ticketIDPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`)

type ReplaySemantics string

const ReplayEvolvingPathChain ReplaySemantics = "evolving_path_chain"

type currentArtifactDefinition struct {
	Kind             ArtifactKind
	ProducerVersion  string
	SchemaKind       artifactschema.Kind
	CanonicalSubstep []string
}

func currentDefinition(kind ArtifactKind) (currentArtifactDefinition, bool) {
	switch kind {
	case ArtifactPlan:
		return currentArtifactDefinition{Kind: kind, ProducerVersion: "1.0", SchemaKind: artifactschema.KindPlan}, true
	case ArtifactExecutionSpec:
		return currentArtifactDefinition{
			Kind: kind, ProducerVersion: "2.0", SchemaKind: artifactschema.KindExecutionSpec,
			CanonicalSubstep: []string{"number", "instruction", "depends_on", "atomic", "files", "completion_criteria"},
		}, true
	default:
		return currentArtifactDefinition{}, false
	}
}

type FilenameInfo struct {
	Kind             ArtifactKind
	FeatureSlug      string
	TicketID         string
	Revision         int64
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
	schemas := make([]SchemaProvenance, 0, 2)
	for _, kind := range []ArtifactKind{ArtifactPlan, ArtifactExecutionSpec} {
		definition, _ := currentDefinition(kind)
		shared, _ := artifactschema.Current(definition.SchemaKind)
		schemas = append(schemas, SchemaProvenance{ArtifactKind: kind, Version: definition.ProducerVersion, Path: shared.AuthorityPath})
	}
	return Provenance{Repository: RelaySpecsRepo, Commit: RelaySpecsCommit, CompilerContract: "contracts/compiler.md", Schemas: schemas}
}

func Compile(filenameBasename string, rawJSON []byte) Result {
	filename, filenameErrors := ParseFilename(filenameBasename)
	if len(filenameErrors) != 0 {
		return failed(filenameErrors, nil)
	}
	switch filename.Kind {
	case ArtifactExecutionSpec:
		result, _ := CompileExecutionSpec(filenameBasename, rawJSON)
		return result
	case ArtifactPlan:
		return compilePlan(filename, rawJSON)
	default:
		return failed([]Diagnostic{{Code: "unsupported_artifact_kind", Path: "", Message: fmt.Sprintf("Artifact kind %q is recognized by filename dispatch but has no current compiler implementation.", filename.Kind)}}, nil)
	}
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
	definition, _ := currentDefinition(filename.Kind)
	notices := schemaVersionNotice(root, definition)
	schemaValid, schemaErr := artifactschema.Validate(definition.SchemaKind, rawJSON)
	errors := validateExecutionSpec(root, filename.FeatureSlug, definition)
	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded current %s schema validation failed: %v", definition.Kind, schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: fmt.Sprintf("Artifact does not satisfy the embedded current %s JSON Schema.", definition.Kind)})
	}
	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices), nil
	}
	document, err := decodeExecutionDocument(rawJSON)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Execution Spec: %v", err)}}, notices), nil
	}
	markdown, err := renderExecutionSpec(document)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Render validated Execution Spec: %v", err)}}, notices), nil
	}
	output := filename.OutputStem + ".executor-brief.md"
	return Result{OutputFilename: &output, Markdown: &markdown, Errors: []Diagnostic{}, Notices: notices}, document
}

func compilePlan(filename FilenameInfo, rawJSON []byte) Result {
	root, lexicalErrors := parseDocument(rawJSON)
	if len(lexicalErrors) != 0 {
		return failed(lexicalErrors, nil)
	}
	definition, _ := currentDefinition(filename.Kind)
	notices := schemaVersionNotice(root, definition)
	schemaValid, schemaErr := artifactschema.Validate(definition.SchemaKind, rawJSON)
	errors := validatePlan(root, filename.FeatureSlug)
	if schemaErr != nil {
		errors = append(errors, Diagnostic{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Embedded current %s schema validation failed: %v", definition.Kind, schemaErr)})
	} else if !schemaValid && len(errors) == 0 {
		errors = append(errors, Diagnostic{Code: "invalid_value_type", Path: "", Message: fmt.Sprintf("Artifact does not satisfy the embedded current %s JSON Schema.", definition.Kind)})
	}
	errors = normalizeDiagnostics(errors)
	notices = normalizeDiagnostics(notices)
	if len(errors) != 0 {
		return failed(errors, notices)
	}
	markdown, err := renderPlan(rawJSON)
	if err != nil {
		return failed([]Diagnostic{{Code: "invalid_json", Path: "", Message: fmt.Sprintf("Decode validated Plan for rendering: %v", err)}}, notices)
	}
	output := filename.OutputStem + ".plan.md"
	return Result{OutputFilename: &output, Markdown: &markdown, Errors: []Diagnostic{}, Notices: notices}
}

func schemaVersionNotice(root *jsonNode, definition currentArtifactDefinition) []Diagnostic {
	supplied := "absent"
	if root != nil && root.kind == nodeObject {
		if member, present := root.objectMember("schema_version"); present {
			supplied = member.value.describe()
			if member.value.kind == nodeString && member.value.text == definition.ProducerVersion {
				return nil
			}
		}
	}
	return []Diagnostic{{Code: "schema_version_anomaly", Path: "/schema_version", Message: fmt.Sprintf("schema_version is %s; current %s producers emit %q, but artifact content is validated with the sole current definition.", supplied, definition.Kind, definition.ProducerVersion)}}
}

func ParseFilename(filename string) (FilenameInfo, []Diagnostic) {
	if strings.ContainsAny(filename, `/\\`) {
		return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_filename_basename", Path: "", Message: "Filename must be a basename without path separators."}}
	}

	for _, rule := range canonicalFilenameRules {
		if !strings.HasSuffix(filename, rule.suffix) {
			continue
		}

		stem := strings.TrimSuffix(filename, rule.suffix)
		info := FilenameInfo{Kind: rule.kind, FeatureSlug: stem, OutputStem: stem}
		if rule.ticketQualified {
			featureSlug, ticketID, revision, diagnostic := parseTicketQualifiedStem(stem)
			if diagnostic != nil {
				return FilenameInfo{}, []Diagnostic{*diagnostic}
			}
			info.FeatureSlug = featureSlug
			info.TicketID = ticketID
			info.Revision = revision
		} else if rule.allowPassQualifier {
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
		} else if strings.Contains(stem, ".pass-") {
			return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_pass_qualifier", Path: "", Message: "Pass qualifiers are supported only by Execution Spec filenames."}}
		}
		if !validFeatureSlug(info.FeatureSlug) {
			return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}}
		}
		return info, nil
	}
	return FilenameInfo{}, []Diagnostic{Diagnostic{Code: "unsupported_artifact_filename", Path: "", Message: "Filename does not identify a supported canonical artifact kind."}}
}

func parseTicketQualifiedStem(stem string) (string, string, int64, *Diagnostic) {
	const ticketMarker = ".ticket-"

	ticketIndex := strings.Index(stem, ticketMarker)
	if ticketIndex <= 0 {
		if !validFeatureSlug(stem) {
			return "", "", 0, &Diagnostic{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}
		}
		return "", "", 0, &Diagnostic{Code: "invalid_ticket_id", Path: "/ticket_id", Message: "Ticket artifact filename must include a canonical ticket ID."}
	}
	featureSlug := stem[:ticketIndex]
	if !validFeatureSlug(featureSlug) {
		return "", "", 0, &Diagnostic{Code: "invalid_feature_slug", Path: "/feature_slug", Message: "Filename feature slug must be lowercase kebab-case."}
	}
	qualified := stem[ticketIndex+len(ticketMarker):]
	revisionIndex := strings.IndexByte(qualified, '.')
	ticketID := qualified
	revisionText := ""
	if revisionIndex >= 0 {
		ticketID = qualified[:revisionIndex]
		revisionText = qualified[revisionIndex+1:]
	}
	if !ticketIDPattern.MatchString(ticketID) {
		return "", "", 0, &Diagnostic{Code: "invalid_ticket_id", Path: "/ticket_id", Message: "Ticket artifact filename ticket ID must be uppercase canonical ticket syntax."}
	}
	if !strings.HasPrefix(revisionText, "r") {
		return "", "", 0, &Diagnostic{Code: "invalid_ticket_revision", Path: "/revision", Message: "Ticket artifact filename revision must use a terminal .r<positive-number> segment."}
	}
	revisionText = strings.TrimPrefix(revisionText, "r")
	revision, ok := parsePassQualifier(revisionText)
	if !ok {
		return "", "", 0, &Diagnostic{Code: "invalid_ticket_revision", Path: "/revision", Message: "Ticket artifact filename revision must be a positive decimal number without leading zeros."}
	}
	return featureSlug, ticketID, revision, nil
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
