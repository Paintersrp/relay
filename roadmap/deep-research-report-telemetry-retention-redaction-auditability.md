# Relay Telemetry Retention, Redaction, and Auditability Policy for Coding-Agent Pipelines

## Executive summary

The strongest pattern across security logging guidance, AI observability standards, and provider data-handling documentation is this: **retain durable lineage and operational metadata by default, but treat content-bearing telemetry as sensitive and exceptional**. Privacy guidance emphasizes data minimization and storage limitation; OWASP says sensitive items such as access tokens, passwords, encryption keys, source code, and sensitive personal data should usually not be recorded directly in logs; and OpenTelemetry’s current GenAI guidance notes that prompt content, tool arguments, and tool results are not captured by default because they can contain sensitive data. citeturn9view9turn9view10turn21view1turn24view0

For Relay, that means the default store should preserve **artifact lineage, hashes, versions, timestamps, routing choices, token/cost metrics, validation outcomes, audit outcomes, approval events, and changed-file summaries**, while making **full rendered prompts, provider request/response bodies, raw logs, raw diffs, and retrieved source excerpts** opt-in, redacted, size-bounded, and short-lived. This recommendation is consistent with provenance standards that prioritize entities, activities, and agents; with GenAI telemetry conventions that standardize model, token, latency, and evaluation metadata; and with provider platforms that expose zero-retention or limited-retention modes while treating full prompt/response persistence as sensitive. citeturn17view6turn17view5turn17view7turn22view0turn11view1turn9view3turn10view3turn9view5

My bottom-line recommendation for Relay v1 is straightforward: **do not store full rendered executor prompts by default; do not store provider request or response payloads by default; never store secrets, hidden chain-of-thought, encrypted reasoning items, unrelated chat history, or unredacted bulk logs; and fail closed when sensitive content cannot be redacted safely**. Instead, make Relay’s own canonical packet, brief hash, result summary, validation report, and audit packet the primary durable artifacts, with content capture available only under explicit operator control and bounded retention. citeturn24view0turn21view1turn17view3turn10view3turn13view0

## Standards and design basis

A useful standards baseline for Relay comes from four places. First, NIST’s AI Risk Management Framework emphasizes systematic documentation, traceable testing/evaluation/validation, ongoing risk tracking, and transparency/accountability practices; it also notes that explainable systems are easier to debug and monitor and lend themselves to more thorough documentation, audit, and governance. citeturn15view4

Second, W3C PROV, SLSA provenance, and OpenLineage all converge on the same core idea: trustworthy systems need durable records of **what artifact was produced, by which activity, by which actor, from which inputs, and at what time**. W3C PROV defines provenance around entities, activities, and people; SLSA provenance records subject, builder, recipe, and materials; and OpenLineage models runtime and design lineage through run events and job metadata events. Those concepts map directly onto Relay’s handoff, packet, brief, execution, validation, and audit artifacts. citeturn17view6turn17view5turn17view7

Third, OWASP and NIST log-management guidance give Relay its operational security baseline. OWASP says logs and logging mechanisms must be protected from misuse, tampering, and unauthorized access; it recommends tamper detection, restricted read privileges, and recording access to logs. NIST SP 800-92 similarly frames sound log management as an enterprise discipline, not an ad hoc debugging aid. citeturn9view0turn21view0turn9view2

Fourth, privacy and AI-regulatory rules strongly favor scoped retention. UK GDPR guidance from the ICO says organizations should hold only the minimum personal data necessary and no more, and should document retention periods by category while allowing early deletion when appropriate. California’s CCPA/CPRA regulations similarly require collection, use, retention, and sharing to be reasonably necessary and proportionate. For some EU AI Act high-risk deployments, explanatory official service-desk materials summarize Article 26(6) as requiring deployers to keep automatically generated logs under their control for at least six months, subject to other applicable law. citeturn9view9turn9view10turn17view9turn14search4

These sources support a Relay policy that is **metadata-first, provenance-strong, redaction-heavy, operator-controlled, and retention-configurable**. They do not support a policy of indiscriminate prompt/body logging or indefinite storage of execution content. citeturn15view4turn21view1turn9view10turn24view0

## Recommended telemetry taxonomy and retention classes

### Recommended telemetry taxonomy

Relay should separate telemetry into six classes, because different classes have different privacy and audit properties. A practical taxonomy is:

