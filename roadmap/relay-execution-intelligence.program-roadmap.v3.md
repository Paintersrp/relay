# Relay Execution Intelligence — Program Roadmap / Plan of Plans v3

Artifact status: planning anchor / program roadmap / plan-of-plans  
Revision: v3 research-evidence policy refinement  
Created: 2026-06-25  
Refined: 2026-06-25  

Companion artifacts:

- `2026-06-25_relay-execution-intelligence.research-integration-brief.v3.md`
- `2026-06-25_relay-execution-intelligence.capability-map.v3.md`
- `2026-06-25_relay-execution-intelligence.precommit-gap-review.md`
- `2026-06-25_relay-execution-intelligence.research-evidence-policy-review.md`

Primary repositories:

- `Paintersrp/relay`
- `Paintersrp/relay-contracts`

Contract manifest used: `agents/knowledge/planner_github_knowledge_manifest.json` from `Paintersrp/relay-contracts` at `main`  
Manifest blob SHA observed in refinement session: `0b31c40b223c4f649576f849cb9520a2e7669d91`  
Manifest-reported commit: `2aff6dd84cef7480e44109a43c90b5b2e6463b11`


---

## 1. Purpose

This roadmap is the parent planning artifact for pivoting Relay from its current handoff pipeline toward the research-backed endgame described in the Research Integration Brief and Capability Map.

It exists to prevent the five Deep Research reports and synthesis work from being compressed into a partial v1 implementation and then mistakenly treated as the completed destination.

This roadmap is not itself a schema-valid Relay Plan v2 JSON. It is a **Program Roadmap / Plan of Plans**. Its job is to define the complete implementation program so that later schema-valid child Plans of Passes can be created without reinterpreting the research from scratch.

---

## 2. Roadmap status

```text
planning_anchor: true
implementation_handoff: false
relay_plan_submission_payload: false
run_creation_input: false
```

It does not authorize code changes, plan submission, run creation, executor dispatch, or audit acceptance. It defines the program that later child Plans of Passes must implement.

---

## 3. Controlling document set

This roadmap has two companion planning anchors:

1. **Research Integration Brief v3** — accepted interpretation of the five Deep Research reports and synthesis work.
2. **Capability Map v3** — traceability matrix from research findings to Relay capabilities, gaps, implementation tracks, and evidence expectations.

The three-anchor set is the minimum durable context for future child plan creation:

```text
Program Roadmap v3
+ Research Integration Brief v3
+ Capability Map v3
```

Future child plans should not be created from the Deep Research reports alone, and the reports should not be included in the default child-plan packet. The reports are archival source evidence; these three artifacts preserve the accepted Relay-specific interpretation and should control future planning.

---

## 4. North Star

Relay’s long-term goal is to become an **operator-controlled execution intelligence layer for coding-agent work**.

Relay should transform Planner intent into bounded execution contracts, assess execution risk, render model-tier-appropriate briefs, recommend routing based on evidence, measure outcomes, and improve future routing/prompt/context decisions from audit-backed telemetry.

This does not mean automatic executor dispatch in the current roadmap. The system should become more intelligent, measurable, and recommendation-driven while preserving human/operator approval gates.

---

## 5. Scope of the program

The full program covers the transition from current Relay to a research-backed execution intelligence system across these areas:

1. Research preservation and planning governance.
2. Artifact taxonomy, lifecycle, naming, and approval policies.
3. Packet assessment as a separate Planner-authored advisory artifact.
4. Planner handoff profile hardening and stable repo guidance.
5. Canonical packet vNext with task atoms, implementation steps, context tiers, source maps, render policy, and telemetry seeds.
6. Compiler hardening and packet validation.
7. Deterministic renderer profiles and brief validation.
8. Model/provider capability profiles, executor tiers, token accounting, and config-driven cost accounting.
9. Telemetry, provenance, retention, redaction, safe exports, and content-capture controls.
10. Evaluation harnesses, fixture regression, trace evaluation, root-cause attribution, and calibration.
11. Operator UX for packet assessment, prompt/brief quality, routing, cost, override, dispatch, and audit support.
12. Migration, compatibility, documentation, release hardening, and long-term optimization loops.

---

## 6. Non-negotiable program constraints

These constraints apply to every child plan and pass handoff unless explicitly superseded by a later reviewed program-level decision.

