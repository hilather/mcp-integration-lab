---
name: review-plan
description: Review a Cursor plan or goal by gathering the plan text, then running skeptic-plan-review. Use when asked to review a plan, a goal, or a Plan-mode draft that is not already loaded into a skeptic pass.
---

# Review Plan

1. Obtain the plan or goal text: open the plan file, read the Cursor goal, or use the user's paste. If none is available, stop and ask — do not invent a plan.
2. Run the `skeptic-plan-review` skill with that text, the original user request, and the workspace path.
3. Stop-the-line, effectiveness, and `capture-lesson` rules from that skill apply. After the loop, run `record-hint-outcome` if there is signal; otherwise say `no effectiveness signal`. Product-lab / hilather plans are covered by `skeptic-plan-review`’s gated hilather invariants (skip this hints repo).