- **Lineage and identity telemetry**: artifact IDs, run IDs, hashes, parent/child relationships, schema/profile/template versions, timestamps, repo commit or source reference IDs, and actor identities for planner, operator, executor, and reviewer. This class is the backbone of auditability and should be durable. citeturn17view6turn17view5turn17view7
- **Routing and approval telemetry**: routing recommendation, selected provider/model/profile, whether the recommendation was followed, dispatch approval, rejection/revision decisions, and reviewer escalation events. This class is essential for human-gated Relay workflows and for later routing-quality analysis. citeturn17view6turn22view0turn11view1
- **Operational and cost telemetry**: estimated tokens, actual input/output/reasoning/cache tokens, latency, time to first chunk, retries, finish reasons, error classes, and provider response IDs when available. This class supports cost control and model-tier comparison without requiring full content capture. citeturn23view0turn23view1turn23view2turn23view3turn11view1
- **Structured validation and audit telemetry**: validation command IDs, pass/fail results, audit finding IDs, severity, correction-loop count, and changed-file summaries. This class is durable because it is the most compact representation of outcome quality. citeturn15view4turn22view0turn26view2
- **Content-bearing debug telemetry**: full briefs, source excerpts, raw provider bodies, stdout/stderr excerpts, tool arguments/results, retrieved document content, and code diffs. This class is the most useful for debugging and the highest risk for privacy/security; it should therefore be optional, redacted, sampled, and short-lived. citeturn24view0turn11view1turn11view0turn27view3
- **Security and policy telemetry**: secret-detection hits, redaction actions, blocked persistence/export events, access-to-logs events, and retention deletions. This class should be durable enough to investigate incidents and prove policy enforcement. citeturn21view0turn25search0turn25search4

### Always-retain fields

Relay should always retain the following fields because they are comparatively low-risk and disproportionately valuable for reproducibility, auditability, and routing analysis:

- Stable IDs for the run, artifact, task, approval event, validation event, and audit event; artifact type; schema version; and content hash for each durable artifact. These are the minimum fields needed to reconstruct lineage. citeturn17view6turn17view5turn17view7
- Source references rather than source bodies: source handoff path/hash, canonical packet path/hash, executor brief path/hash, render profile ID, template version, and any repo commit or file reference identifiers. This preserves traceability while minimizing content retention. citeturn17view5turn17view6turn11view0
- Actor and approval metadata: planner identity, operator identity, selected executor profile/model ID, manual dispatch decision, audit decision, and timestamps for creation, approval, dispatch, completion, validation, and audit. Provenance models depend on recording the responsible agents as well as the activities they performed. citeturn17view6turn17view5
- Routing and effectiveness metadata: routing recommendation, whether it was followed, retry count, correction-loop count, acceptance state, and whether the cheap/mid/strong/reviewer tier appeared effective based on validation/audit outcomes. OpenTelemetry and OpenInference support comparable provider/model/evaluation metadata without requiring content capture. citeturn22view0turn11view1turn26view2
- Usage and performance metadata: estimated tokens before dispatch, actual input/output tokens, reasoning-token counts when exposed, cache read/write token counts where exposed, finish reason, response ID, wall-clock latency, and time to first chunk for streaming. This is enough to compare providers and prompt profiles without keeping raw payloads. citeturn23view0turn23view1turn23view2turn23view3
- Structured outcomes: validation pass/fail state, command identifiers, audit findings, risk severity, changed-file summary, and a compact status summary of the executor result. NIST AI RMF stresses structured, documented, traceable evaluation and risk tracking over time. citeturn15view4turn26view2

### Retain-if-redacted-and-configured fields

Relay should retain the following only when content capture is explicitly enabled, a redaction pipeline succeeds, and the deployment’s retention policy allows it:

