# Code Review Skepticism

**Hint:** When performing a code review on any changes, spawn a skeptic agent to analyze the implementation for gaps and problems, and do not LGTM while blocking findings remain.

The full workflow is implemented as the `skeptic-code-review` skill (`.cursor/skills/skeptic-code-review/SKILL.md`). Gather first with `review-pr` or `review-uncommitted` when the diff is not already in hand. Summary:

## The review sweep loop

1. **Perform the normal review** of the diff already in hand, writing down its findings as a candidate list (file/line, problem, evidence, proposed blocking/non-blocking severity).
2. **Run a finding-skeptic pass on those findings.** Spawn a skeptic subagent whose only job is to refute them — first-pass findings are often shallow, and deeper analysis downgrades or removes many. One verdict per finding: CONFIRMED, UPGRADED, DOWNGRADED, or REMOVED; a verdict that changes or removes a finding needs quoted code evidence ("seems unlikely" is not a refutation), and ambiguous evidence keeps the proposed severity. Every upgrade, downgrade, or removal is reported in the final review output with its verdict and evidence — findings are never silently dropped. The pass runs once per review; it is not a sweep and does not consume the three-sweep budget. Skip only when the normal review produced zero findings.
3. **Spawn a skeptic subagent** whose only job is to attack the implementation. Give it a complete review package — the prepared diff text or an exact changed/untracked-file manifest (never a `git` command a read-only reviewer cannot run), the change's stated intent, the workspace path, source references, evidence receipts, and a covered/remaining/unresolved checklist — so it can read surrounding code: a diff hunk alone hides most bugs. Require findings incrementally as they are confirmed, not buffered to the end. It returns concrete findings classified as **blocking** or **non-blocking**.
4. **Triage (A) vs (B) first** over the surviving findings at their adopted severities. Ordinary blocking findings: fix the code when reviewing your own changes, or report them as required changes when reviewing someone else's. A SHAPE / DIRECTION kick-back is not fixed on this PR/branch — stop the line and emit the short replan with a failed-sweep autopsy. If the changes are yours, abandon or close this attempt. If reviewing someone else's work, report the kick-back; do not close their PR and do not patch it.
5. **If this was (A) and code was changed as a result, classify the correction.** A material change (scope, design, interfaces, security boundary, claim/acceptance meaning, or behavior the executed checks cannot observe) gets a fresh skeptic sweep over the updated diff. A bounded correction within the reviewed approach is verified by re-running the covering checks and recording the delta; for trust-critical artifacts prefer a focused follow-up from the existing reviewer. Do not re-sweep a kicked-back attempt.
6. **Stop when a sweep returns zero blocking findings or every reported blocker is resolved and verified, or after 3 sweeps, or immediately on (B).** Verified bounded corrections do not consume sweeps. Do not LGTM, approve, or say the change looks good while blocking remain. After 3 sweeps still blocking, present the review labeled **BLOCKED**. A kick-back is **BLOCKED** with the replan and autopsy, not a rewrite-in-place. Do not invent a fourth numbered sweep.
7. After the loop: effectiveness check (`record-hint-outcome` or `no effectiveness signal`); reusable cross-repo findings go to `capture-lesson`.

## What the skeptic looks for

The skill contains the full checklist. The major categories:

- **Shape / direction** — first-class, every review. Decide (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN. Never mix “LGTM after you also rewrite the architecture”.
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
- Kick-back is **blocking**: reject this implementation. Produce a short replan that includes a failed-sweep autopsy — quoted blockers, what was never probed, the cheapest experiment that would have shown it, what the next plan may not guess, and which assumption was wrong — plus new implementation/design notes, and an instruction to start that change again from scratch on a fresh branch. A reusable process mistake is one separate paragraph, not a workflow rewrite inside the same packet. Report the kick-back; close or abandon only when the changes are yours — do not close someone else's PR. A kick-back is resolved by that fresh branch, not by editing this one.
- Threshold for “too big”: more than a handful of local fixes; architectural mismatch; compensatory complexity; or the reviewer cannot honestly LGTM even after imagined patches. When in doubt on shape vs nits, kick back rather than rubber-stamp a rewrite-in-place.
- Output must make the decision obvious: (A) ordinary findings, proceed with fixes, or (B) KICK BACK AND REPLAN with the autopsy and design notes.
- Do not LGTM a wrong-shape change. Kick back and replan (with autopsy) instead of patching it into shape. `review-pr` remains the gatherer; a kick-back stops the line (no “just fix it”).

## Repository rules

Product-specific invariants live in the target repository's AGENTS.md and design docs. This playbook does not carry project workflow. Invented architecture / discarding the design already in the repo is blocking unless the user asked for a redesign.
