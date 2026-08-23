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
  profile.env              ports, LAB_PUBLIC_HOST, LAB_DEV_MODE, storage
  dev-credentials.yaml     lab-only catalog (used iff LAB_DEV_MODE=true)
  labdns/bootstrap.yaml    permanent DNS zones and records
  labldap/scenario.yaml    directory users, ACLs, TLS, MCP features
  labinfo/services.yaml    endpoint + connection catalog
  labmail/bootstrap.yaml   LabMail desired state (relay keys rejected)
  labmitm/bootstrap.yaml   LabMITM desired state (exact Origins; no "*")
  mcpjungle/
    servers/*.json         upstream MCP registrations
    groups/integration.json curated tool group
```

TacLab’s baseline is generated, not hand-written. `labgen` materializes
users, groups, clients, policies, PKI, and shared secrets into
`third_party/go-lab-tacacs-mcp/deployments/compose/` on first `make up`.
The profile owns the host ports (`TACLAB_*`). When `LAB_DEV_MODE=true`,
`mcplab secrets` post-processes the secret files to the catalog (token,
shared secrets, lab-user passwords, Argon2id verifiers) and leaves PKI
and YAML alone.

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
key is required. LabLDAP passwords must be at least 12 characters; TacLab
shared secrets must pass the appliance policy (length ≥16, ≥3 character
classes, exact-match known-weak list). The active profile's file is the only
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

`make preflight` (also run automatically by `make up` and `make register`) checks:

1. **Profile drift** — critical endpoint/mode values in `.env` or process env
   must match `profiles/<name>/profile.env` (unless
   `MCPLAB_ALLOW_PROFILE_OVERRIDES=true`).
2. **Host ports** — every published port from the active profile must be free,
   or already held only by this lab's Docker containers (idempotent restarts).

If a profile uses IANA port 53 for DNS and systemd-resolved already holds it,
preflight fails before compose starts. Stop the conflicting service or pick a
different `LABDNS_DNS_PORT` in your team profile.

## profile.env — every knob

| Variable | Default | What it is |
| --- | --- | --- |
| `LABDNS_DNS_PORT` | `10053` | DNS data plane (udp+tcp). `53` usually collides with systemd-resolved. |
| `LABDNS_REST_PORT` | `18080` | LabDNS REST `/v1` + MCP `/mcp` + operator console `GET /`. |
| `LABLDAP_LDAP_PORT` | `3389` | LDAP (StartTLS). Cleartext simple bind is disabled. |
| `LABLDAP_LDAPS_PORT` | `3636` | LDAPS. Cert SAN is `directory`. |
| `LABLDAP_HTTPS_PORT` | `8443` | LabLDAP UI + REST + MCP, lab TLS. |
| `MCP_GATEWAY_PORT` | `8080` | MCPJungle streamable HTTP. |
| `NFS_PORT` | `20490` | ratarmount-rs userspace NFSv3 (writable overlay). |
| `TACLAB_LEGACY_PORT` | `49` | TACACS+ (RFC 8907). |
| `TACLAB_TLS_PORT` | `300` | TACACS+ TLS 1.3 (RFC 9887). |
| `TACLAB_RADIUS_ACCESS_PORT` | `1812` | RADIUS access (udp). Message-Authenticator required. |
| `TACLAB_RADIUS_ACCT_PORT` | `1813` | RADIUS accounting (udp). |
| `TACLAB_RADIUS_RADSEC_PORT` | `2083` | RadSec. Published; listener default off. |
| `TACLAB_RADIUS_DYNAUTH_PORT` | `3799` | RADIUS DAS. Published; listener default off. |
| `TACLAB_HTTP_PORT` | `18049` | TacLab UI + REST + MCP. |
| `MAILDEV_SMTP_PORT` | `1025` | Receive-only SMTP ingest. |
| `MAILDEV_WEB_PORT` | `1080` | LabMail UI + `/email` + `/v1` + MCP. |
| `MAILDEV_WEB_USER` | `admin` | Web basic-auth user. Frozen at `admin` (YAML does not interpolate this). Password is minted, or the catalog `maildevWeb` value in dev mode. |
| `LABMITM_PROXY_PORT` | `18888` | Unauthenticated HTTP/1.1 forward proxy (absolute-form + CONNECT). |
| `LABMITM_WEB_PORT` | `18088` | LabMITM inspector UI + `/v1` + MCP. |
| `LABINFO_PORT` | `18090` | Service-directory MCP. |
| `LAB_PUBLIC_HOST` | `localhost` | Hostname labinfo puts in every URL. Set this to the name remote testers use. |
| `LAB_DEV_MODE` | `false` | Single security knob. See below. Also consumes `dev-credentials.yaml`. |
| `MCPJUNGLE_MODE` | follows `LAB_DEV_MODE` | Pin to decouple gateway mode from labinfo reveal and catalog reconcile. |
| `NFS_ARCHIVE_DIR` | `.data/nfs` | Empty-root `fixtures.tar.zst` (writable; live commit replaces it). |
| `NFS_DATA_DIR` | `.data/nfs-work` | Indexes plus the durable write overlay. Give it real disk. |
| `NFS_COMMIT_OVERLAY_INTERVAL` | `15m` | How often overlay writes are spliced into the `.tar.zst`. |

## Reload vs full redeploy

`make up` vendors, mints or reconciles secrets, builds every image, starts three compose
projects, and registers the gateway. Use it for first bring-up, a vendor pin
bump, or a profile switch. After a `dev-credentials.yaml` or `LAB_DEV_MODE`
edit, `mcplab secrets` reloads running apps whose files changed; `make up`
skips those names.

After that, recreate **one** application:

```bash
make reload APP=labdns|maildev|nfs|labinfo|mcpjungle|labldap|labtacacs|labmitm
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
| TacLab labgen output / image | `make reload APP=labtacacs` | in-process AAA state gone; labgen files stay |
| `labmitm/bootstrap.yaml` | `make reload APP=labmitm` | captured flows gone; generate-mode CA rotates |

`make labldap-up` / `make labtacacs-up` are idempotent project bring-up
(the path `make up` uses). They do not force-recreate a running directory.

## LabDNS

`labdns/bootstrap.yaml` is the desired state, mounted read-only. MCP
`dns_state_reset` returns the service to this file. After editing the file,
`make reload APP=labdns` recreates only LabDNS. Non-loopback peers
present the bearer in `secrets/labdns-token`.

LabDNS **v1.1.0** serves an operator console at `GET /` on
`LABDNS_REST_PORT` (`spec.ui.enabled`, default true). Paste the bearer on
the login screen. Loopback Origins are allowed. A remote browser must list
its Origin in `spec.management.allowedOrigins` as an exact
`http(s)://host[:port]` string (no `"*"` sentinel). `spec.ui.enabled:
false` 404s the SPA only; REST and MCP stay.

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

`labldap/scenario.yaml` is a LabScenario. LabLDAP **v0.3.0** defaults to
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
    insecureLabMode: false
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
- LDAPS cert SAN is `directory`. Trust
  `third_party/go-lab-ldap-mcp/secrets/tls/ca.crt`.
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
In `LAB_DEV_MODE=true` those values (and the TACACS+/RADIUS shared secrets)
come from `dev-credentials.yaml`. RADIUS Access-Requests must carry
Message-Authenticator (RFC 3579).

## LabMail

The compose service name stays `maildev`. Desired state is
`labmail/bootstrap.yaml` (`labmail.dev/v1alpha1`). `internal/maildev`
rejects leftover `maildev/maildev.yaml` and every relay/outbound key.
Host ports and web Basic/bearer files are lab-managed. `allowLegacyClients:
true` is required for MCPJungle. After editing, `make reload APP=maildev`
(wipes the inbox).

LabMail **v1.0.0-rc.3** hashed inbox JS sends `Origin`. An empty
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

Point the system under test at `<lab-host>:1025`. Read captured mail at
`:1080` (Basic user `admin`). Native REST is `/v1`; MCP is `/mcp` (bearer
in `secrets/labmail-token`). Nothing is relayed. Captured mail is wiped on
restart.

## LabMITM

Desired state is `labmitm/bootstrap.yaml` (`labmitm.dev/v1alpha1`), a
lab-owned overlay copy — do not recopy from the v1.1.0 examples tree.
`allowLegacyClients: true` is required for MCPJungle. Compose must pass
`--management-listen=:8088`. After editing, `make reload APP=labmitm`
(wipes captured flows; generate-mode CA rotates).

The HTTP/1.1 data plane is **unauthenticated**. Do not publish without a
network boundary. HTTPS intercept is **:443 only**; CONNECT to LabLDAP
LDAPS or TacLab TLS is tunnel-not-decrypt. `allowHosts` is HTTP-useful
compose DNS (`*.lab`, labdns, labinfo, maildev, mcpjungle, control,
taclab).

Origin allowlist is exact Origins (loopback already allowed; no `"*"` /
`"private"`). When `LAB_PUBLIC_HOST` is not loopback, add
`http://<LAB_PUBLIC_HOST>:18088` (or the profile's `LABMITM_WEB_PORT`)
or the inspector SPA 403s `/v1`. Preflight warns (never fails) if the
allowlist is empty.

```yaml
apiVersion: labmitm.dev/v1alpha1
kind: LabMITM
metadata:
  name: lab-proxy
spec:
  listeners:
    proxy: { address: ":8888" }
    management: { address: ":8088", restPath: /v1, mcpPath: /mcp }
  proxy:
    hostname: labmitm.lab
    targets:
      allowHosts: ["*.lab", labdns, labinfo, maildev, mcpjungle, control, taclab]
  tls:
    intercept: true
    ports: [443]
  management:
    auth:
      mode: bearer
      tokens:
        - { id: admin, secretFile: /run/secrets/labmitm-token, role: administrator }
    mcp:
      allowLegacyClients: true
    originAllowlist: []
```

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

AUTH_SYS only. No MCP wrapper yet (phase 1). ratarmount-rs **v0.1.24**
also ships NFSv4.1 (`--nfs-vers 4`); this lab stays on v3. After changing
the interval or the image pin, `make reload APP=nfs`.

## labinfo catalog

Every service in `labinfo/services.yaml` must carry a `connection`
block — labinfo refuses to start without one. URLs are `${VAR}`
templates over the profile env. Host comes from `LAB_PUBLIC_HOST`.
Credentials point at staged copies under `secrets/labinfo-creds/` and
are revealed only when `LAB_DEV_MODE=true`.

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
  "included_servers": ["labdns", "labldap", "labtacacs", "labinfo", "labmail", "labmitm"]
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
  and removes the marker last. LabLDAP TLS is not rotated.
- **true** — open gateway, labinfo reveals web tokens and connection
  secrets (LDAP bind password, RADIUS shared secret), and `mcplab secrets`
  writes this profile's `dev-credentials.yaml` (fail-closed if missing or
  incomplete; no merge with `default`), including TacLab lab-user
  passwords and AAA shared secrets after `labgen`. The default profile ships
  `lab-dev-*` values; they are inert unless this knob is on. Never
  default a shared team profile to dev mode. Set the knob in **that
  profile's** `profile.env` (process env on `default` fails preflight).

Catalog reconcile follows `LAB_DEV_MODE` only — never `MCPJUNGLE_MODE`.

Design and phase-1 OAuth plan: [architecture.md](../architecture.md).