- Full rendered executor brief text, planner handoff text, packet assessment text, and canonical-packet render output. OpenTelemetry’s current GenAI guidance recommends metadata-only by default because prompts and tool content can contain sensitive data; prompt/content capture should be a deliberate opt-in. citeturn24view0
- Prompt template text, template variables, and prompt fragments used for prompt-quality analysis. OpenInference explicitly models prompt template, variables, and template version as separate fields, which is useful for evaluation, but those fields can remain content-sensitive and should not be persisted casually. citeturn11view0turn11view2
- Provider request and response bodies, including structured tool definitions, tool-call arguments, tool results, and retrieved document content. Google’s request-response logging is disabled by default, supports sampling, and writes full request/response data to BigQuery only when enabled; OpenTelemetry likewise treats content capture as an opt-in. citeturn13view0turn24view0
- Source excerpts, retrieved-document excerpts, validation output excerpts, stdout/stderr excerpts, and code-diff hunks. OWASP notes that detailed bodies, stack traces, and debug data may be useful, but should often be handled as extracts or summaries and may require special treatment before recording. citeturn27view0turn27view2turn27view3
- User-visible reasoning summaries, if a provider exposes them and the team intentionally enables them for debugging or evaluation. Even here, Relay should prefer brief summaries over full reasoning-like bodies, and should subject them to the same redaction and retention controls as any other content-bearing artifact. OpenAI exposes reasoning summaries only when explicitly requested and does not expose raw reasoning tokens. citeturn17view3

### Never-retain fields

Relay should never retain the following in telemetry storage or export packages:

- Secrets and primary credentials: access tokens, cookies, signed URLs, passwords, private keys, database connection strings, API keys, encryption keys, and other primary secrets. OWASP explicitly lists access tokens, authentication passwords, database connection strings, and encryption keys/primary secrets among the data that should usually not be recorded directly in logs. citeturn21view1turn21view4turn21view2turn21view3
- Hidden model chain-of-thought, encrypted reasoning items, or raw provider-internal reasoning traces. OpenAI states that raw reasoning tokens are not exposed, while encrypted reasoning items exist to be passed between turns in stateless or zero-retention contexts; Relay should treat them as transient transport state, not durable telemetry. citeturn17view3
- Unrelated chat history, unbounded source dumps, full workspace snapshots, and raw unredacted logs. These violate the minimization and storage-limitation principles and create unnecessary breach surface. citeturn9view9turn9view10turn21view1
- Full application source code in log-like telemetry stores. OWASP explicitly includes application source code in its “data to exclude” guidance; if code evidence is needed, Relay should retain hashes, file references, or minimal redacted diff hunks under a stricter content-capture mode. citeturn21view1
- Data that is illegal to collect, exceeds the logging system’s security classification, or was not consented to by the data subject. OWASP and privacy regulators both make this a baseline requirement, not an optional best practice. citeturn21view1turn17view9turn9view10

## Redaction and secret-handling policy

### Redaction strategy

Relay should use a **pre-persistence redaction pipeline**, not a “store first, clean later” approach. NIST’s de-identification guidance defines de-identification as removing the association between identifying data and the data subject, and Google’s Sensitive Data Protection documentation highlights de-identification through masking and tokenization. OpenInference also explicitly notes that prompts and completions often contain personal information and must be maskable before export with per-field granularity. citeturn17view0turn17view1turn11view1

A strong Relay redaction sequence is: first classify each field as structured metadata, structured content, or freeform blob; then drop disallowed fields entirely; then mask or pseudonymize direct identifiers and secret-like values; then size-cap large content bodies; then persist either a redacted excerpt plus content hash or only a hash/reference if the content remains risky. OWASP recommends that sensitive log data be removed, masked, sanitized, hashed, or encrypted, and also recommends de-identification techniques such as deletion, scrambling, or pseudonymization of identifiers where identity is not required. citeturn21view1turn27view3

In practice, Relay should prefer **structured references over freeform bodies**. For example, keep `source_doc_id`, `source_doc_hash`, and excerpt offsets before storing `source_excerpt`; keep `stderr_hash` and a short redacted preview before storing raw stderr; and keep `diff_summary` and `changed_file_summary` before storing full patch hunks. OWASP explicitly notes that logs often need extract or summary properties instead of full content data, and that extended bodies and stack traces may be kept separately if truly needed. citeturn27view3turn27view2

Relay should also sanitize for log-injection and serialization risk before persistence or export. OWASP recommends sanitizing event data to prevent CR/LF and delimiter injection and validating event data from other trust zones before logging it. This matters for coding-agent pipelines because retrieved web pages, repo files, command output, and model messages may all carry hostile control characters or attack strings. citeturn27view3turn20view0

### Secret-detection and block policy

Secret detection should run at **every boundary where text may leave memory**: before disk persistence, before telemetry export, before copy-to-clipboard or artifact download, and before human-shared debug bundles. GitHub’s secret scanning documentation and push protection documentation show a mature pattern: detect known secret formats, prevent the leak before it lands, and raise alerts or require an explicit bypass if a push is blocked. citeturn17view2turn25search0turn25search4

