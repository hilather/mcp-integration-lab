---
name: review-plan
description: Review a Cursor plan or goal by gathering the plan text, then running skeptic-plan-review. Use when asked to review a plan, a goal, or a Plan-mode draft that is not already loaded into a skeptic pass. Do not use from Grok Build or Codex.
---

# Review Plan

**Do not use from Grok Build or Codex.** Grok uses bundled `/design`. Codex is paused from this repository. If the current client is Grok or Codex, stop here.

1. Obtain the plan or goal text: open the plan file, read the Cursor goal, or use the user's paste. If none is available, stop and ask — do not invent a plan.
2. Run the `skeptic-plan-review` skill with that text, the original user request, and the workspace path.
3. Stop-the-line, effectiveness, and `capture-lesson` rules from that skill apply. After three blocking sweeps, present **BLOCKED**; a successor plan for the same task must include a failed-sweep autopsy. After the loop, run `record-hint-outcome` if there is signal; otherwise say `no effectiveness signal`.
