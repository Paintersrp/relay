# Relay Evaluation Harness for Prompt Efficiency and Coding-Agent Execution

## Executive summary

Relay should treat evaluation as a first-class product subsystem, not a post-launch dashboard. The strongest evidence does **not** support optimizing for raw token count alone. Instead, the best-supported direction is to evaluate the whole handoff pipeline as a sequence of testable transformations: Planner handoff, packet assessment, canonical packet, validation report, executor brief, manual dispatch, executor result, and audit packet. Official provider guidance converges on the same core pattern: define specific and measurable success criteria first, use task-specific evals, prefer automated and code-based grading when possible, and version prompts plus eval datasets because model behavior and prompt behavior both drift over time. citeturn21view1turn5view1turn22view0turn21view0

For Relay specifically, the most useful architecture is a **three-layer evaluation stack**. The first layer is **artifact correctness**, covering schema validity, deterministic rendering, field completeness, redaction, lineage, and hard constraints on what may or may not appear in each artifact. The second layer is **process quality**, covering prompt clarity, context precision and recall, task-atom coverage, routing recommendations, and operator override behavior. The third layer is **outcome quality**, covering validation pass rate, regression rate, audit outcomes, correction loops, cost, latency, and whether accepted runs remain correct under stronger validation. This layered design matters because public coding benchmarks increasingly show that final pass/fail alone hides major failure modes, including flawed tests, contamination, weak patch validation, and long-horizon maintainability problems. citeturn17view0turn16view0turn13view3turn13view4turn27view0turn27view1

The best empirical support for Relay’s prompt-quality gates comes from several directions. Requirement ambiguity reliably hurts code generation; benchmark prompt quality issues such as spelling, style inconsistency, noisy tokens, and malformed examples can change measured performance; and instruction-following and structured prompting improve consistency. In long contexts, models still degrade when relevant information is buried in the middle, and repository context can become harmful when it is bloated or poorly targeted. That means Relay should score prompt quality primarily on **clarity, grounding, testability, structure, and relevance**, then combine that with context-efficiency metrics that reward sending the **smallest high-signal execution-required context** rather than the shortest possible prompt. citeturn15view1turn37view0turn37view1turn37view2turn6view0turn36search0turn5view2turn30view0

On routing, the evidence supports comparing routers against an **oracle cost-quality frontier**, not against one fixed model. Research on FrugalGPT, RouteLLM, and constrained routing systems shows that routing can significantly reduce cost while maintaining quality, but that routing success depends on a calibrated signal; miscalibrated confidence makes thresholds brittle. For Relay v1, routing should therefore remain **advisory and operator-controlled**, with explicit cheap, mid, and strong recommendations plus confidence bands, expected cost, and expected latency, rather than automatic dispatch. That aligns with both the evidence and your product constraints. citeturn28view1turn29view1turn28view2turn28view3

The evidence is weaker for **audit-quality metrics** than for execution or prompt metrics. There is strong evidence that public benchmark validation can overstate correctness, and there is useful guidance on traces and evaluators, but few public studies directly measure the quality of post-execution audit packets in a structured handoff pipeline. My recommendation is to make audit quality a mix of hard verifications and sampled human review in v1, and to avoid turning LLM-judge scores into hard blockers until they are calibrated against humans on your own tasks. citeturn16view0turn21view1turn24search1turn24search8

## Recommended Relay evaluation architecture

Relay’s evaluation harness should mirror Relay’s artifact lineage. The harness should execute the same deterministic transforms that production uses, then attach graders to each boundary. Anthropic’s guidance for agent evals is a close fit here: define tasks, trials, graders, transcripts or traces, outcomes, an evaluation harness, and a clear distinction between the harness and the model. OpenAI’s eval guidance similarly emphasizes task definition, test inputs, and iterative analysis. citeturn21view0turn5view1

### Harness design

The recommended evaluation architecture is:

**Artifact layer.** Validate that the Planner handoff, packet assessment, canonical packet, validation report, executor brief, and audit packet conform to schema and policy. Canonical packets should be structured, schema-backed artifacts rather than prose-only contracts; official structured-output guidance exists precisely because schema adherence is more reliable than relying on freeform formatting conventions. citeturn31view0

