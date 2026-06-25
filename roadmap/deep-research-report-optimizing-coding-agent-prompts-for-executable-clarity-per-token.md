# Optimizing Coding-Agent Prompts for Executable Clarity per Token

## Executive summary

Relay’s design goal is well aligned with the strongest available evidence: the best coding-agent prompts are not the shortest prompts, but the prompts that create the clearest executable contract with the least irrelevant context. Across controlled agent studies, benchmark papers, and current vendor guidance, the recurring pattern is a three-layer structure: a stable instruction contract, targeted repository-grounded facts, and explicit verification/done criteria. Performance improves when agents are given clear persistence/tool-use guidance, concise but unambiguous scope boundaries, targeted retrieval rather than raw context dumps, and validation loops that define what counts as success. Performance usually degrades when prompts mix facts with assumptions, bury key constraints in narrative text, overstuff edge cases, or let the agent search exhaustively without a stopping rule. citeturn21view0turn22view0turn20view0turn19view0turn17view0

The most directly relevant empirical result for Relay is that small prompt-structure changes can translate into large execution gains when they affect agent behavior rather than prose style. OpenAI reports that three simple agent reminders—persistence, tool use instead of guessing, and optional planning—raised its internal GPT-4.1 SWE-bench Verified score by close to 20%. In SWE-agent, interface and context-shaping choices materially changed outcomes: summarized search outperformed iterative search, a 100-line viewer outperformed both 30-line windows and full-file views, and keeping only the last five observations outperformed full history. These are all strong signals that “executable clarity per token” is primarily about behavioral affordances and context curation, not terseness alone. citeturn21view0turn20view0turn12view5

Relay should therefore separate planning from execution more sharply than many generic agent stacks do. The planner should produce a structured, auditable handoff that locks product decisions, scope, preserved behavior, validation commands, and stop conditions before dispatch. The executor brief should be a deterministic render of that handoff, with no product ambiguity left for the coding model to resolve. This recommendation is consistent with Plan-and-Act’s planner/executor split, OpenAI’s “planner” versus “workhorse” model distinction, and current agentic coding guidance that emphasizes explicit contracts, repository-specific guidance, and verifiable stopping conditions. citeturn14view2turn21view2turn29view0turn28view3

Where evidence is weakest is in isolating prompt text from the rest of the scaffold. Many benchmark gains come from the combination of prompting, tools, interfaces, search primitives, and context management rather than from text phrasing alone. Even recent analyses of the SWE-bench ecosystem find no single architectural pattern that always wins, and they note incomplete disclosure on leaderboards. Relay should treat prompt design as one layer in a measured system, with evals and trace-based audit as the source of truth. citeturn32view0turn10view8turn4view9

**Key findings.** *Empirical:* targeted context beats full context; explicit persistence/tool/verification contracts improve coding success; compression can preserve quality when it prunes irrelevant text, but implicit embedding-style compression can fail badly in multi-step software agents; exploration quality is a first-order determinant of repair quality. *Practitioner consensus:* structure prompts into distinct sections, keep repository guidance reusable and externalized, define what “done” means before execution, and tell agents how to react to blockers and risky actions. *Synthesis:* Relay should optimize for contract density, not prose brevity—every prompt token should either constrain behavior, ground the task, define validation, or reduce audit ambiguity. citeturn20view0turn15view1turn15view6turn19view0turn22view1turn21view4turn28view3

## What the evidence says

The clearest empirical evidence comes from coding-agent systems and repository benchmarks rather than generic prompt-engineering literature. SWE-agent showed that interface design alone can substantially improve software-repair performance without changing model weights, and its ablations are unusually relevant to Relay because they directly test context and action presentation. In that work, summarized search beat iterative search, a 100-line file window beat both smaller windows and full-file views, and “last 5 observations” beat “full history.” The authors explain that iterative search tempts agents into exhaustively paging through matches, which can consume cost budget and context window so badly that it performs worse than giving the agent no additional search tool at all. citeturn12view1turn20view0turn20view1

