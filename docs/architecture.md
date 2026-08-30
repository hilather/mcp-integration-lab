# MCP Integration Lab — Architecture

Published overview: [hilather.github.io/mcp-integration-lab](https://hilather.github.io/mcp-integration-lab/).
Guides: [quickstart](guides/quickstart.md), [configuration](guides/configuration.md).

A docker-compose orchestrated suite of AI-ready lab services behind a single
MCP gateway endpoint. Services are YAML-configured for permanence; runtime
state is ephemeral and wiped on restart. This repo owns all configuration,
secrets layout, and gateway policy.


## Services (POC)

| Service | Role | MCP | External host ports (default profile) |
| --- | --- | --- | --- |
| LabDNS (`go-lab-dns` **v1.3.0**) | Lab DNS: overrides, wildcards, forwarding, chaos, operator console | `http://labdns:8080/mcp` (bearer; `allowLegacyClients: true`) | DNS 10053† (UDP/TCP), REST/MCP/UI 18080 |
| LabLDAP (`go-lab-ldap-mcp` **v0.5.0**) | Native Go directory (`labldapd`) with control plane | `https://control:8443/mcp` (bearer, lab CA) | LDAP 3389† / LDAPS 3636†, control HTTPS 8443 |
| TacLab (`go-lab-tacacs-mcp` **v1.5.0**) | TACACS+ (legacy + TLS 1.3) and RADIUS lab appliance | `http://taclab:8080/mcp` (bearer) | TACACS+ 49/300, RADIUS 1812/1813 (UDP), RadSec 2083 / DAS 3799 (default off), control HTTP 18049 |
| LabMail (`go-lab-maildev` **v1.0.0-rc.4**, compose service `maildev`) | Receive-only SMTP sink with inbox UI, `/email` compat, `/v1`, MCP | `http://maildev:1080/mcp` (bearer; `allowLegacyClients: true`) | SMTP 1025†, web 1080 |
| LabMITM (`go-lab-mitmproxy` **v1.6.0**) | HTTP(S) intercepting forward proxy with flow-inspector UI, `/v1`, MCP | `http://labmitm:8088/mcp` (bearer; `allowLegacyClients: true`) | proxy 18888 (unauthenticated; not dest 443), inspector 18088 |
| ratarmount-rs **v0.1.28** | Archive-backed userspace NFSv3 export with write overlay + 15m live commit | none yet (phase 1 wrapper) | NFS 20490† |
| labinfo (first-party) | Service directory: user-facing URLs + protocol connection details (+credentials in dev mode) | `http://labinfo:8080/mcp` (bearer) | 18090 |
| MCPJungle (**0.4.6**) | MCP gateway: aggregation, tool groups, ACLs; operator dashboard `GET /` in development mode only | `http://<host>:8080/mcp` | gateway 8080 |

† Residual default-profile dest, not the designed IANA dest (DNS 53, LDAP
389 / LDAPS 636, SMTP 25, NFS 2049). TacLab 49/300/1812/1813 are already
native. LabMITM 18888 is a forward-proxy listen (SUT sets `HTTP_PROXY`),
not dest 443. Remaps are follow-on PRs per service. See
[Host ports and preflight](#host-ports-and-preflight).

All host ports are profile-defined (`profiles/<name>/profile.env`) and bind on
all interfaces: the lab exists for remote systems to test against. Container
listen ports are unprivileged and irrelevant to consumers (compose maps
host:container).

## Host ports and preflight

Protocol data planes SUTs already speak use the IANA/standard dest on the
**host**: DNS 53, NTP 123, LDAP 389 / LDAPS 636, SMTP 25, TACACS+ 49 / 300,
RADIUS 1812/1813, NFS 2049 (AGENTS.md rule 15). Management/control planes
stay high (collision with the gateway and with each other is fine).
Today's default profile numbers above that are not native are residual, not
a second policy — do not invent 10053-as-design.

LabMITM is a forward proxy, not an IANA dest-443 service. Do not move
`LABMITM_PROXY_PORT` to 443; HTTPS intercept of CONNECT `:443` stays inside
the proxy. NFS userspace 2049 is native when remapped; 20490 today is
residual. Operator escape (a non-native host port in `profile.env`) is
allowed when preflight cannot free the IANA port; it is an escape, not the
default.

**Host occupancy** (`internal/lab/ports.go`): every published host port.
`EACCES` / `EPERM` is not occupied (TacLab 49; the operator uid may not
bind privileged ports, but dockerd can still publish). Occupancy is
`/proc/net` TCP LISTEN / UDP bound. Fail closed if a non-lab process holds
the port.

**Docker/host feature knobs.** When a lab feature depends on a Docker
daemon or host setting, preflight fails closed **with the configuration
change in the error**. Do not boot a lab that pretends the feature works.
Example for the LabNTP integrator slice (LabNTP is not in compose yet; do
not add this Go check until then): per-IP views need published UDP source
IPs, which requires `userland-proxy: false`. Normative error copy when
those checks exist:

- DNS 53 held by systemd-resolved: stop/disable resolved **or** extra IP
  for `LAB_PUBLIC_HOST` **or** escape `LABDNS_DNS_PORT` (SUTs that cannot
  set dest port cannot follow the escape).
- NTP 123 held by systemd-timesyncd: stop/disable timesyncd (lab host
  clock may drift; LabNTP never settimeofday) **or** extra IP **or**
  escape `LABNTP_NTP_PORT=10123` (timesyncd/W32Time cannot).
- `userland-proxy` true: write `/etc/docker/daemon.json`
  `{"userland-proxy": false}` then `systemctl restart docker`.
- Docker Desktop / VM NAT cannot preserve UDP source: Linux dockerd +
  `userland-proxy` false, or macvlan/ipvlan. Do not start that feature
  there.

## Topology

```mermaid
flowchart LR
  Agents[MCP clients / agents] -->|streamable HTTP| Jungle["MCPJungle :8080/mcp"]
  subgraph mcplab [compose project mcplab]
    Jungle
    DNS["labdns (scratch image)"]
    NFS["ratarmount-rs --nfs"]
    Mail["LabMail (compose: maildev)"]
    MITM["labmitm"]
    Info["labinfo (service directory)"]
  end
  subgraph labldap [compose project labldap]
    Control["control plane :8443"]
    Dir["labldapd"]
  end
  subgraph labtacacs [compose project labtacacs]
    Taclab["taclabd :49/:300 tacacs, :1812/:1813 radius, :8080 http"]
  end
  Jungle -->|"HTTP + bearer"| DNS
  Jungle -->|"HTTP + bearer"| Info
  Jungle -->|"HTTPS + bearer, lab CA"| Control
  Jungle -->|"HTTP + bearer"| Taclab
  Jungle -->|"HTTP + bearer"| Mail
  Jungle -->|"HTTP + bearer"| MITM
  Control --> Dir
  Testers[integration test clients] -->|"DNS, LDAP/LDAPS, NFS, TACACS+, RADIUS, SMTP, HTTP proxy"| mcplab
  Testers --> labldap
  Testers --> labtacacs
```

- The compose projects meet on the shared external docker network
  `mcplab-shared` (`LAB_DOCKER_SUBNET`, default `10.99.42.0/24`). Docker's
  default for an unspecified user-defined network is a /16 from a ~15-slot
  pool; this lab used two of those and ran out. Main compose `default` is
  that same external network (no `mcplab_default`). REST/management planes
  are published externally too (they are part of what teams
  integration-test), authenticated by bearer tokens or TLS.
- Gateway registration state (SQLite) sits on tmpfs: ephemeral by design.
  `make register` reapplies the JSON configs in the active profile's
  `mcpjungle/` directory, which are the source of truth.
- LabLDAP's control plane serves TLS from a lab-CA-signed cert
  (`labtlsEnsure` in `internal/lab` + scenario `tls.mode: files`); the gateway trusts the
  CA via `SSL_CERT_FILE`. Leaves include DNS SAN `directory`/`control`
  plus `LAB_PUBLIC_HOST` as a DNS or IP SAN in both modes. The CA
  private key stays under `third_party/go-lab-ldap-mcp/secrets/tls/` and
  is not committed.

## Configuration ownership

Per-team configuration lives in profiles (`profiles/<name>/`), selected via
`PROFILE` (`.env` or environment). Within the active profile:

- `profile.env` — host ports, gateway mode, container storage paths,
  NFS overlay-commit interval.
- `labdns/bootstrap.yaml` — LabDNS desired state (read-only mount; MCP
  `dns_state_reset` returns to it).
- `labldap/scenario.yaml` — LabLDAP scenario (users, groups, ACLs,
  tokens; MCP mutations enabled for agent-driven scenarios).
- `labmail/bootstrap.yaml` — LabMail desired state (`labmail.dev/v1alpha1`;
  `internal/maildev` rejects leftover `maildev/maildev.yaml` and every
  relay/outbound key). Compose service name stays `maildev`.
- `labmitm/bootstrap.yaml` — LabMITM desired state (`labmitm.dev/v1alpha1`;
  lab-owned overlay copy, exact Origins, `allowLegacyClients: true`).
- `mcpjungle/servers/*.json` — upstream registrations with
  `${ENV}` token injection.
- `mcpjungle/groups/integration.json` — curated tool-group endpoint.

TacLab is generated rather than hand-written: its `labgen` tool materializes
the full lab baseline (combined TACACS+RADIUS config, PKI, shared secrets,
lab-user passwords) into `third_party/go-lab-tacacs-mcp/deployments/compose/`
on first `mcplab secrets`; the lab runs that bundle as compose project
`labtacacs` with an overlay for the shared network, and the profile owns
the host ports (`TACLAB_*`). In `LAB_DEV_MODE=true`, first mint and pin-bump
pass `labgen -secrets-from` from `dev-credentials.yaml` (PKI and YAML stay
generated). Catalog-only enter-dev still pins secret files via
`internal/taclabcfg` after labgen.

Outside profiles: `secrets/` and `third_party/*/secrets/` — generated, gitignored.
`profiles/<name>/dev-credentials.yaml` is documented lab-only catalog (the
default profile ships `lab-dev-*` values) and is inert unless `LAB_DEV_MODE=true`.
LabMail and LabMITM tokens must be at least 32 bytes (`auth.MinTokenBytes`);
catalog validate fail-closes so a short value cannot crash-loop those
containers.
Container storage is profile-definable (`NFS_ARCHIVE_DIR`, `NFS_DATA_DIR`);
the NFS work dir is a host bind mount so it gets real disk for indexes and
the durable write overlay. The archive dir is also writable: live overlay
commit (`--commit-overlay-interval` / `--commit-overlay-on-exit`) copies the
`.tar.zst` and atomically replaces it (last-frame rewrite; plan 2× compressed
headroom).

## Orchestration CLI

All lifecycle operations are implemented by a typed Go CLI (`cmd/mcplab`,
packages under `internal/`); make targets are thin wrappers. Pure logic
(profile/env precedence, mcpjungle CLI output parsing, fixture archives) is
separated from the exec layer and covered by unit/regression tests
(`make test`). Changes to orchestration behavior require tests where the
logic is testable without docker.

`make up` is the full bring-up (vendor, secrets, fixtures, all three compose
projects, gateway registration). It runs preflight first: profile drift,
host-port occupancy, and (when a lab feature depends on a Docker daemon or
host setting) those feature knobs. Privileged ports (default TacLab 49/300)
are probed without requiring the operator uid to bind — `EACCES` is not
"in use"; occupancy is `/proc/net` (TCP LISTEN / UDP bound). Feature-knob
preflights fail closed with the configuration change in the error (see
[Host ports and preflight](#host-ports-and-preflight)); LabNTP's
`userland-proxy` check is not in Go yet because LabNTP is not in compose.
`mcplab reload <app>` / `make reload`
APP=<app>` rebuilds and recreates **one** application so a YAML or image
tweak does not bounce the rest of the lab:

| App | What reload does | Runtime state discarded |
| --- | --- | --- |
| `labdns` | `compose up --no-deps --force-recreate labdns` | in-process DNS overlay / sessions |
| `maildev` | same for compose service `maildev` | captured inbox |
| `nfs` | same for `nfs` (on-exit overlay commit runs first) | NFS client mounts; overlay already committed |
| `labinfo` | same for `labinfo` | none (catalog is bind-mounted YAML) |
| `mcpjungle` | recreate gateway, then `register` | tmpfs SQLite (re-applied from profile JSON) |
| `labldap` | rebuild images, force-recreate directory + control, re-run bootstrap | ephemeral `/data` (re-seeded from scenario.yaml) |
| `labtacacs` | `compose up --force-recreate` on the TacLab project | in-process AAA state (labgen files on disk survive) |
| `labmitm` | same for compose service `labmitm` | captured flows; generate-mode CA rotates (does not re-register) |

`make labldap-up` / `make labtacacs-up` stay idempotent project bring-up
(used by `make up`). Use full `make up` after a vendor pin bump, a profile
switch, or first run.

## Security model

One profile knob, `LAB_DEV_MODE` (default `false`), drives the posture.
Internal hops always use static bearer tokens on an isolated docker network.

- Hardened (`LAB_DEV_MODE=false` → gateway `enterprise`): clients must present
  the token in `secrets/mcp-client-token`; per-client server allow-lists
  apply. labinfo redacts credentials and only describes auth. Secret files
  are random-if-missing. Leaving dev mode (marker `secrets/.lab-dev-mode`)
  unlinks and remints orchestrator tokens, runs LabLDAP `setupsecrets --force`,
  and `labgen -force`. LabLDAP TLS is not rotated (extra SANs are
  mode-independent); leaves may still be re-signed if `LAB_PUBLIC_HOST`
  is missing from the SAN set.
- Dev (`LAB_DEV_MODE=true` → gateway `development`): no client auth, and the
  labinfo tools reveal credentials — `endpoints_list` each web service's
  token, `connections_list` the on-the-wire secrets (LDAP bind password,
  RADIUS and TACACS+ shared secrets, TacLab lab-user passwords, LabLDAP CA
  PEM, optional TacLab client certs) — all staged world-readable in
  `secrets/labinfo-creds/` (lab-grade static secrets, gitignored).
  `mcplab secrets` writes the active profile's `dev-credentials.yaml` into
  those files, including TacLab lab-user passwords and AAA shared secrets
  (first mint / pin-bump via `labgen -secrets-from`; catalog-only enter-dev
  still pins after labgen; fail-closed if the catalog is missing; no merge with
  `default`). The default profile ships `lab-dev-*` values; they are inert
  unless this knob is on. LabMail and LabMITM tokens must be at least 32
  bytes (`auth.MinTokenBytes`). Catalog reconcile never inspects `MCPJUNGLE_MODE`.
  `MCPJUNGLE_MODE` can still be pinned explicitly to decouple the gateway
  from reveal. `Secrets()` reloads running containers whose files changed
  (or when the marker is missing / `reloads` is not `done`, so a crash
  retries against leftover LabLDAP `/data`, or when LabLDAP leaves were
  re-signed for `LAB_PUBLIC_HOST`) and re-registers the gateway
  when registrar tokens change; `make up` skips those apps. The
  `reloads=pending` marker is written before catalog apply and
  setupsecrets/labgen, so a crash after those files land still retries.
  `mcplab creds` / `make creds` prints the same sheet from files on disk
  (fails closed outside dev; never prints TLS private keys).
  Dev-mode `make smoke` asserts those catalog values on the wire (Alice
  bind, RADIUS `taclabAdmin`, `connections_list` equal to disk).
  Default-profile smoke stays on random secrets and redaction.

labinfo's catalog (`profiles/<name>/labinfo/services.yaml`) requires a
`connection` block per service — protocol endpoints, client parameters (LDAP
DNs, DNS zones, NFS mount options, SMTP posture, AAA specifics), and
connection credentials — so agents can configure clients and systems under
test without human spelunking. labinfo fails to start if a service lacks one
(fail-closed, AGENTS.md rule 9).

### Phase 1: OAuth/OIDC/SAML at the proxy layer

The tools do not need modification. MCP authorization is defined at the HTTP
transport: an OAuth 2.1 resource/authorization server fronts the MCP endpoint.
The plan is a chained auth proxy in front of the gateway:

```
client -> auth proxy (OAuth 2.1 + OIDC discovery) -> mcpjungle -> services
```

- Candidate proxies: `oauth2-proxy` (battle-tested, ext-auth style) or
  `babs/mcp-auth-proxy` (implements the RFC 8414/7591/9728/7636 discovery and
  dynamic-registration chain MCP clients expect out of the box).
- IdP: Keycloak (or Dex) as a lab container. SAML support comes from Keycloak
  brokering (SAML upstream -> OIDC downstream), so SAML also requires no tool
  changes.
- MCPJungle currently lacks downstream OAuth; the chained proxy keeps auth
  swappable and gateway-agnostic.

Phase 1 also adds a thin stdio MCP wrapper for ratarmount-rs (mount/unmount/
list exports via its control interface), registered in MCPJungle, plus
per-persona tool groups and OTel metrics scraping.

## Design notes / limitations

- MCPJungle is pinned to **0.4.6**
  (`ghcr.io/mcpjungle/mcpjungle:${MCPJUNGLE_IMAGE_TAG:-0.4.6}` for both
  `mcpjungle` and `registrar`). This lab’s default is 0.4.6, not upstream
  Compose `latest` / `latest-stdio` (same env name, different default).
  Copied profiles that omit the key still interpolate compose `:-0.4.6`;
  set the key only to override. Do not set `MCPJUNGLE_BIND_HOST` (unset =
  all interfaces; loopback would break remote clients and the registrar).
  Operator dashboard is `GET /` in development mode only (enterprise
  404s). Observed GHCR digest 2026-08-29:
  `sha256:59940d2e3a586ab9a063cf24fa37460bc993686fa056729fd1ada25436123dd9`
  (informational; the tag is the lock). `SERVER_MODE` still follows
  `MCPJUNGLE_MODE` / `LAB_DEV_MODE`; SQLite stays tmpfs; `OTEL_ENABLED`
  stays `"false"`. Appliances still use `allowLegacyClients`.
- LabDNS is pinned to **v1.3.0**. MCP Streamable HTTP is wired into `serve`
  upstream. MCPJungle compatibility uses
  `spec.management.mcp.allowLegacyClients: true` in the profile bootstrap
  (no LabDNS patch). 1.1.0 added the embedded operator console (`GET /` on
  the management listener, `spec.ui.enabled` default true) and
  `spec.management.allowedOrigins` (exact Origins; loopback already
  allowed). 1.2.0 accepts over-length owners in desired state; they
  answer on management `resolve` / `explain` only (the DNS wire is still
  RFC 1035). Do not add those records to `lab.test.`. Canonical
  `sha256:` revisions of previously valid documents are unchanged vs
  1.1.1; they still differ vs 1.0.0-rc.* because omitted `spec.ui` is
  materialized. 1.3.0 is operator-console chrome only (DNS/MCP/schema
  unchanged).
- TacLab is pinned to release **v1.5.0**. Its MCP pin is relaxed with the
  upstream `api.mcp.allow_legacy_clients` knob (default `false`; this lab
  turns it on after `labgen`). There is no TacLab patch in `patches/`.
  1.2.0 also added must-change flags on `taclab.users.*`; 1.3.0 added
  RADIUS Challenge/EAP/MS-CHAP/PEAP, named Cisco-AVPair, optional RadSec
  (TCP 2083, default off) and inbound DAS (UDP 3799, default off). v1.4.0
  added `labgen -secrets-from`; in `LAB_DEV_MODE=true` this lab passes it
  on first mint and pin-bump so Argon2id is labgen-minted. Leave-dev is
  `labgen -force` without the flag. Catalog-only enter-dev still
  post-processes after labgen (`ApplyDevSecrets`; no PKI wipe). The flag
  does not replace `EnableLegacyClientsDir`. Dev mode does not patch the
  vendor. v1.5.0 is operator SPA chrome plus cookie restore via
  `GET /api/v1/session` (labgen and the AAA data plane are unchanged).
- LabLDAP is pinned to release **v0.5.0**. Native is now the default
  engine (omitted `spec.directory.engine` compiles as `native`); this lab
  still sets `engine: native` explicitly. Compose is upstream `compose.yaml`
  + `compose.ephemeral.yaml` plus `compose/labldap.overlay.yaml`. The
  overlay maps `LABLDAP_MANAGEMENT_ALLOWED_HOSTS` from `LAB_PUBLIC_HOST`
  as a mapping merge (not `environment: !override`). Changing
  `LAB_PUBLIC_HOST` needs `mcplab secrets` plus `make reload APP=labldap`.
  MCP account-workflow tools (`ldap_get_account_state`, expire/lock/enable)
  register because the profile sets `registerMutations` and
  `registerPassword`. No LabLDAP patch. Directory TLS is the lab CA
  (`ca.crt`) minted by `labtlsEnsure` (replaces skip-if-exists
  `setuptls generate`); switching from a leftover 389 volume is a
  re-bootstrap (`LabLDAPUp` wipes uid-389 `/data`). v0.5.0 is operator
  SPA chrome (Dark Directory); compose and `labtlsEnsure` are unchanged.
- LabMail is pinned to **v1.0.0-rc.4**. MCP pin is relaxed with upstream
  `spec.management.mcp.allowLegacyClients: true` in the profile bootstrap
  (same idea as TacLab; no LabMail patch). Compose service name and labinfo
  catalog id stay `maildev`. Healthcheck is HTTP `/v1/health/ready` (the
  Node SMTP TCP probe is gone; scratch has no `node`). Bind-mounted secrets
  are 0o644. Captured mail is process memory and wiped by restart/reset.
  `POST /email/:id/relay` is 403. rc.3 adds `originAllowlist` sentinels
  `"*"` and `"private"`; the default profile uses `"*"` so remote inbox JS
  loads (bearer + Basic still required; CORS stays off). rc.4 adds dark
  inbox chrome and additive `POST /v1/messages/{id}:read` /
  MCP `mail_message_read`.
- LabMITM is pinned to **v1.6.0**. MCP pin is relaxed with
  `spec.management.mcp.allowLegacyClients: true` in the profile bootstrap
  (no LabMITM patch). Compose must pass `--management-listen=:8088`
  (binary default is off). The HTTP/1.1 data plane is unauthenticated;
  do not publish without a network boundary. Write
  `spec.proxy.httpAuth.enabled: false` (legal on v1.5.0). 1.2 nested
  flags (SOCKS BIND/UDP/user-pass, inspectFrames, orig-dest) are present
  and off. D22-carve hop gates stay on. Native `/v1` catalog is 31 (includes)
  `features.get`); `GET /v1/features` / MCP `mitm_features_list` is the
  frozen 11-row hop/accept catalog. HTTPS intercept is `:443` only —
  CONNECT to LabLDAP LDAPS or TacLab TLS is tunnel-not-decrypt.
  Origin allowlist is exact Origins (loopback already allowed; no `"*"`).
  When `LAB_PUBLIC_HOST` is not loopback, add
  `http://<LAB_PUBLIC_HOST>:18088` (or the profile's `LABMITM_WEB_PORT`)
  or the inspector SPA 403s `/v1`. Bind-mounted `secrets/labmitm-token`
  is 0o644. Captured flows and a generate-mode CA are wiped on reload;
  `make reload APP=labmitm` does not re-register. v1.6.0 is Status
  live-apply (notes on ffb220b); native `/v1` catalog stays 31.
- ratarmount-rs is pinned to **v0.1.28** (`.deb` in
  `docker/ratarmount/Dockerfile`). NFSv3 has no locking (`nolock` required)
  and AUTH_SYS only; the lab boundary is the docker network / host. The
  export is writable via `-w` (durable overlay under `NFS_DATA_DIR`); live
  commit into the empty-root `.tar.zst` is `--commit-overlay-interval`
  (profile `NFS_COMMIT_OVERLAY_INTERVAL`, default 15m) plus
  `--commit-overlay-on-exit`. Gzip is rejected; `:temp:` overlays are
  rejected. Persist still copies the compressed prefix; remount uses the
  patched SQLite sidecar and does not rescan prefix frames (`:memory:` /
  a discarded sidecar still full-rebuilds). NFSv4.1 (`--nfs-vers 4`) is
  compiled into the package; this lab stays on v3.
- ratarmount image is Ubuntu-based (release .deb). Alpine/musl source build is
  a size optimization for later.
- LabLDAP and TacLab images build locally from the vendored repos (TacLab's
  build compiles its embedded React UI too); LabMail and LabMITM build from
  `third_party/go-lab-maildev` and `third_party/go-lab-mitmproxy` the same
  way LabDNS does. First `make up` takes several minutes.
