---
name: skeptic-code-review
description: Run skeptic sweeps on a code diff that is already in hand. Use when the diff and intent are gathered and need adversarial review.
---

# Skeptic Code Review

Every code review includes a skeptic pass. The normal review checks that the change looks right; the skeptic assumes the change is broken and tries to prove it.

## The sweep loop

1. **Do the normal review first** so you understand the change's intent and structure.
2. **Spawn a skeptic subagent** (general-purpose subagent via the Task tool). Give it, verbatim: the full diff (or how to obtain it, e.g. `git diff main...HEAD`), the stated intent of the change (PR description, commit messages, or user request), and the workspace path. Use the prompt template below.
3. **Triage the findings.** First decide (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN. On (B): do not fix this PR/branch, do not list a long patch plan, and do not run further sweeps. If the changes are yours, emit the short replan and abandon or close this attempt. If reviewing someone else's work, report the kick-back and replan — do not close their PR and do not patch it. On (A): for each **blocking** finding, fix the code when the changes are yours to edit, or report it as a required change when reviewing someone else's work. Apply **non-blocking** findings at your discretion and mention the worthwhile ones in the review.
4. **If this was (A) and code changed as a result, run a fresh sweep** with a new skeptic subagent over the updated diff. Do not reuse the previous subagent. Do not re-sweep a kicked-back attempt.
5. **Stop when a sweep returns zero blocking findings**, or after **3 sweeps**, or immediately on (B). If blocking findings remain, do not LGTM, approve, or say the change looks good. After 3 sweeps still blocking, present the review labeled **BLOCKED**. A kick-back is **BLOCKED** with the replan, not a rewrite-in-place.
6. Include in the final review output how many skeptic sweeps ran and what they caught.

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
  replan: what the failed attempt taught (what broke, which assumption
  was wrong), new implementation/design notes, and an instruction to
  start that change again from scratch on a fresh branch. Report the
  kick-back; close or abandon only when the changes are yours — do not
  close someone else's PR.
- Threshold for “too big”: more than a handful of local fixes;
  architectural mismatch; compensatory complexity; or the reviewer
  cannot honestly LGTM even after imagined patches. When in doubt on
  shape vs nits, kick back rather than rubber-stamp a rewrite-in-place.
- Output must make the decision obvious: either (A) ordinary findings,
  proceed with fixes, or (B) KICK BACK AND REPLAN with the design notes.
  Never mix “LGTM after you also rewrite the architecture”.

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

HILATHER PRODUCT INVARIANTS — same gate as plan-skeptic (hilather product
repos only; skip agent-skills and unrelated workspaces). Apply only when
the workspace is a hilather product (labs, Helm charts, mcp-integration-lab,
LabLDAP, LabMITM, or a repo whose AGENTS.md / existing design already
describes these systems). Skip for unrelated repos, including the
agent-skills hints repo itself. Never treat this hints repo as hilather
even though these skills name those systems. BLOCKING if the
diff violates any of these unless Matt explicitly overrode them.

- Invented architecture / discarded the design already in the repo.
- Labs YAML that is fail-open on unknown fields, or secrets inlined
  instead of file refs.
- MCP implemented by proxying REST, or a new operation that is not in
  the shared registry, or Web UI / REST / MCP parity broken.
- Product code silently in a language other than Rust or Go (suggestion
  to Matt, not a silent pick). Do not apply this to this hints repo
  (TypeScript is required there).
- Merge/approve of Helm without the Helm release path, or merge without
  the release manager. Keystone does not merge unless Matt says so.
  Follow repo AGENTS.md. Do not tell reviewers to sign as Keystone.
- LabLDAP flattened onto plan/apply instead of engine + bootstrap +
  control.
- LabMITM wrapping/vendoring/execing Python mitmproxy; overlay missing
  knobs 1.1–1.4; treating intercept ports as an appliance limit rather
  than a pin.
- Product logic in mcp-integration-lab, or integrator not last in Helm.
- New product UI with no Mira review after first implementation.
  On a first product-UI change, scheduling or recording that review
  satisfies this; do not require it to already be complete.

First state (A) ordinary findings, proceed with fixes, or (B) KICK BACK
AND REPLAN. If (B), do not also list a long in-place patch plan. Then
return a list of findings. Classify each as BLOCKING (bug, security issue,
data loss, broken contract, wrong shape / kick-back, or a gap that makes
the change wrong or incomplete) or NON-BLOCKING (improvement or
noteworthy risk). For each finding give: file and line, the concrete
problem, the evidence from the code, and a suggested fix (ordinary) or
the short replan (kick-back). If (A) and you find no blocking problems
after genuinely attempting to break the change, say exactly: NO BLOCKING
FINDINGS.
```

## Rules

- Never skip the skeptic pass, even for small or "obvious" diffs — small diffs with unexamined blast radius are where regressions live.
- Do not LGTM, approve, or say the change looks good while any blocking finding remains.
- Do not LGTM a wrong-shape change. Kick back and replan instead of patching it into shape.
- Do not LGTM/merge a hilather product change that violates the invariants.
- Follow repo AGENTS.md; do not merge without the release manager.
- A finding is only resolved by changing the code, requesting the change, or concrete evidence that the skeptic is wrong; "seems unlikely" is not a resolution. A kick-back is resolved by a fresh branch from the replan, not by editing this one.
- After every finished loop (clean or BLOCKED): run `record-hint-outcome` if a hint clearly helped or missed; otherwise say `no effectiveness signal`.
- After every finished loop: if a finding is reusable across repos, use `capture-lesson`.
- For large diffs, split the work across multiple skeptic subagents by area (e.g. per package or per concern) in a single sweep, then merge their findings.

Related knowledge: `knowledge/code-review-skepticism/README.md`, `knowledge/dependencies/README.md`, `knowledge/documentation-and-tests/README.md` in the agent-hints repository.
