# Relay Execution Intelligence — Endgame Capability Map v3

Artifact status: planning anchor / capability traceability map
Revision: v3 research-evidence policy refinement  
Created: 2026-06-25  
Companion artifacts:

- `2026-06-25_relay-execution-intelligence.research-integration-brief.v3.md`
- `2026-06-25_relay-execution-intelligence.program-roadmap.v3.md`

## 1. Purpose

This capability map converts the research program into traceable Relay target capabilities. It is intended to prevent research recommendations from disappearing during implementation planning.

Each future Program Roadmap section, child Plan of Passes, and Planner handoff should reference these capability IDs where relevant.


### 1.1 Research evidence representation policy

The five Deep Research reports are represented by the Research Integration Brief v3, Program Roadmap v3, and this Capability Map v3. Future child Plans of Passes should not attach the raw reports by default. The raw reports remain archival evidence for targeted verification, provenance checks, grep/rg, ambiguity resolution, or explicit program amendments.

Capability rows in this map are the durable planning units. Future agents should preserve, implement, defer, or amend capability IDs rather than reinterpreting the original reports from scratch.

## 2. Status vocabulary

| Status | Meaning |
|---|---|
| `planned` | Capability should be implemented in the program roadmap. |
| `foundation` | Capability is an enabling primitive for later capabilities. |
| `deferred_preserved` | Capability is intentionally not first-wave but remains in the long-term roadmap. |
| `open_decision` | Product/security/legal/operator decision required. |
| `explicitly_rejected` | Capability is intentionally not part of Relay’s direction. |

## 3. Track vocabulary

| Track | Meaning |
|---|---|
| `program_governance` | Research preservation, roadmap, plan-of-plans, traceability. |
| `contracts_schema` | Source-controlled contracts, policies, schemas, artifact taxonomy. |
| `packet_assessment` | Advisory packet assessment artifacts and decisions. |
| `handoff_packet` | Planner handoff profile, canonical packet, compiler validation. |
| `renderer_brief` | Deterministic rendering and brief validation. |
| `routing_cost` | Executor tiers, provider/model profiles, token/cost accounting. |
| `telemetry_security` | Provenance, retention, redaction, safe exports. |
| `evaluation_calibration` | Fixtures, trace evals, metrics, calibration. |
| `operator_ux` | Operator-facing review, routing, prompt-quality, override screens. |
| `migration_docs` | Compatibility, examples, docs, release hardening. |

---

# 4. Capability matrix

## A. Program governance and research preservation

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-A01 | All reports + synthesis | Research must not be compressed into partial v1 work and then treated as complete. | Research Integration Brief becomes required planning context for the program pivot. | No durable research-preservation artifact exists. | `program_governance` | Brief exists and later roadmap cites it. | `foundation` |
| CAP-A02 | All reports + synthesis | Full endgame must be explicit before implementation starts. | Program Roadmap / Plan of Plans covering the whole transition from current Relay to execution intelligence. | Existing draft plan undercaptured long-term endgame. | `program_governance` | Program Roadmap includes all capability IDs or explicit deferrals. | `planned` |
| CAP-A03 | All reports + synthesis | Every recommendation should map to planned, deferred, rejected, or open. | Capability coverage ledger. | No traceability layer from research to plans. | `program_governance` | Every capability has status and child plan assignment. | `foundation` |
| CAP-A04 | Planner workflow constraints | Implementation handoffs should be bounded but plans must preserve full destination. | Parent program + child Plans of Passes model. | Normal plan formula is too small for the pivot. | `program_governance` | Child plans cite parent capability IDs. | `planned` |
| CAP-A05 | Synthesis | Future agents should not re-litigate accepted vocabulary. | Accepted vocabulary list: packet assessment, task atom, implementation step, context tier, executor profile, provider profile, render profile, ECPT. | Vocabulary scattered across reports/chats. | `program_governance` | Terms defined in contracts/docs. | `planned` |
| CAP-A06 | Research evidence policy refinement | Raw research reports should not become default planning context once accepted anchors exist. | Default child-plan packets use Roadmap v3, Brief v3, and Capability Map v3; Deep Research reports are archival targeted evidence only. | v2 roadmap still encouraged too much raw-report attachment. | `program_governance` | Child-plan packet guide states reports are non-default and child plans record targeted evidence use only when consulted. | `foundation` |