**Transformation layer.** For every handoff → packet → brief render, record a reproducible transform fingerprint: input artifact hash, renderer profile, version, output hash, token counts by context tier, and a normalized semantic diff. This is where Relay should test determinism. If the same canonical packet and renderer profile do not render the same executor brief, that is a rendering defect, not a model defect. This recommendation is mostly synthesis, but it follows directly from the need for versioned prompt+model tracking under changing APIs and from Relay’s requirement for artifact lineage. citeturn22view0turn21view0

**Outcome layer.** Run the executor in a controlled environment, collect traces, apply validation checks, then create an audit packet that is graded separately from execution correctness. This separation matters because benchmark research now shows that “passed tests” does not always mean “actually correct.” On SWE-bench Verified, one empirical study found that 7.8% of patches marked correct failed the full developer test suite, and 29.6% of plausible patches showed behavioral divergence from the oracle patch under differential testing. citeturn16view0

### Failure attribution model

Relay should distinguish failures by the **earliest artifact boundary where the defect is observable**.

A **packetization failure** happens when the canonical packet drops, mutates, or invents semantics relative to the Planner handoff, or when structured fields that should exist are missing. A **rendering failure** happens when the canonical packet is valid, but the executor brief omits required constraints, introduces contradictions, or changes scope during deterministic rendering. A **prompt failure** happens when the brief faithfully reflects the packet but is still ambiguous, noisy, incomplete, or weakly testable, which the ambiguity and prompt-quality literature shows can materially hurt coding performance. A **context failure** happens when required repository evidence is absent, drowned in irrelevant context, duplicated into low-signal bulk, or never utilized by the agent even though it was present; ContextBench explicitly argues for measuring explored versus utilized context to expose this class of error. An **executor or model failure** happens when the handoff, packet, brief, and supplied context are all adequate, but the chosen executor still fails relative to stronger baselines or repeated trials. A **validation failure** happens when the apparent fix does not survive stronger tests or differential checks. An **audit failure** happens when the change may pass validation but still violates scope, leaves missing lineage, introduces review risk, or fails rollback safety expectations. citeturn15view1turn37view1turn13view3turn16view0turn13view4

### Hard-block and warning-only gates

For v1, Relay should use a small set of **hard blocks** and a larger set of **warning-only** signals.

Hard blocks should include schema-invalid canonical packets; missing required fields for goal, scope, non-goals, validation expectations, or source references; missing acceptance criteria or validation commands for any task atom; inclusion of packet assessment advice as executable scope inside the handoff body or executor brief; redaction or security violations; contradictory instructions in the executor brief; and nondeterministic rendering from the same packet and renderer profile. These are the kinds of failures that undermine the contract itself, and structured-output guidance supports enforcing these through schemas rather than prose parsing. citeturn31view0turn5view1turn21view1

Warning-only signals should include low “executable clarity per token,” high optional-context ratio, context redundancy, low router confidence, suggested task splitting opportunities, low LLM-judge rubric scores, or unusually long briefs that are still within the selected tier budget. The reason to keep these as warnings in v1 is that the literature supports their relevance, but their decision thresholds are workload-specific and often brittle under drift, non-determinism, or routing miscalibration. citeturn22view0turn28view3turn24search1turn24search8

## Metric framework

The metric framework below is my synthesis from benchmark evidence, provider guidance, and software-agent measurement practice. The central design choice is that Relay should score **artifact quality**, **context quality**, **execution quality**, **audit quality**, **routing quality**, and **split-task effects** separately, then combine them only for ranking and triage, not for single-score decision making in v1. That recommendation follows from the repeated benchmark finding that one aggregate pass rate hides the real reason a coding run succeeded or failed. citeturn13view3turn13view4turn27view0turn27view1turn21view0

### Prompt-quality metrics

The most predictive prompt-quality metrics for Relay are the ones that measure whether the executor brief is **actionable without guesswork**.

The first is **instruction completeness**: every task atom should have executable instructions, a validation command, and an acceptance criterion. This is strongly supported by provider guidance to define success criteria specifically and measurably, plus evidence that instruction-following quality matters for code tasks. citeturn21view1turn15view2