1. Packet assessment remains a separate Planner-authored advisory artifact.
2. Packet assessment must not become executable scope unless the operator creates or approves a child handoff from it.
3. Canonical packet is the execution/audit source of truth after packet creation.
4. Executor brief is a deterministic render from canonical packet plus render profile; it is not an LLM reinterpretation.
5. Task atoms are the smallest executable/checkable units.
6. Implementation steps are readable ordered workflow phases that group task atoms.
7. Context tiers govern what may render to executor briefs.
8. Review-only and audit-only context must not render to executor briefs unless deliberately promoted through policy.
9. Model/provider routing is advisory and operator-controlled.
10. Provider/model capabilities, token accounting, and cost accounting are config/adapter concerns, not prompt-template constants.
11. Evaluation is a first-class subsystem, not an afterthought dashboard.
12. Telemetry is metadata-first by default.
13. Full prompts, provider payloads, source excerpts, diffs, and logs are retained only if redacted, configured, bounded, and policy-allowed.
14. Secrets, hidden chain-of-thought, unredacted logs, unbounded source dumps, and unrelated chat history are never retained.
15. Numeric prompt-quality thresholds are calibrated from Relay data later; initial hard-blocks should be deterministic failures only.
16. Human/operator approval gates remain available and must not be weakened by routing recommendations.
17. Future automation, if ever considered, must be a later explicit program decision, not a side effect of this roadmap.

---

## 7. Research loss prevention controls

Every child plan and pass handoff under this roadmap must preserve a clear distinction between:

- **planned now** — capability will be implemented by the current child plan;
- **prepared now** — schema/contract fields are created but runtime behavior belongs to a later child plan;
- **deferred but preserved** — explicitly kept in the roadmap and capability map;
- **open decision** — requires product/security/legal/operator decision;
- **explicitly rejected** — rejected with reason and evidence.

A future agent must not treat omission from one child plan as rejection from the program. Only explicit status in the Capability Map or a reviewed roadmap amendment may change program scope.

---

## 8. End-state architecture

```text
Research Integration Brief + Capability Map
  ↓
Program Roadmap / Plan of Plans
  ↓
Child Plans of Passes
  ↓
Selected pass handoffs
  ↓
Planner handoff
  ↓
packet assessment
  ↓
canonical packet
  ↓
validation report
  ↓
executor brief + brief validation report
  ↓
operator dispatch decision
  ↓
executor result
  ↓
audit packet
  ↓
accepted | revision_required | rejected
  ↓
telemetry + evaluation + calibration loop
```

---

## 9. Child Plan Creation Packet Guide

This section is part of the roadmap, not optional guidance. Its purpose is to prevent future planning agents from reinterpreting the original research or treating raw research reports as the controlling plan.

The five Deep Research reports are represented in the three anchor artifacts. They are **archival evidence**, not default planning attachments.

### 9.1 Default child-plan packet

Every child Plan of Passes created under this roadmap should receive these controlling artifacts:

1. `2026-06-25_relay-execution-intelligence.program-roadmap.v3.md`
2. `2026-06-25_relay-execution-intelligence.research-integration-brief.v3.md`
3. `2026-06-25_relay-execution-intelligence.capability-map.v3.md`
4. The current `Paintersrp/relay-contracts` Planner GitHub knowledge manifest fetched from source control.
5. Current Relay source-controlled contracts, schemas, policies, templates, and examples required for that child plan's task domain.

Do **not** attach the five Deep Research reports by default when creating a child Plan of Passes.

### 9.2 Source priority for child-plan agents

Use this priority order when creating child Plans of Passes or pass handoffs:

```text
1. Current fetched Relay source-controlled contracts, schemas, policies, templates, and examples
2. Program Roadmap / Plan of Plans v3
3. Research Integration Brief v3
4. Capability Map v3
5. Targeted excerpts or grep/rg findings from Deep Research reports, only when explicitly needed
6. Prior synthesis reports, only when explicitly needed
7. Prior packet assessment plan or historical handoffs, only as historical context
8. Prior chat context
```

The Deep Research reports must not be used to reopen decisions already preserved in the Roadmap, Brief, and Capability Map unless the user explicitly asks for a program amendment.

### 9.3 Deep Research archival evidence policy

The Deep Research reports may be consulted only as targeted archival evidence when at least one condition is true:

