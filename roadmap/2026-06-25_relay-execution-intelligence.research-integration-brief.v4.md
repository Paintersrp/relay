# Relay Execution Intelligence — Research Integration Brief v4

Artifact status: planning anchor / research preservation artifact
Revision: v4 confirmatory-review traceability refinement  
Created: 2026-06-25  
Primary repositories: `Paintersrp/relay`, `Paintersrp/relay-contracts`  
Contract manifest used: `agents/knowledge/planner_github_knowledge_manifest.json` from `Paintersrp/relay-contracts` at `main`  
Manifest blob SHA observed in session: `0b31c40b223c4f649576f849cb9520a2e7669d91`  
Manifest-reported commit: `2aff6dd84cef7480e44109a43c90b5b2e6463b11`
Version status: v4 incorporates the external v3 planning-packet audit and supersedes v3 as the controlling research integration brief.

## 1. Purpose

This brief preserves the accepted interpretation of the Relay execution-efficiency research program before implementation planning begins.

It exists to prevent the research from being reduced to a small v1 implementation slice and then misinterpreted by future agents as the full destination. Future program roadmaps, managed Plans of Passes, Planner handoffs, audits, and implementation decisions should treat this brief as the durable research-to-product interpretation anchor.

This is not an implementation handoff. It does not authorize code changes. It defines the endgame, vocabulary, decisions, constraints, and capability targets that the later Program Roadmap and child Plans of Passes must account for.

## 2. Source basis

### 2.1 Deep Research reports

The research program is based on five Deep Research reports:

1. `deep-research-report.md` — Optimizing Coding-Agent Prompts for Executable Clarity per Token.
2. `deep-research-report-#2.md` — Deterministic Parsing, Packetization, and Executor-Brief Rendering for Coding-Agent Pipelines.
3. `Model-provider-research.md` — Relay Model and Provider Capability Profiles for Execution Routing.
4. `Evaluation-harness-research.md` — Relay Evaluation Harness for Prompt Efficiency and Coding-Agent Execution.
5. `Telemetry-research.md` — Relay Telemetry Retention, Redaction, and Auditability Policy for Coding-Agent Pipelines.

These reports are the original research source corpus. They are represented by this brief, the Program Roadmap v4, and the Capability Map v4. They are not default attachments for child-plan creation; they are archival evidence for targeted verification, provenance checks, grep/rg, ambiguity resolution, or explicit program amendment work.

### 2.2 Synthesis inputs

The research was synthesized into Relay-specific conclusions covering:

- executable clarity per token;
- Planner handoff structure;
- canonical packet structure;
- executor brief structure;
- context tiers;
- task atoms and implementation steps;
- deterministic handoff → packet → brief rendering;
- packet assessment as a separate Planner-authored artifact;
- model/provider capability profiles;
- token and cost accounting;
- evaluation harness and metrics;
- telemetry retention, redaction, provenance, and auditability.

### 2.3 Source-controlled Relay constraints

This brief also aligns with current source-controlled Relay contracts and policies from `Paintersrp/relay-contracts`, including:

- Planner agent instructions;
- Planner pass plan schema and contract;
- Planner-to-compiler contract;
- pipeline artifact model;
- MCP context broker, plan submission, and orchestrator work contracts;
- pipeline lifecycle policy;
- human approval gate policy;
- artifact naming policy;
- security redaction policy.

## 3. Problem this brief prevents

The risk is not only technical scope creep. The larger risk is research loss.

A small implementation plan that only adds a few v1 features could accidentally cause later agents to treat those partial features as the completed research outcome. That would make future work depend on reinterpretation of the original research, prior chats, and partial artifacts. This brief prevents that by establishing the complete accepted endgame and by requiring future plans to trace their passes back to target capabilities.

Every later plan should answer:

```text
Which research-backed target capability does this pass advance?
Which target capabilities are intentionally deferred but preserved?
Which recommendations are explicitly rejected, if any?
What evidence will prove the capability is implemented?
```

## 4. North Star

Relay’s long-term goal is to become an operator-controlled execution intelligence layer for coding-agent work.

Relay should transform Planner intent into bounded execution contracts, assess execution risk, render model-tier-appropriate briefs, recommend routing based on evidence, measure outcomes, and improve future routing/prompt/context decisions from audit-backed telemetry.

This does not mean automatic execution. It means policy-governed, evidence-backed recommendations and measured improvement while preserving manual operator control.