That result maps neatly onto Relay’s problem. A good executor brief should not simulate an IDE transcript or hand the model a giant issue packet. It should front-load only the highest-signal task contract, then direct the model toward targeted retrieval. Anthropic’s context-engineering guidance makes the same argument in more general terms: tokens have diminishing marginal value, the goal is the smallest set of high-signal tokens that maximizes the desired outcome, and agents should often use just-in-time retrieval via references like file paths, links, or queries instead of preloading everything. Anthropic also recommends distinct prompt sections and warns against both brittle over-specification and vague, context-assuming high-level instructions. citeturn22view0turn22view1turn23view4

Current vendor guidance is surprisingly consistent on what makes agent prompts work. OpenAI’s GPT-4.1 guidance says that three simple reminders—keep going until resolved, use tools instead of guessing, and plan if helpful—materially improved agentic coding performance, and the same guide recommends a top-level rules section, explicit workflow steps, and checking for conflicting instructions because GPT-4.1 follows prompts more literally than earlier models. Anthropic similarly advises clear and direct instruction, XML or section tags for mixed-content prompts, and canonical examples rather than laundry lists of edge cases. Google’s Gemini 3 guidance also pushes toward direct, clear instructions and notes that verbose “old-style” prompt engineering can cause over-analysis. citeturn21view0turn27view3turn9view1turn9view4turn21view3

For what should always be in a coding-agent brief, the strongest convergence is around six items: a precise objective, repository grounding, constraints, a concrete workflow or plan, validation commands, and a definition of done. OpenAI’s Codex guidance says reusable repo guidance should include repo layout, run/build/test/lint commands, conventions, constraints, and “what done means and how to verify work.” Codex’s `/goal` guidance similarly emphasizes one objective, one stopping condition, what not to change, how to validate progress, and what artifacts prove completion. Anthropic’s multi-context workflow guidance independently recommends setting up tests and setup scripts early, then iterating against structured task state. citeturn28view3turn29view0turn18view5

For what should be excluded or demoted to on-demand lookup, the evidence is equally consistent. Anthropic warns against bloated tool sets, vague “shared context” assumptions, and prompts stuffed with edge cases. SWE-agent’s ablations indicate that raw full-file or full-history inclusion is often worse than targeted slices. Lost in the Middle shows why: models do not use long context uniformly, and retrieval quality degrades when relevant information sits in the middle of long sequences. In practice, that means Relay should inline the contract and the minimum working context, but push large logs, wide repo maps, long historical chat, and exhaustive design rationale into referenced attachments or lookup handles. citeturn23view0turn20view0turn16view0

The best available evidence also supports preserving a hard distinction among immutable goals, implementation guidance, verified facts, assumptions, non-goals, validation, stop conditions, acceptance criteria, and audit priorities. OpenAI’s message-role guidance explicitly separates stable instructions from dynamic inputs. Anthropic recommends distinct prompt sections for background, instructions, tool guidance, and output description. RACE-bench is relevant here because it decomposes successful repository work into issue understanding, file localization, implementation tasks, and step decomposition rather than treating patch correctness as a black box. That is not a prompt schema by itself, but it is strong evidence that these categories are operationally meaningful. citeturn21view4turn22view1turn18view9

## Recommended Relay prompt architecture

**Recommended structure for Planner handoffs.** Relay’s planner handoff should be a structured artifact first and prose second. The handoff should contain: artifact metadata; repo/ref/commit; required source files and directories; verified current-state summary; immutable goal statement; explicit preserved behaviors and interfaces; non-goals; planner-approved implementation plan; validation commands; acceptance criteria; stop conditions and blocker protocol; and audit priorities. This is the best fit to current evidence because it mirrors the categories that both coding platforms and new software-agent benchmarks treat as high value: repo layout and run/test commands, constraints and “done means,” issue understanding, file localization, implementation tasks, decomposition, and patch verification. citeturn28view3turn18view9turn32view0

