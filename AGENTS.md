# Agent guide — MCP Integration Lab

This repo orchestrates AI-ready lab services (DNS, LDAP, TACACS+/RADIUS,
mail, NFS) behind one MCP gateway for integration testing. It owns all configuration, secrets layout,
and gateway policy; the services themselves are vendored. These are the rules
we work by.

## Ground rules

1. **Configuration lives in profiles.** Everything a team varies — host ports,
   gateway mode, storage paths, service YAML/JSON — belongs in
   `profiles/<name>/` (see `profiles/default`). Never hardcode a port or a
   config path in `docker-compose.yaml`; add a variable to `profile.env` with
   a sane default in the compose file. `.env` selects the profile (`PROFILE=`)
   and may override individual values; process env overrides both.
2. **Permanent config is YAML; runtime state is ephemeral.** Bootstrap and
   scenario files are the desired state; anything mutated at runtime (via MCP
   tools) must be wipeable by `make reset` and restorable by restart or
   `*_state_reset` tools. Gateway registration state is deliberately on tmpfs;
   `make register` reapplies the profile's JSON configs. Never introduce
   persistent state outside the profile-definable storage dirs.
3. **All orchestration is the typed Go CLI** (`cmd/mcplab`, logic under
   `internal/`). Make targets are thin wrappers. Do not add bash scripts for
   lifecycle logic. Keep pure logic (parsing, env precedence, file generation)
   in `internal/` packages separated from the docker exec layer so it is
   testable without docker.
4. **Changes require regression tests where appropriate.** If you change or
   add testable logic (env/profile resolution, output parsing, archive
   generation, compose interpolation contracts), add or update tests next to
   it. `make test` (vet + `go test ./...`) must pass, and `make up && make
   smoke` is the end-to-end acceptance gate — all checks green.
5. **Every service is externally exposed on purpose.** Data planes and
   REST/management planes publish on all interfaces so remote systems can
   test against the lab; host ports are profile-defined, container-internal
   ports are irrelevant. Don't bind new services to loopback; do give them
   bearer/TLS auth like the existing ones.
6. **Never commit secrets.** `secrets/` and `third_party/*/secrets/` are generated
   by `mcplab secrets` and gitignored. Tokens are static bearer credentials
   for the lab only.
7. **Never edit `third_party/` in place as the fix.** Vendored repos are cloned by
   `mcplab vendor`. If an upstream change is needed: add a patch in
   `patches/` (applied idempotently by `mcplab vendor`), document it in
   `docs/architecture.md`, and open a PR against the upstream repo (they live
   as siblings in `~/projects/`). Drop the patch once upstream merges.
8. **AI-first services.** New services should expose MCP (streamable HTTP
   preferred), take YAML for permanence, run in minimal containers as
   unprivileged users (read-only rootfs, `cap_drop: ALL`), and be registered
   through the profile's `mcpjungle/servers/*.json` + tool group. The
   registrar discovers servers from that directory (filename must match the
   JSON `name`), so adding a service to a profile is a single-file change
   plus its catalog entry (rule 9).
9. **Every user-facing surface goes in the labinfo catalog — URLs *and*
 connection details.** The `labinfo` MCP service (first-party,
 `cmd/labinfo`) serves `endpoints_list` (web/REST URLs so agents can direct
 users to the right place) and `connections_list` (protocol-level client
 config so agents can configure systems under test). When you add or
 re-port a service, update the profile's `labinfo/services.yaml`: URLs are
 `${VAR}` templates over the profile env (host comes from
 `LAB_PUBLIC_HOST`), and every service **must** carry a `connection` block
 — labinfo fails to start without one. The connection block holds what a
 client needs from scratch: one endpoint per protocol (host:port), the
 non-secret client parameters (LDAP base/bind DNs and OUs, DNS zones, NFS
 mount options, SMTP auth/TLS posture, AAA specifics like RFC 3579
 Message-Authenticator), and the on-the-wire credentials (bind passwords,
 shared secrets). Any credential — web token or connection secret — is
 referenced from a staged copy under `secrets/labinfo-creds/` (add it to
 `stageLabinfoCreds` in `internal/lab/secrets.go`) so dev mode can reveal
 it; new ports go in the labinfo service's compose environment so `${VAR}`
 expansion sees them.
10. **Dev mode is one knob: `LAB_DEV_MODE` in the profile.** `true` opens the
 gateway (MCPJungle development mode, no client auth) and makes labinfo
 reveal credentials (web-service tokens and connection secrets alike);
 `false` (default) hardens the gateway
    (enterprise: client tokens + ACLs) and labinfo only describes how auth
    works. `MCPJUNGLE_MODE` may still be pinned explicitly to decouple the
    two. Never default a shared/team profile to dev mode.
11. **The mail sink never sends mail.** maildev exists to *receive* mail from
    systems under test. Its command line is rendered from the profile's
    `maildev/maildev.yaml` by `internal/maildev`, which rejects every relay /
    outbound flag (`outgoing-*`, `auto-relay*`) — keep that guard fail-closed
    and regression-tested. Host ports and web basic-auth are lab-managed
    (profile.env + `mcplab secrets`), everything else in maildev's flag list
    is fair game for profiles.
