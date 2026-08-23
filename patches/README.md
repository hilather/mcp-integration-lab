# Vendor patches

`mcplab vendor` applies `patches/<repo-basename>-*.patch` onto the matching
`third_party/` checkout (idempotent: skip if already present).

There are currently **no** appliance patches. MCPJungle compatibility uses
upstream `allowLegacyClients` / `allow_legacy_clients` knobs in profile YAML
(or labgen post-process for TacLab), not a protocol-pin patch.
