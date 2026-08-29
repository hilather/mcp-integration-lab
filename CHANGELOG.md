# Changelog

High-level summaries of user-visible changes to the MCP Integration Lab.
Land changes with an entry under `[Unreleased]`; cutting a release promotes
that section to a dated version heading, so every release summarizes all
changes since the previous one (AGENTS.md rule 13).

## [Unreleased]

## [0.10.0] - 2026-08-29

### Changed

- Vendor pins: LabDNS **v1.3.0**, LabLDAP **v0.5.0**, TacLab **v1.5.0**,
  LabMail **v1.0.0-rc.4**, LabMITM **v1.5.0**. Operator-console chrome;
  TacLab also rehydrates cookie sessions via `GET /api/v1/session`;
  LabMail adds `mail_message_read`. Stay on LabMail rc (not 1.0.0 GA).
  MCPJungle **0.4.6** and ratarmount-rs **0.1.28** are unchanged. A pin
  bump re-gens the TacLab labgen bundle; overlay ports stay the same.
- Docs landing matches the approved mock: one topology, eight service
  cards in a four-column grid, client JSON in the hero
  (`http://<lab-host>:8080/mcp`) instead of the logo, primary CTA
  **Get a lab running**. Dropped the Ecosystem repeat and the ports
  table duplicate. Shared chrome names the product MCP Integration Lab.
  Accessibility: skip link, menu `aria-expanded` / `aria-controls`,
  `:focus-visible`, escaped `<lab-host>` placeholders, `--fg-subtle`
  meets WCAG AA. Code blocks copy to the clipboard.
- Remaining docs pages (quick start, configure, services,
  architecture) share that chrome: landing footer line, current-page
  underline, catalog cards use the landing name/meta row (versions
  where a pin exists), no leftover inline type.

## [0.9.0] - 2026-08-29

### Changed

- Pin MCPJungle gateway + registrar to **0.4.6** (`MCPJUNGLE_IMAGE_TAG`;
  compose default `:-0.4.6`). Copied profiles that omit the key still
  get that compose default. Set the key only to override. This lab’s
  default is 0.4.6, not upstream Compose `latest` / `latest-stdio`. Do
  not set `MCPJUNGLE_BIND_HOST` (unset = all interfaces).
- In `LAB_DEV_MODE=true`, first mint and pin-bump feed catalog secrets
  into TacLab `labgen -secrets-from` so Argon2id is labgen-minted.
  Leave-dev stays random `labgen -force` (unlinks leftover YAML).
  Catalog-only enter-dev still uses `ApplyDevSecrets` (no PKI wipe).

### Added

- labinfo gateway catalog lists the development-mode operator dashboard
  (`GET /`) with a service `note:` that enterprise returns 404.

## [0.8.0] - 2026-08-29

### Changed

- Vendor pin: LabDNS **v1.2.0** (over-length desired-state names).
  Management `resolve` / `explain` can answer those owners; the DNS
  wire is still RFC 1035. The demo zone does not add over-length
  records. `allowLegacyClients` stays true.
- NFS pin: ratarmount-rs **v0.1.28** (was 0.1.24). The lab export stays
  NFSv3 (`nolock`); NFSv4.1 stays off. Overlay commit still last-frame
  splices `.tar.zst`. Remount patches the SQLite sidecar instead of
  reindexing the whole TAR (F-2, v0.1.27).
- Vendor pin: LabMITM **v1.4.0** (was v1.1.1). Flags-off overlay: SOCKS,
  inspectFrames, orig-dest, `httpAuth`, and 1.4 rule items stay omitted
  (`httpAuth` is absent, not `enabled: false`). Native `/v1` catalog is
  31 (includes `features.get`); `GET /v1/features` and MCP
  `mitm_features_list` are the frozen 11-row hop/accept catalog.
  `make reload APP=labmitm` does not re-register.

### Added

- LabLDAP management Host allow-list now includes `LAB_PUBLIC_HOST` via
  the compose overlay (`LABLDAP_MANAGEMENT_ALLOWED_HOSTS`, mapping merge
  — not `environment: !override`). A DNS `LAB_PUBLIC_HOST` can load the
  control UI without `400 host is not allowed`. Changing `LAB_PUBLIC_HOST`
  needs `mcplab secrets` (leaf SAN) and `make reload APP=labldap`
  (container env). Default `localhost` smoke does not prove that
  extra-hostname path.

