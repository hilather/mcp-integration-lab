---
name: review-pr
description: Normal (non-skeptic) review of a PR or uncommitted diff. Use after implement, before skeptic-code-review. If there is no PR yet, review the uncommitted/unpushed diff the same way. Do not LGTM while blocking issues remain.
---

# Review PR (or uncommitted)

Read the diff. Open the surrounding files. Do not implement fixes in this pass
unless the author asks; report them.

## When to use

- After implementation, **before** `skeptic-code-review`.
- No PR yet: review the working tree / index the same way (`review-uncommitted`).

## Procedure

1. `git diff` / PR files. Read every changed path.
2. Check correctness against the plan and this repo’s `AGENTS.md`.
3. Check scope: no extra vendor pins, no `third_party/` edits, no forbidden
   product work, no TypeScript hints machinery if the plan excluded it.
4. Check docs-ship-with-change and `CHANGELOG.md` `[Unreleased]` when the
   change is user-visible.
5. Check tests: logic changes need tests; docs-only must not fail `make test`.

## Output

- Verdict: **PASS** or **CHANGES REQUESTED**.
- Findings (blocking vs note).
- What you opened.

Do not give LGTM while a blocking finding remains. Do not skip the skeptic pass.