## 5. Endgame architecture

The target architecture is:

```text
Planner handoff
  → packet assessment
  → canonical packet
  → packet validation report
  → deterministic executor brief render
  → brief validation / prompt quality report
  → model/provider/cost recommendation
  → manual operator dispatch decision
  → executor result
  → validation evidence
  → audit packet
  → accepted | revision_required | rejected
  → telemetry / evaluation / calibration loop
```

The canonical packet becomes the execution and audit source of truth after packet creation. The executor brief is a deterministic render of validated packet content, not an LLM reinterpretation. Packet assessment is separate and advisory.

## 6. Accepted design decisions

These decisions should not be re-litigated unless later source-controlled work explicitly supersedes this brief.

### 6.1 Planning and execution boundaries

1. The Planner decides product behavior, workflow behavior, scope, assumptions, validation expectations, and audit priorities before execution.
2. The Executor executes the already-bounded task. It should not infer product behavior, expand scope, or make architecture/product decisions unless the Planner explicitly authorized a bounded decision.
3. Planner handoff remains the durable planning artifact before canonical packet creation.
4. Canonical packet becomes the execution/audit source of truth after packet creation.
5. Executor brief contains only executable instructions and required execution context.

### 6.2 Packet assessment

6. Packet assessment is a separate Planner-authored artifact created as part of the Planner handoff process.
7. Packet assessment evaluates clarity, repo grounding, blast radius, semantic risk, observability, coupling, rollback safety, task atoms, split recommendations, executor tier hints, and review requirements.
8. Packet assessment is advisory only.
9. Packet assessment must not become executable scope by being embedded into the handoff body or rendered executor brief.
10. Packet assessment can recommend child handoffs, but child execution requires explicit operator action and later Planner handoffs.

### 6.3 Packetization and rendering

11. Handoff machine-parsed zones should be structured enough for deterministic compilation.
12. Canonical packets should be schema-backed JSON artifacts with stable IDs, explicit enums, task atoms, implementation steps, context tiers, source maps, render policy, and telemetry seed fields.
13. Executor briefs should be deterministic renders from canonical packet fields and render profiles.
14. Rendering must not perform semantic planning.
15. Rendering must enforce context-tier allowlists and exclude review-only/audit-only content unless deliberately promoted by a policy-gated mechanism.

### 6.4 Task structure

16. A task atom is the smallest executable/checkable unit.
17. An implementation step is an ordered Planner-authored workflow phase that groups task atoms for readable execution sequencing.
18. A validation command is proof/check evidence.
19. An acceptance criterion is the success definition.
20. Task atoms support machine validation, splitting, routing, risk scoring, and audit mapping.
21. Implementation steps support readable execution order and avoid overly fragmented executor briefs.

### 6.5 Context efficiency

22. Relay should optimize for executable clarity per token, not shortest prompt.
23. Context should be classified into execution-required, execution-optional, reference-only, review-only, audit-only, and excluded.
24. Executor briefs should receive execution-required context by default.
25. Optional context should be budgeted and measured.
26. Review-only and audit-only context should not leak into executor briefs.

### 6.6 Model/provider routing

27. Executor tiers are abstract capability classes: cheap, mid, strong, reviewer.
28. Provider/model profiles are config-driven and updatable.
29. Provider-specific token accounting, caching, structured output controls, cost calculation, and privacy defaults belong in provider adapters/config, not in prompts.
30. Routing is advisory and operator-controlled in v1.
31. Relay should record recommendation, selected route, operator override, cost, latency, and outcome for calibration.

### 6.7 Evaluation

32. Evaluation is a first-class product subsystem, not a dashboard afterthought.
33. Relay evaluates artifact correctness, process quality, and outcome quality separately.
34. Fixture-based regression tests protect deterministic transformations.
35. Trace-based real-run evals attribute failures to the earliest observable boundary: handoff, packetization, rendering, prompt quality, context, executor/model, validation, or audit.
36. Public coding benchmarks are useful context but not the source of truth for Relay’s structured pipeline.
37. Deterministic structural hard-blocks are allowed early when they detect objective failures such as schema invalidity, missing required atom coverage, forbidden context leakage, missing validation, unresolved blocking unknowns, or redaction failure.
38. Numeric prompt-quality thresholds, ECPT thresholds, and warning-to-hard-block promotion should be calibrated from Relay telemetry and reviewed program policy, not invented upfront.
39. Executor reasoning/exploration alignment should be measured where trace evidence permits, including file-localization recall, decomposition quality, and over-prediction rate.