The second is **ambiguity burden**: count unresolved ambiguous, incomplete, or inconsistent statements in the Planner handoff before packetization, and count any that survive into the brief. Orchid shows that ambiguity consistently degrades code-generation performance, while HumanEvalComm shows that ambiguous, inconsistent, and incomplete task descriptions lower pass rates and that communication-oriented clarification improves outcomes. For Relay, this should become a Planner-side assessment metric rather than something delegated to the executor. citeturn15view1turn34search4

The third is **repo-grounding density**: the share of executable instructions that point to grounded repository facts, source files, functions, tests, or cited references rather than abstract intent alone. This is partly synthesis, but it is also motivated by RACE-bench’s finding that agents do better on high-level intent than on turning intent into concrete implementation steps. citeturn13view4

The fourth is **format and noise quality**: spelling and grammar defects, invalid example pairs, inconsistent style, unnecessary URLs or interrogative noise, and missing standard structure. An empirical study of prompt quality across code benchmarks found that spelling, grammatical cleanup, standard JavaDoc or docstring structure, and prompt consistency can improve measured code-generation performance, while noisy tokens and malformed examples degrade benchmark quality. citeturn37view0turn37view1turn37view2

The fifth is **structure adherence**: can the packet and rendered brief be represented with deterministic structured fields instead of prose blobs? Provider guidance on structured outputs exists because schema-backed artifacts reduce omission and invalid-value errors. Relay should use this principle for canonical packets and any scorecards. citeturn31view0

The sixth is **correction-loop pressure**: average number of executor retries or post-run revisions required per task family. This is an outcome-linked prompt metric: if a prompt shape systematically increases revision loops, it is not actually clear enough, even if it looks elegant on inspection. Anthropic’s evaluation framing around multi-turn trials and outcomes supports tracking this kind of process signal. citeturn21view0

### Context-efficiency metrics

Relay’s context-efficiency metrics should reward **useful signal density**, not minimal tokens as an end in itself. Anthropic explicitly frames context as finite and diminishing in utility; long-context research shows retrieval and reasoning degrade when relevant information is badly positioned or diluted; and coding-agent studies show that added repository context can be harmful when it is generic rather than targeted. citeturn5view2turn36search0turn30view0

The most important context metrics are:

**Context precision.** Of the files, snippets, or instructions sent to the executor, what fraction were actually relevant to the final change? ContextBench makes precision a first-class metric for coding agents, and it reports that models often favor recall over precision. citeturn13view3turn33view0

**Context recall.** Of the relevant files, snippets, tests, or constraints needed to solve the task, what fraction were present in the brief or attached execution-required context? ContextBench is the clearest public foundation for this metric. citeturn13view3turn33view1

**Utilization gap.** What percentage of supplied context was actually used in the trace, versus merely present? ContextBench highlights a substantial gap between explored and utilized context; Relay should explicitly track this because a context tier can be present yet ineffective. citeturn13view3

**Tier leakage.** How often does review-only, audit-only, or reference-only context leak into executor briefs without deliberate promotion? This is primarily Relay synthesis, but it directly aligns with the evidence that extra context has diminishing returns and can hurt performance when it is not targeted. citeturn5view2turn30view0

**Optional-context yield.** Compare resolved rate, validation quality, and cost when optional context is included versus excluded. Repoformer and AGENTS.md work both support selective inclusion over indiscriminate inclusion. Repoformer argues that unconditional retrieval is often unnecessary or harmful, and AGENTS.md finds that context files do not generally improve task success while increasing cost by more than 20% on average. citeturn19search15turn30view0

**Position sensitivity.** Measure whether moving relevant context earlier or later changes success. “Lost in the Middle” shows that relevant information in the middle of long contexts is reliably harder for models to use. This is especially relevant for brief rendering order. citeturn36search0turn36search2

### Execution-quality, audit-quality, routing-quality, and split-task metrics

For **execution quality**, Relay should track resolved rate, patch apply rate, validation pass rate, full-test pass rate, regression rate, correction-loop count, file-touch count, time-to-green, and partial progress. SWE-EVO’s proposed Fix Rate is useful here for tasks where full resolution is too coarse because it captures partial progress on long-horizon work. SWE-CI adds a maintainability perspective by tracking functional correctness across time rather than only at the current patch. citeturn27view0turn27view1

