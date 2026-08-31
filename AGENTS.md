# Agent guide — MCP Integration Lab

This repo orchestrates AI-ready lab services (DNS, LDAP, TACACS+/RADIUS,
mail, NFS, HTTP intercept, NTP, SSO, LabScenario orchestration) behind one MCP gateway for integration testing. It owns all configuration, secrets layout,
and gateway policy; the services themselves are vendored. These are the rules
we work by.

## Ground rules

1. **Configuration lives in profiles.** Everything a team varies — host ports,
   gateway mode, storage paths, service YAML/JSON — belongs in
   `profiles/<name>/` (see `profiles/default`). Never hardcode a port or a
   config path in `docker-compose.yaml`; add a variable to `profile.env` with
   a sane default in the compose file. `.env` selects the profile (`PROFILE=`)
   and may override individual values; process env overrides both. Only
   `profiles/default` is shipped; other `profiles/<name>/` directories are
   gitignored so teams can maintain local profiles across `git pull`.
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
   test against the lab; host ports are profile-defined (IANA/native dests
   for protocol data planes — rule 15), and container listen ports are
   unprivileged and irrelevant to consumers. Don't bind new services to
   loopback; do give them bearer/TLS auth like the existing ones.
6. **Never commit generated/runtime secrets.** `secrets/` and
   `third_party/*/secrets/` are produced by `mcplab secrets` and gitignored.
   Documented lab-only values in `profiles/<name>/dev-credentials.yaml` are
   allowed and are inert unless `LAB_DEV_MODE=true`.
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
 Message-Authenticator, SSO issuer/JWKS), and the on-the-wire credentials (bind passwords,
 shared secrets). Any credential — web token or connection secret — is
 referenced from a staged copy under `secrets/labinfo-creds/` (add it to
 `stageLabinfoCreds` in `internal/lab/secrets.go`) so dev mode can reveal
 it; new ports go in the labinfo service's compose environment so `${VAR}`
 expansion sees them.
10. **Dev mode is one knob: `LAB_DEV_MODE` in the profile.** `true` opens the
 gateway (MCPJungle development mode, no client auth), makes labinfo
 reveal credentials (web-service tokens and connection secrets alike,
 including the LabLDAP CA PEM, LabSSO CA PEM, IdP Alice password, TacLab lab-user passwords, and the TACACS+
 shared secret), and reconciles secret files from that profile's
 `dev-credentials.yaml` (no merge with `default`; fail-closed if the
 catalog is missing). LabMail, LabMITM, LabNTP, and LabSSO tokens must be ≥32 bytes
 (`auth.MinTokenBytes`); Validate fail-closes so a short catalog cannot
 crash-loop `maildev`/`labmitm`/`labntp`/`labsso`. `mcplab creds` / `make creds` prints the same sheet
 from files on disk (fails closed outside dev; never prints TLS private
 keys). `false` (default) hardens the gateway (enterprise: client tokens +
 ACLs), labinfo only describes how auth works, and minting stays
 random-if-missing. Leaving dev mode remints orchestrator tokens,
 `setupsecrets --force`, and `labgen -force`; `Secrets()` reloads running
 containers. `MCPJUNGLE_MODE` may still be pinned explicitly to decouple
 gateway mode from catalog reconcile and labinfo reveal. Never default a
 shared/team profile to dev mode.
