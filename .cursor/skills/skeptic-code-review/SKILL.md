---
name: skeptic-code-review
description: Run skeptic sweeps on a code diff that is already in hand. Use when the diff and intent are gathered and need adversarial review. Do not use from Grok Build or Codex.
---

# Skeptic Code Review

**Do not use from Grok Build or Codex.** Grok uses bundled `/review`. Codex is paused from this repository. If the current client is Grok or Codex, stop here.

Every code review includes skeptic passes. The normal review checks that the change looks right; the skeptics assume it is broken — or that the findings are wrong — and try to prove it.

## The sweep loop

1. **Do the normal review first** so you understand the change's intent and structure. Write down its findings as a candidate list — each with file/line, the concrete problem, the evidence, and a proposed BLOCKING/NON-BLOCKING severity. If the normal review produced no findings, say so and skip the next step.
2. **Run a finding-skeptic pass over the normal review's findings.** Spawn a skeptic subagent (general-purpose subagent via the Task tool) whose only job is to refute them — first-pass findings are often shallow: they flag behavior the surrounding code already handles, report bugs that cannot actually fire, or demand checks that exist elsewhere. Give it the candidate findings, the full diff (or how to obtain it), the stated intent, and the workspace path. It returns one verdict per finding — CONFIRMED, UPGRADED, DOWNGRADED, or REMOVED. Verdicts that change or remove a finding are adopted only on concrete code evidence — "seems unlikely" is not a refutation; ambiguous evidence keeps the proposed severity. The pass runs exactly once per review: it is not a sweep, does not consume the three-sweep budget, and is not re-run after corrections; the implementation sweep always gets its own fresh subagent. Use the finding-skeptic prompt template below.
3. **Spawn a skeptic subagent** (general-purpose subagent via the Task tool) to attack the implementation itself. Give it, verbatim: the full diff (the prepared patch text or an exact changed/untracked-file manifest the reviewer can read — never a `git` command a read-only reviewer cannot run), the stated intent of the change (PR description, commit messages, or user request), and the workspace path. Use the skeptic prompt template below.
4. **Triage the findings** — the finding-skeptic's surviving findings at their adopted severities, plus the implementation sweep's. First decide (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN. On (B): do not fix this PR/branch, do not list a long patch plan, and do not run further sweeps. If the changes are yours, emit the short replan with a failed-sweep autopsy (quoted blockers, what was never probed, cheapest experiment, what the next plan may not guess) and abandon or close this attempt. If reviewing someone else's work, report the kick-back and replan — do not close their PR and do not patch it. On (A): for each **blocking** finding, fix the code when the changes are yours to edit, or report it as a required change when reviewing someone else's work. Apply **non-blocking** findings at your discretion and mention the worthwhile ones in the review.
5. **If this was (A) and code changed as a result, classify the correction.** A material change — scope, design, interfaces, a security boundary, claim or acceptance meaning, or behavior the executed checks cannot observe — gets a fresh sweep with a new skeptic subagent over the updated diff. A bounded correction within the reviewed approach is verified by re-running the covering checks and recording the delta, without a fresh sweep; for trust-critical artifacts (review or verification tooling, auth, migrations/schema, destructive operations), prefer a focused follow-up from the existing reviewer over pure self-verification. Do not re-sweep a kicked-back attempt.
6. **Stop when a sweep returns zero blocking findings, or every reported blocker is resolved and verified**, or after **3 sweeps**, or immediately on (B). Verified bounded corrections do not consume sweeps. If blocking findings remain, do not LGTM, approve, or say the change looks good. After 3 sweeps still blocking, present the review labeled **BLOCKED**. A kick-back is **BLOCKED** with the replan and failed-sweep autopsy, not a rewrite-in-place. Do not invent a fourth numbered sweep.
7. Include in the final review output how many skeptic sweeps ran and what they caught, **and the finding-skeptic verdicts**: every finding it downgraded, upgraded, or removed must appear in the output with its verdict and the evidence behind it. Downgrades and removals are review results, not silent edits — a finding the first pass reported stays visible with its refutation.

## Reviewer package, monitoring, and delivery

Every skeptic assignment must be a complete package usable with the reviewer's actual tools:

- **Prepared artifacts, not retrieval commands.** A read-only reviewer without shell/exec cannot run `git diff` or `git status`. Give it the prepared patch text or an exact manifest — changed paths, untracked/new files with sizes or hashes — plus source references and the executed test/evidence receipts. Never instruct a reviewer to run a command outside its tool set.
- **A compact covered/remaining/unresolved checklist** of the assigned scope, so context compaction cannot silently drop coverage. Scope the reviewer never reached is an inspection limitation, not a pass.
- **Incremental findings.** Require each skeptic to emit every finding as soon as it is confirmed rather than buffering the whole report for the end. A truncated response then still yields recoverable partial findings, which are preserved as evidence — never as a verdict.

A sweep is complete only when the reviewer returns a non-empty report ending in its required verdict line. An empty response, an output-limit or context-terminated reply, or a missing verdict is a **failed run** — not a completed sweep, not a pass, and not a consumed sweep from the budget. Preserve the transcript and partial findings, then dispatch a fresh skeptic. Verify delivery when the reviewer exits: process success, review completion, and a gate verdict are three different states.

Monitor progress, not only liveness. Watch distinct-file coverage, repeated full-file re-reads of the same paths, and context-compaction count. Escalating re-reads or compactions mean the reviewer is rebuilding context in a loop — stop it and redispatch bounded (smaller area, prepared package) rather than waiting for the output budget to die. Never convert a timeout or dead run into a pass.

## Finding-skeptic prompt template

```text
You are a skeptic reviewing another reviewer's findings on a code change.
Assume each finding is wrong or shallower than claimed and try to prove it.
Do not praise the findings or rubber-stamp them. Read the actual code in the
workspace at <workspace path> — most shallow findings die on contact with
code the first reviewer never read.

Stated intent of the change:
<intent / PR description / user request>

The change under review:
<diff, or the command to produce it>

Candidate findings from the first-pass review:
<each finding: file/line, the claimed problem, the evidence, proposed severity>

For EVERY finding, verify independently:
- Does the claimed defect exist? Quote the code it points at.
- Can it actually fire — is the bad input or state reachable, is the caller
  real, is the path live?
- Is it already handled — a guard, validation, or test the first reviewer
  missed?
- Is the proposed severity right? A "blocking" bug reachable only through
  inputs the system rejects earlier is at most non-blocking. A "minor" issue
  on a path every request hits may be blocking.
- Is it a real defect, or a style/taste disagreement dressed as one?

Shape/direction findings (wrong layer, wrong approach, a design that cannot
be finished cleanly) are judged on whether the direction problem is real —
the reachability questions above do not apply to them. Refute one only with
evidence that the direction is sound; when in doubt, keep it.

Return exactly one verdict per finding — never merge or silently drop one:
- CONFIRMED — stands at the proposed severity; quote the confirming evidence.
- UPGRADED — real and more severe than proposed; state the new severity and
  why.
- DOWNGRADED — real but less severe; state the new severity and the evidence
  for lowering it.
- REMOVED — refuted: not a defect, cannot fire, already handled, or based on
  a misreading; quote the refuting evidence.

Every UPGRADED, DOWNGRADED, or REMOVED verdict needs concrete code evidence —
"probably fine" is not a refutation and "could be worse" is not an upgrade.
When the evidence is ambiguous, keep the proposed severity and say why.
Do not hunt for new findings; the implementation sweep that follows owns the
code. If you trip over a glaring unrelated bug, report it separately as a
note, not a verdict.
```

## Skeptic prompt template

```text
You are a skeptic reviewing a code change. Assume the implementation is broken
or incomplete and try to prove it. Do not praise the change or rubber-stamp it.
Read the surrounding code in the workspace at <workspace path> - most bugs are
invisible in the diff hunks alone. Trace callers, callees, and data flow.

Stated intent of the change:
<intent / PR description / user request>

The change under review:
<diff, or the command to produce it>

Hunt through every category below.

SHAPE / DIRECTION
- Analyze the shape of the change: does this diff implement the intended
  design, at the right layer, in a way that can be finished cleanly? Or
  did the attempt take a wrong direction / grow a hole too big to patch?
- Local, bounded defects (wrong name, missing test, off-by-one,
  incomplete but same design) stay as ordinary findings. The implementer
  may fix those in place.
- If the problems are too big to patch, OR the change itself is the
  wrong direction (fights the existing design, would need a pile of
  compensatory edits, wrong layer, scope explosion, cannot be made
  correct without rewriting most of the diff): do NOT attempt to fix it
  in this PR/branch. Do NOT list a long patch plan. Kick it back.
- Kick-back means BLOCKING: reject this implementation. Produce a short
  replan with a failed-sweep autopsy: quoted blockers, what was never
  probed, the cheapest experiment that would have shown it, what the next
  plan may not guess, which assumption was wrong, new implementation/design
  notes, and an instruction to start that change again from scratch on a
  fresh branch. A reusable process mistake is one separate paragraph, not a
  workflow rewrite in the same packet. Report the kick-back; close or
  abandon only when the changes are yours — do not close someone else's PR.
- Threshold for “too big”: more than a handful of local fixes;
  architectural mismatch; compensatory complexity; or the reviewer
  cannot honestly LGTM even after imagined patches. When in doubt on
  shape vs nits, kick back rather than rubber-stamp a rewrite-in-place.
- Output must make the decision obvious: either (A) ordinary findings,
  proceed with fixes, or (B) KICK BACK AND REPLAN with the autopsy and
  design notes. Never mix “LGTM after you also rewrite the architecture”.

INTENT VS IMPLEMENTATION
- Does the code actually do what the description claims? Diff the claims
  against the behavior line by line.
- Hidden scope: changes unrelated to the stated intent, especially behavior
  changes disguised as refactors.
- Claimed-but-missing: things the description says happen that no code does.

CORRECTNESS
- Edge cases: empty/null/undefined inputs, zero, negative numbers, boundary
  values, unicode, very large inputs, duplicate entries.
- Off-by-one errors in loops, slices, ranges, and pagination.
- Error paths: what happens when the fallible calls (I/O, network, parse)
  fail? Are errors swallowed, mis-typed, or left to corrupt state?
- Concurrency: races, missing awaits, shared mutable state, non-idempotent
  retries, TOCTOU between check and use.
- Resource handling: unclosed files/connections/listeners, unbounded growth
  of caches, queues, or accumulated arrays.
- State machines: unreachable or unhandled states, invalid transitions.
- Time: timezone handling, DST, clock skew, expiry comparisons.

INCOMPLETENESS
- Callers not updated: renamed/changed functions with stale call sites,
  including strings, configs, docs, and reflection/dynamic references.
- Partial application of a pattern: the same fix or rename needed elsewhere
  and not done (search for siblings of every changed symbol).
- Data migrations missing for schema or serialized-format changes; old data
  that the new code can no longer read.
- Backwards compatibility: breaking API/contract changes without versioning
  or coordination; consumers that will break.
- Dead code left behind, half-removed features, orphaned flags.

TESTS
- Would each new test fail on the pre-change code? If not, it tests nothing.
- Bug fixes without a regression test that pins the fix.
- Tests that mock away the very behavior they claim to verify.
- Assertions on incidental details instead of the observable contract.
- Missing negative tests for new validation or error handling.

SECURITY
- Unvalidated input at system boundaries (user input, HTTP, files, env).
- Injection: SQL, shell, path traversal, template, header.
- Authorization: new endpoints or operations missing permission checks that
  comparable existing ones have.
- Secrets in code, logs, error messages, or test fixtures.
- Unsafe deserialization, SSRF, open redirects where applicable.

SLOP SIGNALS
- catch blocks that swallow errors or log-and-continue past corruption.
- Type assertions (as/any/casts) papering over a design problem.
- Copy-pasted near-duplicates instead of a shared path.
- Names or comments that no longer match what the code does.
- Leftover debug code, commented-out blocks, stray TODOs for required work.

REPOSITORY HINTS
- New dependencies: unnecessary, or necessary but not well supported / highly
  used / well regarded.
- Documentation invalidated by this change and not updated.

REPOSITORY RULES
- Follow the target repository's AGENTS.md and existing design documents.
  Product-specific invariants belong there, not in this shared workflow.
- Invented architecture / discarding the design already in the repo is
  BLOCKING unless the user asked for a redesign.

First state (A) ordinary findings, proceed with fixes, or (B) KICK BACK
AND REPLAN. If (B), do not also list a long in-place patch plan; include
the failed-sweep autopsy in the short replan. Then return a list of
findings. Classify each as BLOCKING (bug, security issue, data loss,
broken contract, wrong shape / kick-back, or a gap that makes the change
wrong or incomplete) or NON-BLOCKING (improvement or noteworthy risk).
For each finding give: file and line, the concrete problem, the evidence
from the code, and a suggested fix (ordinary) or the short replan with
autopsy (kick-back). Emit each finding as soon as you confirm it — do
not hold the full list for the end; a truncated reply must still carry
what you already found. Keep the assigned scope's covered/remaining
checklist current as you go and report it. Prefer grep and ranged reads
over re-reading whole files you already covered; if context runs short,
report confirmed findings and name the uncovered remainder as an
inspection limitation — never trade findings for more coverage. If (A)
and you find no blocking problems after genuinely attempting to break
the change, say exactly: NO BLOCKING FINDINGS.
```

## Rules

- Never skip the skeptic pass on a requested review, even for small or "obvious" diffs — small diffs with unexamined blast radius are where regressions live. The same applies to the finding-skeptic: run it whenever the normal review produced findings, even when they look airtight — airtight-looking findings are exactly the ones nobody re-reads. Skip it only when the normal review produced zero findings. For task-acceptance gates, the client playbooks (codex-workflows, muse-workflows) decide which slices invoke this skill: trust-critical slices get the full sweep; verification-covered slices close through ordinary review plus their named executed checks.
- The finding-skeptic's verdicts are part of the review record: findings it downgraded, upgraded, or removed stay in the final output labeled with the verdict and its evidence. Never let a reported finding silently disappear between the normal review and the final review. A verdict is a resolution or severity change only when backed by concrete code evidence — if the finding-skeptic is itself wrong, keep the finding and say why. A note the finding-skeptic reports outside its verdict list is triaged like a finding or explicitly waived in the output — never silently dropped.
- Do not LGTM, approve, or say the change looks good while any blocking finding remains.
- Do not LGTM a wrong-shape change. Kick back and replan (with autopsy) instead of patching it into shape.
- Follow the target repository's AGENTS.md and existing design documents.
- A finding is only resolved by changing the code, requesting the change, or concrete evidence that the skeptic is wrong; "seems unlikely" is not a resolution. A kick-back is resolved by a fresh branch from the replan that used the autopsy, not by editing this one.
- After every finished loop (clean or BLOCKED): run `record-hint-outcome` if a hint clearly helped or missed; otherwise say `no effectiveness signal`.
- After every finished loop: if a finding is reusable across repos, use `capture-lesson`.
- For large diffs, split the work across multiple skeptic subagents by area (e.g. per package or per concern) in a single sweep, then merge their findings. For trust-critical candidates beyond roughly ten files or a thousand changed lines — and always after a context-exhaustion failure on the same candidate — prefer the bounded-area split over one whole-candidate skeptic: disjoint areas plus an explicit integration reviewer covering cross-area contracts, the diff boundary, and shared helpers. A single skeptic rebuilding the whole candidate's context in a loop is a measured failure mode, not thoroughness.

Related knowledge: `knowledge/code-review-skepticism/README.md`, `knowledge/dependencies/README.md`, `knowledge/documentation-and-tests/README.md` in the agent-hints repository.