## B. Artifact model, lifecycle, and approval gates

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-B01 | Deterministic packetization research | Use the format that fits the artifact consumer; canonical packet should be strict execution contract. | Update artifact taxonomy to include packet assessment, brief validation report, routing recommendation, telemetry event, evaluation fixture/run artifacts. | Current artifact model lacks these new artifact classes. | `contracts_schema` | Contract and naming policy include new artifacts. | `planned` |
| CAP-B02 | Telemetry research | Durable lineage must capture entities, activities, and agents. | Artifact lineage/provenance model covering handoff → assessment → packet → brief → execution → audit. | Current artifact chain exists but lacks full provenance schema. | `telemetry_security` | Provenance events generated for all lifecycle transitions. | `planned` |
| CAP-B03 | Current policies + synthesis | Human approval gates preserve operator control. | Approval-gate updates for assessment review, brief quality review, route selection, redaction/export decisions. | Existing gates do not mention assessment/routing/prompt-quality surfaces. | `contracts_schema` | Gate policy updated; UI enforces manual dispatch. | `planned` |
| CAP-B04 | Artifact naming policy + synthesis | New artifacts must be durable and repo-relative. | Naming policy entries for packet assessment, brief validation report, prompt quality scorecard, model profile export, telemetry event, eval report. | No suffixes for new artifact types. | `contracts_schema` | Naming policy examples validate. | `planned` |
| CAP-B05 | Plan submission contract | Plans create plan/pass records only; runs are separate. | Program Roadmap and child plans preserve plan/run separation. | Risk of conflating roadmap with execution. | `program_governance` | Child plan submission docs state no run creation. | `planned` |

## C. Packet assessment system

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-C01 | Prompt architecture synthesis | Packet assessment should be separate and Planner-authored. | Packet assessment artifact schema. | Prior packet assessment plan not implemented. | `packet_assessment` | Schema validates independent assessment JSON. | `planned` |
| CAP-C02 | Packet assessment synthesis | Assessment is advisory and must not become executable scope. | Renderer/compiler exclusion rules for assessment body. | No system-level enforcement yet. | `packet_assessment` | Brief validation fails if assessment advice leaks into executable scope. | `planned` |
| CAP-C03 | Evaluation research | Assess clarity, repo grounding, blast radius, semantic risk, observability, coupling, rollback safety. | Dimension ratings in packet assessment. | No assessment dimensions persisted. | `packet_assessment` | Assessment can be submitted and listed with dimensions. | `planned` |
| CAP-C04 | Routing research | Assessment should produce executor tier hints and review requirements. | `executor_tier_hint`, `review_required`, `escalation_conditions`. | No advisory routing object. | `packet_assessment` | UI displays recommendation separate from dispatch. | `planned` |
| CAP-C05 | Prompt architecture synthesis | Split recommendations should create child handoff recommendations, not hidden child execution. | Child handoff recommendation model and operator decision record. | No child-handoff recommendation lifecycle. | `packet_assessment` | Operator can register child handoff recommendation without creating a run. | `planned` |
| CAP-C06 | Telemetry/eval research | Assessment usefulness should be measured. | Assessment recommendation outcome telemetry. | No assessment follow-through tracking. | `evaluation_calibration` | Track recommendation followed/overridden and outcome. | `planned` |

## D. Planner handoff profile and repo guidance

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-D01 | Prompt architecture research | Handoffs should be contract-dense, not prose-only. | Relay Handoff Profile vNext with structured machine-parsed zones. | Current handoff structure exists, but machine-zone restrictions need hardening. | `handoff_packet` | Handoff profile contract defines allowed machine zones. | `planned` |
| CAP-D02 | Deterministic packetization research | Avoid freeform parsing where structured fields are possible. | Fenced JSON or schema-backed blocks for machine-critical handoff data. | Current handoff may rely too much on Markdown prose. | `handoff_packet` | Compiler rejects missing structured fields. | `planned` |
| CAP-D03 | Prompt architecture research | Distinguish verified facts, decisions, assumptions, blockers. | Claim status taxonomy: verified_repo_fact, planner_decision, allowed_assumption, blocking_unknown, advisory_note. | Claim statuses not normalized. | `handoff_packet` | Compiler validates/blocking unknown handling. | `planned` |
| CAP-D04 | Prompt/context research | Stable repo guidance should not be reauthored in every handoff. | Repo guidance artifact/profile. | Relay has ingredients but no normalized repo guidance artifact. | `contracts_schema` | Repo guidance can be referenced by handoffs/packets/render policy. | `planned` |
| CAP-D05 | Evaluation research | Ambiguity burden predicts execution issues. | Handoff ambiguity score/warnings. | No structured ambiguity metric. | `evaluation_calibration` | Prompt quality report includes ambiguity burden. | `planned` |