The most important design choice is that the planner handoff should clearly label epistemic status. Relay should distinguish **verified repo facts**, **planner decisions**, **open assumptions allowed for execution**, and **questions that remain unresolved and therefore block execution**. This matters because coding models are now highly literal instruction followers: GPT-4.1 guidance explicitly says newer models infer less than their predecessors, while Anthropic emphasizes that agents should not be expected to resolve ambiguous tool or context choices better than humans if the design itself is ambiguous. A handoff that blends observed fact with desired end state invites implementation drift. citeturn27view3turn23view2

For long-running or multi-window work, stable repository guidance should live outside the task handoff in a reusable repo-level instruction file, such as AGENTS.md or an equivalent internal Relay artifact. OpenAI’s Codex docs say AGENTS.md is the best place for reusable repository guidance and that a short, accurate file is better than a long vague one. The AGENTS.md project itself defines the format as a predictable place for setup commands, tests, and conventions so that per-task briefs do not have to restate them. Relay should therefore put slow-changing “how this repo works” material in a canonical policy file and keep the planner handoff focused on the specific approved change. citeturn21view5turn24search0

**Recommended structure for executor briefs.** The executor brief should be a deterministic render of the planner handoff optimized for execution rather than for human authoring. It should begin with the immutable mission contract, immediately followed by hard boundaries, then verified repo facts, then the implementation steps already chosen by the planner, then validation and acceptance checks, and finally a short blocker/audit block. This order reflects the evidence that long-context relevance is sensitive to placement and that the most important behavioral constraints should be both explicit and easy to retrieve. Anthropic recommends structured sections with XML or markdown boundaries; OpenAI recommends top-level response rules plus explicit workflow steps; and Lost in the Middle suggests that burying key constraints in the body of a long prompt creates avoidable retrieval risk. citeturn22view1turn27view2turn16view0

**Context inclusion and exclusion framework.** Relay should classify candidate context into four buckets. *Inline required:* immutable goals, preserved contracts, non-goals, exact file/path anchors, exact commands, and pass/fail criteria. *Inline summarized:* current-state summary, relevant prior attempts, and repo facts that matter immediately. *Reference only:* large docs, long logs, broad architectural background, full repo trees, and lower-probability edge-case material. *Exclude:* duplicated instructions, stale transcripts, unresolved product debates, and sensitive data. This follows Anthropic’s smallest-high-signal-token principle, just-in-time retrieval guidance, and the observed harms of full history and exhaustive search behavior. citeturn22view0turn23view4turn20view0

**Prompt section taxonomy.** A Relay brief should use a stable taxonomy that the renderer never changes casually: **Mission**, **Boundaries**, **Verified facts**, **Current state**, **Implementation steps**, **Validation**, **Acceptance**, **Stop and report**, **Audit focus**. The taxonomy matters because it reduces semantic bleed. “Mission” is what must be true at the end. “Boundaries” is what must not change. “Verified facts” are grounded observations. “Current state” is the working snapshot. “Implementation steps” are planner-approved means, not new product decisions. “Validation” is what to run. “Acceptance” is what success looks like. “Stop and report” covers blockers and risky actions. “Audit focus” tells reviewers what matters most. This structure is my synthesis, but it is directly informed by the sectioning patterns recommended by Anthropic and OpenAI and by the task decomposition categories used in RACE-bench. citeturn22view1turn21view4turn18view9

## Model-tier prompt profiles

**Cheap or mechanical coding models.** These models should receive the most constrained briefs and the least discretionary ambiguity. OpenAI’s model guidance explicitly frames lower-latency GPT models as “workhorses” that are best for straightforward, well-defined execution, while Gemini 3 says direct, clear prompts work better than verbose prompt engineering. For this tier, Relay should provide exact file paths, exact interfaces to preserve, explicit ordered steps, exact commands to run, and narrow stop conditions. Do not rely on inference of product intent, implicit scope, or “reasonable defaults” unless the planner has encoded them explicitly. citeturn21view2turn21view3

