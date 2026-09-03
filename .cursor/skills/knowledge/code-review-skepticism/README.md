# Code Review Skepticism

**Hint:** When performing a code review on any changes, spawn a skeptic agent to analyze the implementation for gaps and problems, and do not LGTM while blocking findings remain.

The full workflow is implemented as the `skeptic-code-review` skill (`.cursor/skills/skeptic-code-review/SKILL.md`). Gather first with `review-pr` or `review-uncommitted` when the diff is not already in hand. Summary:

## The review sweep loop

1. **Perform the normal review** of the diff already in hand.
2. **Spawn a skeptic subagent** whose only job is to attack the implementation. Give it the diff, the change's stated intent, and the workspace path so it can read surrounding code — a diff hunk alone hides most bugs. It returns concrete findings classified as **blocking** or **non-blocking**.
3. **Triage (A) vs (B) first.** Ordinary blocking findings: fix the code when reviewing your own changes, or report them as required changes when reviewing someone else's. A SHAPE / DIRECTION kick-back is not fixed on this PR/branch — stop the line and emit the short replan. If the changes are yours, abandon or close this attempt. If reviewing someone else's work, report the kick-back; do not close their PR and do not patch it.
4. **If this was (A) and code was changed as a result, run a fresh skeptic sweep** over the updated diff. Do not re-sweep a kicked-back attempt.
5. **Stop when a sweep returns zero blocking findings, or after 3 sweeps, or immediately on (B).** Do not LGTM, approve, or say the change looks good while blocking remain. After 3 sweeps still blocking, present the review labeled **BLOCKED**. A kick-back is **BLOCKED** with the replan, not a rewrite-in-place.
6. After the loop: effectiveness check (`record-hint-outcome` or `no effectiveness signal`); reusable cross-repo findings go to `capture-lesson`.

## What the skeptic looks for

The skill contains the full checklist. The major categories:

- **Shape / direction** — first-class, every review (not hilather-only). Decide (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN. Never mix “LGTM after you also rewrite the architecture”.
- **Intent vs. implementation** — the change does what it claims, nothing more hidden, nothing claimed but missing
- **Correctness** — edge cases, error paths, concurrency, resource handling, off-by-one and boundary conditions
- **Incompleteness** — callers not updated, states not handled, migrations missing, docs stale
- **Test quality** — tests that assert real behavior and would fail without the change, not mocked-to-meaninglessness
- **Security** — unvalidated boundaries, injection, authorization gaps, secrets
- **Slop signals** — silenced errors, type assertions, copy-paste, misleading names, leftover debug code
- **Repository hints** — new dependencies justified ([dependencies](../dependencies/README.md)), documentation and regression tests updated ([documentation-and-tests](../documentation-and-tests/README.md))

## Shape / direction (every review)

Analyze the *shape* of the change before line-level nits: does this diff implement the intended design, at the right layer, in a way that can be finished cleanly? Or did the attempt take a wrong direction / grow a hole too big to patch?

- Local, bounded defects (wrong name, missing test, off-by-one, incomplete but same design) stay as ordinary findings. The implementer may fix those in place.
- If the problems are too big to patch, **or** the change itself is the wrong direction (fights the existing design, would need a pile of compensatory edits, wrong layer, scope explosion, cannot be made correct without rewriting most of the diff): do **not** attempt to fix it in this PR/branch. Do **not** list a long patch plan. Kick it back.
- Kick-back is **blocking**: reject this implementation. Produce a short replan — what the failed attempt taught (what broke, which assumption was wrong), new implementation/design notes, and an instruction to start that change again from scratch on a fresh branch. Report the kick-back; close or abandon only when the changes are yours — do not close someone else's PR. A kick-back is resolved by that fresh branch, not by editing this one.
- Threshold for “too big”: more than a handful of local fixes; architectural mismatch; compensatory complexity; or the reviewer cannot honestly LGTM even after imagined patches. When in doubt on shape vs nits, kick back rather than rubber-stamp a rewrite-in-place.
- Output must make the decision obvious: (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN with the design notes.
- Do not LGTM a wrong-shape change. Kick back and replan instead of patching it into shape. `review-pr` remains the gatherer; a kick-back stops the line (no “just fix it”).

## Hilather product invariants (gated)

Same gate as plan-skeptic: hilather product repos only (labs, Helm charts, mcp-integration-lab, LabLDAP, LabMITM, or a repo whose AGENTS.md / existing design already describes these systems). Skip this agent-skills hints repo and unrelated workspaces. Never treat this hints repo as hilather even though these skills name those systems. **Blocking** if the diff violates any of these unless Matt explicitly overrode them. Do not LGTM/merge a hilather product change that violates them. Follow repo AGENTS.md; do not merge without the release manager. Do not tell reviewers to sign as Keystone.

- Invented architecture / discarded the design already in the repo.
- Labs YAML that is fail-open on unknown fields, or secrets inlined instead of file refs.
- MCP implemented by proxying REST, or a new operation that is not in the shared registry, or Web UI / REST / MCP parity broken.
- Product code silently in a language other than Rust or Go (suggestion to Matt, not a silent pick). Do not apply this to this hints repo (TypeScript is required there).
- Merge/approve of Helm without the Helm release path, or merge without the release manager. Keystone does not merge unless Matt says so.
- LabLDAP flattened onto plan/apply instead of engine + bootstrap + control.
- LabMITM wrapping/vendoring/execing Python mitmproxy; overlay missing knobs 1.1–1.4; treating intercept ports as an appliance limit rather than a pin.
- Product logic in mcp-integration-lab, or integrator not last in Helm.
- New product UI with no Mira review after first implementation. Mira is after first implementation: a first product-UI change satisfies this by scheduling or recording that review, not by having already completed it.
