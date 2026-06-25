# Deterministic Relay Handoffs for Coding Agent Pipelines

## Executive summary

Relay should treat the Planner handoff as a **restricted authoring format**, not as the execution contract. The execution contract should be a **canonical packet**: a closed, schema-validated JSON document with stable IDs, explicit enums, discriminated unions for task atoms, source-location mappings back to the handoff, and canonical serialization for hashing and signing. The executor brief should then be a **pure render** of that canonical packet, produced by a deterministic template engine with stable ordering and explicit inclusion rules. This “authoring artifact → intermediate representation → rendered brief” split mirrors the compiler pattern of source → IR → target, which is useful because IRs are designed to preserve meaning while supporting validation, transformation, and auditability. LLVM’s documentation explicitly describes IR as a common representation used throughout compilation, and source maps show how transformed artifacts can remain traceable to original sources. citeturn8search1turn8search4turn26view6

The strongest evidence base for Relay’s design comes from four areas. First, structured data standards strongly favor **JSON plus JSON Schema** when deterministic validation matters: JSON requires interoperable behavior only when object names are unique, JSON Pointer provides a standard path language for locating fields, and JSON Canonicalization Scheme provides deterministic serialization for hashing and signing. JSON Schema Draft 2020-12 adds reusable `$defs`, better tuple handling via `prefixItems`, and explicit output meta-schemas for validation reports. citeturn18search0turn15view4turn15view3turn15view2turn7search0

Second, empirical work on LLMs shows that long context is fragile. “Lost in the Middle” found that models often use information best when it is near the beginning or end of context, with degraded usage in the middle. LongLLMLingua likewise frames long prompts as suffering from higher cost, performance reduction, and position bias. Provider guidance is directionally consistent: Anthropic advises structuring long prompts carefully, placing longform data high in the prompt and the query near the end, while OpenAI’s recent guidance notes that newer models often prefer shorter, outcome-first prompts rather than deeply procedural prompt stacks. citeturn2search0turn2search2turn13view1turn13view0

Third, modern structured-generation systems overwhelmingly converge on **schema-backed outputs** rather than “please return JSON” prompting. OpenAI’s Structured Outputs explicitly guarantee conformance to a supplied JSON Schema, and JSONSchemaBench evaluates constrained decoding across efficiency, schema-feature coverage, and compliance. That directly supports Relay’s requirement that anything machine-critical should be represented structurally, not inferred from prose. citeturn13view2turn9search3

Fourth, instruction-conflict research increasingly supports **explicit priority rules**. OpenAI’s Model Spec centers a chain of command, and instruction-hierarchy research shows that models become more robust when trained to prioritize trusted instructions over less-trusted ones, including against prompt injection and malicious tool outputs. This matters for Relay because the executor brief must not mix executable scope with advisory routing, audit commentary, or child-task planning notes unless those are intentionally promoted into execution scope. citeturn27view0turn27view2turn10search1

My central recommendation is therefore: keep the Planner handoff rich enough for humans, but keep the canonical packet strict enough for machines. Relay should parse only a **small, fixed, documented subset** of handoff structure into executable semantics; everything else should either remain non-executable metadata or fail validation. The executor brief should be rendered from the packet alone, with no fresh semantic interpretation at render time beyond deterministic inclusion, ordering, and formatting decisions. That is the cleanest way to minimize semantic drift, control context bloat, support auditing, and retain operator review before dispatch. This final recommendation is a synthesis built on the standards, empirical papers, and platform guidance cited above. citeturn15view2turn15view3turn13view2turn2search0turn2search2turn27view2

## Artifact architecture and packet schema

**Recommended artifact architecture.** Relay should use four distinct artifacts: a **Planner handoff** for human-authored planning, a **canonical packet** as the post-parse source of truth, a **validation report** as a separate machine-generated or policy-generated artifact, and an **executor brief** as the only execution-facing rendered prompt. This separation is worth preserving because provenance systems work best when entities, activities, and agents are explicitly separated, and supply-chain provenance systems such as SLSA model production artifacts as attestable outputs of named build definitions rather than as mutable, freeform documents. citeturn26view4turn26view5