For Relay, that means a three-level policy. High-confidence hits on secrets, credentials, signed URLs, or keys should **block persistence of the content itself** and record only a security event with field name, detector name, confidence, and artifact hash. Medium-confidence hits should quarantine the content for local-only operator review. Low-confidence hits may allow persistence only into an encrypted local debug store and only with explicit override. This recommendation is consistent with GitHub’s “prevent first, justify bypass later” pattern and with OWASP’s position that primary secrets should not ordinarily be recorded in logs. citeturn25search0turn25search4turn21view1

When safe redaction is impossible, Relay should **fail closed on content and fall back to metadata-only telemetry**. Anthropic’s API docs show that some features are blocked under stronger privacy constraints and that non-eligible HIPAA requests return errors; Google states that zero data retention may not be possible for some features and recommends not enabling request-response logging when zero retention is required. Relay should adopt the same philosophy locally: if the content cannot be made safe, do not persist it. citeturn10view3turn9view5turn13view0

### Prompt and provider payload retention recommendation

**Should Relay store full rendered executor prompts by default?** No. By default, Relay should store the brief hash, canonical packet hash, render profile ID, prompt template version or name, token estimate, actual token usage, routing metadata, and outcome metrics. OpenTelemetry’s GenAI observability guidance says prompt/tool content is excluded by default because it can contain sensitive data; content capture is opt-in. OpenInference and prompt-management tools show that prompt version, variables, and trace linkage can support evaluation without requiring blanket persistence of every full prompt body. citeturn24view0turn11view0turn11view2turn11view5

**Should Relay store provider request payloads?** Also no, not by default. Provider request bodies are content-rich, provider-specific, and often redundant with Relay’s own native artifacts. Google’s request-response logging is disabled by default, supports sampling, and can store full requests only when deliberately configured; OpenAI notes that abuse-monitoring logs may contain prompts and responses; Azure says models sold by Azure store and process data to provide the service and monitor misuse. Because upstream providers already have their own retention behaviors, Relay should minimize duplication and keep normalized metadata unless a debug capture mode is explicitly enabled. citeturn13view0turn9view3turn9view6

**Should Relay store provider response payloads or only response metadata?** Metadata by default. Keep response ID, response model, finish reason, latency, status/error class, token counts, and a content hash. Store redacted bodies only when configured for debugging, evaluation, or incident review. OpenTelemetry’s GenAI schema already provides stable fields for provider name, request model, response model, finish reasons, token usage, and time to first chunk; those fields are enough for most cost and comparison workflows. citeturn22view0turn23view0turn23view1turn23view3

For **source excerpts, command outputs, logs, diffs, validation results, and audit findings**, Relay should keep structured summaries and references always, and raw content only when redacted and configured. Validation results and audit findings are especially well-suited to structured retention because they are naturally represented as command ID, evidence type, pass/fail, severity, and finding text. Raw command output, raw logs, and raw diffs should remain bounded exceptions because OWASP treats debug bodies, stack traces, and similar fields as sensitive extended details rather than default log content. citeturn15view4turn27view0turn27view3

## Provenance, retention durations, and export policy

### Artifact lineage and provenance model

Relay’s provenance model should be built explicitly on **entities, activities, and agents**. In W3C PROV terms, the entities are the planner handoff, packet assessment, canonical packet, validation report, executor brief, executor result, audit packet, and final decision record; the activities are creation, rendering, dispatch, execution, validation, audit, redaction, export, and deletion; and the agents are the planner, operator, executor profile/provider/model, and reviewer. citeturn17view6

To make this operational rather than merely conceptual, each Relay artifact should carry: a stable artifact ID; artifact type; schema version; content hash; parent artifact IDs; `generated_by` activity ID; `used` input artifact IDs/hashes; creator/approver agent IDs; timestamps; and policy/profile IDs such as render profile, template version, and routing profile. SLSA’s provenance vocabulary offers a practical complement here: record the artifact `subject`, the execution `recipe`, the upstream `materials`, and the responsible `builder`. citeturn17view5turn17view6

A useful implementation pattern is to record Relay run-state events similarly to OpenLineage: start, ready-for-review, approved, dispatched, completed, validated, audited, exported, deleted. This event stream gives Relay a deterministic timeline without forcing the team to keep every content body forever. OpenLineage’s run-event pattern is a good fit because it separates runtime state changes from the metadata that describes the job or artifact itself. citeturn17view7

### Retention duration model