### Fixed

- Host-port preflight no longer treats `EACCES` / permission-denied as
  occupied. Privileged TacLab ports `49` and `300` cannot be bound by the
  GitHub-hosted `runner` user; dockerd can still publish them, so
  `smoke-dev` can `make up` on those runners.
- `mcplab secrets` no longer fail-closes with `unknown service "labmitm"`
  on enter-dev / token apply. `serviceExists` inspects the main compose
  `labmitm` service the same way as labdns/maildev/labinfo.

## [0.7.0] - 2026-08-23

### Changed

- Vendor pins: LabDNS **v1.1.1**, LabLDAP **v0.4.1**, TacLab **v1.4.0**,
  LabMITM **v1.1.1**. LabMail stays **v1.0.0-rc.3** (still latest). Dropped
  `patches/go-lab-dns-relax-mcp-pin.patch`; LabDNS MCPJungle compatibility
  is now `spec.management.mcp.allowLegacyClients: true` in the profile
  bootstrap, same pattern as LabMail/LabMITM. No appliance patches remain.

## [0.6.0] - 2026-08-23

### Added

- When `LAB_DEV_MODE=true`, `mcplab secrets` reconciles LabDNS, LabMail,
  labinfo, LabMITM, MCP client, LabLDAP tokens/passwords, and TacLab
  lab-user passwords plus AAA shared secrets from the profile's
  `dev-credentials.yaml`. The default profile ships the `lab-dev-*`
  catalog (inert unless that knob is on). Alice's bind password, mail
  Basic, RADIUS/TACACS shared secrets, and the well-known tokens are
  then the same on every clone of that profile. Leaving dev mode remints
  those files and reloads running containers; `mcplab secrets` is enough
  after a catalog or mode change. An interrupted enter-dev retries those
  reloads even if files already match.
- labinfo `connections_list` in dev mode now includes the LabLDAP CA PEM,
  TacLab lab-user passwords, and the TACACS+ shared secret (plus optional
  TacLab client certs). `mcplab creds` / `make creds` prints the same
  shareable sheet from files on disk; it fails closed outside
  `LAB_DEV_MODE` and never prints TLS private keys.
- LabLDAP leaf certs minted by `mcplab secrets` include `LAB_PUBLIC_HOST`
  as a DNS SAN, or as an IP SAN when that value is an IPv4/IPv6 literal.
  Extra SANs are mode-independent (first mint and re-sign use the same
  set) so remote LDAPS and control HTTPS verify without skipping hostname
  checks. Existing labs re-sign leaves with the current CA — the CA
  private key is not rotated or committed. `Secrets()` reloads LabLDAP
  when those leaves change, and retries that reload on the next
  `mcplab secrets` if it failed (SANs already matching is not enough).
- `make smoke` in `LAB_DEV_MODE` asserts Alice's LDAPS bind, RADIUS Accept
  for catalog `taclabAdmin`, and labinfo `connections_list` secrets equal
  the profile catalog / disk files (`devMode=true`). CI copies
  `profiles/default` to gitignored `profiles/ci-dev/` with
  `LAB_DEV_MODE=true` in that `profile.env` and runs that smoke; the
  default-profile job stays non-dev (random secrets, redaction). Never
  set `LAB_DEV_MODE=true` as process env on `default`.

### Changed

- AGENTS.md rule 6: never commit generated/runtime secrets (`secrets/`,
  `third_party/*/secrets/`); documented lab-only values in
  `dev-credentials.yaml` are allowed and inert unless `LAB_DEV_MODE=true`.

## [0.5.0] - 2026-08-23

### Added

- **LabMITM v1.1.0** compose-in: `labmitm:local` (`--management-listen=:8088`),
  0o644 `secrets/labmitm-token`, host ports **18888** (unauthenticated HTTP/1.1
  proxy) / **18088** (inspector / REST / MCP). HTTPS intercept is **:443 only**;
  CONNECT to LabLDAP/TacLab TLS is tunnel-not-decrypt. MCP server `labmitm` is
  in the integration tool group. `make smoke` drives a host-side ProxyURL GET
  and `mitm_flows_wait`; `make reload APP=labmitm` recreates the container
  (wipes flows). Preflight warns (never fails) when `LAB_PUBLIC_HOST` is not
  loopback and `originAllowlist` is empty.

