# AP2 PP CLI — Research Brief
**run_id:** 20260525-051800-hand-scaffold
**date:** 2026-05-25

## What is AP2?

AP2 (Agent Payments Protocol v2) is the payment-completion counterpart to UCP (Universal Checkout Protocol).
UCP handles cart discovery and produces a `FinalizationEnvelope`; AP2 consumes that envelope to sign and
authorize the actual payment with a merchant's MCP endpoint.

## What this CLI does

`ap2-pp-cli` is a Go CLI (also exposed as an MCP server via `ap2-pp-mcp`) that implements:

1. **Mandate sign/verify** — ECDSA-P256 key generation, envelope signing, and signature verification
2. **Payment authorize** — submits a signed `FinalizationEnvelope` to a merchant PSP endpoint
3. **Keys management** — `keys generate`, `keys list`, `keys rotate` for ECDSA keypairs in `AP2_KEYS_DIR`
4. **Profile** — agent identity profile serving (compatible with UCP `ucp-agent.profile` field)
5. **Doctor** — credential and config health check; reports `auth_source`, `base_url`, key inventory

## Integration with ucp-pp-cli

```
ucp-pp-cli checkout finalize --cart <id>   →  FinalizationEnvelope (stdout)
ap2-pp-cli mandate sign < envelope.json    →  SignedEnvelope (stdout)
ap2-pp-cli payment authorize < signed.json →  CompleteResult (stdout), saved to AP2_TXN_DIR
```

## Source location
- Source: `/Users/dave/printing-press/library/ap2/` (Go module `ap2-pp-cli`)
- Publish path: `library/payments/ap2/`
- MCP server binary: `bin/ap2-pp-mcp`

## Key env vars consumed
- `AP2_TOKEN` / `AP2_API_KEY` — merchant auth credential (AP2-specific; no generic fallback)
- `AP2_BASE_URL` — PSP endpoint override
- `AP2_CONFIG` — config file path override
- `AP2_GPAY_TOKEN` — Google Pay token for `--live` flows
- `AP2_KEYS_DIR` — ECDSA keypair storage
- `AP2_TXN_DIR` — transaction record storage