Retention durations should be **category-based and configurable**, not global. ICO guidance explicitly recommends documented retention schedules by category and stresses that data should not be kept longer than needed; CPRA proportionality rules point in the same direction. Relay should therefore have separate schedules for metadata, redacted content, caches, and incident records. citeturn9view10turn17view9

A defensible starting model for Relay v1 is:

- **Ephemeral runtime state**: in-memory only where possible; local caches and transient debug artifacts should expire within hours or a day unless a user deliberately preserves them. This matches the risk profile of provider cache/session features, which often use short TTLs and treat them as transient state. citeturn9view5turn10view3turn17view3
- **Redacted content-bearing telemetry**: default 7–30 days, with the shorter end for prompts/provider bodies and the longer end for incident debugging needing short retrospective analysis. This aligns with the common provider pattern of short operational retention windows for content-bearing logs. citeturn9view3turn9view5turn10view3
- **Operational metadata and lineage records**: default 90 days to 1 year, depending on team needs for debugging, model comparison, and audit. This class is lower risk and higher value than raw bodies, so it can justifiably live longer. citeturn17view6turn17view5turn9view10
- **Audit packets, approval records, and security-policy events**: default at least 180 days, and often 1 year or more where organizational audit requirements justify it. Where Relay participates in regulated EU high-risk AI deployment, a six-month minimum for controlled automatically generated logs may become relevant. citeturn14search4turn9view10

Those are **product recommendations, not universal legal rules**. In practice, the legally correct duration depends on jurisdiction, sector, contract, and whether Relay is used in contexts involving employment, health, finance, or regulated/high-risk AI. The important design point is that Relay’s storage classes and deletion controls must make those policy differences configurable. citeturn9view10turn17view9turn14search4

### Export and debugging policy

Relay should have two export profiles: a **default audit/debug bundle** and a **restricted local forensic bundle**. The default bundle should include lineage graph data, artifact IDs/hashes, render/template/profile versions, provider/model IDs, token/cost/latency metrics, routing decisions, approval events, validation/audit findings, correction-loop count, and redacted excerpts where content capture was enabled. This is enough for most debugging and audit work without creating a secondary leak surface. citeturn17view6turn22view0turn26view2

The restricted local forensic bundle may include additional redacted content bodies, but only under explicit operator action and only into a protected local store. OWASP recommends restricted privileges for reading log data, recording and monitoring all access to logs, using secure transport for log movement, and copying logs to read-only media as soon as possible when integrity matters. Relay exports should inherit those controls. citeturn21view0

Exports should **exclude** the never-retain class entirely: secrets, raw hidden reasoning, encrypted reasoning items, unrelated history, raw cookies, raw signed URLs, unredacted provider bodies, and unbounded source dumps. If content was blocked at ingest because safe redaction failed, that content should remain absent from all export forms and be represented only by a security event plus artifact hash. citeturn21view1turn17view3turn25search0

## Cost telemetry, evaluation support, and Relay v1 policy

### Cost and token telemetry model

Relay should collect enough telemetry to compare model tiers and routing decisions **without storing full content**. The minimum cost/efficiency set is: estimated input tokens before dispatch; actual input, output, and reasoning-token counts where providers expose them; cache read/write token counts where available; total latency; time to first chunk for streaming; retry count; tool-call count; finish reason; and final acceptance/revision/rejection outcome. OpenTelemetry’s GenAI attributes and OpenInference’s token fields provide a ready-made vocabulary for this. citeturn23view0turn23view1turn23view2turn23view3turn11view0

That telemetry supports the exact Relay questions you care about: whether a cheap/mid/strong/reviewer tier was effective, whether the recommended route was followed, whether stronger models meaningfully reduced correction loops, and whether a prompt or render-profile change increased cost or decreased validation failures. OpenTelemetry’s GenAI documentation explicitly notes that token and latency metrics let teams estimate per-request cost, detect token-hungry prompts, and compare models via metadata filters. citeturn24view0

### Prompt-quality evaluation support

Prompt-quality evaluation does not require storing every full prompt forever. OpenInference supports prompt template, variables, version, and evaluation score attributes; Langfuse and Anthropic’s prompt tooling both emphasize versioning prompts by separating fixed prompt structure from dynamic inputs; and OpenAI’s evaluation guidance recommends task-specific evals, continuous evaluation, and strong logging so teams can mine logs for eval cases. Relay can therefore support prompt-quality analysis by retaining **prompt/template version IDs, canonical packet hashes, routing profile IDs, evaluation metric IDs, acceptance labels, and sampled redacted traces**, rather than raw prompt archives by default. citeturn11view0turn11view2turn11view5turn11view3turn26view2