11. **The mail sink never sends mail.** Compose service name and labinfo
    catalog id stay `maildev` for the swap release (rename later, not in
    the image-pin change). The image is LabMail (`go-lab-maildev`, pinned
    `v1.0.0-rc.4`). Desired state is
    `profiles/<name>/labmail/bootstrap.yaml` (`labmail.dev/v1alpha1`).
    Receive-only is structural in LabMail: no outbound SMTP, reserved-key
    reject, `POST /email/:id/relay` is 403. `internal/maildev` fail-closes
    on leftover `maildev/maildev.yaml`, relay/outbound keys, and implicit
    SMTPS — keep that guard tested. Do not reintroduce `MAILDEV_ARGS` /
    `MAILDEV_WEB_PASS` injection in `runner.go`. Host ports stay
    `MAILDEV_SMTP_PORT` / `MAILDEV_WEB_PORT`. Web Basic username is frozen
    at `admin` (`MAILDEV_WEB_USER=admin`; LabMail YAML does not interpolate
    that env — changing profile.env alone 401s smoke). Password and bearer
    files are `secrets/maildev-web-password` and `secrets/labmail-token`,
    both **0o644** so UID 65532 can read the bind-mounts (0o600 was only
    safe while MAILDEV_WEB_PASS was injected). They share one principal via
    `tokenRef`. If a profile must change the Basic user, it must also edit
    `spec.management.auth.basic.username`. `allowLegacyClients: true` is
    required for MCPJungle. Do not add relay/outbound keys. Implicit SMTPS
    (`incoming-secure`) is 1.1; do not silently map it to STARTTLS.
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
15. **Native host ports.** Protocol data planes SUTs already speak use the
    IANA/standard dest on the **host**: DNS 53, NTP 123, LDAP 389 / LDAPS
    636, SMTP 25, TACACS+ 49 / 300, RADIUS 1812/1813, NFS 2049, HTTPS 443
    (LabSSO). Container
    listen ports are unprivileged and irrelevant to consumers (compose maps
    host:container). Management/control planes stay high ports (collision
    with the gateway and with each other is fine).
    - LabMITM is a **forward proxy** (SUT sets `HTTP_PROXY`), not an IANA
      dest-443 service. Do not move `LABMITM_PROXY_PORT` to 443. HTTPS
      intercept of CONNECT `:443` stays inside the proxy. LabSSO **is**
      dest-443 (`LABSSO_HTTPS_PORT`). NFS userspace
      2049 is native when remapped; 20490 today is residual.
    - Operator escape (non-native host port) is allowed in `profile.env`
      when preflight cannot free the IANA port; it is an escape, not the
      default policy.
    - **Preflight host occupancy** (already in `internal/lab/ports.go`):
      every published host port. `EACCES` / `EPERM` is NOT occupied
      (TacLab 49; dockerd can publish). Occupancy is `/proc/net` TCP
      LISTEN / UDP bound. Fail closed if a non-lab process holds the port.
    - **Preflight Docker/host feature knobs.** When a lab feature depends
      on a Docker daemon or host setting, add a preflight that fails
      closed **with the configuration change in the error**. Do not boot
      a lab that pretends the feature works. LabNTP is in compose;
      **do not** add a Go `userland-proxy` probe (appliance ADR 0014:
      document NAT collision; compose-network + `views.preview` are the
      reliable paths). The daemon.json / Desktop-cannot copy below is
      operator documentation only.
    - **Error messages must name the fix.** Normative copy for when those
      checks exist:
      - DNS 53 held by systemd-resolved: stop/disable resolved **or**
        extra IP for `LAB_PUBLIC_HOST` **or** escape `LABDNS_DNS_PORT`
        (SUTs that cannot set dest port cannot follow the escape).
      - NTP 123 held by systemd-timesyncd: stop/disable timesyncd (lab
        host clock may drift; LabNTP never settimeofday) **or** extra IP
        **or** escape `LABNTP_NTP_PORT=10123` (timesyncd/W32Time cannot).
      - HTTPS 443 held by nginx/caddy/apache/another compose stack:
        stop/disable the occupant **or** extra IP for `LAB_PUBLIC_HOST`
        **or** escape `LABSSO_HTTPS_PORT` (SUTs that hardcode dest 443
        cannot follow).
      - `userland-proxy` true: `/etc/docker/daemon.json`
        `{"userland-proxy": false}` then `systemctl restart docker`.
      - Docker Desktop / VM NAT cannot preserve UDP source: Linux dockerd
        + `userland-proxy` false, or macvlan/ipvlan. Do not start that
        feature there.
    - **Today's default profile is residual**, not a second policy.
      Residuals (do not change these numbers until the per-service remap
      PR): DNS 10053 → native 53; LDAP 3389/3636 → 389/636; SMTP 1025 →
      25; NFS 20490 → 2049. TacLab 49/300/1812/1813 already native.
      Management ports stay high (18080, 18088, 18049, 8080, 8443, 1080,
      18090, 18091, 18123, 18443). LabNTP data-plane default 10123 is the FR /
      ADR 0014 publish (not a residual-as-design); privileged 123 is
      the operator escape. Port remaps for residual dests are follow-on
      PRs per service, with preflight errors, tests, labinfo dest-port,
      Pages. Do not invent 10053-as-design.