## E. Canonical packet and compiler vNext

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-E01 | Deterministic packetization research | Canonical packet should be schema-backed JSON with stable IDs. | Canonical packet vNext schema. | Current packet schema lacks vNext fields. | `contracts_schema` | vNext packet schema validates. | `planned` |
| CAP-E02 | Prompt architecture synthesis | Task atoms are smallest executable/checkable units. | `task_atoms[]` in canonical packet. | No task atom model. | `handoff_packet` | Every required atom has operation, files, validation, acceptance. | `planned` |
| CAP-E03 | Synthesis | Implementation steps group task atoms for readable sequencing. | `implementation_steps[]` referencing task atom IDs. | No formal step/atom relationship. | `handoff_packet` | Every required atom referenced by at least one step. | `planned` |
| CAP-E04 | Context research | Context tiers govern render inclusion. | `context_items[]` with tier and inclusion policy. | Context tiers not encoded in packet. | `handoff_packet` | Brief validation detects forbidden tier leakage. | `planned` |
| CAP-E05 | Deterministic packetization research | Source maps make compilation auditable. | Source map from packet fields to handoff sections/source evidence. | No full source-map model. | `handoff_packet` | Packet validation includes source-map coverage. | `planned` |
| CAP-E06 | Evaluation research | Failure attribution needs boundary visibility. | Packet validation failure codes distinguishing handoff vs compiler vs policy failure. | Failure modes not normalized. | `evaluation_calibration` | Validation report emits stable failure codes. | `planned` |
| CAP-E07 | Migration needs | Existing packets/runs must remain usable during rollout. | Packet compatibility/migration profile. | vNext transition could break old flows. | `migration_docs` | Legacy and vNext packet fixtures both pass. | `planned` |

## F. Deterministic renderer and brief validation

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-F01 | Deterministic rendering research | Executor brief should be pure deterministic render. | Renderer middleware with stable profile templates. | Renderer profile system not implemented. | `renderer_brief` | Same packet/profile yields same brief hash. | `planned` |
| CAP-F02 | Model routing research | Briefs should vary by executor tier. | Render profiles: cheap, mid, strong, reviewer. | No tier-specific render profiles. | `renderer_brief` | Fixtures compare profile output. | `planned` |
| CAP-F03 | Prompt architecture research | Brief should include only executable instructions and required context. | Render allowlist and context-tier filter. | No explicit allowlist enforcement. | `renderer_brief` | Brief validation blocks review/audit context leakage. | `planned` |
| CAP-F04 | Evaluation research | Required task atom coverage must be checked. | Brief validation report. | No render-time coverage report. | `renderer_brief` | Required atom coverage = 100% or hard-block. | `planned` |
| CAP-F05 | Telemetry research | Render identity supports auditability. | Render evidence: packet hash, profile ID, template version, brief hash, token estimate. | Render evidence not durable. | `telemetry_security` | Render evidence event stored. | `planned` |
| CAP-F06 | Prompt quality research | Prompt quality should be visible before dispatch. | Prompt quality scorecard attached to brief validation. | No pre-dispatch scorecard. | `renderer_brief` | UI displays hard blocks/warnings/ECPT. | `planned` |

## G. Model/provider routing and cost profiles

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-G01 | Model-provider research | Executor tiers should be abstract capability classes. | Executor tier model: cheap, mid, strong, reviewer. | Existing executor targeting is too concrete. | `routing_cost` | Config resolves tier to provider/model candidates. | `planned` |
| CAP-G02 | Model-provider research | Provider/model capability differs materially. | Provider/model capability profile schema. | No provider profile registry. | `routing_cost` | Profiles include context, tools, structured output, privacy, token/cost behavior. | `planned` |
| CAP-G03 | Model-provider research | Token accounting is provider-specific. | Token accounting adapter interface. | No unified estimate/reconcile model. | `routing_cost` | Estimated vs actual usage stored per run. | `planned` |
| CAP-G04 | Model-provider research | Cost must be config-driven and current. | Cost profile schema with effective dates and provider-specific billing fields. | No cost ledger. | `routing_cost` | Cost estimate and actual reconcile for runs. | `planned` |
| CAP-G05 | Routing research | Routing should compare cost-quality tradeoffs but remain operator-controlled. | Advisory routing recommendation object. | No route recommendation object. | `routing_cost` | Recommendation shown before dispatch, not dispatched automatically. | `planned` |
| CAP-G06 | Telemetry/evaluation research | Routing quality requires outcome measurement. | Recommendation-vs-selection-vs-outcome telemetry. | No calibration loop. | `evaluation_calibration` | Dashboards show tier effectiveness and override outcomes. | `planned` |
| CAP-G07 | Privacy research | Provider privacy/retention defaults differ. | Provider privacy flags and policy warnings in route selection. | Provider risk not surfaced in routing. | `routing_cost` | UI warns on provider privacy/capability constraints. | `planned` |

