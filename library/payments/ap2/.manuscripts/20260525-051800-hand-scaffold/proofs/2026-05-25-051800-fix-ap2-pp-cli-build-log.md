# AP2 PP CLI — Build Log
**run_id:** 20260525-051800-hand-scaffold
**date:** 2026-05-25

## What was built

Hand-scaffolded `ap2-pp-cli` v0.1.0 as companion to `ucp-pp-cli` v1.3.

### Commands implemented
- `mandate sign` — sign a FinalizationEnvelope with ECDSA-P256 key
- `mandate verify` — verify a SignedEnvelope signature
- `payment authorize` — submit signed envelope to merchant PSP endpoint
- `payment authorize --live` — live flow with Google Pay token (`AP2_GPAY_TOKEN`)
- `keys generate` — create ECDSA-P256 keypair in `AP2_KEYS_DIR`
- `keys list` — list stored keypairs
- `keys rotate` — rotate active keypair
- `profile` — serve agent profile JSON
- `doctor` — config/credential health check
- `version` — print semver

### MCP tools exposed (10 total)
`mandate_sign`, `mandate_verify`, `payment_authorize`, `keys_generate`, `keys_list`,
`keys_rotate`, `profile_get`, `profile_set`, `doctor`, `txn_list`

### Key internal packages
- `internal/config/config.go` — TOML config + env var overrides (AP2_* prefix only)
- `internal/transport/txnstore.go` — transaction record persistence with ID validation
- `internal/mandate/` — ECDSA sign/verify
- `internal/payment/` — PSP HTTP client
- `internal/keys/` — keypair CRUD