- an anchor artifact is ambiguous or internally inconsistent;
- a Capability Map row requires source verification;
- a reviewer asks for research provenance;
- a child plan requires targeted grep/rg over original wording;
- a program amendment is explicitly requested by the user.

When a child-plan agent consults a Deep Research report, the child plan must record:

```text
research_evidence_consulted:
  - report_or_file:
    query_or_section:
    reason_consulted:
    decision_affected:
```

If none are consulted, the child plan should state:

```text
research_evidence_consulted: none; represented by Roadmap v3, Research Integration Brief v3, and Capability Map v3
```

### 9.4 Child-plan-specific planning packet matrix

| Child plan | Required anchor artifacts | Research evidence status | Historical/supporting artifacts | Required Relay source fetch domains |
|---|---|---|---|---|
| PLAN-A | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; consult original reports only for targeted provenance checks or amendment work. | Prior synthesis reports only if checking anchor fidelity. | `plan_authoring_mode`, managed-plan contracts, artifact naming, lifecycle, security, approval gates |
| PLAN-B | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for prompt architecture, deterministic packetization, or telemetry wording. | Prior synthesis reports only if needed. | artifact model, planner-to-compiler contract, artifact naming, lifecycle, approval, security, schema versioning, relevant schemas |
| PLAN-C | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for assessment/routing/evaluation details. | Prior packet assessment v2 plan, prior synthesis. | planner handoff contract, plan/run submission contracts, orchestrator/context contracts, lifecycle, approval, security, relevant schemas |
| PLAN-D | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for handoff/context hygiene. | Prior synthesis only if needed. | planner handoff template/schema, planner-to-compiler contract, repo guidance policy if present, context broker contract |
| PLAN-E | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for packetization/source-map/atom details. | Prior synthesis only if needed. | canonical packet schema/contract, validation report schema, middleware failure codes, planner-to-compiler contract, lifecycle |
| PLAN-F | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for deterministic rendering/profile details. | Prior synthesis only if needed. | executor brief contract, canonical packet schema, validation report schema, renderer/brief-related policies |
| PLAN-G | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for provider profile fields, token accounting, and cost policy. | Prior synthesis only if needed. | runtime config conventions, executor/adapter surfaces, telemetry/security policies, UI/API contracts if relevant |
| PLAN-H | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for retention/redaction/provenance details. | Prior synthesis only if needed. | security redaction policy, lifecycle policy, artifact model, telemetry/export/runtime storage surfaces |
| PLAN-I | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for fixture/trace metric details. | Prior synthesis only if needed. | validation report schema, middleware failure codes, telemetry/provenance contracts, packet/brief schemas |
| PLAN-J | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference for operator decision/evaluation/routing display details. | Current UI screenshots/design notes if relevant. | current UI routes/components, API surfaces, approval-gate/lifecycle contracts, telemetry/routing contracts |
| PLAN-K | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; optional targeted reference only for documentation provenance or release examples. | Prior examples/handoffs. | all changed contracts/schemas/examples, artifact naming, release validation, docs surfaces |
| PLAN-L | Roadmap v3, Brief v3, Capability Map v3 | Represented in anchors; use accumulated run/eval/telemetry results first; consult original research only for targeted calibration questions. | Accumulated run/eval/telemetry results from prior plans. | evaluation, telemetry, routing, renderer, context, and operator UX source surfaces |

### 9.5 Required child-plan coverage section

Every child Plan of Passes should include this section before pass definitions:

```text
Capability coverage:
- Covered capability IDs:
- Capability IDs this child plan prepares but does not complete:
- Capability IDs intentionally deferred:
- Open decisions preserved:
- Explicitly rejected capability IDs:
- Research evidence consulted:
- Source-controlled files fetched:
```

A child plan that cannot identify covered capability IDs should be revised before implementation handoffs are generated.

### 9.6 Child-plan prompt template

Use this prompt shape when asking a future agent to create a child Plan of Passes:

```text
Create a schema-valid Relay Plan of Passes for [PLAN-X] from the attached Program Roadmap v3.

Treat the Program Roadmap v3, Research Integration Brief v3, and Capability Map v3 as the controlling planning artifacts. Do not reinterpret the program from the original Deep Research reports.

Do not attach or use the five Deep Research reports as default planning context. They are archival evidence represented by the three anchor artifacts. Consult them only for targeted provenance, ambiguity resolution, grep/rg verification, or a user-requested program amendment. If consulted, record the report, query/section, reason, and decision affected.

Before producing the plan, identify which Capability Map rows this child plan covers, prepares, defers, or leaves open.

Fetch the current Planner GitHub knowledge manifest from Paintersrp/relay-contracts and fetch every required source-controlled file for this plan's task domain. Treat fetched GitHub files as authoritative over uploaded files, prior chat, and memory.

Do not create implementation handoffs. Create only the child Plan of Passes JSON and readable Markdown companion.
```