## Layout

- `profiles/<name>/` — per-team config (see rule 1)
- `cmd/mcplab`, `internal/` — orchestration CLI + tests
- `cmd/labinfo`, `internal/labinfo` — first-party service-directory MCP
  service (rule 9); built by `docker/labinfo/`
- `cmd/labgraph`, `internal/labgraph` — first-party LabScenario orchestrator
  (REST + MCP + SPA); built by `docker/labgraph/`
- `docker-compose.yaml` — main project (gateway, LabDNS, LabMail as compose
  service `maildev`, LabMITM, LabNTP, LabSSO, NFS, labinfo, labgraph); MCPJungle image is
  `ghcr.io/mcpjungle/mcpjungle:${MCPJUNGLE_IMAGE_TAG:-0.4.6}` (gateway +
  registrar). `compose/*.overlay.yaml` — overlays merging the vendored
  LabLDAP and TacLab compose projects onto the shared network
- `third_party/` — vendored service repos, cloned by `mcplab vendor` (rule 7);
  release tags are pinned in `internal/lab/vendor.go` (LabDNS `v1.3.0`,
  LabLDAP `v0.5.0`, TacLab `v1.5.0`, LabMail `v1.0.0-rc.4`, LabMITM `v1.6.0`,
  LabNTP `v1.0.0-rc.2`, LabSSO `v1.0.0-rc.1`).
  ratarmount-rs is the signed `.deb` in `docker/ratarmount/Dockerfile`
  (`0.1.28`). TacLab's generated lab baseline also lives under its checkout
- `patches/` — local patches to vendored repos (rule 7)
- `docs/architecture.md` — design, security model, phase-1 plan
- `docs/guides/` — human quick start and configuration (mirrored on the Pages site)
- `docs/index.html` and siblings — GitHub Pages site
- `CHANGELOG.md` — high-level change summaries; releases promote
  `[Unreleased]` (rule 13)
- `.cursor/skills/` — Cloud-agent review skills and related knowledge
  (see Cloud). Not a lab service pin; do not flatten into product docs.


## Cloud

This section belongs only in this repo. It is not product logic and does
not belong in `README.md` or `docs/architecture.md`.

Cloud agents on this checkout load `.cursor/skills/` (those four skills:
`review-plan`, `skeptic-plan-review`, `review-pr`,
`skeptic-code-review`). Related knowledge is `.cursor/skills/knowledge/`
(`plan-skepticism`, `code-review-skepticism`, `dependencies`,
`documentation-and-tests`). Do not vendor the TypeScript hints
machinery.

Do not git clone `origin.cursor.com` from a GitHub cloud VM (401
git-keeper).

Canonical source remains Origin `matt-brewer/agent-skills` @ `77c4644`
(`77c4644a616a86a74219adb5bc31976a5636f6cb`). Helm merges this vendor
PR. Keystone runs a Thursday drift check.


## Known quirks (learned the hard way)

- Host ports 53 and 5353 collide with systemd-resolved/avahi on typical
  Linux hosts. The default profile's `LABDNS_DNS_PORT=10053` is residual,
  not the design (rule 15). Native dest is 53; remap is a follow-on PR
  with preflight errors that name the fix (stop/disable resolved, extra
  IP for `LAB_PUBLIC_HOST`, or operator escape). Do not invent
  10053-as-design.