**Mid-tier implementation models.** This is the default Relay executor profile for routine repository changes. The brief can be moderately compact as long as it includes the hard contract, planner-approved steps, and validation loop. OpenAI’s coding guidance suggests that well-specified prompts plus completeness and verification contracts often matter more than higher reasoning settings, and Codex recommends medium reasoning effort as a good all-around interactive coding default. For this tier, Relay can summarize current state more aggressively than for cheap models, but it still should not leave product or scope decisions open. citeturn25view2turn25view1

**High-reasoning semantic implementation models.** These models are useful when the problem has ambiguity, many interacting artifacts, or a large evidence surface. OpenAI’s reasoning guidance recommends reasoning models for ambiguity, needle-in-a-haystack retrieval, multistep planning, and code review; Anthropic similarly recommends starting with the most capable model when tasks require nuanced understanding and high-autonomy coding. For this tier, Relay can include richer current-state synthesis and more compressed references because stronger models are better at retrieving relevance from complex evidence. But Relay should still preserve the planner/executor split; strong models are a better place to resolve technical uncertainty, not to invent product behavior. citeturn10view7turn26view0turn31view0

**Reviewer or auditor models.** Reviewer prompts should differ from executor prompts. OpenAI’s Codex guidance says that review mode should prioritize findings, risks, behavioral regressions, and missing tests, present findings first ordered by severity with file/line references, and treat summaries as secondary. OpenAI also notes that reasoning models are particularly effective for reviewing and improving large amounts of code. Relay should therefore generate reviewer briefs that include the intended change contract, the changed files or diff summary, the claimed validation evidence, unresolved assumptions, and explicit audit priorities. The reviewer’s job is not to continue implementing; it is to test whether the executor honored the contract. citeturn26view4turn26view5turn26view0

Across all tiers, Relay should avoid provider-specific prompt superstition. Anthropic’s model-selection guide recommends starting either with a fast model for straightforward tasks or with the strongest model for complex ones, then optimizing prompts and downgrading only after evals confirm acceptable performance. That is a good meta-policy for Relay: maintain one canonical packet, derive different brief renderings by tier, and validate each profile against the same task suite. citeturn31view0turn4view9

## Context compression, anti-patterns, and failure modes

The strongest compression pattern is not “summarize everything,” but “compress only what is safe to compress and retrieve the rest on demand.” Anthropic recommends just-in-time retrieval, structured note-taking, compacted histories that clear raw tool results, and subagents that explore deeply but return condensed summaries. Its guidance also stresses progressive disclosure: agents should assemble understanding layer by layer and keep only what is necessary in working memory. Relay should adopt that principle directly by turning canonical packets into short executor briefs plus stable handles to richer context. citeturn22view2turn22view4turn23view4

There is also new empirical support for safe pruning. SWE-Pruner reports 23–38% token reduction on SWE-bench Verified while keeping success rates nearly unchanged and reducing interaction rounds by 18–26%. SWE-Compressor reports a 57.6% solved rate on SWE-bench Verified under a bounded context budget and outperforms relevant baselines. These results suggest that careful pruning and bounded-memory mechanisms can improve efficiency without sacrificing execution quality, especially when they preserve line-level relevance and the agent’s working set. citeturn15view1turn15view3turn15view4

The converse risk is equally important. Implicit context compression for software-engineering agents—compressing observations into dense embeddings rather than explicit text—showed efficiency gains but failed to improve resolved issues and significantly degraded multi-step SWE-bench Verified performance. The paper attributes this to reconstruction errors, hallucinated paths or URLs, and failure to preserve information needed for future steps. For Relay, that is a strong warning against opaque compression that hides assumptions or destroys auditability. Compression should remain inspectable, textual, and attributable whenever it affects execution. citeturn15view6turn15view9

**Prompt anti-patterns that increase failure rates.** First, avoid exhaustive context dumps. SWE-agent’s full-file and full-history variants underperformed tighter alternatives, and Lost in the Middle shows why long contexts can degrade retrieval precisely where buried details matter. Second, avoid brittle pseudo-programming inside prompts. Anthropic explicitly warns against hardcoded brittle logic in system prompts. Third, avoid ambiguous tool menus and overlapping tools: Anthropic notes that bloated tool sets create failure because even humans cannot tell which tool should be used. Fourth, avoid edge-case laundry lists; Anthropic recommends diverse canonical examples instead. Fifth, avoid long polluted sessions; Claude Code guidance says long sessions filled with irrelevant context can reduce performance and that a fresh session with a more specific prompt often outperforms a long corrected one. citeturn20view0turn16view0turn22view1turn23view2turn23view0turn18view3turn5view11

