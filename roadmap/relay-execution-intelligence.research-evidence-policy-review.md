# Relay Execution Intelligence — Research Evidence Policy Review

Artifact status: planning review / not a Relay plan / not an implementation handoff  
Created: 2026-06-25  
Review target: v2 anchor artifacts  
Output revision: v3 anchor artifacts

## 1. Review conclusion

Commit confidence is now high for using the three anchor artifacts as the default context for future child Plans of Passes.

The v2 artifacts correctly preserved the research direction, but they still instructed future agents to attach the Deep Research reports too often. That created a risk of reinterpretation drift and context dilution.

The v3 revision resolves that risk by making the Deep Research reports archival evidence only. Future child-plan agents should use the Program Roadmap v3, Research Integration Brief v3, and Capability Map v3 as the controlling research representation.

## 2. Policy decision

The five Deep Research reports are not default child-plan attachments.

They may be consulted only for targeted verification, provenance checks, grep/rg, ambiguity resolution, or an explicit user-requested program amendment. When consulted, child plans must record the report, query or section, reason, and decision affected.

## 3. Files updated

- `2026-06-25_relay-execution-intelligence.program-roadmap.v3.md`
- `2026-06-25_relay-execution-intelligence.research-integration-brief.v3.md`
- `2026-06-25_relay-execution-intelligence.capability-map.v3.md`

## 4. Changes made

| Artifact | Change |
|---|---|
| Program Roadmap | Replaced raw-report attachment matrix with default child-plan packet, archival evidence policy, targeted-use rules, research evidence status matrix, and updated child-plan prompt template. |
| Research Integration Brief | Added explicit rule that Deep Research reports are represented by anchors and are not default attachments. Replaced child-plan packet section accordingly. |
| Capability Map | Added research evidence representation policy and CAP-A06 to make the non-default raw-report rule a first-class governance capability. |

## 5. Confidence assessment

| Criterion | Assessment |
|---|---|
| Research preservation | High |
| Future-agent drift resistance | High |
| Default context packet clarity | High |
| Risk of raw-report reinterpretation | Low |
| Need to attach Deep Research by default | No |

## 6. Remaining caveat

If the user later wants to amend the endgame itself, the original Deep Research reports should be available as archival source material. They should still not override the three anchors unless the amendment explicitly revises the anchors.
