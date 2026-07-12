package speccompiler

import (
	"fmt"
	"sort"
	"strings"
)

type ExecutionProjection struct {
	Replay             ReplaySemantics
	Substeps           []ProjectedSubstep
	PathChains         []ProjectedPathChain
	FileWork           []ProjectedFileWork
	ValidationCommands []ProjectedValidationCommand
}

type ProjectedSubstep struct {
	Ref                  string
	AuthoredDependencies []string
	ImplicitDependencies []string
	Dependencies         []string
	AtomicPresent        bool
	Atomic               bool
	FileWorkRefs         []string
}

type ProjectedPathChain struct {
	Ref              string
	FileWorkRefs     []string
	PathEndpoints    []string
	SubstepRefs      []string
	Replay           ReplaySemantics
	FirstSourceOrder int
}

type ProjectedFileWork struct {
	Ref                 string
	SubstepRef          string
	PathChainRef        string
	SourceOrder         int
	Path                string
	DestinationPath     string
	Operation           string
	Purpose             string
	SourcePreconditions []ProjectedSourcePrecondition
	Directives          []ProjectedDirective
	Content             string
	DeleteFile          bool
	PreserveContent     bool
}

type ProjectedSourcePrecondition struct {
	Kind string
	Path string
}

type ProjectedDirective struct {
	Ref                 string
	SourceOrder         int
	Kind                string
	OldText             string
	NewText             string
	Anchor              string
	Content             string
	ExpectedOccurrences int
	Grounding           *SelectorGrounding
}

type ProjectedValidationCommand struct {
	Command          string
	WorkingDirectory string
	Expected         string
	SuccessSignal    string
}

type SelectorGrounding struct {
	DirectiveRef         string
	Selector             string
	ExpectedOccurrences  int
	Replay               ReplaySemantics
	BaseRequired         bool
	ProducerDirectiveRef string
}

func ProjectExecutionSpec(document *ExecutionDocument) (ExecutionProjection, []Diagnostic) {
	if document == nil {
		return ExecutionProjection{}, []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: "Execution document is required."}}
	}
	projection := ExecutionProjection{Replay: ReplayEvolvingPathChain}
	substepOrder := map[string]int{}
	fileIndexByRef := map[string]int{}
	directiveOrder := 0
	for _, step := range document.Steps {
		for _, substep := range step.Substeps {
			substepRef := fmt.Sprintf("%d.%d", step.Number, substep.Number)
			projectedSubstep := ProjectedSubstep{
				Ref:                  substepRef,
				AuthoredDependencies: append([]string(nil), substep.DependsOn...),
				AtomicPresent:        substep.Atomic != nil,
			}
			if substep.Atomic != nil {
				projectedSubstep.Atomic = *substep.Atomic
			}
			substepOrder[substepRef] = len(projection.Substeps)
			for filePosition, file := range substep.Files {
				fileRef := fmt.Sprintf("%s.file.%d", substepRef, filePosition+1)
				work := ProjectedFileWork{
					Ref:                 fileRef,
					SubstepRef:          substepRef,
					SourceOrder:         len(projection.FileWork),
					Path:                file.Path,
					DestinationPath:     file.DestinationPath,
					Operation:           file.Operation,
					Purpose:             file.Purpose,
					Content:             file.Implementation.Content,
					DeleteFile:          file.Implementation.DeleteFile,
					PreserveContent:     file.Implementation.PreserveContent,
					SourcePreconditions: sourcePreconditionsForFile(file),
				}
				for changePosition, change := range file.Implementation.Changes {
					directiveOrder++
					work.Directives = append(work.Directives, ProjectedDirective{
						Ref:                 fmt.Sprintf("%s.change.%d", fileRef, changePosition+1),
						SourceOrder:         directiveOrder,
						Kind:                change.Kind,
						OldText:             change.OldText,
						NewText:             change.NewText,
						Anchor:              change.Anchor,
						Content:             change.Content,
						ExpectedOccurrences: change.ExpectedOccurrences,
					})
				}
				fileIndexByRef[fileRef] = len(projection.FileWork)
				projection.FileWork = append(projection.FileWork, work)
				projectedSubstep.FileWorkRefs = append(projectedSubstep.FileWorkRefs, fileRef)
			}
			projection.Substeps = append(projection.Substeps, projectedSubstep)
		}
	}

	projection.PathChains = buildPathChains(projection.FileWork, ReplayEvolvingPathChain)
	for chainIndex := range projection.PathChains {
		chain := &projection.PathChains[chainIndex]
		for _, fileRef := range chain.FileWorkRefs {
			index, exists := fileIndexByRef[fileRef]
			if !exists {
				return ExecutionProjection{}, []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("Path chain references unknown file work %q.", fileRef)}}
			}
			projection.FileWork[index].PathChainRef = chain.Ref
		}
	}

	implicitBySubstep := deriveImplicitDependencies(projection.PathChains, projection.FileWork, fileIndexByRef, substepOrder)
	for index := range projection.Substeps {
		substep := &projection.Substeps[index]
		substep.ImplicitDependencies = implicitBySubstep[substep.Ref]
		substep.Dependencies = mergeDependencies(substep.AuthoredDependencies, substep.ImplicitDependencies)
	}
	applySelectorGrounding(&projection, fileIndexByRef)

	for _, command := range document.Validation.Commands {
		projection.ValidationCommands = append(projection.ValidationCommands, ProjectedValidationCommand{
			Command:          command.Command,
			WorkingDirectory: command.WorkingDirectory,
			Expected:         command.Expected,
		})
	}
	if diagnostics := validateProjectionCoverage(projection); len(diagnostics) != 0 {
		return ExecutionProjection{}, diagnostics
	}
	return projection, nil
}