## 10. Child plan overview

The program should be implemented through these child Plans of Passes.

| Child Plan | Name | Purpose | Primary tracks | Key capability coverage |
|---|---|---|---|---|
| PLAN-A | Research Preservation and Program Governance | Preserve research interpretation, vocabulary, coverage rules, and child-plan governance. | `program_governance` | CAP-A01–CAP-A05 |
| PLAN-B | Contract, Artifact, Lifecycle, and Approval Foundation | Update contracts, schemas, artifact taxonomy, lifecycle states, naming policy, and approval gates for the new program. | `contracts_schema`, `telemetry_security` | CAP-B01–CAP-B05, supporting CAP-H/I/J/K |
| PLAN-C | Packet Assessment System | Implement packet assessment as a separate advisory artifact with persistence, APIs/MCP, operator decisions, child-handoff recommendations, and outcome tracking. | `packet_assessment`, `operator_ux`, `evaluation_calibration` | CAP-C01–CAP-C06 |
| PLAN-D | Handoff Profile, Repo Guidance, and Context Classification | Harden handoff parseable zones, claim statuses, repo guidance artifacts, context classification, and prompt-context hygiene. | `handoff_packet`, `contracts_schema`, `evaluation_calibration` | CAP-D01–CAP-D05, CAP-F03, CAP-I03 |
| PLAN-E | Canonical Packet vNext and Compiler Validation | Add task atoms, implementation steps, context tiers, source maps, render policy, telemetry seeds, and compiler hardening. | `handoff_packet`, `contracts_schema` | CAP-E01–CAP-E07 |
| PLAN-F | Deterministic Renderer and Brief Validation | Implement render profiles, brief validation report, token budgets, stable hashes, context allowlists, and hard-block/warning gates. | `renderer_brief`, `evaluation_calibration` | CAP-F01–CAP-F06, CAP-I01 |
| PLAN-G | Model/Provider Profiles, Executor Tiers, Token and Cost Accounting | Add provider/model capability profile registry, executor tiers, token accounting, cost accounting, and advisory routing objects. | `routing_cost` | CAP-G01–CAP-G07 |
| PLAN-H | Telemetry, Provenance, Redaction, and Safe Export | Implement metadata-first telemetry, provenance, retention classes, redaction/blocking, and safe export/debug bundles. | `telemetry_security` | CAP-H01–CAP-H07 |
| PLAN-I | Evaluation Harness, Metrics, and Calibration | Implement fixtures, trace evals, prompt/context/execution/audit/routing/split metrics, ECPT, and calibration. | `evaluation_calibration` | CAP-I01–CAP-I09 |
| PLAN-J | Operator UX and Workflow Integration | Build operator surfaces for assessment review, prompt/brief quality, routing/cost, content-capture controls, overrides, dispatch, and audit support. | `operator_ux` | CAP-J01–CAP-J06 |
| PLAN-K | Migration, Compatibility, Documentation, and Release Hardening | Preserve existing pipeline behavior during rollout, document artifacts and workflows, validate examples, and harden release. | `migration_docs`, all tracks | CAP-K01–CAP-K05 |
| PLAN-L | Execution Intelligence Optimization Loop | Turn accumulated telemetry/evals into profile tuning, routing calibration, context optimization, warning promotion, and controlled prompt/render experiments. | `evaluation_calibration`, `routing_cost`, `renderer_brief`, `operator_ux` | CAP-L01–CAP-L08 plus cross-cutting CAP-C/G/I/J |


PLAN-L is intentionally later. It does not mean automatic dispatch. It means evidence-backed continuous improvement of recommendations and render/context policies.

---

## 10. Child plan details

### PLAN-A — Research Preservation and Program Governance

**Purpose:** Make the research interpretation durable before implementation planning starts.