### 6.8 Telemetry and retention

40. Telemetry is metadata-first by default.
41. Always retain lineage metadata, artifact paths/hashes, schema/template/profile versions, operator decisions, token/cost summaries, validation outcomes, audit outcomes, and changed-file summaries.
42. Retain full prompts, provider payloads, source excerpts, command output excerpts, and diffs only when redacted, configured, bounded, and justified.
43. Never retain secrets, hidden chain-of-thought, unredacted logs, unbounded source dumps, or unrelated chat history.
44. If safe redaction is impossible, block content persistence and preserve only safe metadata/security events.

### 6.9 Operator workflow

45. Human approval gates remain required or at least available.
46. The operator sees assessment, prompt quality, context budget, route recommendation, provider/model candidates, cost estimate, and override controls before dispatch.
47. Recommendations do not dispatch executors.
48. Manual override is allowed and should be recorded.

## 7. Vocabulary

| Term | Definition |
|---|---|
| Planner handoff | Human-readable, Planner-authored implementation planning artifact. |
| Packet assessment | Separate Planner-authored advisory artifact evaluating one handoff for clarity, risk, split/routing/review needs. |
| Canonical packet | Machine-readable execution contract derived from a handoff. Source of truth after packet creation. |
| Executor brief | Deterministic render of canonical packet execution payload and allowed context. |
| Task atom | Smallest executable/checkable unit. |
| Implementation step | Ordered workflow phase grouping task atoms for readable execution. |
| Validation command | Command/check that produces proof evidence. |
| Acceptance criterion | Success definition for a task or atom. |
| Context tier | Classification determining whether context may render into executor brief or remains reference/review/audit-only. |
| Executor tier | Abstract capability class such as cheap, mid, strong, or reviewer. |
| Provider/model profile | Config-driven record describing model capabilities, token accounting, cost, tool support, structured output support, privacy posture, and routing suitability. |
| Render profile | Deterministic brief-rendering profile tuned for an executor tier or reviewer role. |
| Brief validation report | Structured report verifying required atom coverage, forbidden context exclusion, token budget, and DONE/BLOCKED clarity. |
| ECPT | Executable clarity per token; internal comparative metric, not v1 hard blocker. |
| Trace eval | Real-run evaluation using artifact lineage, telemetry, outcomes, and root-cause attribution. |

## 8. Non-negotiable constraints

Future plans and handoffs must preserve these constraints:

- No automatic executor dispatch in v1.
- No hardcoded provider/model names in prompt templates.
- No provider pricing hardcoded into prompts.
- No packet assessment body rendered into executor briefs.
- No freeform prose parsing where structured fields are practical.
- No optimization for token count alone.
- No executor authority to decide product/workflow behavior.
- No raw provider payload retention by default.
- No hidden chain-of-thought retention.
- No secrets or unredacted sensitive data in persisted artifacts.
- No child handoff or split execution without explicit operator decision.

## 9. Research preservation rules for future agents

Future Planner/Auditor/implementation agents should follow these rules:

1. Do not treat the first implementation pass as the whole research outcome.
2. Do not treat a v1 schema field as the final form if the capability map marks it as a foundation.
3. Do not collapse packet assessment into handoff prose or executor brief content.
4. Do not collapse provider/model routing into a hardcoded model name.
5. Do not reduce evaluation to a single pass/fail result.
6. Do not store raw content simply because it helps debugging.
7. Do not delete deferred capabilities from the roadmap unless explicitly rejected by the operator.
8. Every child plan should cite capability IDs from the Capability Map.
9. Every implementation handoff should target a selected pass only, but the pass should trace back to the full program roadmap.
10. If a future agent cannot find where a research recommendation went, the plan is incomplete and must be revised before implementation continues.

## 10. Planned program structure

The program should proceed through a Program Roadmap / Plan of Plans, then child Plans of Passes.

A child Plan of Passes means a bounded implementation plan under the larger program roadmap. It is not a single handoff. It is a normal Relay-managed plan focused on one coherent section of the program.

The Program Roadmap v4 defines the canonical child-plan decomposition as PLAN-A through PLAN-L. The families below are explanatory groupings only and should not be treated as a competing count or sequence.

Suggested child plan families:

