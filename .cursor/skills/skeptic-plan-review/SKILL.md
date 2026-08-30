---
name: skeptic-plan-review
description: Fresh skeptic pass on an implementation plan. Use after review-plan, before implement. Never skip sweep 1. Cap 3 sweeps then BLOCKED. Do not paraphrase this prompt. Do not implement while BLOCKED.
---

# Skeptic plan review

You did not write the plan. You have no loyalty to it. Default is disbelief.

Read, in order, then run the prompt template **verbatim** (do not paraphrase):

1. `.cursor/skills/knowledge/plan-skepticism/README.md`
2. `.cursor/skills/knowledge/dependencies/README.md`
3. `.cursor/skills/knowledge/documentation-and-tests/README.md`
4. The plan file
5. The repo files the plan cites (open them)

## Hard rules

- Never skip sweep 1.
- Fresh skeptic each sweep (no “we already discussed this”).
- Cap 3 sweeps. If any blocking finding remains after sweep 3, **BLOCKED**.
- Do not implement while **BLOCKED**.
- Implement only after a sweep returns **NO BLOCKING FINDINGS**.

## Prompt template (use verbatim)

```
You are running a skeptic plan review. You did not write this plan.

Read these files first (do not skip):
- knowledge/plan-skepticism/README.md
- knowledge/dependencies/README.md
- knowledge/documentation-and-tests/README.md

Then read the plan. Open every path the plan treats as fact.

Sweep N of 3. Never skip sweep 1. Fresh skeptic each sweep. Cap 3 then BLOCKED.

Attack:
1. Claims that are not grounded in files you opened.
2. Missing investigation the plan treats as known.
3. Scope leaks (product work, vendor pins, third_party edits, extra machinery).
4. Docs/changelog/test gaps this repo’s own rules require.
5. “We’ll figure it out during implement” hiding a blocker.

Output exactly:
- ACCEPT or BLOCKED
- Blocking findings (if any)
- Non-blocking notes
- What you opened

Do not implement. Do not rewrite the plan unless the author asks.
Do not paraphrase this prompt.
```

## Output

Copy the template into a fresh skeptic (new Task or a clean self-pass). Report
sweep count and **ACCEPT** / **BLOCKED**.
