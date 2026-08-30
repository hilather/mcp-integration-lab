---
name: skeptic-code-review
description: Fresh skeptic pass on an implemented diff or PR. Use after review-pr (or review-uncommitted). Never skip sweep 1. Cap 3 then BLOCKED. No LGTM while blocking. Do not paraphrase this prompt.
---

# Skeptic code review

You did not write this change. You have no loyalty to it. Default is disbelief.

Read, in order, then run the prompt template **verbatim** (do not paraphrase):

1. `.cursor/skills/knowledge/code-review-skepticism/README.md`
2. `.cursor/skills/knowledge/dependencies/README.md`
3. `.cursor/skills/knowledge/documentation-and-tests/README.md`
4. The plan (if any)
5. The full diff and the files it touches

## Hard rules

- Never skip sweep 1.
- Fresh skeptic each sweep.
- Cap 3 sweeps. Remaining blocking findings → **BLOCKED**.
- No LGTM while blocking.
- Normal `review-pr` runs **first**; this skill is the second pass.

## Prompt template (use verbatim)

```
You are running a skeptic code review. You did not write this change.

Read these files first (do not skip):
- knowledge/code-review-skepticism/README.md
- knowledge/dependencies/README.md
- knowledge/documentation-and-tests/README.md

Then read the plan (if any) and the full diff. Open every changed file.

Sweep N of 3. Never skip sweep 1. Fresh skeptic each sweep. Cap 3 then BLOCKED.

Attack:
1. Bytes that are not what the plan promised (rewritten skill text, extra files, missing files).
2. Scope leaks (vendor.go / LabMITM pin, third_party, Jenkins/Entra/labgraph/fixtures, hints machinery).
3. Docs/changelog gaps this repo’s rules require, or product docs that flattened Cloud-only notes.
4. Test story: docs-only must not fail `make test`; logic changes need a regression.
5. Authorship claims you cannot prove (verbatim, SHA, clone).

Output exactly:
- PASS or BLOCKED
- Blocking findings (if any)
- Non-blocking notes
- What you opened

No LGTM while blocking. Do not implement. Do not paraphrase this prompt.
```

## Output

Copy the template into a fresh skeptic. Report sweep count and **PASS** / **BLOCKED**.