**Failure modes around scope and unauthorized expansion.** The two biggest are overengineering and risky action drift. Anthropic documents that recent Claude coding models may create extra files, unnecessary abstractions, or flexibility not requested, and recommends prompts that explicitly demand simple, focused solutions. It also advises explicit confirmation rules for destructive or hard-to-reverse actions such as deleting files, force-pushing, or modifying shared systems. OpenAI’s coding guidance similarly forbids destructive git operations without approval and tells agents to stop immediately if they notice unexpected changes not made by them. Relay should encode these not as general “be careful” prose, but as explicit hard-boundary lines in the brief. citeturn18view4turn18view6turn28view0

**Failure modes around blockers and guessing.** Good briefs should teach the executor to stop and report with evidence rather than guess silently. OpenAI’s coding guidance says every rollout should conclude with either a concrete edit or an explicit blocker plus a targeted question, and plans should end with items marked done, blocked, or cancelled. Anthropic’s coding prompt guidance says that if a task is unreasonable, infeasible, or the tests are incorrect, the model should inform the operator rather than work around them. That is exactly the behavior Relay wants when the executor lacks product authority. citeturn28view0turn28view2turn28view4

**Validation and audit phrasing.** Validation commands should be concrete, runnable, and tied to success criteria. OpenAI recommends frequent testing, hidden-test awareness, and final reflection after visible tests pass; Codex `/goal` guidance says long-running work should specify commands or artifacts that prove progress and a verifiable stopping condition; and AGENTS.md guidance recommends encoding how to verify work. Relay should therefore phrase validation as “Run X; success means Y; if Z fails, stop/report or iterate as specified,” rather than “make sure it works.” Audit checks should name the specific regressions to inspect, such as preserved routes, input/output shape, migration compatibility, or absent scope creep. citeturn21view1turn29view0turn28view3

## Metrics, design recommendations, and example brief

**Metrics for executable clarity per token.** The metric that best fits Relay is not a single scalar but a family of eval-backed ratios. A useful top-line measure is **Executable Clarity per Token = weighted execution quality ÷ input prompt tokens**, where execution quality combines task success, boundary compliance, validation quality, blocker honesty, and auditability. This formulation is a synthesis, but it is grounded in current trace-based eval practice and recent repository-agent benchmarks: OpenAI defines agent evals as prompt → captured run with trace and artifacts → checks → score; RACE-bench jointly evaluates patch correctness and intermediate reasoning quality; SWE-Explore measures coverage, ranking, and context efficiency; and RepoReason adds white-box cognitive metrics such as reading load, simulation depth, and integration width. citeturn4view9turn18view9turn19view0turn18view10

For Relay’s dashboard, the most useful operational metrics are: **goal fidelity** (did the patch satisfy acceptance criteria), **boundary compliance** (did it avoid forbidden scope changes), **validation completeness** (did it run the required checks and interpret failures correctly), **blocker honesty rate** (did it report infeasibility instead of fabricating progress), **audit sufficiency** (can a reviewer reconstruct what facts, commands, and outputs justified the change), and **context efficiency** (success achieved per prompt token and per retrieval token). Repo exploration metrics should also be tracked because retrieval quality strongly correlates with downstream repair behavior in SWE-Explore. citeturn19view0turn4view9turn18view10

**Concrete Relay design recommendations.** First, make the canonical packet the source of truth and the executor brief a pure render, not a second reasoning step. Second, split packet fields into immutable contract, verified facts, allowed assumptions, and references. Third, move slow-changing repo instructions into an AGENTS.md-like repository policy file. Fourth, generate tier-specific briefs from one packet, with increasing compression and discretion only as model capability rises. Fifth, require an explicit blocker contract and a verifiable stopping condition in every brief. Sixth, log trace-level audit artifacts: packet version, rendered brief, files read, commands run, validation outputs, and reviewer findings. Seventh, treat prompt changes like code changes: store them in code, test them with fixtures, and ship them through eval gates. citeturn21view4turn21view5turn29view0turn4view9

