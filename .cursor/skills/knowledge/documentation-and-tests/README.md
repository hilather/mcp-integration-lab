# Documentation and tests

## Docs ship with the change

This repo’s `AGENTS.md` rule 12: every surface the change **touches**
updates in the same change. Rule 13: user-visible work gets a
`CHANGELOG.md` `[Unreleased]` entry.

A Cloud-only vendor **touches**:

- `AGENTS.md` (Cloud section + Layout bullet)
- `CHANGELOG.md`

It does **not** flatten into `README.md` or `docs/architecture.md`.
`CONTRIBUTING.md` keeps those as human lab docs. Do not put git-keeper
or Origin SHA notes on the Pages site.

## Tests

- Logic / CLI / compose changes: regression test next to the logic;
  `make test` and (if lifecycle) `make up && make smoke`.
- Docs-only: no new Go tests required. `make test` must still be green.
- Do not mark a PR ready if CI is red for a regression **you**
  introduced. Docs-only should not fail `test`.

## Voice

Do not flatten product logic. Do not describe this vendor as a lab
service or a compose pin.
