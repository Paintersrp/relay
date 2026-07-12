package applier

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"relay/internal/speccompiler"
)

type pathChainPlan struct {
	ref            string
	firstOrder     int
	fileWorkRefs   []string
	substepRefs    []string
	operations     []string
	paths          []string
	dependencies   []string
	replay         speccompiler.ReplaySemantics
	disposition    Disposition
	outcome        OperationOutcome
	reason         string
	failure        FailureClass
	actions        []mutationAction
	changedPaths   []string
	protectedPaths []string
	uncertainPaths []string
}

func buildPathChainPlans(ctx context.Context, root string, projection speccompiler.ExecutionProjection) []pathChainPlan {
	fileByRef := make(map[string]speccompiler.ProjectedFileWork, len(projection.FileWork))
	for _, work := range projection.FileWork {
		fileByRef[work.Ref] = work
	}
	substepByRef := make(map[string]speccompiler.ProjectedSubstep, len(projection.Substeps))
	for _, substep := range projection.Substeps {
		substepByRef[substep.Ref] = substep
	}
	plans := make([]pathChainPlan, 0, len(projection.PathChains))
	for _, chain := range projection.PathChains {
		plan := pathChainPlan{
			ref:          chain.Ref,
			firstOrder:   chain.FirstSourceOrder,
			fileWorkRefs: append([]string(nil), chain.FileWorkRefs...),
			substepRefs:  append([]string(nil), chain.SubstepRefs...),
			paths:        append([]string(nil), chain.PathEndpoints...),
			replay:       chain.Replay,
			disposition:  DispositionDeterministic,
			outcome:      OperationNotAttempted,
		}
		if err := ctx.Err(); err != nil {
			blockPlan(&plan, FailureClassEnvironment, err.Error())
			plans = append(plans, plan)
			continue
		}
		works := make([]speccompiler.ProjectedFileWork, 0, len(chain.FileWorkRefs))
		for _, ref := range chain.FileWorkRefs {
			work, ok := fileByRef[ref]
			if !ok {
				blockPlan(&plan, FailureClassMaterialSpecGap, "path chain references unknown file work: "+ref)
				continue
			}
			works = append(works, work)
			plan.operations = append(plan.operations, work.Operation)
			if work.PathChainRef != chain.Ref {
				blockPlan(&plan, FailureClassMaterialSpecGap, fmt.Sprintf("file work %s belongs to path chain %s, not %s", work.Ref, work.PathChainRef, chain.Ref))
			}
		}
		for _, substepRef := range chain.SubstepRefs {
			substep, ok := substepByRef[substepRef]
			if !ok {
				blockPlan(&plan, FailureClassMaterialSpecGap, "path chain references unknown substep: "+substepRef)
				continue
			}
			plan.dependencies = appendUnique(plan.dependencies, substep.Dependencies...)
		}
		if plan.disposition != DispositionBlocked {
			classifyPathChain(root, &plan, works)
		}
		plans = append(plans, plan)
	}
	sort.SliceStable(plans, func(i, j int) bool { return plans[i].firstOrder < plans[j].firstOrder })
	return plans
}

func classifyPathChain(root string, plan *pathChainPlan, works []speccompiler.ProjectedFileWork) {
	if len(works) == 0 {
		blockPlan(plan, FailureClassMaterialSpecGap, "path chain contains no file work")
		return
	}
	for _, path := range plan.paths {
		if _, _, err := safePath(root, path); err != nil {
			blockPlan(plan, FailureClassUnsafeSource, err.Error())
			return
		}
	}
	residualReason := ""
	for _, work := range works {
		switch work.Operation {
		case "create":
			if work.Content == "" {
				blockPlan(plan, FailureClassMaterialSpecGap, "create file work requires complete content: "+work.Ref)
				return
			}
		case "modify":
			if len(work.Directives) == 0 {
				blockPlan(plan, FailureClassMaterialSpecGap, "modify file work requires directives: "+work.Ref)
				return
			}
			for _, directive := range work.Directives {
				switch directive.Kind {
				case "replace":
					if directive.OldText == "" || directive.NewText == "" || directive.ExpectedOccurrences < 1 {
						blockPlan(plan, FailureClassMaterialSpecGap, "replace directive is incomplete: "+directive.Ref)
						return
					}
				case "insert_before", "insert_after":
					if directive.Anchor == "" || directive.Content == "" || directive.ExpectedOccurrences < 1 {
						blockPlan(plan, FailureClassMaterialSpecGap, "insert directive is incomplete: "+directive.Ref)
						return
					}
				case "remove":
					if directive.OldText == "" || directive.ExpectedOccurrences < 1 {
						blockPlan(plan, FailureClassMaterialSpecGap, "remove directive is incomplete: "+directive.Ref)
						return
					}
				case "replace_file":
					if directive.Content == "" {
						blockPlan(plan, FailureClassMaterialSpecGap, "replace_file directive is incomplete: "+directive.Ref)
						return
					}
				default:
					residualReason = "unsupported deterministic directive: " + directive.Ref
				}
			}
		case "delete":
			if !work.DeleteFile {
				blockPlan(plan, FailureClassMaterialSpecGap, "delete file work requires delete_file true: "+work.Ref)
				return
			}
		case "rename":
			if strings.TrimSpace(work.DestinationPath) == "" {
				blockPlan(plan, FailureClassMaterialSpecGap, "rename file work requires destination path: "+work.Ref)
				return
			}
			if _, _, err := safePath(root, work.DestinationPath); err != nil {
				blockPlan(plan, FailureClassUnsafeSource, err.Error())
				return
			}
			if !work.PreserveContent {
				residualReason = "rename with replacement content requires model execution: " + work.Ref
			}
		default:
			blockPlan(plan, FailureClassMaterialSpecGap, "unsupported projected file operation: "+work.Operation)
			return
		}
	}
	if residualReason != "" {
		if err := preflightResidualPathChain(root, plan.replay, works); err != nil {
			blockPlan(plan, FailureClassUnsafeSource, err.Error())
			return
		}
		plan.disposition = DispositionResidual
		plan.outcome = OperationResidual
		plan.reason = residualReason
		return
	}
	actions, err := preflightPathChain(root, plan.replay, works)
	if err != nil {
		blockPlan(plan, FailureClassUnsafeSource, err.Error())
		return
	}
	plan.actions = actions
}

