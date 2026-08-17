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

`make up` is idempotent. It clones pinned vendors into `third_party/`, applies patches, mints gitignored secrets, builds local images, starts three compose projects on `mcplab-shared`, and registers every MCP server with the gateway. First run is image-build heavy.

`make smoke` runs an agent-style scenario through the gateway: DNS, LDAP, NFS, TACACS+/RADIUS, and mail.

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
# cert SAN is 'directory'

mount -t nfs -o vers=3,tcp,nolock,port=20490,mountport=20490 \
  <lab-host>:/ /mnt
```

Point outbound SMTP at `<lab-host>:1025` and read captured mail in the maildev UI on port 1080. Nothing is relayed. RADIUS PAP needs the shared secret under `third_party/go-lab-tacacs-mcp/deployments/compose/secrets/` and a Message-Authenticator attribute.

## Pick a profile

Copy `profiles/default` to `profiles/teamx`, edit ports and YAML, then:

```bash
cp .env.example .env
# PROFILE=teamx
make up PROFILE=teamx
```

Set `LAB_PUBLIC_HOST` to the DNS name remote testers use. That hostname is what labinfo puts in every URL. Full reference: [configuration.md](configuration.md).

## Stop and reset

```bash
make down     # stop; bind-mounted storage survives
make reset    # wipe all runtime state
make register # reapply gateway JSON from the profile
```