1. Research preservation and contract baseline.
2. Packet assessment system.
3. Canonical packet and compiler vNext.
4. Deterministic renderer and brief validation.
5. Model/provider routing and cost profiles.
6. Telemetry, provenance, and redaction.
7. Evaluation harness and calibration.
8. Operator UX for assessment/routing/prompt quality.
9. Migration, documentation, and release hardening.

The Program Roadmap v4 is the controlling parent roadmap. Implementation planning should proceed through child Plans of Passes selected from PLAN-A through PLAN-L, beginning with the first three child plans only when requested.

## 11. Open decisions to carry forward

These are not blockers for preserving the research, but they must be explicitly handled by later planning:

- Exact directory and suffix for research-integration and capability-map artifacts.
- Exact first provider/model catalog entries.
- Whether redacted full-brief capture is allowed in v1 or deferred.
- Default retention durations by deployment model.
- Whether provider request/response capture is admin-only.
- Exact UI placement of assessment/routing/prompt-quality panels.
- Whether ECPT or related warning scores can become hard gates after calibration.
- Whether any future automation beyond recommendation is ever allowed, and under what policy.
- How much rationale belongs in executor-facing briefs versus reviewer/audit surfaces; resolve through PLAN-L experiments rather than upfront assumption.
- Whether constraints should be front-loaded, repeated, or tail-loaded in render profiles; resolve through controlled render-profile evaluation.
- Whether XML-style structure, terse prose, or hybrid formats perform best by task family and executor tier.
- How to measure executor reasoning/exploration alignment from trace evidence without retaining hidden chain-of-thought.

## 12. Success criteria for the planning pivot

The project pivot is successfully planned when:

- the Research Integration Brief exists and is referenced by the Program Roadmap;
- the Capability Map covers all major research recommendations;
- every capability is assigned one of: planned, deferred-but-preserved, explicitly-rejected, or open-decision;
- the Program Roadmap defines child plans that cover all planned capabilities;
- child Plans of Passes cite capability IDs;
- implementation handoffs are generated one pass at a time without redefining the long-term destination.

---

## 13. Child-plan packet and research evidence policy

This section preserves the intended use of the research corpus after the Program Roadmap and Capability Map exist.

### 13.1 Controlling hierarchy for future planning

Future agents creating child Plans of Passes must treat sources in this order:

```text
1. Current fetched Relay source-controlled contracts, schemas, policies, templates, and examples
2. Program Roadmap / Plan of Plans v4
3. Research Integration Brief v4
4. Capability Map v4
5. Targeted excerpts or grep/rg findings from Deep Research reports, only when explicitly needed
6. Prior synthesis reports, only when explicitly needed
7. Prior packet assessment plan or historical handoffs, only as historical context
8. Prior chat context
```

The Deep Research reports remain archival evidence. They should not be used to reopen decisions already preserved in this brief, the roadmap, or the capability map unless the user explicitly asks for a program amendment.

### 13.2 Required planning packet

Every child plan should receive:

- Program Roadmap v4;
- this Research Integration Brief v4;
- Capability Map v4;
- the current source-controlled Relay contracts/schemas/policies required by the Planner GitHub knowledge manifest for the child plan domain.

The five Deep Research reports should not be included as default child-plan attachments. Their content is represented in the three anchors.

### 13.3 Optional archival evidence use

A child-plan agent may consult a Deep Research report only when there is a targeted reason:

- an anchor artifact is ambiguous or inconsistent;
- a capability-map row needs source verification;
- a reviewer asks for provenance;
- grep/rg over the original research wording is needed;
- the user explicitly requests a program amendment.

When a report is consulted, the child plan must record the file, query or section, reason, and decision affected. If no report is consulted, the child plan should state that research evidence is represented by the three anchors.

### 13.4 Missing-items review outcome

The v2 review did not identify a missing major research theme, but it identified preservation weaknesses in the original anchor set. The v3 revision resolved the raw Deep Research attachment policy. The external v3 audit identified traceability and consistency defects rather than endgame gaps; v4 resolves those defects by assigning CAP-A06, defining the optimization track, adding CAP-I10, clarifying deterministic-versus-calibrated prompt gates, and preserving prompt-shape open questions for empirical calibration.

### 13.5 Rule for future amendments

A future amendment may add, split, merge, defer, or reject capabilities only if it updates:

- the Program Roadmap;
- the Capability Map;
- affected child-plan packet requirements;
- and the research preservation rationale.

A normal pass handoff must not alter program direction.