## [0.4.0] - 2026-08-20

### Changed

- **LabDNS v1.1.0** (was an unpinned `main` checkout). MCP is wired into
  `serve` upstream; the local patch is only
  `patches/go-lab-dns-relax-mcp-pin.patch` so MCPJungle can still register.
  The operator console is on the management listener (`GET /` on port
  18080). Omitted `spec.ui` enables it; remote browsers need exact Origins
  in `spec.management.allowedOrigins`.
- **LabLDAP v0.3.0** (was v0.2.2). Native `labldapd` is now upstream's
  default engine; this lab still sets `engine: native` explicitly. Compose
  stacks `compose.yaml` + `compose.ephemeral.yaml` (the v0.2
  `compose.native*.yaml` aliases are not used).
- **LabMail v1.0.0-rc.3** (was rc.2). Default profile sets
  `spec.management.originAllowlist: ["*"]` so the inbox SPA's hashed JS
  loads from a non-loopback browser Origin. Bearer + Basic still required;
  CORS stays off. Tighten with `"private"` or an exact Origin.
- **ratarmount-rs v0.1.24** (was 0.1.22). The lab export stays NFSv3;
  NFSv4.1 (`--nfs-vers 4`) is in the package but not enabled here.

### Added

- `mcplab reload <app>` / `make reload APP=<app>` rebuilds and recreates
  one application (labdns, maildev, nfs, labinfo, mcpjungle, labldap,
  labtacacs) without a full `make up`. Gateway reload re-runs
  `register` because SQLite is tmpfs.
- `mcplab preflight` / `make preflight` now fail fast when critical endpoint
  and mode values (host, control-plane ports, dev/enterprise mode) are
  overridden outside `profiles/<name>/profile.env`, preventing stale
  `localhost`/port drift from being registered into labinfo.
- Preflight probes every host port the active profile publishes (mcplab,
  LabLDAP, and TacLab compose projects) and fails before bring-up when a
  port is held by a non-lab process. Ports already published by this lab's
  containers are allowed so idempotent `make up` still works.
- `profiles/README.md` documents that only `default` is shipped and other
  profile directories are gitignored for team-local customization.

### Changed

- `mcplab up` and `mcplab register` now run the same preflight check before
  starting containers or registering servers. Intentional env overrides can
  still proceed by setting `MCPLAB_ALLOW_PROFILE_OVERRIDES=true` (port checks
  always run).
- Preflight now includes `LABDNS_DNS_PORT`, so a stale `.env` override (for
  example a leftover `10053` while the active profile expects `53`) fails
  before bring-up and labinfo would advertise the wrong resolver port.
- Local Cursor MCP config moves to `.cursor/mcp.json.example`; `.cursor/mcp.json`
  is gitignored so host-specific gateway URLs are not committed.
- Only `profiles/default` is shipped; team-specific profiles live in gitignored
  `profiles/<name>/` directories locally.

## [0.3.0] - 2026-08-18

### Changed

- **Mail sink is LabMail** (`go-lab-maildev` **v1.0.0-rc.2**) instead of the
  Node `maildev/maildev` image. Compose service name and labinfo catalog id
  stay `maildev`. Desired state is `labmail/bootstrap.yaml`; leftover
  `maildev/maildev.yaml` and relay/outbound keys are rejected fail-closed.
  MCP is registered (`labmail` server, `allowLegacyClients: true`). Bind-
  mounted secrets are 0o644 so the unprivileged container can read them.

## [0.2.0] - 2026-08-17

### Changed

- **NFS export is writable.** ratarmount-rs serves an empty-root
  `.tar.zst` (`fixtures.tar.zst` contains only `./`) with a durable write
  overlay (`-w`) and live overlay commit (`--commit-overlay-interval`
  default 15m, plus `--commit-overlay-on-exit`). Writes are visible
  immediately; they land in the archive on that cadence and again when the
  container stops. Gzip is rejected; persist copies the compressed file
  (plan 2× headroom).

