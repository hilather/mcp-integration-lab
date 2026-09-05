# Documentation and Tests

**Hint:** All documentation must always be kept up to date with the changes you make. Add regression tests when appropriate.

## Documentation is part of the change

A change is not complete until every document it invalidates has been updated. Before finishing any task:

1. Search the repository for documentation that references what you changed: READMEs, docs/ directories, inline usage examples, architecture notes, CHANGELOGs, API references, configuration samples, and comments that describe behavior (not just code).
2. Update anything now inaccurate — renamed commands, changed defaults, removed flags, new required steps, altered behavior.
3. If the change introduces something a future reader needs to know (new setup step, new environment variable, new workflow), add documentation for it in the place a reader would look first.

Stale documentation is treated as a bug introduced by the change that made it stale.

## Regression tests

Add a regression test when appropriate, which means:

- **Always** when fixing a bug: write a test that fails on the old code and passes on the fix, so the bug cannot silently return.
- **Usually** when changing behavior that other code or users depend on: pin the new contract with a test.
- **Not required** for pure documentation changes, formatting, or code with no observable behavior (though existing tests must still pass).

Regression tests should target the observable behavior that broke, not implementation details, so they survive refactors. Name or comment them clearly enough that a future reader knows which failure they guard against.