12. **Documentation ships with the change.** A change is not done until every
    doc surface it touches is updated in the same change: `README.md`,
    `docs/architecture.md`, this file (rules, layout, quirks), the mcplab CLI
    usage text, Make targets, compose/profile comments, and the labinfo
    catalog + tool descriptions (what agents actually read). When finishing
    work, sweep for stale mentions (service lists, port tables, project
    counts, renamed paths) — don't leave them for the next person to trip on.
13. **Releases carry high-level summaries.** Maintain `CHANGELOG.md`: land
    user-visible changes with an entry under `[Unreleased]` (short, high
    level — what changed and why it matters, not a commit list). Cutting a
    release means promoting that section to a version heading with the date,
    so every release summarizes all changes since the previous one. No
    release without its changelog section.
14. **CI failures get investigated and hardened, never retried away.** When a
    CI (or smoke/test) failure appears, root-cause it before touching the
    retry button — a flake is a bug in the test or the lab until proven
    otherwise. The fix must include hardening: a regression test, a
    fail-closed guard, a health/wait condition, or a doc'd quirk here, so the
    same class of failure is caught earlier or can't recur silently.

## Layout

- `profiles/<name>/` — per-team config (see rule 1)
- `cmd/mcplab`, `internal/` — orchestration CLI + tests
- `cmd/labinfo`, `internal/labinfo` — first-party service-directory MCP
  service (rule 9); built by `docker/labinfo/`
- `docker-compose.yaml` — main project (gateway, LabDNS, maildev, NFS,
  labinfo); `compose/*.overlay.yaml` — overlays merging the vendored LabLDAP
  and TacLab compose projects onto the shared network
- `third_party/` — vendored service repos, cloned by `mcplab vendor` (rule 7);
  release tags are pinned in `internal/lab/vendor.go` (LabLDAP `v0.2.2`,
  TacLab `v1.3.0`). TacLab's generated lab baseline also lives under its
  checkout
- `patches/` — local patches to vendored repos (rule 7)
- `docs/architecture.md` — design, security model, phase-1 plan
- `docs/guides/` — human quick start and configuration (mirrored on the Pages site)
- `docs/index.html` and siblings — GitHub Pages site
- `CHANGELOG.md` — high-level change summaries; releases promote
  `[Unreleased]` (rule 13)


## Known quirks (learned the hard way)

- Host ports 53 and 5353 collide with systemd-resolved/avahi; that's why DNS
  defaults to 10053.
- LabDNS pins MCP protocol `2026-07-28`; the vendored
  `patches/go-lab-dns-wire-mcp.patch` both wires MCP into `serve` and
  relaxes the pin because the gateway's client speaks an older generation.
- TacLab (pinned `v1.3.0`) still pins `2026-07-28` by default. Do **not**
  patch it: `mcplab secrets` sets `api.mcp.allow_legacy_clients: true` on
  the labgen YAML (upstream knob from 1.2.0). `subscriptions/listen` stays
  strict. Bumping the vendor pin re-runs `labgen -force`.
- TacLab is self-contained: `tools/labgen` generates its whole compose bundle
  (configs incl. the combined TACACS+RADIUS variant, PKI, shared secrets,
  plaintext lab passwords in `secrets/PASSWORDS.txt`). Don't hand-write those
  files. Its RADIUS/UDP listener requires Message-Authenticator (RFC 3579)
  — `internal/radius` implements that for the smoke test. RadSec (TCP 2083)
  and inbound DAS (UDP 3799) are published but default off.
- LabLDAP's LDAPS cert SAN is `directory`: verify from a container on
  `mcplab-shared` (as the smoke test does) or use a hosts entry. Trust
  `third_party/go-lab-ldap-mcp/secrets/tls/ca.crt` (lab CA); native
  `labldapd` serves that cert directly — there is no 389 instance-CA.
- The LabLDAP overlay uses compose `!override` for ports; the native
  engine overlay uses `!reset` (Compose v2.24.4+). Plain merging would
  append to upstream's loopback publishes. TacLab's vendored
  `compose.combined.yaml` uses `!override` too.
- Switching LabLDAP from 389 DS to native is a re-bootstrap: leftover
  389 `/data` (uid 389 tmpfs) fail-closes `labldapd`. `LabLDAPUp` wipes
  that volume when it sees a dirsrv image or uid=389 volume opts.
- LabLDAP native one-shots (`native-secret-prep`, `secret-prep`) finish
  in milliseconds. `docker compose wait` then errors with "no containers
  for project"; `LabLDAPUp` runs them in the foreground
  (`--exit-code-from`) instead.
- ratarmount NFSv3 needs `nolock,port=...,mountport=...`; it writes archive
  indexes to `$HOME`, which we point at the `NFS_DATA_DIR` bind mount. Live
  overlay commit (`--commit-overlay-interval` / `--commit-overlay-on-exit`)
  requires uncompressed TAR or `.tar.zst` (gzip is rejected) and a durable
  `-w` dir (not `:temp:`). The fixture is an empty-root `.tar.zst` (`./`
  only). Zstd commit rewrites the last frame and still copies the compressed
  file; the archive bind mount must be writable (plan 2× compressed headroom).
- `mcpjungle invoke` output is human-oriented; parse it only through
  `internal/mcpout` (regression-tested against the pinned CLI framing).