For practical evaluation, Relay should store: prompt/template version; render profile ID; model/provider ID; dataset or replay-case ID; validation/audit result; correction-loop count; operator acceptance label; and evaluator scores or pass/fail decisions. OpenTelemetry’s GenAI schema includes standardized evaluation name, score label, score value, and explanation fields, which are well suited to structured prompt testing without content-heavy retention. citeturn22view0

### Security and privacy risks

The main security risk in Relay telemetry is not “observability” in the abstract; it is **content accumulation**. Raw prompts, system instructions, tool schemas, tool arguments, retrieved documents, command output, and diffs all raise disclosure risk. OpenTelemetry’s GenAI observability guidance warns that full content capture can include prompts, completions, tool arguments, and tool results, and keeps them off by default because they may contain sensitive data. citeturn24view0

A second risk is **prompt leakage and prompt injection blast radius**. OWASP’s Prompt Injection project warns that hostile text can disclose sensitive information or influence downstream actions, while the System Prompt Leakage guidance explicitly says sensitive data such as API keys, auth keys, user roles, and permission structures should not be embedded in system prompts and that critical controls should be enforced outside the LLM. If Relay stores raw prompt bodies broadly, a later leak of telemetry storage can expose exactly the material OWASP says not to rely on or disclose. citeturn20view0turn20view1

A third risk is **privacy-law overcollection**. GDPR/UK GDPR and CPRA-based rules do not forbid telemetry, but they do make indefinite or excessive retention hard to justify, especially when the same debugging value could be achieved with hashes, references, and short redacted snippets. That makes metadata-first design not just a security optimization, but a legal-risk reduction strategy. citeturn9view9turn9view10turn17view9

### Recommended Relay v1 policy

For Relay v1, I recommend the following concrete policy:

Relay should **always retain** lineage metadata, artifact hashes, schema/profile/template versions, operator approvals, routing recommendations and actual selections, token/cost/latency metrics, validation outcomes, audit outcomes, correction-loop counts, and changed-file summaries. These records are the minimum durable evidence set for debugging and auditability. citeturn17view6turn17view5turn23view0turn26view2

Relay should **not store full rendered executor prompts by default**. It should store brief hash, canonical packet hash, template/render profile IDs, model/provider IDs, and usage/outcome metrics instead. Full rendered briefs should be retained only when content capture is explicitly enabled, redaction succeeds, and the retention window is short. citeturn24view0turn11view0turn11view2

Relay should **not store provider request or response payloads by default**. It should store normalized response metadata and usage metrics instead. Provider bodies may be captured only in short-lived, redacted, sampled debugging profiles. citeturn13view0turn9view3turn22view0

Relay should **never retain** secrets, cookies, tokens, signed URLs, private keys, hidden/raw chain-of-thought, encrypted reasoning items, unrelated chat history, full unredacted logs, or unbounded source dumps. If such material is detected, Relay should block persistence of the body, store only a policy/security event plus a content hash, and require explicit human handling if any further action is needed. citeturn21view1turn17view3turn25search0

Relay should enforce **pre-persistence scanning and redaction**, with field-level classification, secret detection, de-identification/masking, sanitization against log injection, and bounded excerpts. When safe redaction is impossible, the system should fail closed on content and continue with metadata-only retention. citeturn17view0turn17view1turn27view3turn25search0turn10view3

This policy fits Relay’s local-first and operator-gated philosophy well: it preserves the durable execution contract and audit chain, but it avoids turning telemetry into a parallel archive of sensitive repo and prompt content. It is also consistent with current GenAI observability practice, which increasingly standardizes metadata while making content capture an explicit opt-in. citeturn24view0turn22view1turn11view1

### Open questions requiring product, legal, or security decisions

Several important decisions are not purely technical.

Relay needs a product decision on whether **redacted full-brief capture** should be available in v1 at all, or postponed until the redaction pipeline and secret-blocking workflow are proven. The sources strongly support making this opt-in, but the exact UX and default posture are product choices. citeturn24view0turn21view1

Relay also needs a legal decision on **default retention durations by deployment model**. A solo-developer local install, a commercial team SaaS deployment, and an EU high-risk AI deployment may need materially different baselines, especially if employment, health, or customer support data appears in prompts or repo content. citeturn9view10turn17view9turn14search4

