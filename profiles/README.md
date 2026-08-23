# Profiles

The only profile shipped in this repository is **`default`** (local/CI baseline;
LabDNS on port 10053 to avoid systemd-resolved).

## Team-local profiles

Every other directory under `profiles/` is **gitignored**. Copy `default` and
edit it locally:

```bash
cp -a profiles/default profiles/teamx
# edit profiles/teamx/profile.env and the YAML/JSON under it
cp .env.example .env
# PROFILE=teamx
```

Your local profile survives `git pull` — only `default` changes upstream.

Select the active profile with `PROFILE=` in `.env` or `make up PROFILE=<name>`.

## Dev credentials catalog

The orchestrator understands `dev-credentials.yaml` as a `DevCredentials`
document (`apiVersion: mcplab.dev/v1alpha1`; see `internal/lab/devcreds.go`).
Parse is fail-closed: unknown fields are rejected, every token / password /
shared-secret key is required, LabLDAP passwords must be at least 12
characters, and TacLab shared secrets must pass the appliance's v1.3.0
policy (length ≥16, ≥3 unicode character classes, exact-match known-weak
list — not a substring match).

The catalog is consumed only when `LAB_DEV_MODE=true` (never from
`MCPJUNGLE_MODE`). The default profile ships live `lab-dev-*` values in
`dev-credentials.yaml`; they are inert while `LAB_DEV_MODE=false`. There
is no merge with `default` — a team profile that enables dev mode must
have its own complete catalog. TacLab lab-user passwords and AAA shared
secrets are pinned from this file after `labgen` (PKI stays generated).