func propagateDispositions(plans []pathChainPlan, projection speccompiler.ExecutionProjection) {
	chainIndexesBySubstep := map[string][]int{}
	for index := range plans {
		for _, substepRef := range plans[index].substepRefs {
			chainIndexesBySubstep[substepRef] = append(chainIndexesBySubstep[substepRef], index)
		}
	}
	substepByRef := map[string]speccompiler.ProjectedSubstep{}
	for _, substep := range projection.Substeps {
		substepByRef[substep.Ref] = substep
	}
	changed := true
	for changed {
		changed = false
		for _, substep := range projection.Substeps {
			if !substep.AtomicPresent || !substep.Atomic {
				continue
			}
			indexes := chainIndexesBySubstep[substep.Ref]
			hasBlocked, hasResidual := false, false
			for _, index := range indexes {
				hasBlocked = hasBlocked || plans[index].disposition == DispositionBlocked
				hasResidual = hasResidual || plans[index].disposition == DispositionResidual
			}
			for _, index := range indexes {
				if hasBlocked && plans[index].disposition != DispositionBlocked {
					blockPlan(&plans[index], FailureClassGroup, "atomic substep contains blocked path-chain work: "+substep.Ref)
					changed = true
				} else if !hasBlocked && hasResidual && plans[index].disposition == DispositionDeterministic {
					residualizePlan(&plans[index], "atomic substep contains residual path-chain work: "+substep.Ref)
					changed = true
				}
			}
		}
		for _, substep := range projection.Substeps {
			status := dispositionForIndexes(plans, chainIndexesBySubstep[substep.Ref])
			_ = status
			for _, dependency := range substep.Dependencies {
				dependencyIndexes, ok := chainIndexesBySubstep[dependency]
				if !ok {
					for _, index := range chainIndexesBySubstep[substep.Ref] {
						if plans[index].disposition != DispositionBlocked {
							blockPlan(&plans[index], FailureClassDependency, "dependency substep is not projected: "+dependency)
							changed = true
						}
					}
					continue
				}
				dependencyDisposition := dispositionForIndexes(plans, dependencyIndexes)
				for _, index := range chainIndexesBySubstep[substep.Ref] {
					switch dependencyDisposition {
					case DispositionBlocked:
						if plans[index].disposition != DispositionBlocked {
							blockPlan(&plans[index], FailureClassDependency, "dependency substep is blocked: "+dependency)
							changed = true
						}
					case DispositionResidual:
						if plans[index].disposition == DispositionDeterministic {
							residualizePlan(&plans[index], "dependency substep is residual: "+dependency)
							changed = true
						}
					}
				}
			}
		}
	}
	_ = substepByRef
}

func dispositionForIndexes(plans []pathChainPlan, indexes []int) Disposition {
	disposition := DispositionDeterministic
	for _, index := range indexes {
		if plans[index].disposition == DispositionBlocked {
			return DispositionBlocked
		}
		if plans[index].disposition == DispositionResidual {
			disposition = DispositionResidual
		}
	}
	return disposition
}

