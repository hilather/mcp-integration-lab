---
name: review-plan
description: Review an implementation plan before any code is written. Use after a plan file exists and before implement. Checks investigation, scope, repo rules, and missing steps. Do not use for code review (review-pr / skeptic-code-review) or to replace skeptic-plan-review.
---

# Review plan

Read the plan. Open the files it cites. Do not implement.

## When to use

- A plan file (or Plan-mode write-up) exists and implementation has not started.
- Always run this **before** `skeptic-plan-review`. Skeptic is a second, fresh pass.

## Procedure

1. Read the plan end to end.
2. Open every path the plan treats as known (`AGENTS.md`, gitignore, existing
   dirs, pins). If the plan cites a file you did not open, that claim is
   ungrounded.
3. Score the plan against:
   - **Investigation first** — facts before prescriptions.
   - **Scope** — in-repo only; no drive-by vendor pins, `third_party/` edits,
     or product work the user forbade.
   - **Repo rules** — this checkout’s `AGENTS.md` / changelog / docs-ship-with-change.
   - **Acceptance** — what must be green, and what must not run.
4. List concrete fixes (file + change). Do not rewrite the plan unless the
   author asks.

## Output

- Verdict: **READY** or **REVISE**.
- Findings (blocking vs note).
- What you opened.

Do not start implementation from this skill.