- MCPJungle is pinned to **0.4.6** (`MCPJUNGLE_IMAGE_TAG`; compose default
  `:-0.4.6` if omitted). This lab’s default is 0.4.6, not upstream Compose
  `latest` / `latest-stdio`. Do not set `MCPJUNGLE_BIND_HOST` (unset = all
  interfaces). Operator dashboard is `GET /` in development mode only
  (enterprise 404s). Observed GHCR digest is in `docs/architecture.md`
  (informational; the tag is the lock).
- Host-port preflight (`probePort`) must not treat `EACCES` / permission-denied
  as occupied. Default TacLab ports 49/300 are privileged; the GH-hosted
  `runner` user cannot `net.Listen` them, but dockerd can still publish them.
  Occupancy is `/proc/net/tcp{,6}` LISTEN and `udp{,6}` bound (TCP dial
  fallback if proc is missing) — do not skip those ports, and do not remap
  them in CI to paper over a probe bug. `EADDRINUSE` still fails preflight
  unless the holder is a lab container (`mcplab-` / `labldap-` /
  `labtacacs-` prefixes, or exact vendored `container_name` `taclab`).
  Attribute publishes from `docker inspect` `HostConfig.PortBindings`
  (UDP included). `docker ps` Ports and `--filter publish=` omit RADIUS
  1812/1813 on GH-hosted Engine; `Register` re-runs this check after
  `LabTacacsUp`.
  Docker/host feature knobs are the same class of preflight (rule 15): fail
  closed with the configuration change in the error when a feature actually
  depends on a daemon setting. LabNTP is in compose; there is **no** Go
  `userland-proxy` probe (ADR 0014 — document NAT collision only).
- Docker user-defined networks without `--subnet` take a /16 from the daemon
  default pool (~15 slots of 172.17–31). This lab used two (`mcplab-shared`
  plus compose `mcplab_default`) and exhausted the pool on a busy host.
  `EnsureNetwork` creates one `mcplab-shared` with `LAB_DOCKER_SUBNET`
  (default `10.99.42.0/24`); the main compose `default` is that external
  network. `make down` removes it when empty. A leftover /16 with endpoints
  fail-closes (`make down` then `make up`). A missing-network inspect is
  classic `No such network` or Engine `network NAME not found` — both
  mean create; do not fail-close on the latter. Do not leave compose
  `default:` unconfigured.
- LabDNS is pinned to **v1.3.0**. MCP is wired into `serve` upstream. Do
  **not** patch it: the profile bootstrap sets
  `spec.management.mcp.allowLegacyClients: true` so MCPJungle can
  register (default pin is still `2026-07-28`). Operator console is
  `GET /` on the management listener (`spec.ui.enabled`, default true).
  Remote browsers need exact Origins in `spec.management.allowedOrigins`
  (no `"*"` sentinel; loopback is already allowed). Over-length desired-state
  names answer on management `resolve` / `explain` only — do not add them
  to `lab.test.` (they cannot appear on the DNS wire). `make reload
  APP=labdns` recreates only that container.
- TacLab (pinned `v1.5.0`) still pins `2026-07-28` by default. Do **not**
  patch it: `mcplab secrets` sets `api.mcp.allow_legacy_clients: true` on
  the labgen YAML (upstream knob from 1.2.0). `subscriptions/listen` stays
  strict. Bumping the vendor pin re-runs `labgen -force`. In
  `LAB_DEV_MODE=true`, first mint and pin-bump pass `labgen -secrets-from`
  a YAML generated from the catalog (`secrets/taclab-secrets-from.yaml`)
  so Argon2id is labgen-minted. Enter-dev on an existing baseline skips
  labgen and `applyDevTaclabSecrets` still pins token, shared secrets,
  challenge, `PASSWORDS.txt`, and Argon2id verifiers (PHC rewrite only
  when `VerifyArgon2id` fails). PKI and YAML stay labgen's. Leave-dev is
  `labgen -force` without the flag (unlinks leftover YAML). Always
  EnableLegacyClientsDir.