**What belongs in the Planner handoff.** The handoff should remain a human planning artifact, but its machine-parsed portions should be limited to a **restricted CommonMark profile**. CommonMark is valuable because it attempts to specify Markdown parsing unambiguously and ships conformance tests, while ATX headings have strict syntax rules that reduce ambiguity. Relay should therefore allow only fixed top-level ATX headings, fenced code blocks, and simple lists for parseable sections. It should avoid relying on tables, nested blockquote semantics, raw HTML, or arbitrary Markdown extensions for execution semantics. citeturn17search9turn17search6turn17search7

**What should not be the packet format.** The canonical packet should not be YAML. YAML is useful for human-friendly configuration, but the YAML spec itself says it is often seen as overly complicated. By contrast, JSON has clearer interoperability guidance, including the requirement that object member names should be unique for portable interpretation. For Relay, where low semantic drift and deterministic tooling matter more than authoring convenience, JSON plus JSON Schema is a better machine boundary. citeturn15view0turn18search0

**What belongs in the canonical packet.** The packet should contain only execution-relevant semantics and lineage metadata. That means: normalized objective, task atoms, file targets, acceptance criteria, validation commands, stop conditions, context references, non-goals, constraints, and provenance fields such as source spans and content hashes. It should also contain explicit packet versioning and schema identifiers. JSON Schema 2020-12 is well-suited here because it supports reusable `$defs`, dynamic references, explicit vocabularies, and recommended output structures for validation tooling. citeturn15view2turn25view5turn7search0

**Closed-world schema design.** The top-level packet schema should be **closed by default**. In practice that means `unevaluatedProperties: false` at the right boundaries, coupled with explicit internal `$defs` and `$ref` references for reusable subschemas. JSON Schema’s guidance on `unevaluatedProperties` is especially relevant because it works properly even across composed schemas, making it better than simpler “additional properties” closures when Relay’s packet evolves into modular sub-objects. citeturn25view4turn25view5

**Schema patterns for task atoms and related fields.** Task-like structures should use **discriminated unions** rather than loose unions or ad hoc strings. Pydantic’s guidance is directly aligned with this: discriminated unions validate more efficiently, avoid noisy multi-branch errors, and map cleanly into JSON Schema/OpenAPI discriminators. For Relay, `TaskAtom` should therefore be a tagged union such as `edit_file`, `create_file`, `run_command`, `inspect_path`, `request_human`, `write_test`, `validate_change`, and `stop_if`. citeturn25view2turn23search8

A good Relay packet schema would treat these fields as first-class objects:

```json
{
  "packet_id": "relay.packet.v1.01H...",
  "schema_version": "relay.packet/1.0.0",
  "planner_handoff_ref": {
    "artifact_id": "handoff.01H...",
    "content_hash": "sha256:...",
    "source_map": [
      {
        "packet_pointer": "/objective",
        "handoff_section": "Task",
        "span": {"start_line": 12, "end_line": 18}
      }
    ]
  },
  "objective": "Implement retry-safe webhook deduplication for inbound events.",
  "scope": {
    "in_scope": ["src/webhooks/ingest.py", "tests/test_ingest.py"],
    "out_of_scope": ["database schema redesign", "UI changes"]
  },
  "task_atoms": [
    {
      "kind": "edit_file",
      "id": "atom_01",
      "path": "src/webhooks/ingest.py",
      "intent": "Add idempotency key lookup before processing",
      "required": true
    },
    {
      "kind": "run_command",
      "id": "atom_02",
      "command": "pytest tests/test_ingest.py -q",
      "gating": true
    }
  ],
  "acceptance_criteria": [
    {
      "id": "ac_01",
      "check_type": "behavioral",
      "statement": "Duplicate webhooks with the same event_id are ignored without reapplying side effects."
    }
  ],
  "stop_conditions": [
    {
      "id": "stop_01",
      "reason": "required file missing",
      "action": "halt_and_report"
    }
  ]
}
```

That example is my synthesis, but the design choices behind it follow JSON Schema, tagged-union, closure, and provenance best practices. citeturn15view2turn25view2turn25view4turn26view4

**Child handoffs and split tasks.** Child tasks should be represented as **linked planning relations**, not as implicit executable scope. JSON Schema and related API-description ecosystems consistently separate reusable or referential components from active operations: components do nothing until referenced. Relay should copy that pattern. A parent packet may include `child_packets` or `planned_splits`, but only a designated `execution_scope` object should be considered dispatchable. This clean separation matters because otherwise downstream models can confuse “future decomposition” with “do this now.” citeturn12search3turn12search7

