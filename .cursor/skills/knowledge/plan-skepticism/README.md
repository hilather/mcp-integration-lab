# Plan Skepticism

**Hint:** After a plan or goal is put together in Cursor, spawn a skeptic agent to review it for implementation problems and gaps, and do not implement while blocking findings remain.

The full workflow is implemented as the `skeptic-plan-review` skill (`.cursor/skills/skeptic-plan-review/SKILL.md`). Gather first with `review-plan` when the plan text is not already in hand. Summary:

## The skeptic sweep loop

1. **Draft the plan** as usual (Plan mode, goal, or ad hoc).
2. **Spawn a skeptic subagent** whose only job is to attack the plan. Give it the complete plan text plus enough repository context to check claims. It must not rubber-stamp: it returns concrete findings, each classified as **blocking** (the plan will fail, produce wrong results, or has a gap that prevents implementation) or **non-blocking** (improvement, risk worth noting).
3. **Resolve every blocking finding** by revising the plan. Resolve them yourself when the fix is clear; only escalate to the user for genuine scope or product decisions.
4. **Verify each resolution yourself** against the finding's evidence — a resolved blocker does not require another sweep. Run a fresh skeptic sweep only when you cannot verify a resolution, the revision materially changes scope, approach, contract or validation, or the plan was replanned after a kick-back.
5. **Stop when a sweep returns zero blocking findings or every blocker is resolved and verified; cap at 3 full sweeps** (initial plus reopen/replan sweeps — verified resolutions do not consume them). If blocking findings remain, present the plan labeled **BLOCKED**. Do not implement and do not present it as final unless the user explicitly overrides. A successor plan for the same task must include a failed-sweep autopsy (quoted blockers, what was never probed, cheapest experiment, what the next plan may not guess). Do not invent a fourth numbered sweep.
6. After the loop: effectiveness check (`record-hint-outcome` or `no effectiveness signal`); reusable cross-repo findings go to `capture-lesson`.

## What the skeptic looks for

- Steps that cannot work as written (wrong APIs, files, or assumptions about the codebase)
- Missing steps: migrations, error paths, rollback, configuration, deployment, permissions
- Unstated assumptions and unverified claims about existing behavior
- Ordering problems and hidden dependencies between steps
- Missing testing and validation strategy (see [documentation-and-tests](../documentation-and-tests/README.md))
- Unjustified new dependencies (see [dependencies](../dependencies/README.md))
- Scope gaps between what the user asked for and what the plan delivers

## Repository rules

Product-specific invariants (architecture, language, merge, review gates) live in the target repository's AGENTS.md and design docs. This playbook does not carry project workflow. A plan that replaces an existing in-repo design without the user asking for a redesign is still blocking.