For **audit quality**, Relay should track scope-drift rate, undocumented assumption rate, missing-lineage rate, rollback-unsafety flags, audit-to-validation disagreement rate, and post-acceptance defect discoveries. The direct public evidence here is thinner than for execution metrics, but studies on weak patch validation make a strong case that audit must not be a cosmetic summary after tests pass. citeturn16view0

For **routing quality**, Relay should track success by recommended tier, cost per accepted run, latency per accepted run, router confidence calibration, operator override rate, escalation precision, escalation recall, and regret against an oracle router. RouteLLM, FrugalGPT, and SCORE all frame routing as a cost-quality tradeoff rather than a fixed-model contest, and UCCI shows why calibration must be measured explicitly because raw confidence thresholds drift across workloads. citeturn28view1turn29view1turn28view2turn28view3

For **split-task evaluation**, Relay should track split benefit delta, split cost delta, split latency delta, atom completion rate, merge-conflict rate, dependency-violation rate, and reroute frequency after splitting. The evidence base here is mixed, so Relay should evaluate splitting empirically rather than assuming it helps. Google’s agent-scaling work found that multi-agent or multi-part coordination can help strongly parallelizable tasks but can degrade sequential tasks, in some cases substantially. That makes “should split” a measurable hypothesis, not a design axiom. citeturn10search0turn10search2

### A scoring model for executable clarity per token

“Executable clarity per token” is not a standard benchmark metric, so the scoring model below is explicitly a **Relay synthesis**. It should be treated as an internal comparative score, not a public benchmark number.

I recommend this formulation:

**Task-ready quality score**  
= 0.25 instruction completeness  
+ 0.20 ambiguity penalty inverse  
+ 0.20 repo-grounding density  
+ 0.15 validation specificity  
+ 0.10 structure adherence  
+ 0.10 context precision

Each component should be normalized to a 0–100 scale. The weights reflect the strongest available evidence: ambiguity and grounding failures consistently matter; validation specificity matters because weak validation overstates success; and structure plus context precision improve reliability and efficiency. citeturn15view1turn13view4turn16view0turn31view0turn13view3

**Adjusted token cost**  
= execution_required_tokens + 0.5 × execution_optional_tokens

The half-weight on optional context reflects that optional tokens are not automatically bad, but they should count against efficiency. This avoids the trap of optimizing only for raw brevity. Anthropic’s context-engineering guidance explicitly argues for the smallest high-signal context, not the shortest possible one. citeturn5view2

**Executable clarity per token**  
= task-ready quality score ÷ adjusted token cost

In practice, Relay should compare this score **within the same task family and renderer profile**, because absolute cross-task comparisons will be noisy. In v1, I would report ECPT on dashboards and use it for ranking plus regression detection, but I would **not** make it a hard blocker until it shows stable correlation with accepted outcomes on your own workload. That recommendation follows the general regression-testing lesson that prompt behavior is slice-specific and non-deterministic. citeturn22view0

## Regression tests, trace-based evaluation, and telemetry

Relay needs both **fixture-based regression tests** and **trace-based real-run evals**. Public agent-eval guidance strongly supports this split: fixtures provide stable comparison points, while traces reveal why a real run failed or became expensive. citeturn21view0turn5view1

### Fixture-based regression strategy

Fixture evals should use a versioned corpus of Planner handoffs and expected canonical packets, expected executor briefs, and expected scorecards. The core idea is simple: when a packetizer, renderer, or scoring rubric changes, Relay should re-run the same fixtures and compare structured outputs, semantic diffs, and grades to the saved baseline. OpenAI’s regression cookbook and Anthropic’s eval guidance both support task-oriented regression testing built from explicit schemas and grading criteria. citeturn21view2turn21view1

A good Relay fixture set should include at least four families of cases: high-confidence “golden” handoffs that should always pass, edge cases with ambiguity or missing grounding that should fail or warn, redaction and lineage cases, and context-tier cases that verify correct inclusion or exclusion of execution-required, optional, review-only, and audit-only context. The regression-testing literature on evolving LLM APIs adds an important detail: regression suites should be analyzed by **data slice**, not only by aggregate average, because regressions are often slice-specific. citeturn22view0turn23search0