## Executor brief rendering and context budgeting

**Recommended executor brief rendering model.** The executor brief should be rendered from the canonical packet, not from the Planner handoff. The renderer’s job is not to reinterpret planning prose; it is to select packet fields, order them deterministically, and format them for the target model tier. This is where a logic-light or logic-less template helps. Mustache’s design goal is separation of logic from presentation, and that is exactly what Relay wants: selection and precedence in compiler code, minimal branching in the template itself. Jinja can still be useful for richer rendering, but its flexibility means Relay should keep control flow out of templates unless there is a compelling reason. citeturn25view0turn25view1

**Template design patterns.** A strong default executor brief has a fixed section order such as: objective, hard constraints, file targets, ordered task checklist, validation commands, stop conditions, and expected final report format. The wording should be outcome-oriented and concise. OpenAI’s recent prompt guidance explicitly recommends shorter, outcome-first prompts for newer models, while Anthropic recommends clear tagging and separation of instructions, context, and examples. Relay should therefore treat packet rendering as “structured, brief, outcome-first by default,” with model-tier-specific expansions only when needed. citeturn13view0turn13view1

**Context-tier model.** Relay should classify every context item into five tiers before rendering:

**Required execution context** is needed for the agent to do the task correctly now: exact objective, hard constraints, relevant file paths, validation commands, stop conditions, and acceptance criteria. These belong in the main brief.

**Optional reference context** can improve execution but is not strictly required: selected excerpts, summarized design rationale, or non-binding examples. These should appear only if budget allows, and ideally as compact referenced appendices or retrieved snippets.

**Review-only context** helps a human operator evaluate readiness but should not be given to the executor by default, such as confidence estimates, unresolved ambiguities, or routing recommendations.

**Audit-only context** is for provenance and after-action review: hashes, parser diagnostics, redaction notes, source maps, approval metadata, and signed attestations.

**Excluded context** is anything out of scope, sensitive beyond the execution need-to-know boundary, or likely to confuse the agent, including child-task planning material not promoted into current execution scope.

This tiering scheme is my synthesis, but it is strongly supported by long-context findings, instruction-hierarchy work, and consent-and-tool-boundary guidance in MCP. citeturn2search0turn2search2turn27view2turn21view0turn21view1

**How the renderer should decide what to include.** The renderer should use an explicit allowlist function over packet fields, not semantic similarity or freeform summarization. If a field is marked `execution_required`, include it. If it is `reference_optional`, include it only when both the model tier and token budget permit. If a field is `review_only` or `audit_only`, exclude it from the brief and preserve it elsewhere. This is the safest response to long-context limitations and prompt-injection concerns because the system remains rule-driven rather than heuristic-driven. citeturn2search0turn27view2turn21view1

**Context budgeting.** Relay should budget prompt size as a first-class rendering constraint. Both OpenAI and Anthropic provide token-count endpoints specifically to fit prompts, estimate costs, and make routing decisions; OpenAI also notes that local tokenizers often miss request-structure overhead, tools, and schema costs. That implies Relay should compute prompt budgets using provider-side or provider-accurate token counting where possible, then enforce hard limits before dispatch. citeturn13view3turn14view0

**Prompt ordering and compression.** For long briefs, Relay should place static reusable instructions early for cacheability, keep the brief body compact, and restate the most critical execution checklist near the end to counter middle-context degradation. OpenAI’s caching guide says exact prefix matches are needed for cache hits and recommends static content at the beginning and variable content at the end. Anthropic likewise describes prompt caching as prefix-based and long-context prompting as benefitting from careful organization. LongLLMLingua’s empirical results further support compression when long prompts would otherwise dilute signal. citeturn13view4turn14view1turn13view1turn2search2