func sourcePreconditionsForFile(file ExecutionFile) []ProjectedSourcePrecondition {
	switch file.Operation {
	case "modify", "delete":
		return []ProjectedSourcePrecondition{ProjectedSourcePrecondition{Kind: "path_exists", Path: file.Path}}
	case "create":
		return []ProjectedSourcePrecondition{ProjectedSourcePrecondition{Kind: "path_absent", Path: file.Path}}
	case "rename":
		return []ProjectedSourcePrecondition{
			{Kind: "path_exists", Path: file.Path},
			{Kind: "path_absent", Path: file.DestinationPath},
		}
	default:
		return nil
	}
}

func buildPathChains(fileWork []ProjectedFileWork, replay ReplaySemantics) []ProjectedPathChain {
	parents := make([]int, len(fileWork))
	for i := range parents {
		parents[i] = i
	}
	var find func(int) int
	find = func(value int) int {
		if parents[value] != value {
			parents[value] = find(parents[value])
		}
		return parents[value]
	}
	union := func(left, right int) {
		leftRoot, rightRoot := find(left), find(right)
		if leftRoot != rightRoot {
			parents[rightRoot] = leftRoot
		}
	}
	endpointOwner := map[string]int{}
	for index, work := range fileWork {
		for _, endpoint := range fileWorkEndpoints(work) {
			if prior, exists := endpointOwner[endpoint]; exists {
				union(prior, index)
			} else {
				endpointOwner[endpoint] = index
			}
		}
	}

	chainIndexByRoot := map[int]int{}
	var chains []ProjectedPathChain
	for index, work := range fileWork {
		root := find(index)
		chainIndex, exists := chainIndexByRoot[root]
		if !exists {
			chainIndex = len(chains)
			chainIndexByRoot[root] = chainIndex
			chains = append(chains, ProjectedPathChain{
				Ref:              "chain." + work.Ref,
				Replay:           replay,
				FirstSourceOrder: work.SourceOrder,
			})
		}
		chain := &chains[chainIndex]
		chain.FileWorkRefs = append(chain.FileWorkRefs, work.Ref)
		chain.PathEndpoints = appendUnique(chain.PathEndpoints, fileWorkEndpoints(work)...)
		chain.SubstepRefs = appendUnique(chain.SubstepRefs, work.SubstepRef)
	}
	return chains
}

