# Dependencies

Do not invent a clone, a pin bump, or a runtime service to finish a
docs-only vendor.

## This repo

- Orchestration is the Go CLI. Do not add bash lifecycle scripts.
- Service pins live in `internal/lab/vendor.go`. A docs-only skill vendor
  does **not** bump LabMITM or any other pin.
- Do not edit `third_party/` in place.
- Do not start Jenkins, Entra, labgraph, or fixtures for a docs change.
- `.gitignore`: `secrets/`, `third_party/`, `.cursor/mcp.json`, team
  profiles. `.cursor/skills/` is not ignored.

## Origin

- Canonical source: `matt-brewer/agent-skills` @ the SHA in `AGENTS.md`.
- Do not `git clone origin.cursor.com` from a GitHub cloud VM (401
  git-keeper).
- Exclusive product repos stay unvendored. Do not vendor the TypeScript
  hints machinery (`cursor-global-setup`, `hint-effectiveness`,
  `lesson-capture`, `typescript-standards`, `repo-architecture`,
  `repo-memory`, `knowledge-prune`, `hint-evals`).

## If a byte source fails

Record it. Do not pretend the clone worked. Helm merges; Keystone
drift-checks on Thursday. Missing the eight paths is worse than a
disclosed fallback — but do not expand scope to “fix Origin.”
