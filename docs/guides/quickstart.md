# Quick start

Stand up the full lab, attach an MCP client to the gateway, and exercise one real protocol on the wire.

The same walkthrough is published as [start.html](https://hilather.github.io/mcp-integration-lab/start.html).

## What you need

- Docker Engine 24+ with Compose v2.24.4+ (LabLDAP overlays use `!reset` / `!override`)
- GNU make
- Go 1.26+ (the orchestration CLI and the vendored secret tools)
- A clone of the repository

## Bring the lab up

```bash
git clone https://github.com/hilather/mcp-integration-lab.git
cd mcp-integration-lab
make up
make smoke
```

`make up` is idempotent. It clones pinned vendors into `third_party/`, applies patches, mints gitignored secrets, builds local images, starts the always-on compose projects on `mcplab-shared` (LabJenkins only when `LABJENKINS_ENABLED=true`), and registers every MCP server with the gateway. First run is image-build heavy.

`make smoke` runs an agent-style scenario through the gateway: DNS, LDAP, NFS, TACACS+/RADIUS, mail, and LabMITM. On the default profile that is random secrets and labinfo redaction. Against a profile with `LAB_DEV_MODE=true` it also asserts catalog values on the wire (Alice's bind password, RADIUS Accept for catalog `taclabAdmin`, `connections_list` secrets equal the files on disk, `devMode=true`). Agents: see `AGENTS.md` **Easy Docker testing** — Jenkins jwt-rs / Entra is the same lab (`make up PROFILE=<team>`), not default smoke.

## Attach an MCP client

The default profile runs the gateway in enterprise mode. Clients send a bearer token. Drop the header only if you set `LAB_DEV_MODE=true`.

```json
{
  "mcpServers": {
    "integration-lab": {
      "url": "http://<lab-host>:8080/mcp",
      "headers": {
        "Authorization": "Bearer <contents of secrets/mcp-client-token>"
      }
    }
  }
}
```

The curated tool group lives at `http://<lab-host>:8080/v0/groups/integration/mcp`. Ask labinfo for `endpoints_list` and `connections_list` before you spelunk.

## Hit a data plane

```bash
dig @<lab-host> -p 10053 ns1.lab.test

ldapsearch -H ldaps://<lab-host>:3636 \
  -D 'uid=alice,ou=people,dc=example,dc=test' -W \
  -b dc=example,dc=test '(uid=alice)'
# trust third_party/go-lab-ldap-mcp/secrets/tls/ca.crt
# cert SAN is 'directory' plus LAB_PUBLIC_HOST (DNS or IP, both modes)

mount -t nfs -o vers=3,tcp,nolock,port=20490,mountport=20490 \
  <lab-host>:/ /mnt
```

Point outbound SMTP at `<lab-host>:1025` and read captured mail in the LabMail UI on port 1080. Nothing is relayed. RADIUS PAP needs the shared secret under `third_party/go-lab-tacacs-mcp/deployments/compose/secrets/` and a Message-Authenticator attribute. Point HTTP clients at `<lab-host>:18888` as `HTTP_PROXY` (unauthenticated; intercept is `:443` only). The inspector is `:18088`.

## Pick a profile

Copy `profiles/default` to `profiles/teamx`, edit ports and YAML, then:

```bash
cp .env.example .env
# PROFILE=teamx
make up PROFILE=teamx
```

Set `LAB_PUBLIC_HOST` to the DNS name or address remote testers use. That value is what labinfo puts in every URL, and a DNS or IP SAN on LabLDAP leaf certs in both modes. Full reference: [configuration.md](configuration.md).

## Easy connect (dev mode)

For a throwaway local lab, copy the default profile and set `LAB_DEV_MODE=true` in **that profile's** `profile.env` (not process env on `default` — preflight rejects the drift):

```bash
cp -a profiles/default profiles/teamx
# edit profiles/teamx/profile.env: LAB_DEV_MODE=true
make up PROFILE=teamx
make smoke PROFILE=teamx
make creds PROFILE=teamx
```

Tokens, Alice's bind password (`lab-dev-alice-12` on the default catalog), the mail admin password, and TacLab lab-user / AAA secrets come from `dev-credentials.yaml` and are the same on every clone using that catalog. `make smoke` then checks those catalog values on the wire. labinfo reveals them; `make creds` prints the shareable sheet (PEMs included; fails closed outside dev mode). Never enable this on a shared internet-facing host. Flip the knob back to `false` and run `mcplab secrets` to remint.

CI does the same with a gitignored `profiles/ci-dev/` (copy of `default`, `LAB_DEV_MODE=true` in that `profile.env`). The default-profile unit-test job stays non-dev.

## Reload one app (not a full redeploy)

`make up` rebuilds everything. After you edit one service's YAML, or need to
bounce a wedged container, reload only that app:

```bash
make reload APP=labdns      # labdns/bootstrap.yaml, operator console
make reload APP=maildev     # labmail/bootstrap.yaml; wipes the inbox
make reload APP=nfs         # ratarmount image / overlay interval
make reload APP=labinfo     # labinfo/services.yaml
make reload APP=mcpjungle   # gateway container; also re-registers (tmpfs)
make reload APP=labldap     # labldap/scenario.yaml; re-seeds ephemeral /data
make reload APP=labtacacs   # TacLab compose project
make reload APP=labmitm     # labmitm/bootstrap.yaml; wipes captured flows; does not re-register
make reload APP=labjenkins  # opt-in jwt-rs; requires LABJENKINS_ENABLED
```

Same commands as `mcplab reload <app>`. Aliases: `dns`, `labmail`/`mail`,
`ldap`, `taclab`/`tacacs`, `gateway`, `mitm`, `jenkins`/`jwt-rs`. Sibling services stay up. Use full
`make up` after a vendor pin bump, a profile switch, or first bring-up.

## Stop and reset

```bash
make down     # stop; bind-mounted storage survives
make reset    # wipe all runtime state
make register # reapply gateway JSON from the profile (no container restart)
```