## H. Telemetry, provenance, redaction, and exports

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-H01 | Telemetry research | Retain durable lineage and operational metadata by default. | Metadata-first telemetry event model. | Telemetry classes not normalized. | `telemetry_security` | Events persist safe metadata across pipeline. | `planned` |
| CAP-H02 | Telemetry research | Treat content-bearing telemetry as sensitive and exceptional. | Content capture policy: opt-in, redacted, bounded, short-lived. | Raw content retention policy not implemented. | `telemetry_security` | Full prompt/provider body absent by default. | `planned` |
| CAP-H03 | Telemetry/security research | Never retain secrets or hidden reasoning. | Never-retain enforcement and secret block events. | Redaction policy exists; runtime enforcement needs implementation. | `telemetry_security` | Secret fixture blocks content persistence. | `planned` |
| CAP-H04 | Provenance research | Artifacts should track entities, activities, agents. | Provenance graph model. | Existing artifact chain lacks full provenance graph. | `telemetry_security` | Run export includes lineage graph. | `planned` |
| CAP-H05 | Evaluation research | Telemetry should support prompt and routing eval without unsafe content. | Evaluation-safe telemetry summaries. | No safe eval telemetry model. | `evaluation_calibration` | ECPT/routing metrics calculated without raw prompt retention. | `planned` |
| CAP-H06 | Security/research | Exports must not become leak vectors. | Redacted audit/debug export bundles. | No export policy/runtime. | `telemetry_security` | Export fixtures exclude never-retain data. | `planned` |
| CAP-H07 | Legal/product decision | Retention duration depends on deployment and policy. | Configurable retention schedule by telemetry class. | Defaults undecided. | `telemetry_security` | Product/security selects defaults. | `open_decision` |

## I. Evaluation harness and calibration

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-I01 | Evaluation research | Evaluation is first-class, not post-launch dashboard. | Evaluation subsystem in architecture and roadmap. | Evaluation currently conceptual. | `evaluation_calibration` | Eval service/fixtures included before calibration dashboards. | `planned` |
| CAP-I02 | Evaluation research | Test artifact transformations separately from outcomes. | Fixture harness for handoff → packet → brief. | No fixture regression system. | `evaluation_calibration` | Golden fixtures validate deterministic output. | `planned` |
| CAP-I03 | Evaluation research | Real-run traces should attribute root cause. | Trace-based failure attribution. | No root-cause taxonomy/event fields. | `evaluation_calibration` | Failed runs label earliest observable boundary. | `planned` |
| CAP-I04 | Prompt research | Measure prompt quality: completeness, ambiguity, grounding, validation specificity, structure. | Prompt quality metrics. | No scorecard. | `evaluation_calibration` | Scorecard produced from packet/brief validation. | `planned` |
| CAP-I05 | Context research | Measure precision, recall, utilization gap, leakage, optional-context yield. | Context efficiency metrics. | No context efficiency eval. | `evaluation_calibration` | Dashboards show context metrics by task family/profile. | `planned` |
| CAP-I06 | Routing research | Evaluate routers against cost-quality frontier. | Routing quality metrics and oracle regret calculation. | No cost-quality evaluation. | `evaluation_calibration` | Recommendation quality report by tier/model/profile. | `planned` |
| CAP-I07 | Split-task research | Splitting should be evaluated empirically. | Split recommendation outcome metrics. | No split benefit/cost tracking. | `evaluation_calibration` | Track split benefit delta, completion, conflicts, latency. | `planned` |
| CAP-I08 | Prompt profile research | Render profiles should be experimentally comparable. | Prompt/render profile experiment framework. | No replay/A-B infrastructure. | `evaluation_calibration` | Historical packets replay through alternate profiles. | `planned` |
| CAP-I09 | Synthesis | ECPT is an internal comparative metric, not v1 blocker. | ECPT metric with calibration status. | No ECPT model. | `evaluation_calibration` | ECPT appears as warning/analysis, not hard gate. | `planned` |