**Scope:** Preserve the Research Integration Brief and Capability Map as accepted planning anchors; define capability coverage requirements; define roadmap amendment rules; define how child plans cite capability IDs; define how research recommendations can be marked planned, deferred-preserved, open, or explicitly rejected.

**Non-goals:** No runtime code changes. No packet/compiler/renderer work. No Relay plan submission unless separately reviewed and confirmed.

**Exit criteria:** Program governance docs exist; future child plans have required capability coverage sections; no research-backed capability can be silently dropped.

### PLAN-B — Contract, Artifact, Lifecycle, and Approval Foundation

**Purpose:** Update the source-controlled Relay contract surface so execution intelligence has explicit artifact classes, lifecycle placement, naming rules, and human gates before runtime work starts.

**Scope:** Add or update contracts for packet assessment, brief validation report, prompt quality scorecard, routing recommendation, provider/model profile, telemetry/provenance event, evaluation fixture/report, and program-anchor artifacts. Update naming, lifecycle, approval, security, and versioning policies.

**Non-goals:** No runtime persistence. No UI implementation. No packet/compiler implementation. No actual model/provider catalog.

**Exit criteria:** New artifact classes are named and lifecycle-placed; human/operator gates remain available; packet assessment is contractually separate and advisory; metadata-first telemetry and redaction posture are visible in contract/policy.

### PLAN-C — Packet Assessment System

**Purpose:** Implement packet assessment as a separate Planner-authored, advisory artifact that evaluates a Planner handoff and supports operator-controlled routing, splitting, review, and child-handoff decisions.

**Scope:** Define packet assessment schema; add persistence model; add API/MCP surfaces for submit/get/list/decision/register-child-handoff; link to project, handoff path/hash, optional plan/pass/run; record clarity, repo grounding, blast radius, semantic risk, observability, coupling, rollback safety, validation confidence, audit risk, tier hint, split recommendation, review requirement, and escalation conditions; add outcome telemetry links.

**Non-goals:** No automatic dispatch. No automatic child run creation. No automatic model selection. No assessment body rendered into executor brief. No LLM-generated assessment tool inside Relay unless separately planned.

**Exit criteria:** Assessments can be created, stored, retrieved, reviewed, and linked to source handoffs; operator decisions can be recorded; assessment recommendations are measurable later but remain non-executable unless promoted by human action.

### PLAN-D — Handoff Profile, Repo Guidance, and Context Classification

**Purpose:** Harden the Planner handoff and context surfaces so the compiler and renderer receive structured, bounded, source-backed inputs instead of prose-only instructions.

**Scope:** Define Relay Handoff Profile vNext; restrict machine-parsed zones; add epistemic/claim statuses; define stable repo guidance artifacts; define context classification and promotion rules; connect source snapshots/context packets/repo guidance to packet compilation.

**Non-goals:** No packet vNext implementation unless needed for contract wiring. No renderer implementation.

**Exit criteria:** Future handoffs can distinguish verified repo facts, Planner decisions, assumptions, blockers, review-only notes, and executable requirements; repo guidance can be referenced without reauthoring in every handoff.

### PLAN-E — Canonical Packet vNext and Compiler Validation

**Purpose:** Make the canonical packet the schema-backed execution contract needed for deterministic rendering, routing, validation, audit, and telemetry.

**Scope:** Add task atoms, implementation steps, context tiers, source maps, render policy, executor profile hint, prompt quality seed, telemetry seed, and strict validation of conflicts, required atom coverage, validation/acceptance coverage, and context leakage.

**Non-goals:** No operator UI. No provider/model registry. No automatic repair beyond explicit repair contracts.

**Exit criteria:** Packet vNext can represent executable units and readable sequencing; compiler validation can reject drift-prone, ambiguous, unsafe, or incomplete packets.

### PLAN-F — Deterministic Renderer and Brief Validation

**Purpose:** Render executor briefs deterministically from canonical packet plus render profile and validate them before dispatch approval.

**Scope:** Implement render profiles; implement brief validation report; enforce stable ordering, context allowlists, token budgets, required atom coverage, forbidden context exclusion, stop/DONE/BLOCKED criteria, and stable render hashes.

**Non-goals:** No automatic dispatch. No provider-specific payload capture by default. No LLM reinterpretation of packet content.

**Exit criteria:** Same packet plus same render profile produces the same brief hash; missing atom coverage or forbidden context leakage blocks rendering/dispatch approval.

