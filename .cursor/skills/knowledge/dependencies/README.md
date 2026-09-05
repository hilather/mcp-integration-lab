# Dependencies

**Hint:** When performing big changes or implementing new features, avoid adding new dependencies if unnecessary. If a dependency is absolutely necessary, choose one that is well supported and highly used with good feedback.

## Default: no new dependencies

Before adding any dependency, exhaust these options in order:

1. **Standard library / platform.** Node, the browser, and most languages cover far more than agents assume (fetch, crypto, path handling, test runners, argument parsing, etc.).
2. **Dependencies already in the project.** Check the manifest (`package.json`, `pyproject.toml`, `go.mod`, ...) including transitive capabilities of direct dependencies before reaching for something new.
3. **A small amount of first-party code.** If the needed behavior is under ~100 lines and not security- or correctness-critical (no hand-rolled crypto, parsers for hostile input, or timezone math), write it in the project instead of importing it.

## If a dependency is absolutely necessary

Select it against all of these criteria, and verify rather than assume:

- **Well supported:** actively maintained — recent releases or commits, maintainers responding to issues, no "looking for maintainers" notices.
- **Highly used:** significant adoption (download counts, dependents, stars relative to its niche). Prefer the ecosystem's de facto standard over a clever newcomer.
- **Good feedback:** issue tracker and community signal are healthy — bug reports get fixed, no pile of unresolved critical issues or security advisories.
- **Appropriate weight:** the dependency's size and its own transitive dependency tree are proportional to the problem. Do not pull in a framework to use one function.

Record the justification (what was needed, what alternatives were rejected and why) in the PR description or commit message so reviewers can evaluate the choice.

## Big changes

During large refactors or migrations, dependency review is part of the plan: list any proposed additions explicitly, and treat each as requiring the justification above. A big diff is not cover for slipping in new packages.