**Model-tier-specific rendering without hardcoded provider assumptions.** Relay should not branch on provider names inside templates. Instead it should maintain a **capability registry** describing each dispatch target in terms of context-size tier, structured-output support, tool support, latency tier, cost tier, reasoning profile, and cache behavior. Both OpenAI and Anthropic expose model-list APIs, and both recommend choosing models by capability, speed, and cost rather than by static assumptions. OpenAI also explicitly distinguishes planning-oriented reasoning models from lower-latency workhorse models. For Relay, that means render-policy selection should depend on capabilities like “high ambiguity tolerance” or “strict executor tier,” not “provider X model Y.” citeturn24view1turn14view3turn14view2turn24view0

## Parsing validation and lineage

**Parser and compiler boundaries.** Relay should separate parsing into three stages. First, a **structural parser** converts the restricted CommonMark handoff into a syntax tree with section boundaries and source spans. Second, a **semantic compiler** maps recognized sections into typed packet fields. Third, a **policy validator** evaluates packet correctness, precedence conflicts, and safety or redaction rules. This is preferable to a single “smart parser” because it localizes failure causes and preserves explainability. Compiler architectures use IR precisely because it is easier to optimize, validate, and transform a stable intermediate form than raw source text. citeturn8search1turn8search4turn17search9

**Validation between the Planner handoff and canonical packet.** This stage should check: required sections exist; duplicate or malformed headings are rejected; every executable packet field has a source span; every claimed file target parses into a normalized path; enum values are valid; discriminated unions resolve unambiguously; and no same-priority contradictions survive normalization. Because JSON behavior becomes unpredictable with duplicate object names, and tagged unions validate more efficiently and clearly than loose unions, Relay should treat duplicate keys, unresolved union variants, and ambiguous section mappings as hard failures. citeturn18search0turn25view2turn25view3

**Validation between the canonical packet and executor brief.** This stage should check: the rendered brief includes every `required` task atom exactly once or records an explicit omission reason; no `review_only` or `audit_only` fields leaked into the rendered prompt; order is stable; token budget is within limit; the rendered output hash is stable under repeated rendering; and the brief remains explainably derivable from packet fields. JSON Schema’s output recommendations are helpful here because they show a validation-report shape with evaluation path, schema location, and instance location, which is exactly the kind of machine-readable diagnostics Relay should preserve for explainability. citeturn26view3turn26view2

**Validation between the executor brief and audit packet.** After rendering, Relay should emit an audit packet containing the exact prompt bytes, prompt hash, packet hash, template version, model capability snapshot, approval identity, redaction manifest, and execution constraints used at dispatch time. Provenance standards define provenance as information about entities, activities, and agents involved in producing an artifact, while SLSA provenance explicitly models how artifacts were produced through a build definition. Relay can adopt this logic without overcomplicating it: the brief is the artifact, rendering is the activity, Relay plus the approving operator are agents, and the packet is an input entity. citeturn26view4turn26view5

**Conflict-resolution rules.** Relay should formalize precedence as follows: platform and safety rules first; operator-approved dispatch constraints second; canonical packet executable scope third; optional or advisory packet fields fourth; referenced context fifth; and raw handoff text last. This is not arbitrary. It mirrors the general trust-ordered pattern in instruction-hierarchy work and the Model Spec’s chain of command. Crucially, if two instructions at the same precedence level conflict, Relay should not let the renderer “pick one”; it should mark the packet invalid and require operator repair before dispatch. citeturn27view0turn27view2turn10search1

**How to preserve lineage.** Every packet field that came from the handoff should carry a pointer back to source origin, ideally using JSON Pointer into the packet and line-span references into the handoff. The source-map analogy is useful here: transformed artifacts become debug-friendly when you can map generated positions back to originals. Relay does not need browser-style source maps literally, but it should preserve the same principle of bidirectional mapping between packet fields, validation diagnostics, and source spans. citeturn15view4turn26view6

**Separate policy evaluation from structural validation.** Structural schema validation is not enough for all Relay rules. JSON Schema is excellent for data shape, but policy engines such as OPA and constraint systems such as CUE are better fits for higher-order checks like “child tasks must not appear in executable scope,” “routing recommendations are non-binding,” or “sensitive fields must not render into executor prompts.” OPA is purpose-built for policy over hierarchical data, and CUE is designed to make data validation simple and flexible. citeturn24view3turn24view4

## Evaluation, risks, and evidence gaps

