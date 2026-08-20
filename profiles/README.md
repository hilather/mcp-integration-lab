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