Fixture tests for handoff → packet → brief rendering should include at least these assertions: schema validity, required-field presence, deterministic output hash, semantic equivalence to expected content, token counts by tier, absence of prohibited content, and expected routing recommendation bands. None of this requires freeform judging if Relay keeps its artifacts structured, which is another reason to avoid parsing prose when structured fields are possible. citeturn31view0turn21view1

### Trace-based real-run evaluation strategy

Trace-based evals should instrument every production-like run as a structured trace. OpenTelemetry already provides the right primitive vocabulary: traces, spans, attributes, events, links, and statuses. The Agent Observability Standard usefully maps agent turns, tool calls, memory retrievals, and logical phases onto hierarchical spans. Relay can adopt that model without exposing hidden reasoning or storing secrets. citeturn25view1turn26view1turn26view2

For each real run, Relay should capture: artifact IDs and hashes, token counts by context tier, selected renderer profile, router recommendation and confidence, operator decision, executor type, files touched, commands run, validation results, audit findings, and final disposition. Span attributes should include redacted or hashed prompt and context identifiers, not raw secrets or hidden chain-of-thought. The observability standards explicitly support attaching inputs, outputs, tool data, model metadata, timestamps, and hierarchical task phases as span data. citeturn25view1turn26view1turn26view2

Real-run evals should also sample **repeated trials** on a subset of tasks. Anthropic’s agent-eval definitions emphasize trials because model outputs vary between runs, and the regression-testing literature emphasizes handling non-determinism explicitly rather than pretending it does not exist. citeturn21view0turn22view0

A practical Relay trace rubric should attach one labeled root cause to each failed run, plus optional secondary causes. That makes dashboards and threshold tuning workable. The root-cause taxonomy should use the failure classes described earlier: packetization, rendering, prompt, context, executor or model, validation, and audit. RACE-bench and ContextBench are especially relevant here because they show that intermediate reasoning and context signals expose failures that final success labels hide. citeturn13view4turn13view3

### Dashboard and telemetry recommendations

Relay’s dashboard should center on **funnel visibility** rather than a single success score. At minimum, I recommend five panels.

The first is an **artifact funnel**: handoffs created, packets validated, briefs rendered, dispatches approved, runs completed, validations passed, audits accepted, revisions required, and rejections. This directly reflects Relay’s pipeline and makes drop-off locations visible. citeturn21view0

The second is a **quality panel**: instruction completeness, ambiguity burden, grounding density, context precision and recall, utilization gap, validation specificity, and ECPT by task family. This is where planner and renderer regressions become visible before outcome rates collapse. citeturn15view1turn13view3turn37view0

The third is a **routing panel**: recommended tier mix, accepted quality by tier, cost per accepted run, latency, operator override rate, and regret versus oracle. Routing papers consistently frame the objective as cost-quality optimization under constraints, so the right visual is a frontier, not a single line chart. citeturn28view1turn29view1turn28view2

The fourth is a **validation and audit panel**: pass rate on standard checks versus stronger checks, suspicious patch rate, audit disagreement rate, scope-drift findings, and rollback-safety findings. This is where Relay avoids the benchmark mistake of declaring victory too early. citeturn16view0

The fifth is a **drift panel**: metric movement by slice, renderer version, repository family, task type, and model tier. The regression-testing literature strongly supports slice-based monitoring because prompt regressions are often not uniform. citeturn22view0

## Benchmark landscape and limitations

No single public benchmark is sufficient for Relay. Relay is evaluating a structured handoff system, not just a raw coding agent. So the right external benchmark strategy is a **portfolio**, with each benchmark used for the failure mode it actually measures best. citeturn14view0turn13view3turn13view4turn27view0turn27view1

**SWE-bench** remains useful for end-to-end issue-resolution evaluation because it uses real GitHub issues and repositories, but it is limited to 2,294 problems across 12 popular Python repos and was never designed to evaluate Relay’s planner, packetization, or audit layers. It is strongest as an execution benchmark, not a handoff-contract benchmark. citeturn14view0