**Prompt quality evaluation framework.** Relay should evaluate rendered briefs on four layers. The first is **structural validity**: packet parses, schema validates, render is deterministic, and no forbidden tiers leak. The second is **prompt efficiency**: token count, cacheable-prefix reuse, duplicate-content ratio, and optional-context drop rate. The third is **execution quality**: task success rate, test pass rate, unnecessary edit rate, and stop-condition correctness. The fourth is **reasoning alignment**: whether the brief led the executor to the right files, the right decomposition, and the right validations. OpenAI’s evaluation guidance recommends defining objectives, collecting held-out data, defining explicit metrics, and continuously evaluating; Anthropic similarly frames evals as input-plus-grading-logic systems. RACE-bench is especially relevant because it measures not only final patch correctness but also intermediate reasoning quality, file localization, step decomposition, recall, and over-prediction. citeturn24view5turn24view6turn22view0

**Recommended metrics.** For Relay specifically, the most useful metrics are likely: packet coverage, render determinism rate, token efficiency, context precision, context recall, execution success, acceptance-criteria satisfaction, needless-action rate, and audit completeness. SWE-bench and SWE-bench Verified remain useful outer-loop checks for real software issue resolution, but they do not, by themselves, tell you whether a brief was well-rendered; RACE-bench-style reasoning metrics are closer to the design problem here. citeturn22view1turn22view2turn22view0

**Failure modes and mitigations.** The largest technical failure mode is **semantic drift**: the renderer paraphrases planner intent and changes meaning. The mitigation is to store semantics in the packet, not prose, and render from structured fields only. The second major failure mode is **scope bleed**, where child tasks, audit commentary, or routing suggestions appear executable. The mitigation is explicit context tiers, field-level trust classes, and render allowlists. The third is **context bloat**, where too much reference material buries the operative instructions. The mitigation is token budgeting, optional-context compression, and repetition of only the most critical constraints at stable positions. The fourth is **hidden conflict**, where contradictory instructions slip through because they live in different sections. The mitigation is normalized precedence and hard-fail same-tier contradictions. The fifth is **schema drift**, where packet producers and consumers evolve incompatibly. The mitigation is semantic versioning, contract tests, and migration tooling. These mitigations are my synthesis, but they are consistent with the long-context, structured-output, and instruction-hierarchy evidence described above. citeturn2search0turn2search2turn13view2turn27view2

**What the evidence does and does not show.** The evidence is strong that structured outputs improve machine reliability, that long context has real position-bias problems, that instruction hierarchies help with conflict resolution, and that provenance models improve auditability. The evidence is weaker on the exact best executor-brief format for code agents. There is not yet a widely accepted body of controlled studies comparing, for example, “short imperative checklist brief” versus “rich structured XML brief” across multiple coding-agent systems on the same tasks. Platform guidance is useful, but it is still practitioner guidance rather than a mature academic literature. JSONSchemaBench gives good evidence for schema-backed output reliability, but that is not the same thing as end-to-end coding-agent performance. citeturn9search3turn2search0turn27view2turn26view4

**Open questions and evidence gaps.** Relay should treat these as research items rather than settled facts: how much rationale actually helps executors versus distracting them; whether critical constraints should be front-loaded, tail-loaded, or repeated; when structured XML-like rendering beats plain terse prose; how differences between reasoning-heavy planners and execution-heavy models affect brief shape; how much child-task metadata can safely remain visible without causing scope confusion; and whether packet-level confidence scores help operator review or merely create false reassurance. Those are important because model behavior is still context- and provider-dependent, and recent provider guidance itself says prompt behavior can change across model generations. citeturn13view0turn24view0turn14view2

## Concrete Relay recommendations and bibliography

**Concrete Relay design recommendations.** First, define a **Relay Handoff Profile**: restricted CommonMark with fixed ATX heading names, no YAML front matter, and only documented executable sections. Second, compile that handoff into **Relay Packet JSON** validated by JSON Schema 2020-12, using `$defs`, discriminated unions, and `unevaluatedProperties: false` at key boundaries. Third, record **source grounding** for every executable field using packet JSON Pointers and handoff line spans. Fourth, produce **deterministic rendering** with stable sort order, explicit omission reasons, and a render hash over canonicalized output. Fifth, keep **packet assessment** separate from the handoff body, exactly as your stated constraint requires, so advisory review material cannot accidentally become executor scope. Sixth, require **operator approval** before dispatch and store that approval as a first-class lineage event. Seventh, keep a **capability registry** for model tiers and route by latency, context, structure support, and reasoning profile rather than provider-specific prompt code paths. Eighth, preserve **audit packets** with hashes, versions, approvals, and redaction manifests. These recommendations are my synthesis, grounded in the cited standards and platform guidance. citeturn15view2turn15view3turn15view4turn25view0turn26view4turn21view1turn24view1turn14view2