### Added

- Project site at [hilather.github.io/mcp-integration-lab](https://hilather.github.io/mcp-integration-lab/)
  (overview, quick start, configuration, services, architecture) with a
  first-class README, logo, and guides under `docs/guides/`.
- Configuration guide now includes every `profile.env` knob and working
  YAML/JSON from `profiles/default` for LabDNS, LabLDAP, maildev, labinfo,
  and MCPJungle.
- CONTRIBUTING.md for humans landing changes.

## [0.1.0] - 2026-08-17

### Changed

- **TacLab v1.3.0** (was the v1.1.0 checkout). Dropped
  `patches/go-lab-tacacs-mcp-relax-mcp-pin.patch`; MCPJungle compatibility
  now uses upstream `api.mcp.allow_legacy_clients` (set on the labgen YAML
  by `mcplab secrets`). `mcplab vendor` pins the release tag and re-runs
  labgen on pin bumps. New MCP surface from 1.2/1.3 (must-change flags,
  RADIUS Challenge/EAP/MS-CHAP test methods, CoA tools) is whatever the
  appliance registers; RadSec/DAS ports are published but stay default off.
- **LabLDAP v0.2.2** (was an earlier main snapshot). No local patch. Profile
  already enables mutation/password MCP tools, so the v0.2 account-workflow
  tools (`ldap_get_account_state`, expire/lock/enable) are live. Smoke now
  calls `ldap_get_account_state`.
- **LabLDAP native engine**: the directory is now `labldapd`
  (`directory.engine: native`) instead of 389 DS. LDAPS clients and
  bootstrap trust the lab CA (`ca.crt`); there is no instance-CA publish
  step. First `make up` after this change drops leftover 389 volumes
  (engine switch is a re-bootstrap).

### Added

- **labinfo `connections_list` tool**: agents can now fetch the protocol-level
  connection details of every lab service (optionally filtered by service) —
  endpoints per protocol (SMTP, LDAP/LDAPS, DNS, NFS, TACACS+, RADIUS, MCP),
  client parameters (LDAP DNs, DNS zones, NFS mount options, SMTP posture,
  AAA specifics), and connection credentials (LDAP bind password, RADIUS
  shared secret) revealed only in dev mode. The catalog now requires a
  `connection` block per service (fail-closed), so future services can't
  ship without connection details.
- **TacLab (TACACS+/RADIUS)** integration: the `go-lab-tacacs-mcp` appliance
  runs as its own compose project on the shared network, with its `labgen`
  baseline generated on first `make up`, TacLab MCP tools behind the
  gateway, standard TACACS+ (49/300) and RADIUS (1812/1813 udp) data
  planes, and a control plane on 18049. Smoke coverage includes a real RADIUS
  PAP Access-Request (accept + reject paths).
- **maildev mail sink (receive-only)**: SMTP ingest on 1025 and basic-authed
  web UI/REST on 1080. Profiles can set any maildev flag via
  `maildev/maildev.yaml`, but relay/outbound flags (`outgoing-*`,
  `auto-relay*`) are rejected fail-closed — the sink never sends mail.
  Captured mail is ephemeral (tmpfs).
- **labinfo service directory**: first-party MCP service whose
  `endpoints_list` tool hands agents the user-facing URL catalog of every
  service, with credentials revealed only in dev mode.
- **`LAB_DEV_MODE`** profile knob: one switch between hardened (enterprise
  gateway, redacted credentials) and dev (open gateway, revealed credentials).
- **Configuration profiles** (`profiles/<name>/`): per-team ports, gateway
  mode, storage paths, and service configs, selected via `PROFILE`.
- **Go orchestration CLI** (`cmd/mcplab`) replacing the original bash
  scripts, with unit/regression tests and an end-to-end smoke test
  (DNS, LDAP, NFS, TACACS+/RADIUS, mail, labinfo).

### Initial POC

- MCPJungle gateway aggregating LabDNS, LabLDAP, and a ratarmount-rs
  userspace NFSv3 export, orchestrated with docker compose on a shared
  network, with static bearer auth on every internal hop and vendored-repo
  patches tracked in `patches/` (upstream PRs open).