**SWE-bench Verified** improved reliability by filtering to a 500-instance human-validated subset, but its limitations now matter a great deal. OpenAI said in February 2026 that SWE-bench Verified no longer reliably measures frontier coding capabilities because of flawed narrow or wide tests and contamination, and an ICSE 2026 study found overstated correctness even within plausible patches. Relay should still use Verified for continuity and historical comparability if desired, but not as its main source of truth. citeturn13view0turn13view1turn17view0turn16view0

**RACE-bench** is unusually relevant to Relay because it evaluates repository-level feature addition tasks and includes structured intermediate reasoning ground truth for issue understanding, relevant files, implementation tasks, and step decomposition. That makes it one of the best public references for diagnosing planner and packet quality beyond final patch correctness. Its limitations are that it is newer, focused on feature addition, and still narrower than Relay’s full artifact lifecycle. citeturn13view4

**ContextBench** is the strongest public benchmark match for Relay’s context-tier ideas because it explicitly measures context recall, precision, efficiency, and the gap between explored and utilized context. Its limitation is that it focuses on context retrieval behavior, not on audit packets, operator routing, or full product workflow decisions. citeturn13view3turn33view0

**RepoBench** and **RepoExec** are valuable for repository-level retrieval, cross-file context use, and executability. RepoBench is really an auto-completion and retrieval benchmark; RepoExec asks a more execution-oriented repository-level question and emphasizes executability plus functional correctness. Both are useful for context and repository grounding experiments, but both are still narrower than Relay because they do not evaluate planner-authored contracts, manual approval gates, or audit outcomes. citeturn35view0turn13view2

**SWE-EVO** and **SWE-CI** are important because they expose long-horizon failure modes hidden by one-shot issue benchmarks. SWE-EVO introduces long-horizon evolution tasks and a partial-progress Fix Rate, while SWE-CI evaluates maintainability through continuous integration history over months of commits. These are very relevant to Relay’s desire for stronger validation and audit outcomes, but the public evidence base here is still younger and smaller than for SWE-bench. citeturn27view0turn27view1

The benchmark limitations most relevant to Relay are therefore: contamination and public-data leakage; narrow or misaligned tests; incomplete validation coverage; black-box scoring that hides intermediate failure modes; single-issue bias; weak long-horizon maintainability coverage; and benchmark-task mismatch with Relay’s artifact pipeline. The safest design is to use public benchmarks for **external comparability**, fixture suites for **renderer correctness**, and trace-based evals for **real operational quality**. citeturn17view0turn16view0turn13view3turn13view4turn27view0turn27view1

## Recommended v1 priorities and open questions

Relay v1 should prioritize what is both high leverage and well supported by the evidence.

The first priority should be **canonical packet schemas, deterministic rendering, and artifact lineage**. This is the foundation for everything else, and it is the easiest area to hard-block safely because failures are objective. Structured-output guidance strongly supports schema-backed generation for reliable downstream handling. citeturn31view0

The second priority should be **fixture-based regression tests** for handoff → packet → brief. This gives Relay stable protection against accidental renderer regressions before model- or repository-level variance enters the picture. Official eval guidance from both OpenAI and Anthropic supports this development loop. citeturn5view1turn21view1turn21view2

The third priority should be **trace instrumentation and root-cause labeling**. Without traces, Relay will not be able to tell prompt failures from context failures or executor failures. OpenTelemetry and AOS give enough structure to implement this cleanly. citeturn25view1turn26view1

The fourth priority should be **prompt and context scorecards** focused on ambiguity, completeness, grounding, validation specificity, and context precision or recall. These metrics have a stronger evidence base than more speculative aesthetic metrics. citeturn15view1turn37view0turn13view3

The fifth priority should be **advisory routing evaluation**, not automated routing. Build the cost-quality frontier dashboards, confidence calibration, and operator override feedback loop now, but preserve the manual gate exactly as you specified. Routing evidence is useful, but thresholding remains workload-sensitive. citeturn28view1turn29view1turn28view3

The sixth priority should be **stronger validation sampling**, including differential checks or expanded test suites for accepted runs. The public evidence is now too strong to trust narrow validation blindly. citeturn16view0turn17view0