**Example canonical packet → executor brief mapping.** Suppose the packet contains: objective = implement retry-safe webhook deduplication; file targets = `src/webhooks/ingest.py` and `tests/test_ingest.py`; one task atom to add an event-id dedup check; one task atom to add tests for duplicate delivery; a gating validation command `pytest tests/test_ingest.py -q`; one non-goal prohibiting database redesign; and one stop condition to halt if the existing persistence abstraction cannot safely store an idempotency marker. Relay should render that into a brief such as:

```text
Objective
Implement retry-safe deduplication for inbound webhooks.

Hard constraints
- Do not redesign the database schema.
- Modify only the listed target files unless a stop condition is hit.

Target files
- src/webhooks/ingest.py
- tests/test_ingest.py

Required work
- Add an idempotency check keyed by event_id before processing side effects.
- Add or update tests proving duplicate delivery does not reapply side effects.

Validation
- Run: pytest tests/test_ingest.py -q
- The change is not complete unless this command passes.

Stop if
- The current persistence abstraction cannot safely record an idempotency marker without broader architectural changes.

Final report
- Summarize files changed, behavior added, tests run, and any unresolved blockers.
```

That mapping is intentionally narrow: it promotes only executable content and required context, leaving rationale, assessment, and audit material outside the brief unless deliberately summarized. The design is supported by the evidence favoring outcome-first prompts, careful long-context organization, and schema-backed determinism. citeturn13view0turn13view1turn13view2turn2search0

**Annotated bibliography.**

- **RFC 8259 and RFC 8785.** These are the core standards for using JSON reliably in Relay: RFC 8259 defines interoperable JSON behavior and warns about duplicate object names, while RFC 8785 defines canonical JSON serialization for repeatable hashing and signing. citeturn18search0turn15view3

- **JSON Schema Draft 2020-12.** This is the best fit for Relay packet contracts because it supports reusable `$defs`, improved tuple handling, and standardized validation outputs that can power machine-readable validation reports. citeturn15view2turn7search0turn25view5

- **CommonMark specification.** Useful for defining a strict Planner handoff authoring subset with predictable heading and block parsing, instead of relying on informal Markdown conventions. citeturn17search9turn17search6turn17search7

- **OpenAI Structured Outputs and JSONSchemaBench.** Together these are the clearest evidence that structured, schema-constrained representations are much more reliable than freeform formatting instructions for machine consumption. citeturn13view2turn9search3

- **Lost in the Middle and LongLLMLingua.** These papers are the main empirical basis for strict context budgeting, selective inclusion, and careful prompt ordering in executor briefs. citeturn2search0turn2search2

- **OpenAI and Anthropic prompt/token/caching guidance.** These documents provide practical, current evidence for outcome-first prompting, accurate token budgeting, and prefix-oriented caching strategies that should influence Relay renderer design. citeturn13view0turn13view3turn13view4turn14view0turn14view1

- **Instruction hierarchy research and Model Spec materials.** These are highly relevant to Relay conflict handling because they formalize trust-ordered instruction resolution and show robustness gains when models respect that hierarchy. citeturn27view0turn27view2turn10search1

- **PROV and SLSA provenance.** These standards are the best conceptual basis for Relay lineage, audit packets, and approval/event tracking, especially if Relay later adds signed attestations. citeturn26view4turn26view5

- **OPA and CUE.** These are not packet formats, but they are excellent references for separating schema validation from higher-order policy and constraint evaluation. citeturn24view3turn24view4

- **SWE-bench and RACE-bench.** SWE-bench is still the canonical real-world software-issue benchmark, while RACE-bench is especially useful for Relay because it evaluates intermediate reasoning structure, file localization, and step decomposition rather than only final patch success. citeturn22view1turn22view2turn22view0