# Contributing

This is laboratory software. Changes that keep configuration in profiles
and runtime state ephemeral are welcome.

## Before you open a PR

```bash
make test          # go vet + unit/regression tests
# if you touched compose, profiles, or the CLI lifecycle:
make up && make smoke
# iterating on one service's YAML after the stack is up:
make reload APP=labdns   # or maildev, nfs, labinfo, mcpjungle, labldap, labtacacs
```

## Where things belong

| Kind of change | Put it here |
| --- | --- |
| Ports, YAML, gateway registrations | `profiles/<name>/` (`default` is shipped; other names are gitignored locally) |
| Orchestration logic | `cmd/mcplab` and `internal/` |
| Compose overlays for LabLDAP / TacLab | `compose/` |
| Upstream fix you need now | `patches/` — and open a PR on the upstream repo |
| User-facing docs | `docs/` (HTML site) and `docs/guides/` |

Do not edit `third_party/` in place. Do not commit anything under
`secrets/` or `third_party/*/secrets/`. Do not hardcode a port in
`docker-compose.yaml` — add a variable to `profile.env`.

Every new service needs three things: a compose service, a
`mcpjungle/servers/<name>.json` whose filename matches the JSON `name`,
and a `labinfo/services.yaml` entry with a `connection` block. labinfo
refuses to start without one.

## Project site

The GitHub Pages site is the static HTML under `docs/`. Edit those files
(and the markdown twins under `docs/guides/`) together. After the first
enable — **Settings → Pages → Deploy from a branch → `main` / `docs`** —
pushes to `main` publish automatically.

## Voice

`README.md` and `docs/` are written for humans who want a lab running.
`AGENTS.md` is the working-rules file for people (and agents) changing
the code. Keep them that way.

## License

Unlicensed laboratory software. Not a production identity, mail, or AAA
system.
