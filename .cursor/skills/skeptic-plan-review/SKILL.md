---
name: skeptic-plan-review
description: Run skeptic sweeps on a plan or goal that is already in hand. Use when the plan text is gathered and needs adversarial review.
---

# Skeptic Plan Review

After a plan or goal is put together, it must survive skeptic review before it is treated as final. The skeptic's job is to find implementation problems and gaps; the loop repeats until a sweep finds nothing blocking.

## The sweep loop

1. **Use the plan text already in hand** (do not gather or invent a plan here).
2. **Spawn a skeptic subagent** (a general-purpose subagent via the Task tool). Give it, verbatim: the full plan text, the user's original request, and the repository/workspace paths it needs to verify claims. Use the prompt template below.
3. **Triage the findings.** For each **blocking** finding, revise the plan: fix wrong steps, add missing ones, verify or remove unverified assumptions. Resolve findings yourself when the fix is clear; escalate to the user only for genuine scope or product decisions. Apply **non-blocking** findings at your discretion.
4. **Run another sweep** with a fresh skeptic subagent against the revised plan. Do not reuse the previous subagent — a fresh skeptic has no attachment to earlier feedback.
5. **Stop when a sweep returns zero blocking findings**, or after **3 sweeps**. If blocking findings remain after the third sweep, present the plan labeled **BLOCKED**. Do **not** implement and do not present it as final unless the user explicitly overrides.
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

HILATHER PRODUCT INVARIANTS — apply only when the workspace is a hilather
product (labs, Helm charts, mcp-integration-lab, LabLDAP, LabMITM, or a
repo whose AGENTS.md / existing design already describes these systems).
Skip for unrelated repos, including the agent-skills hints repo itself.
Never treat this hints repo as hilather even though these skills name
those systems. Violations are BLOCKING unless the user (Matt) explicitly
overrode them.

- Architecture: original designs already live in the repos. Do not invent a
  new architecture. A plan that replaces an existing design is BLOCKING
  unless Matt asked for a redesign.
- Labs YAML: fail-closed — unknown fields must reject. Secrets are file
  references, never inline.
- REST and MCP are adapters over one operation registry. NEVER implement
  MCP by proxying REST. Web UI / REST / MCP must stay at feature parity;
  a capability on one surface only is BLOCKING.
- Language: default Rust or Go. Any other language is a suggestion to
  Matt, not a silent pick.
- Merge/release: follow the target repo AGENTS.md. Helm merges and tags.
  Keystone does not merge unless Matt says so. Do not merge without the
  release manager. Do not instruct anyone to sign as Keystone (that is
  Keystone-only, not a shared skill rule).
- LabLDAP: three processes (engine, bootstrap, control). Do not flatten
  onto plan/apply.
- LabMITM: never wrap, vendor, or exec Python mitmproxy. Overlay must
  expose all knobs (1.1–1.4). Intercept ports are a pin, not an appliance
  limit.
- Integrator: mcp-integration-lab is orchestration only and always last
  in the Helm process. No product logic in the integrator; do not
  schedule it earlier.
- Mira: new product UIs get a Mira review after first implementation.
  Plans that add UI without that step are BLOCKING.

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
- After 3 sweeps still blocking: present **BLOCKED**; do not implement unless the user explicitly overrides.
- A finding is only resolved by changing the plan or by concrete evidence that the skeptic is wrong; "the skeptic is being pedantic" is not a resolution.
- Report to the user how many sweeps ran and what changed as a result.
- After every finished loop (clean or BLOCKED): run `record-hint-outcome` if a hint clearly helped or missed; otherwise say `no effectiveness signal`.
- After every finished loop: if a finding is reusable across repos, use `capture-lesson`.
- When the target is a hilather product, blocking includes those invariants even if the rest of the plan looks implementable.
- Follow repo AGENTS.md. Do not merge without the release manager.

Related knowledge: `knowledge/plan-skepticism/README.md`, `knowledge/dependencies/README.md`, `knowledge/documentation-and-tests/README.md` in the agent-hints repository.