**Example optimized coding-agent brief structure.** The template below is my synthesis for Relay, informed by the structured-section patterns recommended by Anthropic and OpenAI, the repository-guidance conventions around AGENTS.md, and the task decomposition used in software-agent benchmarks. It is not copied from any single source. citeturn22view1turn21view4turn28view3turn18view9

```xml
<mission>
  <objective>
    Implement exactly the planner-approved change described below.
  </objective>
  <immutable_goal>
    [One-sentence end state]
  </immutable_goal>
  <preserve>
    [Routes/APIs/behaviors that must remain unchanged]
  </preserve>
  <non_goals>
    [Explicit exclusions]
  </non_goals>
</mission>

<boundaries>
  <allowed_files>
    [Exact files/directories to edit if known]
  </allowed_files>
  <forbidden_changes>
    [Examples: schema changes, route renames, refactors outside scope]
  </forbidden_changes>
  <risk_actions_require_stop>
    [Examples: destructive git ops, deletes, shared-system changes]
  </risk_actions_require_stop>
</boundaries>

<verified_facts>
  <repo>
    repo=[name] ref=[branch/tag] commit=[sha]
  </repo>
  <grounded_observations>
    - [Fact from planner/repo inspection]
    - [Fact from planner/repo inspection]
  </grounded_observations>
</verified_facts>

<current_state>
  [Short summary of the relevant implementation and defect/feature state]
</current_state>

<implementation_steps planner_decided="true">
  <step>...</step>
  <step>...</step>
  <step>...</step>
</implementation_steps>

<executor_rules>
  <investigate_before_editing>
    Read relevant files before editing. Use tools rather than guessing.
  </investigate_before_editing>
  <scope_control>
    Do not infer new product behavior. If the requested result requires a product decision
    not already encoded here, stop and report a blocker.
  </scope_control>
  <blocker_policy>
    If blocked, report:
    - exact blocker
    - evidence observed
    - smallest decision/input needed to proceed
  </blocker_policy>
</executor_rules>

<validation>
  <command>...</command>
  <command>...</command>
  <success_means>
    [Concrete pass condition]
  </success_means>
  <if_validation_fails>
    [Iterate / stop / report rule]
  </if_validation_fails>
</validation>

<acceptance>
  - [User-visible acceptance criterion]
  - [Behavior-preservation criterion]
  - [Test/lint/build criterion]
</acceptance>

<audit_focus>
  - [What reviewer should verify first]
  - [Known regression risk]
  - [Any assumption intentionally exercised]
</audit_focus>

<references>
  - [Pointer to AGENTS.md or repo policy]
  - [Pointer to larger docs/logs only if needed]
</references>
```

This structure is intentionally contract-heavy at the top, because high-signal constraints are more valuable than prose explanation in execution prompts. It also keeps product decisions out of the executor layer, gives the model explicit permission to stop when blocked, and makes validation part of the task rather than an afterthought. Those are the features most likely to improve execution quality, scope discipline, and audit success across model tiers. citeturn21view1turn28view0turn29view0turn22view0

## Evidence gaps and annotated bibliography

The main evidence gap is causal isolation. There is now strong evidence that coding-agent performance depends heavily on how instructions, tools, search, edit affordances, and context are packaged together, but there is still relatively little clean A/B evidence that isolates one prompt-section choice from all tooling and scaffold effects. The field is also benchmark-sensitive: SWE-bench has known dataset and evaluation limitations, newer benchmarks target intermediate reasoning or exploration quality, and leaderboard entries often lack full implementation transparency. Relay should therefore treat the recommendations in this report as evidence-backed design priors to be tested, not as universal laws. citeturn17view0turn32view0turn18view9turn19view0