- LabMail (pinned `v1.0.0-rc.4`) also pins `2026-07-28`. Do **not** patch
  it: `profiles/<name>/labmail/bootstrap.yaml` sets
  `spec.management.mcp.allowLegacyClients: true`. Compose service name and
  labinfo catalog id stay `maildev`. Bind-mounted secrets
  (`secrets/labmail-token`, `secrets/maildev-web-password`) must be **0o644**
  so UID 65532 can read them — 0o600 was only safe while `MAILDEV_WEB_PASS`
  was env-injected. Healthcheck is HTTP `GET /v1/health/ready` (scratch has
  no `node`; ready still requires SMTP bound). Leftover
  `maildev/maildev.yaml` is rejected by `internal/maildev`. rc.3 hashed
  inbox JS sends `Origin`; the default profile sets
  `originAllowlist: ["*"]` so remote browsers can load the SPA (bearer +
  Basic still required; CORS stays off). `"private"` is RFC1918+ULA only.
- TacLab is self-contained: `tools/labgen` generates its whole compose bundle
  (configs incl. the combined TACACS+RADIUS variant, PKI, shared secrets,
  plaintext lab passwords in `secrets/PASSWORDS.txt`). Don't hand-write those
  files. Its RADIUS/UDP listener requires Message-Authenticator (RFC 3579)
  — `internal/radius` implements that for the smoke test. RadSec (TCP 2083)
  and inbound DAS (UDP 3799) are published but default off.
- LabLDAP's LDAPS cert always has DNS SAN `directory` (smoke binds
  `ldaps://directory:3636` on `mcplab-shared`). `mcplab secrets` also
  adds `LAB_PUBLIC_HOST` as a DNS SAN, or as an IP SAN when that value
  is an IPv4/IPv6 literal — first mint and re-sign use the same set, in
  both modes. Never pass `setuptls --host "$LAB_PUBLIC_HOST"`: that
  replaces `directory` and breaks smoke. Trust
  `third_party/go-lab-ldap-mcp/secrets/tls/ca.crt` (lab CA); native
  `labldapd` serves that cert directly — there is no 389 instance-CA.
  The CA private key stays on the host under that tls dir and is not
  committed. A failed directory recreate after a leaf rewrite leaves
  `.reload-pending` in that tls dir so the next `mcplab secrets` still
  reloads LabLDAP (SANs already matching is not enough).
- LabLDAP is pinned to **v0.5.0**. Upstream `compose.yaml` is already
  native `labldapd`; this lab stacks `compose.ephemeral.yaml` plus
  `compose/labldap.overlay.yaml`. Do not stack the v0.2
  `compose.native.yaml` alias. The overlay uses compose `!override` for
  ports (Compose v2.24.4+). Plain merging would append to upstream's
  loopback publishes. TacLab's vendored `compose.combined.yaml` uses
  `!override` too. The overlay maps `LABLDAP_MANAGEMENT_ALLOWED_HOSTS`
  from `LAB_PUBLIC_HOST` as a mapping merge (do not `!override`
  environment — that would wipe upstream `LABLDAP_LDAP_URL` /
  `LABLDAP_DIRECTORY_HOST` / `LABLDAP_DIRECTORY_CA_FILE`). Host extras
  union LoopbackHosts; `"*"` is rejected. Changing `LAB_PUBLIC_HOST`
  needs `mcplab secrets` (SAN) and `make reload APP=labldap` (env).
  `setuptls --dns/--ip` stays unused; labtlsEnsure mints the SANs.
  `make labldap-up` is idempotent; `make reload APP=labldap`
  force-recreates directory + control and re-runs bootstrap
  (ephemeral `/data` is re-seeded).
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
  Pinned **0.1.28**: remount patches the SQLite sidecar after that splice
  (F-2) and does not rescan prefix frames. `:memory:` / a discarded sidecar
  still full-rebuilds. Stay NFSv3; do not add `--nfs-vers 4`.
- `mcpjungle invoke` output is human-oriented; parse it only through
  `internal/mcpout` (regression-tested against the pinned CLI framing).