## J. Operator UX and workflow

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-J01 | Packet assessment synthesis | Operator must review assessment without it becoming scope. | Assessment review surface. | No assessment UI. | `operator_ux` | UI separates advisory assessment from execution payload. | `planned` |
| CAP-J02 | Prompt quality research | Operator needs brief quality before dispatch. | Prompt quality / brief validation panel. | No scorecard UI. | `operator_ux` | Hard blocks/warnings displayed before dispatch. | `planned` |
| CAP-J03 | Routing research | Operator-controlled routing with explainable recommendation. | Routing recommendation and manual model/tier selection panel. | No routing decision UI. | `operator_ux` | Operator selects route and can override with reason. | `planned` |
| CAP-J04 | Cost research | Cost/token estimates should be visible before dispatch. | Cost/token estimate panel. | No cost preview. | `operator_ux` | Estimate shown and reconciled post-run. | `planned` |
| CAP-J05 | Telemetry/security research | Content capture/export should be explicit and controlled. | Operator/admin controls for redacted content capture and export. | No capture/export controls. | `operator_ux` | Content capture off by default; override audited. | `planned` |
| CAP-J06 | Human gate policy | Dispatch remains manually gated. | UI cannot dispatch from recommendation alone. | Must be preserved in new surfaces. | `operator_ux` | Dispatch button requires explicit approval. | `planned` |

## K. Migration, compatibility, docs, and release hardening

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-K01 | Source-controlled schema policy | Existing artifacts should remain understandable during vNext transition. | Migration notes and compatibility strategy. | Current plan does not define migration. | `migration_docs` | Legacy fixtures and vNext fixtures coexist. | `planned` |
| CAP-K02 | All reports | Future agents need examples. | Valid examples for assessment, packet vNext, brief validation, provider profile, telemetry event, eval fixture. | Examples absent. | `migration_docs` | Examples validate in CI. | `planned` |
| CAP-K03 | Program governance | Docs must preserve no-auto-dispatch and advisory-routing constraints. | Operator and Planner/Auditor guidance docs. | Risk of future semantic drift. | `migration_docs` | Docs state no automatic dispatch and assessment boundary. | `planned` |
| CAP-K04 | Evaluation research | Release should include regression fixtures. | Release hardening gate with fixture/eval suite. | Eval gates not integrated with release. | `migration_docs` | CI/release scripts include fixture validation. | `planned` |
| CAP-K05 | Telemetry research | Sensitive artifact examples must be redacted. | Redacted sample artifacts and export examples. | Existing examples may not cover new telemetry classes. | `migration_docs` | Security fixtures verify redaction. | `planned` |

---


## L. Execution intelligence optimization loop