A security decision is required for **override workflows**: who can bypass a persistence block, what justification is required, whether Security must approve, and whether the bypass can ever apply to exports or only to local incident handling. GitHub’s push-protection model suggests that documented bypasses with reviewer visibility are workable, but Relay needs its own governance rule. citeturn25search0turn25search4

Finally, Relay should decide whether **reasoning summaries** are allowed at all in stored telemetry. Raw hidden reasoning should be prohibited, but summarized reasoning may still carry sensitive context or create confusing audit artifacts. If enabled, it should likely be limited to short-lived, redacted evaluation/debug contexts only. citeturn17view3

## Annotated bibliography

**NIST AI RMF 1.0.** Useful for grounding Relay in traceable documentation, TEVV, risk tracking, and accountability rather than mere prompt logging. It supports keeping structured validation/audit evidence as first-class artifacts. citeturn15view4

**NIST SP 800-92, Guide to Computer Security Log Management.** Provides the foundational idea that log management is an enterprise control requiring sound policy, operations, and protection, not just a developer convenience. citeturn9view2

**OWASP Logging Cheat Sheet.** The single most practical source for what logs should exclude, how extended details should be handled, how to sanitize events, and how to protect log integrity and access. It is especially important for the “never retain” and “retain only if redacted” categories. citeturn21view1turn27view3turn21view0

**OWASP Secrets Management Cheat Sheet.** Supports the broader principle that secrets need centralized control, auditing, and careful handling, not casual capture inside telemetry systems. citeturn9view1

**W3C PROV-DM.** Supplies the conceptual model for Relay lineage: entities, activities, and agents. This is the cleanest external standard for modeling handoff-to-execution provenance. citeturn17view6

**SLSA Provenance.** Adds a software-supply-chain-friendly vocabulary—subject, materials, recipe, builder—that maps well to Relay’s artifact chain and helps frame attestable audit packets. citeturn17view5

**OpenLineage Object Model.** Helpful for designing Relay run-state events separately from static artifact metadata. It supports event-based debugging without requiring indefinite raw-content retention. citeturn17view7

**OpenTelemetry GenAI semantic conventions and OpenTelemetry’s 2026 GenAI observability guidance.** These are the strongest sources for a metadata-first AI observability design: provider/model names, token counts, finish reasons, timings, and optional content capture that is off by default because of data sensitivity. citeturn22view0turn23view0turn24view0

**OpenInference specification and semantic conventions.** Particularly useful for prompt-quality evaluation because they model prompt templates, variables, prompt versions, evaluation scores, tool-call fields, and retrieval-document content with explicit recognition of privacy sensitivity. citeturn11view1turn11view0

**OpenAI data controls and reasoning-model documentation.** Important for two reasons: they show that provider-side logs may include prompts/responses and are retained up to 30 days by default for some API usage, and they confirm that raw reasoning tokens are not exposed while encrypted reasoning items exist only for turn continuity. citeturn9view3turn17view3

**Anthropic API retention and feature-eligibility documentation.** Valuable because it shows a modern “smallest possible retention footprint” approach, feature-specific exceptions, and blocking behavior under stricter privacy modes. It also reinforces the recommendation not to store hidden reasoning or server-side stateful artifacts casually. citeturn10view3

**Google Gemini Enterprise Agent Platform retention and request-response logging docs.** These are particularly useful for Relay because they distinguish metadata logging from full request/response logging, show sampling controls, and document cases where zero retention is not possible. They support the recommendation that raw provider payload retention be exceptional and configurable. citeturn9view5turn13view0

**Microsoft Azure data/privacy/security documentation for Azure-sold models.** Useful evidence that provider-side service operations may still involve processing and storage for service provision and abuse monitoring, which is another reason Relay should avoid duplicating provider payloads unnecessarily. citeturn9view6

**ICO guidance on data minimization and storage limitation, plus California’s proportionality rule.** These are the clearest current regulatory anchors for minimizing content retention and documenting category-based retention schedules. citeturn9view9turn9view10turn17view9

**OWASP Prompt Injection and System Prompt Leakage guidance.** These sources are especially relevant to coding-agent pipelines because they show how raw prompts and tool-related instructions can expose system behavior, credentials, and internal permissions if telemetry stores leak or are over-shared. citeturn20view0turn20view1