- LabMITM is pinned to **v1.6.0**. Desired state is
  `profiles/<name>/labmitm/bootstrap.yaml` (`labmitm.dev/v1alpha1`), a
  **lab-owned overlay copy** — do not recopy from the upstream examples
  tree without reviewing `allowHosts`/Origins. Do **not** patch it:
  `allowLegacyClients: true` is in the profile YAML. Binary
  `--management-listen` defaults to off; compose must pass
  `--management-listen=:8088` or HEALTHCHECK / REST / MCP / SPA stay
  unbound. Bind-mounted `secrets/labmitm-token` must be **0o644** (UID
  65532). Management is bearer-only (no HTTP Basic); the HTTP/1.1 data
  plane is unauthenticated — do not publish without a network boundary.
  Write `spec.proxy.httpAuth.enabled: false` (legal on v1.5.0; do not
  omit the key for a stale v1.1.1 KnownFields rule). 1.2 nested flags
  (inspectFrames, h2c, BIND/UDP/user-pass, orig-dest) are present and
  false (Reset-only). D22-carve hop gates `websocket` / `connect` /
  `absoluteForm` stay on. 1.4 rule actions stay off (`rules.enabled:
  false`, `items: []`). Native `/v1`
  catalog is 31 (includes `features.get`); `GET /v1/features` / MCP
  `mitm_features_list` is the frozen 11-row hop/accept catalog. Do not
  write “catalog 11” as the `/v1` surface. `allowHosts` is HTTP-useful
  compose DNS (`*.lab`, labdns, labinfo, maildev, mcpjungle, control,
  taclab, labsso). Do not uncomment `directory` / `nfs` without accepting
  unauthenticated TCP. CONNECT to non-443 TLS (LabLDAP LDAPS, TacLab
  TLS) is tunnel-not-decrypt; intercept is `:443` only. Origin allowlist
  is exact Origins (loopback already allowed; no `"*"` / `"private"`
  sentinel). `make reload APP=labmitm` recreates the container and
  wipes captured flows (generate-mode CA rotates); it does **not**
  re-register (gateway SQLite is tmpfs — only mcpjungle reload /
  `make up` / `make register` refresh the tool list). Preflight warns
  (never errors) when `LAB_PUBLIC_HOST` is not loopback and
  `originAllowlist` is empty.
- LabNTP is pinned to **v1.0.0-rc.2** (`89841a6`). Do **not** patch it:
  the profile bootstrap sets `spec.management.mcp.allowLegacyClients:
  true`. Compose must pass `--management-listen=:8088` (binary default
  is off). `--ntp-listen` empty uses YAML `:123`. UID 65532 needs
  `cap_add: [NET_BIND_SERVICE]` to bind container `:123`. Bind-mounted
  `secrets/labntp-token` is **0o644**. Host UDP default is **10123**
  (FR / ADR 0014, not residual-as-design); privileged 123 is opt-in
  and preflight occupancy uses the timesyncd copy. SPA
  `allowedOrigins` is deny-all (`[]`; no `"*"`); loopback Origins are
  appliance-exempt. Preflight warns (never errors) when
  `LAB_PUBLIC_HOST` is not loopback and `allowedOrigins` is empty —
  parse that key, not LabMITM `originAllowlist`. Host-published UDP is
  SNAT'd (NAT collision / `userland-proxy`); compose-network +
  `ntp_views_preview` stay reliable. No Go `userland-proxy` probe.
  `make reload APP=labntp` recreates only that container and does
  **not** re-register. No labgraph NTP fan-out in this pin.
