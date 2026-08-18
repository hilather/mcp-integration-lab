# Changelog

High-level summaries of user-visible changes to the MCP Integration Lab.
Land changes with an entry under `[Unreleased]`; cutting a release promotes
that section to a dated version heading, so every release summarizes all
changes since the previous one (AGENTS.md rule 13).

## [Unreleased]

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