Open questions remain. The biggest one is how strongly ECPT will correlate with Relay’s own accepted outcomes; that must be calibrated on your workload. Another is how often packet assessment findings can be translated into objective warning signals without becoming de facto executable scope. A third is how much human audit load is economically sustainable while still catching plausible-but-wrong patches. A fourth is whether split-task recommendations can be made reliably enough for anything beyond warnings in v1; the evidence suggests task structure matters a lot, but the exact decision rule will be local to Relay’s workloads. Finally, public evidence on audit-packet scoring is still thin, so Relay should assume that audit metrics will need the most iteration and human calibration. citeturn22view0turn10search0turn10search2turn16view0

## Annotated bibliography

**SWE-bench.** The original benchmark established real-world issue resolution as a software-engineering evaluation problem with 2,294 tasks across 12 Python repositories. It is foundational for execution-quality benchmarking, but it does not measure Relay’s planner, packet, or audit layers. citeturn14view0

**SWE-bench Verified.** The human-validated 500-task subset improved benchmark reliability and became the dominant public coding benchmark for a time. It is still useful for historical continuity, but not sufficient as Relay’s main truth source. citeturn13view0turn13view1

**Why SWE-bench Verified no longer measures frontier coding capabilities.** OpenAI’s 2026 analysis is important because it documents narrow tests, wide tests, and contamination in a benchmark that many teams still treat as definitive. Relay should absorb this lesson directly into its validation and benchmark strategy. citeturn17view0

**Are “Solved Issues” in SWE-bench Really Solved Correctly?** This ICSE 2026 paper is one of the strongest empirical arguments for not equating test passing with true correctness. Its differential patch testing results are especially relevant to Relay’s audit layer. citeturn16view0

**ContextBench.** This is the most directly relevant public benchmark for Relay’s context-tier design because it measures context recall, precision, efficiency, and explored-versus-utilized gaps in coding-agent runs. citeturn13view3turn33view0

**RACE-bench.** Particularly useful for Relay because it adds structured intermediate reasoning ground truth and exposes where agents fail between high-level intent and concrete implementation steps. It is a strong reference for packetization and reasoning diagnostics. citeturn13view4

**RepoBench and RepoExec.** These are useful for repository-level retrieval, completion, and executability analysis. They are good complements for context-grounding experiments, though they remain narrower than Relay’s full handoff pipeline. citeturn35view0turn13view2

**Orchid and HumanEvalComm.** These two works support Relay’s insistence on ambiguity handling before execution. Orchid shows ambiguity degrades code-generation performance; HumanEvalComm shows that incomplete, inconsistent, or ambiguous requirements reduce pass rates and that clarification behavior matters. citeturn15view1turn34search4

**Prompt-quality study of benchmark prompts.** This empirical study is valuable because it ties concrete prompt defects such as spelling issues, noisy tokens, invalid examples, and inconsistent structure to benchmark quality and model performance differences. It is strong evidence for Planner-side prompt scorecards. citeturn37view0turn37view1turn37view2

**Anthropic prompt engineering and context engineering guidance.** These documents are not benchmark papers, but they are high-quality provider guidance on defining success criteria, structuring prompts, minimizing ambiguity, and treating context as a finite resource. They align closely with Relay’s structured-handoff design. citeturn6view0turn5view2turn21view1

**OpenAI evals and structured outputs guidance.** These documents are important for Relay because they support schema-backed artifacts, iterative eval loops, regression testing, and explicit testing criteria rather than loosely judged behavior. citeturn5view1turn31view0turn21view2

**FrugalGPT, RouteLLM, SCORE, and UCCI.** Together, these routing papers provide the best foundation for Relay’s routing-quality metrics. They show that cost-quality routing is valuable, but only when the routing signal is well calibrated and evaluated against cost and latency constraints. citeturn28view1turn29view1turn28view2turn28view3

**OpenTelemetry and the Agent Observability Standard.** These are the best practical references for implementing trace-based evals with hierarchical task spans, attributes, timestamps, and operator-visible lineage while avoiding opaque black-box debugging. citeturn25view1turn26view1turn26view2