| ID | Research source | Research finding / accepted synthesis | Target Relay capability | Current gap | Track | Evidence / validation | Status |
|---|---|---|---|---|---|---|---|
| CAP-L01 | Evaluation + model routing research | Routing quality should be calibrated against observed cost, latency, validation, audit, and operator override outcomes. | Route recommendation calibration loop. | Routing is planned but not yet evidence-calibrated. | `evaluation_calibration`, `routing_cost` | Recommendation confidence improves from observed outcomes, not static benchmark assumptions. | `planned` |
| CAP-L02 | Evaluation research | Prompt/render profile changes should be compared through fixture and trace replay, not intuition. | Prompt/render profile experiment framework. | No replay/A-B infrastructure. | `evaluation_calibration`, `renderer_brief` | Historical packets can be replayed through alternate render profiles with comparable metrics. | `planned` |
| CAP-L03 | Context research | Context inclusion should be tuned by precision, recall, utilization, and leakage, not by raw context size alone. | Context inclusion policy optimization. | Context tiering is planned, but tuning loop is not implemented. | `evaluation_calibration`, `handoff_packet` | Context policy reports optional-context yield and leakage by task family/profile. | `planned` |
| CAP-L04 | Repo guidance synthesis | Stable repo guidance should be measured for usefulness and cost impact. | Repo guidance usefulness analysis. | Repo guidance artifact is planned but not outcome-measured. | `evaluation_calibration`, `handoff_packet` | Runs can compare guidance inclusion against validation/audit/cost outcomes. | `planned` |
| CAP-L05 | Packet assessment synthesis | Split recommendations should be empirically evaluated and not assumed beneficial. | Split recommendation calibration. | Packet assessment can recommend splits, but outcome feedback loop is not yet defined. | `packet_assessment`, `evaluation_calibration` | Track split benefit delta, merge/dependency friction, latency, and accepted outcome. | `planned` |
| CAP-L06 | Evaluation research | Weak validation and audit disagreement reveal hidden defects. | Validation/audit disagreement analysis. | Validation and audit outcomes exist, but disagreement trend analysis is not implemented. | `evaluation_calibration` | Dashboard/report shows audit disagreement, suspicious pass rate, and validation weakness patterns. | `planned` |
| CAP-L07 | Prompt quality research | Warning-only signals may become hard gates only after local calibration. | Reviewed warning-to-hard-block promotion process. | No threshold promotion governance exists. | `program_governance`, `evaluation_calibration` | Promotion requires evidence, review, and program-policy update. | `planned` |
| CAP-L08 | Program synthesis | Execution intelligence means policy-governed recommendation improvement, not automatic dispatch. | Optimization loop preserving human approval gates. | Need explicit long-term guardrail. | `operator_ux`, `program_governance` | Optimization outputs recommendations/policy proposals, not autonomous dispatch. | `planned` |

# 5. Capability coverage summary

| Track | Capability IDs | Count |
|---|---|---:|
| `program_governance` | CAP-A01–CAP-A05, CAP-B05, CAP-K03 | 7 |
| `contracts_schema` | CAP-B01, CAP-B03, CAP-B04, CAP-D04, CAP-E01 | 5 |
| `packet_assessment` | CAP-C01–CAP-C06 | 6 |
| `handoff_packet` | CAP-D01–CAP-D03, CAP-E02–CAP-E07 | 9 |
| `renderer_brief` | CAP-F01–CAP-F06 | 6 |
| `routing_cost` | CAP-G01–CAP-G07 | 7 |
| `telemetry_security` | CAP-B02, CAP-F05, CAP-H01–CAP-H07, CAP-J05, CAP-K05 | 11 |
| `evaluation_calibration` | CAP-C06, CAP-D05, CAP-E06, CAP-H05, CAP-I01–CAP-I09, CAP-K04 | 14 |
| `operator_ux` | CAP-J01–CAP-J06 | 6 |
| `migration_docs` | CAP-E07, CAP-K01–CAP-K05 | 6 |
| `execution_intelligence_optimization` | CAP-L01–CAP-L08 | 8 |

Total first-class capability rows: 76

## 6. Program Roadmap requirements derived from this map

The Program Roadmap should group these capabilities into child plans. A child plan may cover multiple tracks when implementation naturally validates together, but no child plan should silently omit a planned capability.

Minimum child plans expected:

1. Research preservation and contract baseline.
2. Packet assessment system.
3. Handoff profile, repo guidance, canonical packet, and compiler vNext.
4. Renderer profiles and brief validation.
5. Model/provider profiles, token/cost accounting, and advisory routing.
6. Telemetry, provenance, redaction, and export policy.
7. Evaluation harness, trace evals, dashboards, and calibration loop.
8. Execution intelligence optimization loop for routing, render-profile, context-policy, validation/audit, and warning-gate calibration.
9. Operator UX for assessment, prompt quality, routing, cost, and overrides.
10. Migration, examples, documentation, and release hardening.

## 7. Planning rule for future child plans

Every future child Plan of Passes should include a section:

```text
Capability coverage:
- Covered capability IDs:
- Deferred capability IDs:
- Open decisions:
- Explicitly rejected capability IDs:
```

A child plan that cannot identify covered capability IDs should be treated as under-grounded and should be revised before implementation handoffs are generated.


## 8. Pre-commit review notes

The v2 review found that the original capability map covered the major research themes but underrepresented the long-term optimization loop as a first-class capability group. Section L fixes that by making routing calibration, prompt/render experiments, context optimization, repo-guidance analysis, split-calibration, validation/audit disagreement analysis, warning promotion, and no-auto-dispatch optimization guardrails explicit.

Future child plans must not treat PLAN-L as optional cleanup. It is the mechanism that turns telemetry and evaluation into the long-term execution-intelligence endgame.
