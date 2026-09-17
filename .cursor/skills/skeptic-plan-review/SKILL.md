---
name: skeptic-plan-review
description: Run skeptic sweeps on a plan or goal that is already in hand. Use when the plan text is gathered and needs adversarial review. Do not use from Grok Build or Codex.
---

# Skeptic Plan Review

**Do not use from Grok Build or Codex.** Grok uses bundled `/design`. Codex is paused from this repository. If the current client is Grok or Codex, stop here.

After a plan or goal is put together, it must survive skeptic review before it is treated as final. The skeptic's job is to find implementation problems and gaps. One sweep runs; blocking findings are resolved by revising the plan and verifying each resolution — a resolved blocker does not itself require another sweep.

## The sweep loop

1. **Use the plan text already in hand** (do not gather or invent a plan here).
2. **Spawn a skeptic subagent** (a general-purpose subagent via the Task tool). Give it, verbatim: the full plan text, the user's original request, and the repository/workspace paths it needs to verify claims. Use the prompt template below.
3. **Triage the findings.** For each **blocking** finding, revise the plan: fix wrong steps, add missing ones, verify or remove unverified assumptions. Resolve findings yourself when the fix is clear; escalate to the user only for genuine scope or product decisions. Apply **non-blocking** findings at your discretion.
4. **Verify each resolution yourself** against the finding's evidence and record it — resolving a blocker does not itself require a fresh sweep. Run a new sweep with a fresh skeptic subagent only when you cannot verify a resolution, the revision materially changes the plan's scope, approach, contract or validation, or the packet was replanned after a kick-back. Do not reuse a previous skeptic — a fresh skeptic has no attachment to earlier feedback.
5. **Stop when a sweep returns zero blocking findings or every reported blocker is resolved and verified.** New sweeps run only for material revisions, unverifiable resolutions or replans; stop after **3 full sweeps** total (initial plus reopen/replan sweeps). If blocking findings remain unresolved after the third sweep, present the plan labeled **BLOCKED**. Do **not** implement and do not present it as final unless the user explicitly overrides. A successor plan for the same task must include a failed-sweep autopsy (quoted blockers, what was never probed, cheapest experiment, what the next plan may not guess).
6. When updating a Cursor goal, write the revised plan back to the goal after each sweep so the goal always reflects the current state.

## Skeptic prompt template

```text
You are a skeptic reviewing an implementation plan. Your only job is to find
problems; do not praise the plan or rubber-stamp it. Verify claims against the
actual codebase at <workspace path> rather than trusting the plan's assertions.

Original request:
<user request>

Plan under review:
<full plan text>

Hunt specifically for:
- Steps that cannot work as written (wrong APIs, wrong file paths, incorrect
  assumptions about existing code - verify by reading the code)
- Missing steps: migrations, error paths, rollback, configuration, permissions
- Unstated assumptions and unverified claims
- Ordering problems and hidden dependencies between steps
- Missing testing/validation strategy, and missing documentation updates
- New dependencies that are unnecessary, or necessary but poorly chosen
- Gaps between what was requested and what the plan delivers
- Review-scope games: if the plan declares slice classes or covering checks
  for acceptance, each classification must be honest and each named check must
  actually exercise that slice's observable behavior

REPOSITORY RULES
- Follow the target repository's AGENTS.md and existing design documents.
  Product-specific architecture, language, merge, or review gates belong
  there, not in this shared workflow. Do not invent extra product gates.
- A plan that replaces an existing in-repo design without the user asking
  for a redesign is BLOCKING.

Return a list of findings. Classify each as BLOCKING (the plan will fail,
produce wrong results, or cannot be implemented as written) or NON-BLOCKING
(improvement or noteworthy risk). For each finding give: the plan step it
concerns, the concrete problem, the evidence (file/line where applicable), and
a suggested fix. If you find no blocking problems after genuinely attempting
to break the plan, say exactly: NO BLOCKING FINDINGS.
```

## Rules

- Never skip the first sweep, even for plans that look obviously fine — obvious plans hide assumption gaps.
- Do not implement, and do not present the plan as final, while any blocking finding remains.
- After 3 full sweeps still blocking: present **BLOCKED**; do not implement unless the user explicitly overrides. A successor plan for the same task must include a failed-sweep autopsy (quoted blockers, what was never probed, cheapest experiment, what the next plan may not guess). Do not invent a fourth numbered sweep. Verified blocker resolutions do not consume sweeps.
- A finding is only resolved by changing the plan or by concrete evidence that the skeptic is wrong; "the skeptic is being pedantic" is not a resolution.
- Report to the user how many sweeps ran and what changed as a result.
- After every finished loop (clean or BLOCKED): run `record-hint-outcome` if a hint clearly helped or missed; otherwise say `no effectiveness signal`.
- After every finished loop: if a finding is reusable across repos, use `capture-lesson`.
- Follow the target repository's AGENTS.md and existing design documents.

Related knowledge: `knowledge/plan-skepticism/README.md`, `knowledge/dependencies/README.md`, `knowledge/documentation-and-tests/README.md` in the agent-hints repository.
