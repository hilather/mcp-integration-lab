# WS4 — Named fixture packs

Starting ref: `origin/main` @ `3cd249d` (labgraph #21). Helm merges; this change does not merge.

## Investigation (opened before prescriptions)

Landed labgraph is an always-on orchestrator with **one** LabScenario file (`profiles/default/scenarios/default.yaml`, `spec: {}`). There is no fixture-pack type, no MCP resources, no `fixture.apply`.

Facts from files:

| Fact | File |
|------|------|
| Kind `mcplab.dev/v1alpha1` `LabScenario`; `spec` keys `labdns`/`labmitm`/`maildev`/`labldap`/`labtacacs` only; `KnownFields(true)` | `internal/labgraph/schema.go` |
| Apply order DNS → MITM → mail → LDAP → TacLab; stop, no rollback | `schema.go` `ApplyOrder`; `app.go` `walk` |
| Family sections: `operations[]` → native `/v1/changes:*`; document shape is validate-only | `section.go`, `app.go` |
| **Any present `spec.labldap` fails closed today** (`errNoLDAPApply`); TacLab same | `app.go` 264–278 |
| LabLDAP reset already talks control REST | `clients.go`, `app.go` `resetOne` |
| LabLDAP has **no TLS-rotate route**. labgraph stages `labldap-ca.crt` only | `third_party/go-lab-ldap-mcp/internal/api/server.go`; `cmd/labgraph/main.go` |
| `expirePassword` = must-change on **bind-test only**; does not change the password | `mcpserver/catalog.go` |
| `disableUser` sets `nsAccountLock`. Data-plane bind refuses locked/disabled **before password compare** (unwillingToPerform 53) | `catalog.go`; `ldapserver/op_attrs.go` `accountLocked` |
| `AccountState.Revision` is **not** `User.Revision` | `directory/types.go` |
| Enable/disable If-Match is **User** ETag from `GET /api/v1/users/{id}` (`writeEntity` → `SetETag`) | `users.go` `setUserEnabled`; `directory.go` `writeEntity`; `users_test.go` |
| `scenario_get` / `GET /v1/scenarios/{name}` return `{name,kind,apiVersion}` **only — no spec** | `mcp.go` 35; `rest.go` 65 |
| LabDNS add is `{op, target, value}` decoded into `model.Zone` / `model.ClientGroup` | `go-lab-dns` `operation.go`, `app/operations.go` |
| Overlay is a **second zone name** (duplicate `lab.test.` is invalid) | `pack-sample.yaml`; `testdata/config/invalid/duplicate-zone-name.yaml` |
| `clientGroup` is CIDRs + `allowForward` / `chaosExempt` — it does **not** attach to a zone | `model/spec.go` |
| LabMITM `replaceTLS` needs the full `tls` subtree; bootstrap `ports: [443]` | `labmitm/bootstrap.yaml`; `operation.go` |
| MCP tools only; no resources; pin `2026-07-28` + `allowLegacyClients` | `mcp.go`, `protocol.go` |
| Registry is the single catalog | `ops.go` |
| SPA cookie+CSRF, no localStorage | `web/index.html` |
| `LABGRAPH_PORT=18091`; do not steal Ilya 8080/18088/18080/1025/1080/8443 | `profile.env` |
| LabMITM pin already `v1.6.0` — do not rewrite `vendor.go` | `internal/lab/vendor.go` |
| `errMITMDocument` v1.5.0 wording is a follow-on, not this PR | `app.go` |
| Jenkins gone; KnownFields rejects `spec.jenkins` | `schema_test.go` |
| `make reset` = compose `down -v` | `internal/lab/stack.go` |
| Smoke: empty `default` only; unmarshals `scenario_get` `name` only | `smoke.go` `labgraphScenario` |
| mcp-go `v0.58.0` already in `go.mod` | `go.mod` |

## Goal

Four named packs as `LabScenario` files. Agents **load** pack YAML (native resource `labgraph://fixtures/{id}` **and** gateway-safe `scenario_get` with spec). Agents **apply** via `fixture.apply` → `Service.Apply` (never MCP→REST). `make reset` / `scenario.reset` restores bootstraps. Empty `default` stays a no-op.

Exact ids: `broken-bind`, `expired-cert`, `split-horizon-dns`, `mitm-intercept-extra-port`.

**Do not ship `jenkins-auth-flip`.**

## Design

### Packs are LabScenario files

`profiles/default/scenarios/<id>.yaml` (already mounted at `/etc/labgraph/scenarios`).

```yaml
apiVersion: mcplab.dev/v1alpha1
kind: LabScenario
metadata:
  name: <id>           # equals filename stem
  description: "..."   # add optional Metadata.Description (KnownFields)
spec:
  # only appliances this pack mutates
```

`default.yaml` is not a fixture. Closed `FixtureIDs` in code. Unknown `spec` keys fail closed.

### Load path (spec is missing today)

`scenario_get` / `GET /v1/scenarios/{name}` today omit `spec`, so agents **cannot** load YAML through the gateway. Fix that in this PR:

- Expand `scenario_get` and REST GET to `{name, kind, apiVersion, description, spec}` where `spec` is the decoded appliance map (same keys as the file). Smoke only reads `name` — still green.
- MCP resources: `AddResource` for each fixture id, URI exact `labgraph://fixtures/{id}`. Handler calls `GetFixture` (below). `WithResourceCapabilities(false, false)`.
- Native MCP clients use `labgraph://fixtures/{id}`. Gateway agents use `labgraph__scenario_get` (MCPJungle 0.4.6 may rewrite resource URIs; we do **not** depend on gateway `resources/read`).
- `GetFixture(id)` returns `FixtureView` (Service method). Used by the resource handler and `GET /v1/fixtures/{id}`.
- Register `fixture.get` as **REST_ONLY** (`GET /v1/fixtures/{id}`) so it is in the catalog (same class as `health.*`). MCP twin is the resource, not a new tool. Do **not** add `fixture.list` / `fixture_get` tools.

```go
type FixtureView struct {
	ID          string          `json:"id"`
	APIVersion  string          `json:"apiVersion"`
	Kind        string          `json:"kind"`
	Description string          `json:"description,omitempty"`
	Spec        json.RawMessage `json:"spec"`
	Material    *FixtureMaterial `json:"material,omitempty"`
}
type FixtureMaterial struct {
	Kind     string `json:"kind"`               // "expired-tls-leaf" only
	CertPEM  string `json:"certPem,omitempty"`  // PUBLIC only
	NotAfter string `json:"notAfter,omitempty"` // RFC3339
	Subject  string `json:"subject,omitempty"`
}
```

Fail-closed: `GetFixture` and every file under `scenarios/` must not contain `PRIVATE KEY`.

### `fixture.apply` — one new PARITY_REQUIRED row

| ID | REST | MCP | UI | ServiceMethods |
|----|------|-----|-----|----------------|
| `fixture.apply` | `POST /v1/fixtures/{id}:apply` | `fixture_apply` | `/fixtures/:id` mutate | `["Apply"]` |

`fixture_apply` and the REST handler call `svc.Apply` directly. Same walk, same stop-on-failure, no rollback. `id` must be in `FixtureIDs` (reject `default` on this route so operators do not think apply-default is a pack). `scenario.apply default` stays the empty no-op.

### LabLDAP control apply (not flatten, not family `/v1/changes`)

`walk` already special-cases `labldap` **before** `FamilyClient`. Keep that branch. Do **not** POST LabLDAP bodies to `/v1/changes:*`.

Allow **one** control verb in this PR (what `broken-bind` needs):

| `op` | REST | If-Match |
|------|------|----------|
| `disableUser` | `POST /api/v1/users/{id}/disable` | `GET /api/v1/users/{id}` → `ETag` header (`User.Revision`). Never `GET .../account-state` (`AccountState.Revision` is a different object). |

Exact pack fragment (not a family `{target,value}` envelope):

```yaml
spec:
  labldap:
    operations:
      - op: disableUser
        id: alice
```

Unknown `op`, missing `id`, document shape (`apiVersion`+`kind`), or flatten keys (`users`, `groups`, `suffix`) → `errNoLDAPApply` / fail closed. TacLab unchanged.

Validate/plan: allowlist check; optional GET user (no POST). Apply: GET user → `If-Match: <ETag>` → POST disable. Extend `LDAPClient` with `GetUser` + `DisableUser` (and `fakeLDAP`).

This is the same class as reset (control REST), not a union of `labldap.dev/v1alpha1` desired-state.

Do **not** ship `expirePassword` / `setPassword` / `lockUser` in the allowlist this PR. expirePassword does not fail data-plane simple bind; setPassword would put a secret in a pack.

### The four packs

**1. `broken-bind`** — data-plane bind failure via `nsAccountLock`.

YAML: `disableUser` on `alice` (above). Description must say: disable sets `nsAccountLock`; native bind refuses before password compare (`accountLocked`). Not “expired password” (that is bind-test `must_change` only). Reset (`POST /api/v1/reset`) re-enables alice from compiled `labldap/scenario.yaml`.

**2. `expired-cert`** — TLS **client-path** material; no appliance TLS rotate.

Contract (not implement-time):

- Pack YAML: `spec: {}`.
- `fixture.apply expired-cert` is a **successful no-op** on appliances (zero family/LDAP client calls), same walk as empty default. It does **not** error. It does **not** resign `directory.crt`. It does **not** mount `ca.key`.
- `GetFixture("expired-cert")` always returns `material.kind = expired-tls-leaf` with a self-signed leaf generated in memory (`NotAfter = now - 1h`). Public PEM only. `priv` discarded after `CreateCertificate`. Never logged, never in journal, never in resource/REST JSON.
- Consumer: an SUT **TLS client** is configured with `material.certPem` (as a presented client cert or as a trust-store candidate). Verification fails because `notAfter` is in the past. The lab LDAPS listener is unchanged.

**3. `split-horizon-dns`** — LabDNS overlay override (not BIND views).

Exact native change-set (family fan-out already works). Overlay is a **different** zone name. `clientGroup` is an access class, not a per-zone view:

```yaml
spec:
  labdns:
    operations:
      - op: add
        target: {kind: zone, id: split-horizon}
        value:
          id: split-horizon
          name: split.lab.test.
          mode: overlay
          records:
            - id: api-a
              owner: api
              type: A
              values: ["10.42.0.10"]
      - op: add
        target: {kind: clientGroup, id: internal-horizon}
        value:
          id: internal-horizon
          cidrs: ["10.99.42.0/24"]
          allowForward: false
```

`api.split.lab.test.` answers `10.42.0.10` (overlay). `lab.test.` bootstrap zone is untouched. `10.99.42.0/24` is default `LAB_DOCKER_SUBNET` (local-only / no forward). Reset: LabDNS `POST /v1/state:reset`.

**4. `mitm-intercept-extra-port`** — `replaceTLS` ports; overlay file stays `[443]`.

```yaml
spec:
  labmitm:
    operations:
      - op: replaceTLS
        tls:
          intercept: true
          hosts: []
          ports: [443, 9443]
          ca: {mode: generate, certFile: "", keyFile: ""}
          upstream: {insecureSkipVerify: false, extraCAFiles: []}
```

9443 is an intercept **dest**, not a host publish (avoids Ilya 8443 and LabLDAP control). Generate-mode CA rotates on apply (upstream). Reset restores `[443]`. Do not edit `profiles/default/labmitm/bootstrap.yaml`.

### Reset / wipe

| Action | Effect |
|--------|--------|
| `scenario.reset <pack> --appliances=...` | Native reset → bootstraps |
| `make reset` | `down -v`; next `make up` starts from bootstraps; pack **files** remain (profile) |
| `reload APP=labgraph` | drops journal only; does not undo appliance mutations |

Do not change `make reset` into a second fan-out. Smoke must not reset-all.

### Smoke / pins

- Smoke stays on empty `default`. Do not apply the four packs in default smoke.
- `make test` green; no live docker in unit tests.
- Azure-free. TacLab conformance stays partial.
- Do not rewrite `vendor.go`. Do not edit `third_party/`. Do not change `errMITMDocument` string.

### CLI / SPA / labinfo

- `mcplab fixture apply [id]` aliases `Client.Apply` (same token). Usage text + `TestCLIUsageDocumentsScenario`.
- SPA: cookie+CSRF; apply via `POST /v1/fixtures/{id}:apply`; list via existing `GET /v1/scenarios`. No localStorage.
- labinfo `labgraph` description + `connection.parameters`: fixture ids, `labgraph://fixtures/{id}`, `fixture.apply`. Keep URLs + credential block. Update `mcpjungle/servers/labgraph.json` description.

### Docs (same change)

AGENTS.md labgraph quirk; README.md `scenarios/*.yaml` tree; `docs/architecture.md` + Pages that mention labgraph (`architecture.html`, `configure.html`, `services.html`, `docs/index.html`, `docs/guides/configuration.md`); CHANGELOG `[Unreleased]`; CLI usage; distinguish NFS `make fixtures`; sweep stale “LabLDAP has no apply”.

### Out of scope

Jenkins/Entra; `errMITMDocument` wording; vendor pin; host CA resign; LabNTP; `LABGRAPH_PORT` change; applying packs in smoke; BIND views; flatten `labldap/scenario.yaml`; `expirePassword`/`setPassword` allowlist; `fixture_list`/`fixture_get` tools.

## Tests (no docker)

| Test | Assert |
|------|--------|
| default_profile | four files; names match ids; `default` still `spec: {}`; MITM bootstrap still `[443]`; no jenkins |
| KnownFields | `spec.jenkins` rejected; packs parse |
| no private key | scenarios dir + `GetFixture` JSON have no `PRIVATE KEY` |
| expired-cert | `Apply` OK, zero client calls; `GetFixture` leaf `NotAfter` before now; PEM is CERTIFICATE |
| broken-bind | fakeLDAP `GetUser` then `DisableUser` with If-Match; flatten `users:` still fail-closed; TacLab still fail-closed |
| split-horizon | family apply body matches the YAML above |
| mitm extra port | apply body ports `[443, 9443]`; bootstrap file still `[443]` |
| registry | `fixture.apply` parity; `fixture.get` REST_ONLY; no fixture.list/get tools |
| resources | four `labgraph://fixtures/{id}` |
| scenario_get | includes `spec` |
| empty default | still zero client calls |
| CLI | `fixture apply broken-bind` parses |

## Acceptance

`make test` green. Smoke remains default no-op. PR not draft, not merged. Jenkins-free. No private keys in packs/resources. PR body last line: `— Mud Turtle`.
