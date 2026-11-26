# labctl

Stdlib Python 3.8+ MCP streamable-HTTP client for the MCPJungle 0.4.6
gateway. No third-party packages. Uses `LABCTL_URL` **as given** (path
and query included); it does not rewrite a group URL to `/mcp`.

```bash
export LABCTL_URL=http://127.0.0.1:8080/mcp
export LABCTL_TOKEN_FILE=secrets/mcp-client-token
# Default (enterprise) profile requires this token, or pass --token-file.
# Omit the token only in development mode (LAB_DEV_MODE=true).
python3 tools/labctl/labctl.py tools
python3 tools/labctl/labctl.py tools --server labinfo
python3 tools/labctl/labctl.py schema labinfo__endpoints_list
python3 tools/labctl/labctl.py call labmail__mail_state_get --json '{}'
python3 tools/labctl/labctl.py call labldap__ldap_get_account_state --arg id=alice
python3 tools/labctl/labctl.py endpoints
```

JSON on stdout. Transport, JSON-RPC, unknown-tool, and `isError` results
exit non-zero. `--json @file` reads a UTF-8 object; `--arg k=v` is JSON
if it parses, otherwise a string. Do not mix `--json` and `--arg`.

Token sources, in order: `LABCTL_TOKEN`, then `LABCTL_TOKEN_FILE` or
`--token-file` (`LABCTL_TOKEN` wins). There is no `--token` / `--bearer`
flag. Tokens are redacted as `***`. No implicit read of
`secrets/mcp-client-token`. Development mode may omit a token.

Ambient `http_proxy` / `https_proxy` / `ALL_PROXY` (any case) are
ignored. Opt in with `--proxy URL` or `LABCTL_PROXY` (flag wins): only
an https proxy or a loopback proxy is accepted.

`LABCTL_TIMEOUT` / `--timeout` (default 60s) is one deadline for the
network I/O, including DNS, connect, pagination, and best-effort cleanup.
Local JSON encoding after the last network operation is outside that deadline.
`LABCTL_MAX_PAGES` / `LABCTL_MAX_TOOLS` cap pagination (defaults 10000 /
100000). A repeated `nextCursor` is an error that names the cursor.

Handshake: `initialize` (protocol `2025-11-25`, `capabilities: {}`,
`clientInfo`) then `notifications/initialized` (HTTP 202 empty body is
success). `Mcp-Session-Id` is reused and DELETE'd at the end (a 405
keeps the command's exit code). Redirects are not followed.
For chunked responses, Python’s `http.client` consumes the two bytes after each data chunk without checking for CRLF.

```bash
make test-labctl
```

`test_labctl.py` ships stub tests only. The live/parity harness is a
follow-on (not in this file).

A non-admin MCP **client** token can invoke allow-listed tools through
`/mcp` and `/v0/groups/:name/mcp` (MCPJungle 0.4.6
`checkAuthForMcpProxyAccess` + `authorizeProxyServerAccess`), not REST
`/api/v0/tools/invoke`. Source-verified against 0.4.6; not live-verified.
