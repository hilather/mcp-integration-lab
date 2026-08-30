# Configure the lab

Everything a team varies lives in a profile. Compose files stay generic.
Generated secrets (`secrets/`, `third_party/*/secrets/`) are gitignored.
`dev-credentials.yaml` is a documented lab-only catalog, inert unless
`LAB_DEV_MODE=true`. Runtime mutations are wipeable.

The same reference is published as
[configure.html](https://hilather.github.io/mcp-integration-lab/configure.html).
The snippets below are taken from `profiles/default` — copy that directory
to start a team profile. Directories under `profiles/` other than `default`
are gitignored so local profiles survive `git pull`; see
[`profiles/README.md`](../profiles/README.md).

## Who owns what

- **This repo** owns profiles, secrets layout, compose overlays, the
  `mcplab` CLI, and gateway policy.
- **Vendored repos** own the appliances. Do not edit `third_party/` in
  place — add a patch under `patches/` and send it upstream.
- **Generated files** — `secrets/` and `third_party/*/secrets/` — are
  produced by `mcplab secrets` and never committed. Documented lab-only
  values in `dev-credentials.yaml` are allowed and inert unless
  `LAB_DEV_MODE=true`.

## A profile directory

```
profiles/<name>/
  profile.env              ports, LAB_PUBLIC_HOST, LAB_DOCKER_SUBNET, LAB_DEV_MODE, storage
  dev-credentials.yaml     lab-only catalog (used iff LAB_DEV_MODE=true)
  labdns/bootstrap.yaml    permanent DNS zones and records
  labldap/scenario.yaml    directory users, ACLs, TLS, MCP features
  labinfo/services.yaml    endpoint + connection catalog
  labmail/bootstrap.yaml   LabMail desired state (relay keys rejected)
  labmitm/bootstrap.yaml   LabMITM desired state (exact Origins; no "*")
  labgraph/bootstrap.yaml  LabGraph service bootstrap (SPA Origins; no "*")
  scenarios/*.yaml         LabScenario files (empty default is a no-op;
                           named packs: broken-bind, expired-cert,
                           split-horizon-dns, mitm-intercept-extra-port)
  mcpjungle/
    servers/*.json         upstream MCP registrations
    groups/integration.json curated tool group
```

TacLab’s baseline is generated, not hand-written. `labgen` materializes
users, groups, clients, policies, PKI, and shared secrets into
`third_party/go-lab-tacacs-mcp/deployments/compose/` on first `make up`.
The profile owns the host ports (`TACLAB_*`). When `LAB_DEV_MODE=true`,
first mint and pin-bump feed the catalog into labgen (`-secrets-from`);
PKI stays labgen-random. Catalog-only enter-dev still pins secret files
after labgen and leaves PKI and YAML alone.

```bash
cp -a profiles/default profiles/teamx
# edit profiles/teamx/profile.env and the YAML/JSON under it
cp .env.example .env
# PROFILE=teamx
make up PROFILE=teamx
```

Never hardcode a port in `docker-compose.yaml` — add a variable to
`profile.env` with a compose default.

`dev-credentials.yaml` is a `DevCredentials` document
(`apiVersion: mcplab.dev/v1alpha1`). Every token, password, and shared-secret
key is required. LabMail and LabMITM tokens must be at least 32 bytes
(appliance `auth.MinTokenBytes`). LabLDAP passwords must be at least 12
characters; TacLab shared secrets must pass the appliance policy (length ≥16,
≥3 character classes, exact-match known-weak list). The active profile's file is the only
source — there is no merge with `profiles/default`. `mcplab secrets` consumes
it only when `LAB_DEV_MODE=true`.

## Env precedence

Highest wins:

1. Process environment
2. `.env` at the repo root (selects `PROFILE=` and may override any value)
3. `profiles/<name>/profile.env`
4. Compose-file defaults (match the default profile)

```bash
# .env
PROFILE=default
# PROFILE=teamx
```

If your team profile sets different ports in `profile.env`, do not leave stale
overrides in `.env` — preflight rejects drift (for example `LABDNS_DNS_PORT=10053`
in `.env` while the profile sets `53`).

## Preflight

Protocol data planes SUTs already speak use the IANA dest on the **host**
(DNS 53, NTP 123, LDAP 389 / LDAPS 636, SMTP 25, TACACS+ 49 / 300, RADIUS
1812/1813, NFS 2049). Container listen ports are unprivileged and
irrelevant to consumers. Management planes stay high. LabMITM is a forward
proxy (SUT sets `HTTP_PROXY`), not dest 443. Today's default-profile
numbers that are not native (DNS 10053, LDAP 3389/3636, SMTP 1025, NFS
20490) are residual, not a second policy — remaps are follow-on PRs.
Operator escape (a non-native `profile.env` port) is allowed when
preflight cannot free the IANA port; it is an escape, not the default
(AGENTS.md rule 15).

`make preflight` (also run automatically by `make up` and `make register`) checks:

1. **Profile drift** — critical endpoint/mode values in `.env` or process env
   must match `profiles/<name>/profile.env` (unless
   `MCPLAB_ALLOW_PROFILE_OVERRIDES=true`).
2. **Host ports** — every published port from the active profile must be free,
   or already held only by this lab's Docker containers (idempotent restarts).
   `EACCES` / permission-denied on privileged ports (default TacLab `49` /
   `300` when the orchestrator is not root) is not treated as occupied:
   dockerd can still publish them. Preflight then checks `/proc/net` for a
   real listener (TCP LISTEN / UDP bound) instead of skipping those ports.
   Fail closed if a non-lab process holds the port.
3. **Docker/host feature knobs** — when a lab feature depends on a Docker
   daemon or host setting, preflight must fail closed **with the
   configuration change in the error**. Do not boot a lab that pretends
   the feature works. LabNTP is not in compose yet; do not add a
   `userland-proxy` probe in Go until that integrator slice. The
   normative error for that check, when it exists: write
   `/etc/docker/daemon.json` `{"userland-proxy": false}` then
   `systemctl restart docker`. Docker Desktop / VM NAT cannot preserve
   UDP source — Linux dockerd + `userland-proxy` false, or
   macvlan/ipvlan; do not start that feature there.

Error messages must name the fix. If a profile uses IANA port 53 for DNS
and systemd-resolved already holds it, preflight fails before compose
starts: stop/disable resolved **or** extra IP for `LAB_PUBLIC_HOST` **or**
escape `LABDNS_DNS_PORT` (SUTs that cannot set dest port cannot follow
the escape). Same shape for NTP 123 vs systemd-timesyncd when LabNTP
lands (stop/disable timesyncd — lab host clock may drift; LabNTP never
settimeofday — **or** extra IP **or** escape `LABNTP_NTP_PORT=10123`,
which timesyncd/W32Time cannot follow).

## profile.env — every knob

| Variable | Default | What it is |
| --- | --- | --- |
| `LABDNS_DNS_PORT` | `10053` | Residual. Native dest is **53**. DNS data plane (udp+tcp). |
| `LABDNS_REST_PORT` | `18080` | LabDNS REST `/v1` + MCP `/mcp` + operator console `GET /`. |
| `LABLDAP_LDAP_PORT` | `3389` | Residual. Native dest is **389**. LDAP (StartTLS). Cleartext simple bind is disabled. |
| `LABLDAP_LDAPS_PORT` | `3636` | Residual. Native dest is **636**. LDAPS. Cert SAN is `directory` plus `LAB_PUBLIC_HOST` (DNS or IP). |
| `LABLDAP_HTTPS_PORT` | `8443` | LabLDAP UI + REST + MCP, lab TLS. |
| `MCP_GATEWAY_PORT` | `8080` | MCPJungle streamable HTTP. |
| `MCPJUNGLE_IMAGE_TAG` | `0.4.6` | MCPJungle GHCR tag for gateway + registrar. Omit to keep compose `:-0.4.6`. Do not copy upstream `latest` / `latest-stdio`. |
| `NFS_PORT` | `20490` | Residual. Native dest is **2049**. ratarmount-rs userspace NFSv3 (writable overlay). |
| `TACLAB_LEGACY_PORT` | `49` | Already native. TACACS+ (RFC 8907). |
| `TACLAB_TLS_PORT` | `300` | Already native. TACACS+ TLS 1.3 (RFC 9887). |
| `TACLAB_RADIUS_ACCESS_PORT` | `1812` | Already native. RADIUS access (udp). Message-Authenticator required. |
| `TACLAB_RADIUS_ACCT_PORT` | `1813` | Already native. RADIUS accounting (udp). |
| `TACLAB_RADIUS_RADSEC_PORT` | `2083` | RadSec. Published; listener default off. |
| `TACLAB_RADIUS_DYNAUTH_PORT` | `3799` | RADIUS DAS. Published; listener default off. |
| `TACLAB_HTTP_PORT` | `18049` | TacLab UI + REST + MCP. |
| `MAILDEV_SMTP_PORT` | `1025` | Residual. Native dest is **25**. Receive-only SMTP ingest. |
| `MAILDEV_WEB_PORT` | `1080` | LabMail UI + `/email` + `/v1` + MCP. |
| `MAILDEV_WEB_USER` | `admin` | Web basic-auth user. Frozen at `admin` (YAML does not interpolate this). Password is minted, or the catalog `maildevWeb` value in dev mode. |
| `LABMITM_PROXY_PORT` | `18888` | Unauthenticated HTTP/1.1 forward proxy (absolute-form + CONNECT). Not an IANA dest-443 service; do not move this to 443. |
| `LABMITM_WEB_PORT` | `18088` | LabMITM inspector UI + `/v1` + MCP. |
| `LABINFO_PORT` | `18090` | Service-directory MCP. |
| `LABGRAPH_PORT` | `18091` | LabScenario orchestrator REST / MCP / SPA. |
| `LABGRAPH_LABLDAP_SCENARIO_NAME` | `mcp-integration-lab` | Compiled LabLDAP scenario `metadata.name` for `POST /api/v1/reset`. |
| `LAB_PUBLIC_HOST` | `localhost` | Hostname labinfo puts in every URL, a DNS or IP SAN on LabLDAP leaf certs (both modes), and LabLDAP management Host extras (overlay `LABLDAP_MANAGEMENT_ALLOWED_HOSTS`). Set this to the name or address remote testers use. Changing it needs `mcplab secrets` plus `make reload APP=labldap`. |
| `LAB_DOCKER_SUBNET` | `10.99.42.0/24` | IPv4 CIDR for `mcplab-shared` (`/24`–`/27`). Docker's default is a /16 per user-defined network; this lab uses one /24. Leftover /16: `make down` then `make up`. |
| `LAB_DEV_MODE` | `false` | Single security knob. See below. Also consumes `dev-credentials.yaml`. |
| `MCPJUNGLE_MODE` | follows `LAB_DEV_MODE` | Pin to decouple gateway mode from labinfo reveal and catalog reconcile. |
| `NFS_ARCHIVE_DIR` | `.data/nfs` | Empty-root `fixtures.tar.zst` (writable; live commit replaces it). |
| `NFS_DATA_DIR` | `.data/nfs-work` | Indexes plus the durable write overlay. Give it real disk. |
| `NFS_COMMIT_OVERLAY_INTERVAL` | `15m` | How often overlay writes are spliced into the `.tar.zst`. |

## Reload vs full redeploy

`make up` vendors, mints or reconciles secrets, builds every image, starts three compose
projects, and registers the gateway. Use it for first bring-up, a vendor pin
bump, or a profile switch. After a `dev-credentials.yaml` or `LAB_DEV_MODE` edit, or a
`LAB_PUBLIC_HOST` SAN change, `mcplab secrets` reloads running apps
whose files changed; `make up` skips those names. Changing `LAB_PUBLIC_HOST`
also needs `make reload APP=labldap` so the control container picks up
`LABLDAP_MANAGEMENT_ALLOWED_HOSTS` (`mcplab secrets` re-signs the leaf SAN;
reload recreates directory + control).

After that, recreate **one** application:

```bash
make reload APP=labdns|maildev|nfs|labinfo|mcpjungle|labldap|labtacacs|labmitm|labgraph
# equivalent: mcplab reload <app>
```

| You changed | Command | Side effect |
| --- | --- | --- |
| `labdns/bootstrap.yaml` | `make reload APP=labdns` | runtime DNS mutations and UI sessions gone |
| `labmail/bootstrap.yaml` | `make reload APP=maildev` | captured inbox gone |
| NFS interval / ratarmount image | `make reload APP=nfs` | on-exit overlay commit runs; clients remount |
| `labinfo/services.yaml` | `make reload APP=labinfo` | none |
| `mcpjungle/servers/*.json` | `make register` | no container restart (JSON is re-applied) |
| gateway container itself | `make reload APP=mcpjungle` | tmpfs SQLite wiped, then `register` |
| `labldap/scenario.yaml` | `make reload APP=labldap` | ephemeral `/data` re-seeded from the scenario |
| `LAB_PUBLIC_HOST` | `mcplab secrets` then `make reload APP=labldap` | leaf SAN rewrite; control Host allow-list env |
| TacLab labgen output / image | `make reload APP=labtacacs` | in-process AAA state gone; labgen files stay |
| `labmitm/bootstrap.yaml` | `make reload APP=labmitm` | captured flows gone; generate-mode CA rotates; does not re-register |
| `labgraph/bootstrap.yaml` or `scenarios/*.yaml` | `make reload APP=labgraph` | in-memory apply/reset journal gone |

`make labldap-up` / `make labtacacs-up` are idempotent project bring-up
(the path `make up` uses). They do not force-recreate a running directory.

## LabDNS

`labdns/bootstrap.yaml` is the desired state, mounted read-only. MCP
`dns_state_reset` returns the service to this file. After editing the file,
`make reload APP=labdns` recreates only LabDNS. Non-loopback peers
present the bearer in `secrets/labdns-token`.

LabDNS **v1.3.0** serves an operator console at `GET /` on
`LABDNS_REST_PORT` (`spec.ui.enabled`, default true). Paste the bearer on
the login screen. Loopback Origins are allowed. A remote browser must list
its Origin in `spec.management.allowedOrigins` as an exact
`http(s)://host[:port]` string (no `"*"` sentinel). `spec.ui.enabled:
false` 404s the SPA only; REST and MCP stay. Over-length desired-state
owners answer on management `resolve` / `explain` only (the DNS wire is
still RFC 1035); do not add them to `lab.test.`.

Default profile — authoritative for `lab.test.` with `ns1`, `nfs`,
`ldap`, and a `*.tools` wildcard:

```yaml
apiVersion: labdns.dev/v1alpha1
kind: LabDNS
metadata:
  name: mcp-integration-lab
spec:
  listeners:
    dns:
      address: ":5353"
      protocols: [udp, tcp]
    management:
      address: ":8080"
      restPath: /v1
      mcpPath: /mcp
  ui:
    enabled: true
  management:
    auth:
      profile: bearer
      secretRef: /etc/labdns/token
    mcp:
      allowLegacyClients: true  # MCPJungle
    # allowedOrigins: [http://lab.example:18080]  # remote console Origin
  defaults:
    ttl: 30s
    negativeTTL: 10s
  zones:
    - id: lab-zone
      name: lab.test.
      mode: authoritative
      nameservers: [ns1.lab.test.]
      records:
        - { id: ns1-a,  owner: ns1,      type: A, values: [10.42.0.53] }
        - { id: nfs-a,  owner: nfs,      type: A, values: [10.42.0.30] }
        - { id: ldap-a, owner: ldap,     type: A, values: [10.42.0.40] }
        - { id: tools-wildcard-a, owner: "*.tools", type: A, values: [10.42.0.20] }
```

Add a zone or a record here and `make reload APP=labdns`. Runtime
records added through MCP vanish on `dns_state_reset`.

## LabLDAP

`labldap/scenario.yaml` is a LabScenario. LabLDAP **v0.5.0** defaults to
the native engine (omitted `engine` compiles as `native`); this lab still
sets `directory.engine: native` explicitly. Management TLS comes from
lab-CA files, and `registerMutations` / `registerPassword` keep
account-workflow tools live. After editing, `make reload APP=labldap`
force-recreates directory + control and re-seeds ephemeral `/data`.

```yaml
apiVersion: labldap.dev/v1alpha1
kind: LabScenario
metadata:
  name: mcp-integration-lab
spec:
  directory:
    engine: native
    suffix: "dc=example,dc=test"
    peopleRDN: "ou=people"
    groupsRDN: "ou=groups"
  lifecycle:
    storageMode: ephemeral
    startupMode: merge
    softReset: true
  transport:
    insecureLabMode: true
    ldap:  { enabled: true, port: 3389 }
    ldaps: { enabled: true, port: 3636 }
    startTLS: true
    allowCleartextBind: false
    allowAnonymousBind: false
  management:
    listen: "0.0.0.0:8443"
    tls:
      mode: files
      certFile: /run/secrets/management.crt
      keyFile: /run/secrets/management.key
    mcp:
      enabled: true
      registerMutations: true
      registerPassword: true
      registerReset: false
      registerExport: false
  users:
    - { id: alice, uid: alice, passwordFile: /run/secrets/user-alice, enabled: true }
  groups:
    - id: staff
      members: [{ user: alice }]
  acls:
    - id: staff-read
      principal: { kind: group, ref: staff }
      target: { kind: suffix }
      permissions: [read, search, compare]
      attributes: { allow: ["*"], deny: [userPassword] }
```

- Bind as `uid=alice,ou=people,dc=example,dc=test`.
- LDAPS cert SAN is `directory` plus `LAB_PUBLIC_HOST` (DNS name, or an
  IP SAN when that value is an IPv4/IPv6 literal). Extra SANs are
  mode-independent. Trust
  `third_party/go-lab-ldap-mcp/secrets/tls/ca.crt`. The CA private key
  is not committed. `setuptls --dns/--ip` is unused; `labtlsEnsure`
  still mints the SANs.
- `insecureLabMode: true` in the live scenario is intentional (lab-grade).
- Management HTTP Host extras come from overlay
  `LABLDAP_MANAGEMENT_ALLOWED_HOSTS` (`LAB_PUBLIC_HOST`) union
  LoopbackHosts (`localhost`, `127.0.0.1`, `control`, …). Literal IP
  Hosts are accepted. `"*"` is rejected. Extra names can go in
  `spec.management.allowedHosts` (YAML cannot interpolate `${VAR}`).
  Default `localhost` is already a LoopbackHost; an unlisted Host 400
  does not prove this wiring.
- Add users, groups, and ACLs in this file. MCP mutations are ephemeral.

## TacLab

Do not hand-write TacLab configs. `mcplab secrets` runs `labgen` and
sets `api.mcp.allow_legacy_clients: true` so the gateway’s older MCP
client can connect.

What you *do* own in the profile:

```bash
TACLAB_LEGACY_PORT=49
TACLAB_TLS_PORT=300
TACLAB_RADIUS_ACCESS_PORT=1812
TACLAB_RADIUS_ACCT_PORT=1813
TACLAB_HTTP_PORT=18049
# published, listeners default off
TACLAB_RADIUS_RADSEC_PORT=2083
TACLAB_RADIUS_DYNAUTH_PORT=3799
```

Lab-user passwords land in
`third_party/go-lab-tacacs-mcp/deployments/compose/secrets/PASSWORDS.txt`.
In `LAB_DEV_MODE=true`, first mint and pin-bump pass `labgen -secrets-from`
a YAML generated from `dev-credentials.yaml` (`secrets/taclab-secrets-from.yaml`,
gitignored, 0o600) so Argon2id is labgen-minted. Enter-dev on an existing
baseline skips labgen and still post-processes secret files (token, shared
secrets, lab-user passwords, Argon2id verifiers) and leaves PKI and YAML
alone. Leave-dev is `labgen -force` without the flag (unlinks leftover YAML).
Always `EnableLegacyClientsDir`. RADIUS Access-Requests must carry
Message-Authenticator (RFC 3579).

## LabMail

The compose service name stays `maildev`. Desired state is
`labmail/bootstrap.yaml` (`labmail.dev/v1alpha1`). `internal/maildev`
rejects leftover `maildev/maildev.yaml` and every relay/outbound key.
Host ports and web Basic/bearer files are lab-managed. `allowLegacyClients:
true` is required for MCPJungle. After editing, `make reload APP=maildev`
(wipes the inbox).

LabMail **v1.0.0-rc.4** hashed inbox JS sends `Origin`. An empty
`originAllowlist` 403s those GETs from a non-loopback browser (HTML `GET /`
is often 200). This profile sets `"*"` because the UI is published on all
interfaces; bearer + Basic still required; CORS/`OPTIONS` stay disabled.
Tighten with `"private"` (RFC 1918 + ULA, not Tailscale `100.x`) or an
exact `http(s)://host[:port]`. Optional `spec.smtp.behavior` is
deterministic and default-off — not a chaos engine; leave it omitted.

```yaml
apiVersion: labmail.dev/v1alpha1
kind: LabMail
metadata:
  name: lab-sink
spec:
  listeners:
    smtp: { address: ":1025" }
    management: { address: ":1080", restPath: /v1, mcpPath: /mcp, compatEnabled: true }
  smtp:
    hostname: labmail.lab
    auth: { mode: none }
    tls: { mode: off }
  management:
    auth:
      mode: bearer_and_basic
      tokens:
        - { id: admin, secretFile: /run/secrets/labmail-token, role: administrator }
      basic:
        username: admin
        passwordFile: /run/secrets/maildev-web-password
        tokenRef: admin
    mcp:
      allowLegacyClients: true
    originAllowlist: ["*"]
```

Point the system under test at `<lab-host>:1025` (residual; native dest
is 25). Read captured mail at
`:1080` (Basic user `admin`). Native REST is `/v1`; MCP is `/mcp` (bearer
in `secrets/labmail-token`). Nothing is relayed. Captured mail is wiped on
restart.

## LabMITM

Desired state is `labmitm/bootstrap.yaml` (`labmitm.dev/v1alpha1`), a
lab-owned overlay copy — do not recopy from the upstream examples tree.
Pinned **v1.6.0**. `allowLegacyClients: true` is required for MCPJungle.
Compose must pass `--management-listen=:8088`. After editing,
`make reload APP=labmitm` (wipes captured flows; generate-mode CA
rotates). Reload does **not** re-register the gateway tool list
(`make up` / `make register` / mcpjungle reload).

The HTTP/1.1 data plane is **unauthenticated**. Do not publish without a
network boundary. The overlay writes `spec.proxy.httpAuth.enabled:
false` (legal on v1.5.0). 1.2 nested flags (SOCKS BIND/UDP/user-pass,
inspectFrames, orig-dest) are present and off. D22-carve hop gates
(`websocket` / `connect` / `absoluteForm`) stay on. Native `/v1`
catalog is 31 (includes `features.get`); `GET /v1/features` / MCP
`mitm_features_list` is the frozen 11-row hop/accept catalog. HTTPS
intercept is **:443 only**; CONNECT to LabLDAP LDAPS or TacLab TLS is
tunnel-not-decrypt. `allowHosts` is HTTP-useful compose DNS (`*.lab`,
labdns, labinfo, maildev, mcpjungle, control, taclab).

Origin allowlist is exact Origins (loopback already allowed; no `"*"` /
`"private"`). When `LAB_PUBLIC_HOST` is not loopback, add
`http://<LAB_PUBLIC_HOST>:18088` (or the profile's `LABMITM_WEB_PORT`)
or the inspector SPA 403s `/v1`. Preflight warns (never fails) if the
allowlist is empty.

Desired-state shape (1.1–1.4 knobs included, default off except D22-carve
hop gates) lives in `profiles/default/labmitm/bootstrap.yaml`. Do not
recopy from the upstream examples tree.

## LabGraph

First-party LabScenario orchestrator (catalog id `labgraph`). Desired
state is `labgraph/bootstrap.yaml` (`mcplab.dev/v1alpha1` `LabGraph`)
plus `scenarios/*.yaml` (`LabScenario`). The default scenario has
`spec: {}` so list/get/validate/plan/apply/status are no-ops. Named
packs (`broken-bind`, `expired-cert`, `split-horizon-dns`,
`mitm-intercept-extra-port`) are also `LabScenario` files. Load them at
`labgraph://fixtures/{id}` (or `scenario_get`, which includes `spec`)
and apply with `fixture.apply` / `mcplab fixture apply <id>`.
`broken-bind` is LabLDAP control `disableUser` only (do not flatten
`labldap/scenario.yaml`). `expired-cert` is an expired public leaf for
SUT TLS client verify — no private key, apply does not resign lab
certs. NFS `make fixtures` is a different thing (empty-root `.tar.zst`).

labgraph fans out to native appliance APIs in order DNS → MITM → mail →
LDAP → TacLab. Family sections use native validate/plan/apply envelopes
(`expectedRevision` from `GET /v1/state` when omitted). LabLDAP
control-plane `disableUser` is allowed; flatten (`users` / suffix) and
`spec.labtacacs` fail closed. Reset calls native reset only (LabLDAP OCC is
`baseline.expectedRevision`). Partial apply stops; no auto-rollback.

`mcplab scenario validate|plan|apply|reset [name]` and `mcplab fixture apply <id>`
are HTTP clients of
labgraph (`LAB_PUBLIC_HOST`:`LABGRAPH_PORT`, `secrets/labgraph-token`).
`make reload APP=labgraph` drops the in-memory journal. Origin
allowlist is exact Origins (loopback implicit; no `"*"`). Preflight
warns when `LAB_PUBLIC_HOST` is not loopback and the list is empty.
Jenkins is not in the integrator.

Point systems under test at `<lab-host>:18888` as `HTTP_PROXY` /
`HTTPS_PROXY`. Inspector / REST / MCP is `:18088` (bearer in
`secrets/labmitm-token`). Install `GET /v1/ca` into the SUT trust store
for HTTPS intercept.

## NFS

`NFS_ARCHIVE_DIR` holds the empty-root fixture `.tar.zst` (`fixtures.tar.zst`
contains only `./`) after `make fixtures`. The export is writable: `-w`
overlay plus live commit (`NFS_COMMIT_OVERLAY_INTERVAL`, default 15m, and
on process exit). `NFS_DATA_DIR` is indexes plus the durable overlay — give
it real disk. Plan ~2× compressed size on the archive dir during a commit.

```bash
NFS_PORT=20490
NFS_COMMIT_OVERLAY_INTERVAL=15m
NFS_ARCHIVE_DIR=.data/nfs
NFS_DATA_DIR=.data/nfs-work

mount -t nfs -o vers=3,tcp,nolock,port=20490,mountport=20490 \
  <lab-host>:/ /mnt
```

`20490` is residual; native dest is 2049.

AUTH_SYS only. No MCP wrapper yet (phase 1). ratarmount-rs **v0.1.28**
also ships NFSv4.1 (`--nfs-vers 4`); this lab stays on v3. After a zstd
overlay commit, remount patches the SQLite sidecar instead of reindexing
the whole TAR. After changing the interval or the image pin,
`make reload APP=nfs`.

## labinfo catalog

Every service in `labinfo/services.yaml` must carry a `connection`
block — labinfo refuses to start without one. URLs are `${VAR}`
templates over the profile env. Host comes from `LAB_PUBLIC_HOST`.
Credentials point at staged copies under `secrets/labinfo-creds/` and
are revealed only when `LAB_DEV_MODE=true`. The default catalog includes
the LabLDAP CA PEM, TacLab lab-user passwords, the TACACS+ shared secret,
and optional TacLab client certs. There is no labinfo catalog service for
the inbound `labinfo-token`. `mcplab creds` / `make creds` prints the same
sheet from those staged files (dev mode only).

A single service, trimmed from the default catalog:

```yaml
services:
  - id: labldap
    name: LabLDAP control plane
    urls:
      - { name: Web UI,    url: https://${LAB_PUBLIC_HOST}:${LABLDAP_HTTPS_PORT}/ }
      - { name: REST API,  url: https://${LAB_PUBLIC_HOST}:${LABLDAP_HTTPS_PORT}/api/v1 }
    credential:
      file: /run/lab-secrets/labldap-token-admin
      usage: "HTTP header 'Authorization: Bearer <token>'"
    connection:
      endpoints:
        - { name: LDAPS, protocol: ldaps, address: ldaps://${LAB_PUBLIC_HOST}:${LABLDAP_LDAPS_PORT} }
      parameters:
        base_dn: dc=example,dc=test
        bind_dn_example: uid=alice,ou=people,dc=example,dc=test
      credentials:
        - name: bind-password-alice
          file: /run/lab-secrets/labldap-user-alice
          usage: "simple-bind password for alice"
```

Adding a service: write the compose service, add a
`mcpjungle/servers/<name>.json` (filename must match JSON `name`), and
add the catalog entry. New ports must be exported into the labinfo
container environment so expansion sees them.

## MCPJungle

Pinned **0.4.6**. `MCPJUNGLE_IMAGE_TAG` is profile-owned; copied profiles
that omit the key still get compose `:-0.4.6`. This lab’s default is
0.4.6, not upstream Compose `latest` / `latest-stdio`. Do not set
`MCPJUNGLE_BIND_HOST` (unset = all interfaces). Operator dashboard is
`GET /` in development mode only (enterprise 404s).

`mcpjungle/servers/*.json` register upstreams with `${ENV}` token
injection. Filename must match the JSON `name`.

```json
{
  "name": "labldap",
  "transport": "streamable_http",
  "description": "Native Go directory lab (labldapd).",
  "url": "https://control:8443/mcp",
  "bearer_token": "${LABLDAP_TOKEN}"
}
```

The curated tool group — this is what most agents should attach to:

```json
{
  "name": "integration",
  "description": "Curated tool subset: LabDNS, LabLDAP, TacLab, LabMail, LabMITM, labinfo.",
  "included_servers": ["labdns", "labldap", "labtacacs", "labinfo", "labmail", "labmitm", "labgraph"]
}
```

Gateway SQLite sits on tmpfs; `make register` reapplies the JSON. Mode
follows `LAB_DEV_MODE` unless you pin `MCPJUNGLE_MODE`.

## One knob: LAB_DEV_MODE

- **false (default)** — enterprise gateway. Clients send
  `Authorization: Bearer $(cat secrets/mcp-client-token)`. labinfo
  describes auth and never reveals secrets. Secret files are
  random-if-missing. If `secrets/.lab-dev-mode` is present from a previous
  dev bring-up, `mcplab secrets` remints orchestrator tokens, force-runs
  LabLDAP `setupsecrets` and TacLab `labgen`, reloads running containers,
  and removes the marker last. LabLDAP TLS is not rotated (extra SANs
  are mode-independent; leaves may still be re-signed if
  `LAB_PUBLIC_HOST` is missing).
- **true** — open gateway, labinfo reveals web tokens and connection
  secrets (LDAP bind password, RADIUS and TACACS+ shared secrets, TacLab
  lab-user passwords, LabLDAP CA PEM), and `mcplab secrets` writes this
  profile's `dev-credentials.yaml` (fail-closed if missing or incomplete;
  no merge with `default`), including TacLab lab-user passwords and AAA
  shared secrets (first mint / pin-bump via `labgen -secrets-from`;
  catalog-only enter-dev still pins after `labgen`). The default profile ships `lab-dev-*`
  values; they are inert unless this knob is on. LabMail and LabMITM tokens
  must be at least 32 bytes (appliance `auth.MinTokenBytes`). `make creds` prints the
  shareable sheet from files on disk (never TLS private keys). Never
  default a shared team profile to dev mode. Set the knob in **that
  profile's** `profile.env` (process env on `default` fails preflight).

Catalog reconcile follows `LAB_DEV_MODE` only — never `MCPJUNGLE_MODE`.
`make smoke` against a dev profile asserts catalog values on the wire
(Alice bind, RADIUS `taclabAdmin`, `connections_list` equal to disk);
default-profile smoke stays random secrets and redaction. CI copies
`profiles/default` to gitignored `profiles/ci-dev/` and sets the knob in
that `profile.env` — never as process env on `default`.

Design and phase-1 OAuth plan: [architecture.md](../architecture.md).
