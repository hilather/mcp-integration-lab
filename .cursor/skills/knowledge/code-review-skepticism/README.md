# Code-review skepticism

Default is disbelief. You did not author the diff.

## Guilty until opened

- Open every changed file. Do not review from the PR title.
- “verbatim copy” — the path exists and the body is the Origin text, not
  a rewrite you would have written anyway.
- “docs-only” — `git diff` has no `internal/`, `cmd/`, compose, or
  `vendor.go` surprise.
- “tests still pass” — you ran `make test`, or you have a log, not a hope.

## Optimism tells

- LGTM while a blocking finding is still open.
- Extra files the plan did not name (hints machinery, SHA sidecars,
  architecture flatten).
- Missing files the plan did name.
- Changelog skipped because “it’s just markdown.”
- Ready-for-review while `test` is red for a regression this change
  introduced.

## Sweeps

Never skip sweep 1. Fresh skeptic each sweep. Cap 3 then **BLOCKED**.
No LGTM while blocking.