### PLAN-G — Model/Provider Profiles, Executor Tiers, Token and Cost Accounting

**Purpose:** Add advisory routing infrastructure without hardcoding providers or models into prompts.

**Scope:** Define executor tiers; add provider/model capability profile registry; add token estimator modes; add cost profile schema; model provider quirks, privacy defaults, cache behavior, structured-output support, tool support, and benchmark metadata; emit advisory route recommendation objects.

**Non-goals:** No automatic model selection. No automatic dispatch. No provider payload retention by default.

**Exit criteria:** Relay can display tier/model candidates with capability, privacy, cost, and confidence notes; operator can override; recommended vs selected route can be recorded.

### PLAN-H — Telemetry, Provenance, Redaction, and Safe Export

**Purpose:** Implement safe lineage and outcome telemetry that supports evaluation without becoming a raw prompt/source archive.

**Scope:** Add telemetry event model; provenance entities/activities/agents; retention classes; redaction/blocking; never-retain enforcement; metadata-only defaults; opt-in redacted content capture; export/debug bundles.

**Non-goals:** No full prompt/provider body retention by default. No hidden reasoning storage. No unredacted logs/diffs/source dumps.

**Exit criteria:** Metadata-only telemetry supports cost/routing/eval analysis; content capture is off by default; unredactable secrets fail closed.

### PLAN-I — Evaluation Harness, Metrics, and Calibration

**Purpose:** Build the evaluation subsystem that measures artifact correctness, process quality, and outcome quality across the Relay pipeline.

**Scope:** Add fixture regression harness; trace-based real-run evaluation; root-cause taxonomy; prompt/context/execution/audit/routing/split metrics; ECPT as a warning/analysis metric; calibration data model.

**Non-goals:** No public benchmark as source of truth. No LLM judge hard-blocks in first implementation. No automatic routing threshold enforcement.

**Exit criteria:** Relay can distinguish handoff, packetization, rendering, prompt, context, executor/model, validation, and audit failures; renderer/profile changes can be regression-tested.

### PLAN-J — Operator UX and Workflow Integration

**Purpose:** Surface execution intelligence to the human operator at the right decision points without bypassing approval gates.

**Scope:** Add review surfaces for packet assessment, brief validation/prompt quality, context tiers, route/cost recommendation, provider privacy warnings, content-capture controls, manual override reason, dispatch approval, and audit support.

**Non-goals:** No auto-dispatch from recommendations. No UI-only reinterpretation of canonical packet semantics.

**Exit criteria:** Operator can review and act on recommendations while the UI clearly distinguishes advisory information from executable scope and explicit approval.

### PLAN-K — Migration, Compatibility, Documentation, and Release Hardening

**Purpose:** Preserve existing Relay behavior while rolling out the execution-intelligence architecture.

**Scope:** Add compatibility strategy; migration docs; validated examples for every new artifact; release fixtures; operator/Planner/Auditor guides; no-auto-dispatch guarantees; redacted sample artifacts.

**Non-goals:** No new feature semantics beyond documenting and hardening implemented behavior.

**Exit criteria:** Existing artifacts remain understandable; vNext examples validate; release checks cover schema, renderer, telemetry, and redaction fixtures.

### PLAN-L — Execution Intelligence Optimization Loop

**Purpose:** Use accumulated telemetry/evaluation outcomes to tune future recommendations, context policies, renderer profiles, and routing guidance.

**Scope:** Track cost-quality frontier; calibrate route recommendations; evaluate tier effectiveness; replay historical packets through alternate render profiles; tune context inclusion policy; evaluate repo guidance usefulness; measure split recommendation outcomes; evaluate audit disagreements and validation weakness; define reviewed promotion path from warning-only signals to hard-blocks.

**Non-goals:** No automatic executor dispatch by default. No self-modifying policy. No threshold promotion without evidence and human/product approval.

**Exit criteria:** Relay can justify changes to render profiles, context policies, route hints, and quality gates with evidence.


---

## 11. Dependency model

### 11.1 Safe default order

