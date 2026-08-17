# Configure the lab

Everything a team varies lives in a profile. Compose files stay generic. Secrets are generated and gitignored. Runtime mutations are wipeable.

The same reference is published as [configure.html](https://hilather.github.io/mcp-integration-lab/configure.html).

## Who owns what

- **This repo** owns profiles, secrets layout, compose overlays, the `mcplab` CLI, and gateway policy.
- **Vendored repos** own the appliances. Do not edit `third_party/` in place — add a patch under `patches/` and send it upstream.
- **Generated files** — `secrets/` and `third_party/*/secrets/` — are produced by `mcplab secrets` and never committed.

## A profile directory

```
profiles/<name>/
  profile.env              ports, LAB_PUBLIC_HOST, LAB_DEV_MODE, storage
  labdns/bootstrap.yaml    permanent DNS zones and records
  labldap/scenario.yaml    directory users, ACLs, TLS, MCP features
  labinfo/services.yaml    endpoint + connection catalog
  maildev/maildev.yaml     maildev flags (relay flags rejected)
  mcpjungle/
    servers/*.json         upstream MCP registrations
    groups/integration.json curated tool group
```

TacLab’s baseline is generated, not hand-written. `labgen` materializes users, groups, clients, policies, PKI, and shared secrets into `third_party/go-lab-tacacs-mcp/deployments/compose/` on first `make up`. The profile only owns the host ports (`TACLAB_*`).

To create a team profile, copy `profiles/default` and edit. Never hardcode a port in `docker-compose.yaml` — add a variable to `profile.env` with a compose default.

## Env precedence

Highest wins:

1. Process environment
2. `.env` at the repo root (selects `PROFILE=` and may override any value)
3. `profiles/<name>/profile.env`
4. Compose-file defaults (match the default profile)

```bash
# .env
PROFILE=default
# LABDNS_DNS_PORT=53   # uncomment to override the profile
```

## LabDNS

`labdns/bootstrap.yaml` is the desired state, mounted read-only. MCP `dns_state_reset` returns the service to this file. The default profile is authoritative for `lab.test.` with `ns1`, `nfs`, `ldap`, and a `*.tools` wildcard.

Host ports: `LABDNS_DNS_PORT` (data plane) and `LABDNS_REST_PORT` (REST `/v1` + MCP `/mcp`). Non-loopback peers present the bearer in `secrets/labdns-token`.

## LabLDAP

`labldap/scenario.yaml` is a LabScenario. This lab pins `directory.engine: native` (labldapd, not 389 DS), management TLS from lab-CA files, and `registerMutations` / `registerPassword` so account-workflow tools are live.

- Suffix: `dc=example,dc=test`
- Seed user: `alice` in group `staff`
- Cleartext simple bind: disabled. StartTLS or LDAPS.
- LDAPS cert SAN is `directory`. Trust `third_party/go-lab-ldap-mcp/secrets/tls/ca.crt`.

## TacLab

Do not hand-write TacLab configs. `mcplab secrets` runs `labgen` and sets `api.mcp.allow_legacy_clients: true` so the gateway’s older MCP client can connect. Lab-user passwords land in `secrets/PASSWORDS.txt` under the TacLab compose tree. RADIUS Access-Requests must carry Message-Authenticator (RFC 3579). RadSec (2083) and DAS (3799) are published but default off.

## maildev

Any maildev long flag can go under `flags:` in `maildev/maildev.yaml`. The CLI renders it onto the container command line and **rejects** every `outgoing-*` and `auto-relay*` flag. Host ports and web basic-auth are lab-managed.

```yaml
flags: {}
  # verbose: true
  # incoming-user: smtp-user
  # incoming-pass: smtp-pass
```

## NFS

`NFS_ARCHIVE_DIR` is the read-only archive tree. `NFS_DATA_DIR` is writable scratch for SQLite indexes — give it real disk. Mount with `vers=3,tcp,nolock,port=…,mountport=…`. AUTH_SYS only. No MCP wrapper yet (phase 1).

## labinfo catalog

Every service in `labinfo/services.yaml` must carry a `connection` block — labinfo refuses to start without one. URLs are `${VAR}` templates over the profile env. Host comes from `LAB_PUBLIC_HOST`. Credentials point at staged copies under `secrets/labinfo-creds/` and are revealed only when `LAB_DEV_MODE=true`.

Adding a service: write the compose service, add a `mcpjungle/servers/<name>.json` (filename must match JSON `name`), and add the catalog entry. New ports must be exported into the labinfo container environment so expansion sees them.

## MCPJungle

`mcpjungle/servers/*.json` register upstreams with `${ENV}` token injection. `groups/integration.json` is the curated subset (labdns, labldap, labtacacs, labinfo). Gateway SQLite sits on tmpfs; `make register` reapplies the JSON. Mode follows `LAB_DEV_MODE` unless you pin `MCPJUNGLE_MODE`.

## One knob: LAB_DEV_MODE

- **false (default)** — enterprise gateway. Clients send `Authorization: Bearer $(cat secrets/mcp-client-token)`. labinfo describes auth and never reveals secrets.
- **true** — open gateway, and labinfo reveals web tokens and connection secrets (LDAP bind password, RADIUS shared secret). Never default a shared team profile to dev mode.

Design and phase-1 OAuth plan: [architecture.md](../architecture.md).