func fileWorkEndpoints(work ProjectedFileWork) []string {
	endpoints := []string{work.Path}
	if work.Operation == "rename" {
		endpoints = append(endpoints, work.DestinationPath)
	}
	return endpoints
}

func deriveImplicitDependencies(chains []ProjectedPathChain, fileWork []ProjectedFileWork, fileIndexByRef, substepOrder map[string]int) map[string][]string {
	dependencies := map[string][]string{}
	for _, chain := range chains {
		previous := ""
		for _, fileRef := range chain.FileWorkRefs {
			current := fileWork[fileIndexByRef[fileRef]].SubstepRef
			if previous != "" && current != previous {
				dependencies[current] = appendUnique(dependencies[current], previous)
			}
			previous = current
		}
	}
	for substepRef := range dependencies {
		sort.SliceStable(dependencies[substepRef], func(i, j int) bool {
			return substepOrder[dependencies[substepRef][i]] < substepOrder[dependencies[substepRef][j]]
		})
	}
	return dependencies
}

func mergeDependencies(authored, implicit []string) []string {
	combined := append([]string(nil), authored...)
	return appendUnique(combined, implicit...)
}

func applySelectorGrounding(projection *ExecutionProjection, fileIndexByRef map[string]int) {
	for _, chain := range projection.PathChains {
		type directiveLocation struct {
			fileIndex      int
			directiveIndex int
		}
		var locations []directiveLocation
		for _, fileRef := range chain.FileWorkRefs {
			fileIndex := fileIndexByRef[fileRef]
			for directiveIndex := range projection.FileWork[fileIndex].Directives {
				locations = append(locations, directiveLocation{fileIndex: fileIndex, directiveIndex: directiveIndex})
			}
		}
		for position, location := range locations {
			directive := &projection.FileWork[location.fileIndex].Directives[location.directiveIndex]
			selector := directiveSelector(*directive)
			if selector == "" {
				continue
			}
			grounding := &SelectorGrounding{
				DirectiveRef:        directive.Ref,
				Selector:            selector,
				ExpectedOccurrences: directive.ExpectedOccurrences,
				Replay:              projection.Replay,
				BaseRequired:        true,
			}
			for earlier := position - 1; earlier >= 0; earlier-- {
				candidateLocation := locations[earlier]
				candidate := projection.FileWork[candidateLocation.fileIndex].Directives[candidateLocation.directiveIndex]
				if content := directiveTargetContent(candidate); content != "" && strings.Contains(content, selector) {
					grounding.BaseRequired = false
					grounding.ProducerDirectiveRef = candidate.Ref
					break
				}
			}
			directive.Grounding = grounding
		}
	}
}

func directiveSelector(directive ProjectedDirective) string {
	switch directive.Kind {
	case "replace", "remove":
		return directive.OldText
	case "insert_before", "insert_after":
		return directive.Anchor
	default:
		return ""
	}
}

func directiveTargetContent(directive ProjectedDirective) string {
	switch directive.Kind {
	case "replace":
		return directive.NewText
	case "insert_before", "insert_after", "replace_file":
		return directive.Content
	default:
		return ""
	}
}

func validateProjectionCoverage(projection ExecutionProjection) []Diagnostic {
	fileRefs := map[string]struct{}{}
	chainMembership := map[string]int{}
	for _, work := range projection.FileWork {
		if _, duplicate := fileRefs[work.Ref]; duplicate {
			return []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("Duplicate file-work reference %q.", work.Ref)}}
		}
		fileRefs[work.Ref] = struct{}{}
		if work.PathChainRef == "" {
			return []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("File work %q has no path chain.", work.Ref)}}
		}
	}
	for _, chain := range projection.PathChains {
		for _, fileRef := range chain.FileWorkRefs {
			chainMembership[fileRef]++
		}
	}
	for fileRef := range fileRefs {
		if chainMembership[fileRef] != 1 {
			return []Diagnostic{Diagnostic{Code: "projection_invariant", Path: "", Message: fmt.Sprintf("File work %q appears in %d path chains.", fileRef, chainMembership[fileRef])}}
		}
	}
	return nil
}

func appendUnique(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