```text
PLAN-A Research Preservation and Program Governance
  ↓
PLAN-B Contract, Artifact, Lifecycle, and Approval Foundation
  ↓
PLAN-C Packet Assessment System
  ↓
PLAN-D Handoff Profile, Repo Guidance, and Context Classification
  ↓
PLAN-E Canonical Packet vNext and Compiler Validation
  ↓
PLAN-F Deterministic Renderer and Brief Validation
  ↓
PLAN-G Model/Provider Profiles, Executor Tiers, Token and Cost Accounting
  ↓
PLAN-H Telemetry, Provenance, Redaction, and Safe Export
  ↓
PLAN-I Evaluation Harness, Metrics, and Calibration
  ↓
PLAN-J Operator UX and Workflow Integration
  ↓
PLAN-K Migration, Compatibility, Documentation, and Release Hardening
  ↓
PLAN-L Execution Intelligence Optimization Loop
```

### 11.2 Allowed overlap after PLAN-B

Some tracks can overlap after the common contract vocabulary is stable:

```text
PLAN-C packet assessment
PLAN-D handoff/context profile
PLAN-G model/provider profile registry
PLAN-H telemetry contract/runtime foundation
```

Overlap must not bypass capability coverage requirements, source-control fetch requirements, or human approval gates.

---

## 12. Capability coverage checkpoint

A future child Plan of Passes is incomplete if it does not list:

```text
capability_ids_advanced:
capability_ids_prepared_but_not_completed:
capability_ids_deferred:
capability_ids_explicitly_out_of_scope:
validation_evidence_expected:
research_constraints_preserved:
source_control_files_fetched:
planning_packet_used:
```

Every capability from the Capability Map must be in one of these statuses across the roadmap:

```text
planned
foundation
prepared
implemented
deferred_preserved
open_decision
explicitly_rejected
```

No capability may be silently omitted.

---

## 13. Open decisions to carry forward

These are not blockers for roadmap creation, but they must be preserved until decided.

| Decision | Owner / future plan | Notes |
|---|---|---|
| Exact default retention durations | PLAN-H | Must remain deployment-configurable. |
| Whether redacted full-brief capture is available in first release | PLAN-H / PLAN-J | Default remains off. |
| Initial provider/model catalog | PLAN-G | Config-driven; should be current at implementation time. |
| Whether provider request/response capture is admin-only | PLAN-H | Default remains off. |
| Exact operator UI placement | PLAN-J | Should align with current Relay workflow screens. |
| Whether prompt-quality warnings can become hard blockers | PLAN-L | Only after calibration and review. |
| Whether future policy-gated automation is ever allowed | Future program decision | Not part of current no-auto-dispatch roadmap. |
| Whether program anchors live in `relay-contracts`, `relay`, or both | PLAN-A / PLAN-B | Should be decided before generating long-lived child plans. |
| Whether child plans should be submitted individually or as coordinated bundles | PLAN-A | First three child plans may be coordinated but should remain distinct. |
| Exact semantics for child-handoff registration from packet assessment | PLAN-C | Must not create runs or dispatch executors automatically. |

---

## 14. First three child plans for near-term work

The first three child plans to develop after this roadmap are:

1. **PLAN-A — Research Preservation and Program Governance**
2. **PLAN-B — Contract, Artifact, Lifecycle, and Approval Foundation**
3. **PLAN-C — Packet Assessment System**

These three create the durable bridge from research to implementation and ensure the unimplemented packet assessment work is folded into the larger destination.

They should be created as normal schema-valid Planner Pass Plans later, one at a time or as a coordinated bundle, after this roadmap is reviewed.

---

## 15. What later pass handoffs must not do

Later implementation handoffs under this roadmap must not:

- redefine the endgame;
- drop capability IDs without explicit status;
- treat a child plan as the whole program;
- treat packet assessment as executor scope;
- put model/provider choices inside prompt templates;
- bypass human approval gates;
- store raw prompts/provider bodies by default;
- invent hard numeric quality gates before calibration;
- compress evaluation into a post-launch dashboard;
- use prior chat instead of fetched source-controlled contracts for current-source claims.

---

## 16. Roadmap completion definition

This roadmap is complete when:

1. All child plans A–L have been converted into reviewed child Plans of Passes or explicitly superseded by a reviewed program amendment.
2. Every Capability Map row is implemented, prepared, deferred-preserved, explicitly rejected, or marked as an open decision.
3. Packet assessment, canonical packet vNext, deterministic rendering, model/provider routing, telemetry/provenance, evaluation/calibration, operator UX, and migration/docs are all represented in source-controlled contracts and runtime behavior.
4. Relay can use audit-backed telemetry to improve recommendations and prompt/context/render policies without weakening human approval gates.