A second evidence gap concerns long-horizon memory. There is promising work on safe pruning, compaction, notes, and bounded-memory agents, but much less mature evidence for opaque or learned compression in multi-step coding tasks. The current state of the evidence supports textual compaction plus retrieval, not hidden summarization that cannot be audited. citeturn15view1turn22view4turn15view9

A third gap is cross-model portability. Provider docs agree on the broad principles—clarity, structured sections, explicit validation, bounded scope, and retrieval over bloat—but they do differ on some details, such as prompt ordering preferences and how much explicit planning helps. Relay should therefore rely on schema-level invariants and measured render variants rather than a single fixed prose recipe. citeturn9view4turn21view3turn27view3

**Annotated bibliography.** *SWE-agent: Agent-Computer Interfaces Enable Automated Software Engineering* is the single most relevant empirical paper for Relay because it shows that interface and context-presentation choices measurably affect coding-agent results; its ablations directly support summarized search, bounded file views, and reduced history windows. citeturn12view1turn20view0

*OpenAI GPT-4.1 Prompting Guide* is the strongest current vendor source for agentic coding prompt structure. It contains concrete evidence that persistence, tool-grounding, and planning reminders materially improved internal SWE-bench Verified performance, and it explains why newer models need more explicit specification. citeturn21view0turn27view3

*OpenAI Prompt Guidance* is useful not because every embedded starter prompt should be copied, but because it distills production lessons about completeness contracts, verification loops, blocker handling, plan closure, review-mode formatting, and reasoning-effort tuning for coding agents. citeturn25view2turn28view0turn26view3

*OpenAI Reasoning Best Practices* gives the clearest public articulation of a planner/doer split: reasoning models handle ambiguity and strategy, cheaper GPT-style models handle well-defined execution. That maps neatly onto Relay’s planner-versus-executor principle. citeturn21view2

*Anthropic Effective Context Engineering for AI Agents* is the best practitioner document on managing context as a scarce resource. Its smallest-high-signal-token principle, distinct prompt sections, just-in-time retrieval, progress notes, and subagent summaries are all directly relevant to Relay’s “clarity per token” goal. citeturn22view0turn22view2turn22view4

*Anthropic Prompting Best Practices* and *Claude Code Best Practices* are valuable for concrete anti-patterns: overengineering, excessive thoroughness, risky actions without confirmation, test-overfitting, long polluted sessions, and the need for repository-grounded investigation before answering or editing. citeturn11view1turn11view5turn18view3turn28view4

*Lost in the Middle* is not coding-specific, but it remains one of the most important pieces of evidence against indiscriminate prompt bloat. It supports keeping key task contracts highly retrievable and warns against burying critical constraints in long context. citeturn16view0

*SWE-Pruner* and *Context as a Tool: Context Management for Long-Horizon SWE-Agents* provide the most relevant recent empirical evidence that selective pruning and bounded-memory strategies can reduce tokens substantially while preserving or improving coding-agent outcomes. They are useful support for Relay’s compression strategy, though both are recent and should be treated as promising rather than settled. citeturn15view1turn15view4

*On Problems of Implicit Context Compression for Software Engineering Agents* is the clearest cautionary paper on hidden compression risk. It shows why non-auditable compression can break multi-step software reasoning even when it appears efficient. citeturn15view6turn15view9

*RACE-bench* and *RepoReason* matter because they move evaluation beyond “did the final patch pass tests.” They support Relay’s need for auditable intermediate reasoning, traceable decomposition, and white-box diagnostics when briefs fail. citeturn18view9turn18view10

*SWE-Explore* is especially important for Relay because it shows that repository exploration quality—coverage, ranking, and context efficiency—strongly tracks downstream repair behavior. That is why prompt architecture should help the agent find the right code, not just restate the issue attractively. citeturn19view0

*AGENTS.md* and OpenAI’s AGENTS.md guidance are useful operational complements. They support the idea that stable repository instructions belong in a reusable, predictable artifact rather than being retyped into every task brief, which is exactly the separation Relay needs between persistent repo policy and per-task execution context. citeturn24search0turn21view5