- LabSSO is pinned to **v1.0.0-rc.1** (`f4c5f1e`). Do **not** patch it:
  the profile bootstrap sets `spec.listeners.management.mcp.allowLegacyClients:
  true` (a key on `management` itself is a KnownFields reject). Compose
  must pass `--management-listen=:8080`. Container listen is unprivileged
  `:10443` / `:8080` — **no** `NET_BIND_SERVICE`. Host HTTPS default is
  **443** (dest-443; LabMITM stays a forward proxy). Management is
  **18443**. Bind-mounted `secrets/labsso-token`, Alice password, TLS
  dir, and PKCS#8 signing PEM are **0o644** (dir **0o755**). Dedicated
  LabSSO CA under `secrets/labsso-tls/` — do not reuse the LabLDAP CA.
  Signing key is mint-if-missing and **out of the catalog**.
  `spec.issuer` must equal the derived issuer (`https://$LAB_PUBLIC_HOST`
  when `LABSSO_HTTPS_PORT` is empty or 443). Catalog issuer URLs omit
  `:443`. SAML is on (needs the RSA signing key); clothes are generic.
  Do not write `allowedOrigins` (not in the Go model yet). Management
  loopback-unauth is hardcoded (host `127.0.0.1` needs no bearer; remote
  peers do). No time bus. No labgraph SSO fan-out. `make reload APP=labsso`
  (alias `sso`) recreates only that container and does **not** re-register.
- labgraph is first-party orchestration (catalog id `labgraph`, host
  `LABGRAPH_PORT` 18091). It fans out LabScenario YAML to native
  appliance APIs; it does not invent LDAP/TacLab file-level apply.
  Apply order is DNS → MITM → mail → LDAP → TacLab. Partial failure
  stops with no auto-rollback. Empty `profiles/<name>/scenarios/default.yaml`
  is a no-op so smoke stays green. Named fixture packs
  (`broken-bind`, `expired-cert`, `split-horizon-dns`,
  `mitm-intercept-extra-port`) live beside it; MCP resources
  `labgraph://fixtures/{id}` and tool `fixture.apply` share `Service.Apply`.
  `broken-bind` is LabLDAP control `disableUser` only (do not flatten
  `labldap/scenario.yaml`). `expired-cert` returns a public expired leaf
  only — never a private key, and apply does not resign `directory.crt`.
  Do **not** smoke apply those packs or reset-all (omit appliances =
  all five). CLI `mcplab scenario` / `mcplab fixture apply` are HTTP
  clients of labgraph (`secrets/labgraph-token`), not a second fan-out. Scratch
  has no CA: the LDAP client loads `/run/lab-secrets/labldap-ca.crt`
  into `RootCAs`. Token file is 0o644. Journal is process memory;
  `make reload APP=labgraph` drops it. Origin allowlist is exact
  Origins (loopback implicit; no `"*"`). Preflight warns when
  `LAB_PUBLIC_HOST` is not loopback and the list is empty. Jenkins
  is not in the integrator. The operator console is REST-only (cookie
  `labgraph_session` + memory CSRF after `POST /v1/session`; token never
  in web storage). Split inspector: scenario list | five-node order
  (skipped/failed/stopped); JSON is the request log. Reset confirm
  names the walk and states that empty spec does not skip Reset
  (Cancel first, danger last). Mutate is gated until CSRF is in memory.
- Individual reload is `mcplab reload <app>` / `make reload APP=<app>`
  (labdns, maildev, nfs, labinfo, mcpjungle, labldap, labtacacs, labmitm, labgraph, labntp, labsso). It is
  not `make up`: no vendor/secrets/fixtures, `--no-deps` on the main
  compose project, and mcpjungle reload re-runs `register` because
  registration SQLite is tmpfs. Full `make up` after a vendor pin bump,
  profile switch, or first bring-up. After a catalog or `LAB_DEV_MODE`
  change, `mcplab secrets` is enough: it reloads running apps whose files
  changed (and `Register()` if any registrarEnv token changed). Enter-dev
  arms `secrets/.lab-dev-mode` `reloads=pending` before catalog writes or
  setupsecrets/labgen so a crash after files land still retries those
  reloads. `make up` skips those names so they are not bounced twice.
- Dev-mode smoke (`LAB_DEV_MODE=true` in the active `profile.env`) asserts
  catalog values on the wire: Alice's bind password, RADIUS Accept for
  catalog `taclabAdmin`, and `connections_list` secrets equal disk files.
  Default-profile smoke stays random secrets and redaction. Never set
  `LAB_DEV_MODE=true` as process env on `default` (preflight). CI copies
  `profiles/default` to gitignored `profiles/ci-dev/` and flips the knob
  there.
