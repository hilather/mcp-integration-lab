# Plan Skepticism

**Hint:** After a plan or goal is put together in Cursor, spawn a skeptic agent to review it for implementation problems and gaps, and do not implement while blocking findings remain.

The full workflow is implemented as the `skeptic-plan-review` skill (`.cursor/skills/skeptic-plan-review/SKILL.md`). Gather first with `review-plan` when the plan text is not already in hand. Summary:

## The skeptic sweep loop

1. **Draft the plan** as usual (Plan mode, goal, or ad hoc).
2. **Spawn a skeptic subagent** whose only job is to attack the plan. Give it the complete plan text plus enough repository context to check claims. It must not rubber-stamp: it returns concrete findings, each classified as **blocking** (the plan will fail, produce wrong results, or has a gap that prevents implementation) or **non-blocking** (improvement, risk worth noting).
3. **Resolve every blocking finding** by revising the plan. Resolve them yourself when the fix is clear; only escalate to the user for genuine scope or product decisions.
4. **Repeat with a fresh skeptic sweep** against the revised plan. A sweep that returns zero blocking findings ends the loop.
5. **Cap at 3 sweeps.** If blocking findings remain, present the plan labeled **BLOCKED**. Do not implement and do not present it as final unless the user explicitly overrides.
6. After the loop: effectiveness check (`record-hint-outcome` or `no effectiveness signal`); reusable cross-repo findings go to `capture-lesson`.

## What the skeptic looks for

- Steps that cannot work as written (wrong APIs, files, or assumptions about the codebase)
- Missing steps: migrations, error paths, rollback, configuration, deployment, permissions
- Unstated assumptions and unverified claims about existing behavior
- Ordering problems and hidden dependencies between steps
- Missing testing and validation strategy (see [documentation-and-tests](../documentation-and-tests/README.md))
- Unjustified new dependencies (see [dependencies](../dependencies/README.md))
- Scope gaps between what the user asked for and what the plan delivers

## Hilather product invariants (gated)

Apply only when the workspace is a hilather product (labs, Helm charts, mcp-integration-lab, LabLDAP, LabMITM, or a repo whose AGENTS.md / existing design already describes these systems). Skip for unrelated repos, including this agent-skills hints repo. Never treat this hints repo as hilather even though these skills name those systems. Violations are **blocking** unless the user (Matt) explicitly overrode them. On a hilather product, these are blocking even if the rest of the plan looks implementable. Follow the target repo AGENTS.md. Do not merge without the release manager. Do not instruct anyone to sign as Keystone.

- **Architecture** — original designs already live in the repos. Do not invent a new architecture. A plan that replaces an existing design is blocking unless Matt asked for a redesign.
- **Labs YAML** — fail-closed: unknown fields must reject. Secrets are file references, never inline.
- **REST and MCP** — adapters over one operation registry. Never implement MCP by proxying REST. Web UI / REST / MCP must stay at feature parity; a capability on one surface only is blocking.
- **Language** — default Rust or Go. Any other language is a suggestion to Matt, not a silent pick.
- **Merge/release** — follow the target repo AGENTS.md. Helm merges and tags. Keystone does not merge unless Matt says so. Do not merge without the release manager.
- **LabLDAP** — three processes (engine, bootstrap, control). Do not flatten onto plan/apply.
- **LabMITM** — never wrap, vendor, or exec Python mitmproxy. Overlay must expose all knobs (1.1–1.4). Intercept ports are a pin, not an appliance limit.
- **Integrator** — mcp-integration-lab is orchestration only and always last in the Helm process. No product logic in the integrator; do not schedule it earlier.
- **Mira** — new product UIs get a Mira review after first implementation. Plans that add UI without that step are blocking.
