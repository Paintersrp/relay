# Relay Execution Intelligence — Research Evidence Policy Review v4

Artifact status: planning review / not a Relay plan / not an implementation handoff  
Created: 2026-06-25  
Review target: v3 anchor artifacts plus external planning-packet audit  
Output revision: v4 anchor artifacts

## 0. Version status

Controlling anchor set after this revision:

- `2026-06-25_relay-execution-intelligence.program-roadmap.v4.md`
- `2026-06-25_relay-execution-intelligence.research-integration-brief.v4.md`
- `2026-06-25_relay-execution-intelligence.capability-map.v4.md`

The prior v3 anchor set was reviewed as the de facto controlling set after a prompt referenced v4 before v4 existed. The external review returned `READY_WITH_MINOR_REVISIONS`, conditional on resolving the v3/v4 discrepancy and applying traceability corrections. v4 applies those corrections and supersedes v3.

## 1. Review conclusion

Commit confidence is high for using the three v4 anchor artifacts as the default context for future child Plans of Passes.

The research direction remains represented in the anchors. The five Deep Research reports remain archival evidence only, not default planning context. The external review found no missing major research theme; it found consistency, traceability, and terminology issues that could confuse future agents if left unresolved.

## 2. Policy decision retained

The five Deep Research reports are not default child-plan attachments.

They may be consulted only for targeted verification, provenance checks, grep/rg, ambiguity resolution, or an explicit user-requested program amendment. When consulted, child plans must record the report, query or section, reason, and decision affected.

## 3. Files updated in v4

- `2026-06-25_relay-execution-intelligence.program-roadmap.v4.md`
- `2026-06-25_relay-execution-intelligence.research-integration-brief.v4.md`
- `2026-06-25_relay-execution-intelligence.capability-map.v4.md`
- `2026-06-25_relay-execution-intelligence.v4-change-log.md`

## 4. Changes made from v3 to v4

| Artifact | Change | Reason |
|---|---|---|
| Program Roadmap | Updated v3 references to v4, removed dangling `precommit-gap-review` companion reference, fixed duplicate default-packet entry, renamed duplicate `## 10` child-plan-details heading to `## 10a`, and added canonical PLAN-A–PLAN-L note. | Prevent version drift, broken exact-file references, and competing child-plan counts. |
| Program Roadmap | Assigned PLAN-A coverage to CAP-A01–CAP-A06. | CAP-A06 is the research-evidence-policy capability and must not be silently omitted. |
| Program Roadmap | Expanded PLAN-F, PLAN-G, PLAN-I, and PLAN-L descriptions. | Preserve deterministic-vs-calibrated gates, provider cost dimensions, reasoning/exploration metrics, and optimization-loop scope. |
| Program Roadmap | Added open decisions for prompt-shape experiments and reasoning/exploration-alignment metrics. | Preserve research open questions for empirical resolution rather than accidental loss. |
| Research Integration Brief | Updated source hierarchy to v4 and clarified that PLAN-A–PLAN-L is canonical while child-plan families are explanatory. | Prevent future agents from treating illustrative groupings as competing roadmaps. |
| Research Integration Brief | Clarified deterministic hard-blocks versus calibrated numeric thresholds and added reasoning/exploration-alignment measurement. | Preserve evaluation research nuance. |
| Capability Map | Added `execution_intelligence_optimization` track. | Remove undefined track use and make the long-term loop first-class. |
| Capability Map | Fixed coverage summary to include CAP-A06 and updated total unique capability rows to 79. | Restore traceability integrity. |
| Capability Map | Added CAP-F07 and CAP-I10. | Preserve calibrated-threshold governance and reasoning/exploration-alignment metrics. |
| Capability Map | Expanded CAP-G04 and clarified CAP-C02/CAP-E06. | Preserve provider cost quirks and schema-vs-policy validation separation. |

## 5. Confidence assessment

| Criterion | Assessment |
|---|---|
| Research preservation | High |
| Future-agent drift resistance | High |
| Default context packet clarity | High |
| Capability traceability | High after v4 corrections |
| Risk of raw-report reinterpretation | Low |
| Need to attach Deep Research by default | No |

## 6. Remaining caveat

If the user later wants to amend the endgame itself, the original Deep Research reports should be available as archival source material. They should still not override the three v4 anchors unless the amendment explicitly revises the anchors.

If a future agent makes claims about current Relay source-controlled contracts, schemas, policies, templates, or runtime behavior, it must fetch the current Planner GitHub knowledge manifest and required source-controlled files for the task domain before relying on those claims.