func buildPartition(plans []pathChainPlan, projection speccompiler.ExecutionProjection) (Partition, string) {
	partition := Partition{
		DeterministicPathChains: []string{},
		ResidualPathChains:      []string{},
		DeterministicFileWork:   []string{},
		ResidualFileWork:        []string{},
		ProtectedPaths:          []string{},
		CoveredFileWork:         []string{},
	}
	coverage := map[string]int{}
	planIndexByRef := map[string]int{}
	for index, plan := range plans {
		if _, duplicate := planIndexByRef[plan.ref]; duplicate {
			return partition, "duplicate path-chain plan: " + plan.ref
		}
		planIndexByRef[plan.ref] = index
		for _, ref := range plan.fileWorkRefs {
			coverage[ref]++
			partition.CoveredFileWork = append(partition.CoveredFileWork, ref)
		}
		switch plan.disposition {
		case DispositionBlocked:
			continue
		case DispositionResidual:
			partition.ResidualPathChains = append(partition.ResidualPathChains, plan.ref)
			partition.ResidualFileWork = append(partition.ResidualFileWork, plan.fileWorkRefs...)
		case DispositionDeterministic:
			partition.DeterministicPathChains = append(partition.DeterministicPathChains, plan.ref)
			partition.DeterministicFileWork = append(partition.DeterministicFileWork, plan.fileWorkRefs...)
		default:
			return partition, "unknown path-chain disposition: " + string(plan.disposition)
		}
	}

	projectedChainRefs := map[string]struct{}{}
	for _, chain := range projection.PathChains {
		if _, duplicate := projectedChainRefs[chain.Ref]; duplicate {
			return partition, "duplicate projected path chain: " + chain.Ref
		}
		projectedChainRefs[chain.Ref] = struct{}{}
		index, ok := planIndexByRef[chain.Ref]
		if !ok {
			return partition, "projected path chain has no plan: " + chain.Ref
		}
		if !equalStrings(plans[index].fileWorkRefs, chain.FileWorkRefs) {
			return partition, "path chain is split or reordered: " + chain.Ref
		}
	}
	if len(planIndexByRef) != len(projectedChainRefs) {
		return partition, "path-chain plans contain an unknown projected chain"
	}
	for _, work := range projection.FileWork {
		if coverage[work.Ref] != 1 {
			return partition, fmt.Sprintf("file work %s has projection coverage %d, want exactly 1", work.Ref, coverage[work.Ref])
		}
		index, ok := planIndexByRef[work.PathChainRef]
		if !ok {
			return partition, "file work references unknown path chain: " + work.Ref
		}
		if !contains(plans[index].fileWorkRefs, work.Ref) {
			return partition, "file work is missing from its declared path chain: " + work.Ref
		}
	}
	if len(coverage) != len(projection.FileWork) {
		return partition, "projection contains duplicate or unknown file-work coverage"
	}

	planDispositionByFileRef := map[string]Disposition{}
	for _, plan := range plans {
		for _, ref := range plan.fileWorkRefs {
			planDispositionByFileRef[ref] = plan.disposition
		}
	}
	for _, substep := range projection.Substeps {
		if !substep.AtomicPresent || !substep.Atomic {
			continue
		}
		var disposition Disposition
		for _, ref := range substep.FileWorkRefs {
			current, ok := planDispositionByFileRef[ref]
			if !ok {
				return partition, "atomic substep references uncovered file work: " + ref
			}
			if disposition != "" && disposition != current {
				return partition, "atomic substep is split across dispositions: " + substep.Ref
			}
			disposition = current
		}
	}
	return partition, ""
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func markProjectionBlocked(plans []pathChainPlan, reason string) {
	if len(plans) == 0 {
		return
	}
	for index := range plans {
		if plans[index].disposition == DispositionBlocked {
			continue
		}
		blockPlan(&plans[index], FailureClassMaterialSpecGap, reason)
	}
}

func blockPlan(plan *pathChainPlan, class FailureClass, reason string) {
	plan.disposition = DispositionBlocked
	plan.outcome = OperationBlocked
	plan.failure = class
	plan.reason = reason
	plan.actions = nil
}

func residualizePlan(plan *pathChainPlan, reason string) {
	plan.disposition = DispositionResidual
	plan.outcome = OperationResidual
	plan.reason = reason
	plan.failure = FailureClassNone
	plan.actions = nil
}

func hasBlockedPlan(plans []pathChainPlan) bool {
	for _, plan := range plans {
		if plan.disposition == DispositionBlocked || plan.outcome == OperationBlocked {
			return true
		}
	}
	return false
}

func hasEnvironmentalStop(plans []pathChainPlan) bool {
	for _, plan := range plans {
		if plan.outcome == OperationUncertain || (plan.outcome == OperationBlocked && plan.failure == FailureClassEnvironment) {
			return true
		}
	}
	return false
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
