# AP2 CLI

**A Go CLI for the AP2 (Agent Payments Protocol) — sign AP2 FinalizationEnvelopes emitted by ucp-pp-cli and authorize payments with merchant MCP endpoints.**

ap2-pp-cli is the signing and authorization counterpart to ucp-pp-cli. ucp-pp-cli handles merchant discovery, search, cart, and checkout finalization; ap2-pp-cli signs the resulting `FinalizationEnvelope` with a local ECDSA-P256 key and posts the signed payload to the merchant's `complete_checkout` MCP endpoint.

Printed by [@beetz12](https://github.com/beetz12) (david).

## Install

```bash
npx -y @mvanhorn/printing-press-library install ap2
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install ap2 --cli-only
```

Go fallback (requires Go 1.26.3 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/payments/ap2/cmd/ap2-pp-cli@latest
```

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-ap2 --force
```

## Quick Start

```bash
# Generate a local ECDSA-P256 agent key (one-time)
ap2-pp-cli keys generate

# Pipe a FinalizationEnvelope from ucp-pp-cli through sign → authorize
ucp-pp-cli checkout finalize --cart <cart_id> --json \
  | ap2-pp-cli mandate sign --envelope - --json \
  | ap2-pp-cli payment authorize --envelope - --sandbox --json
```

## Architecture

ap2-pp-cli and ucp-pp-cli form a two-CLI pair for agentic commerce:

```
ucp-pp-cli search → cart add → checkout finalize
                                        │
                              FinalizationEnvelope (JSON)
                                        │
ap2-pp-cli mandate sign ←──────────────┘
ap2-pp-cli payment authorize → {status: sandbox_authorized, transaction_id: sandbox-<uuid>}
```

| CLI | Role |
|-----|------|
| ucp-pp-cli | Merchant discovery, search, cart, checkout prep, envelope construction |
| ap2-pp-cli | ECDSA-P256 key management, mandate signing, payment authorization |

Neither CLI requires the other to be installed — they communicate via the `FinalizationEnvelope` JSON format on stdout/stdin.

## Sandbox vs Live

| Mode | Flag | Network call? | Token required? |
|------|------|---------------|-----------------|
| Sandbox (default) | `--sandbox` | No | No |
| Live | `--live` | Yes | `--token <gpay-token>` |

Default is always sandbox. Sandbox builds the complete request payload and returns `{status: "sandbox_authorized", transaction_id: "sandbox-<uuid>"}` without touching any merchant endpoint. No real money moves in sandbox mode.

Live mode POSTs the signed `FinalizationEnvelope` to the merchant's `complete_checkout` MCP endpoint. It requires a valid Google Pay token via `--token`. ap2-pp-cli does not obtain GPay tokens — that is the calling agent's responsibility.

## Validating against a live merchant (probe)

`payment probe` sends a `complete_checkout` request to the merchant's live MCP endpoint using a deliberately-invalid stub token — no money moves. The expected GOOD outcome is `classification=request_shape_ok`, which means the merchant accepted your request structure but rejected the stub token. That proves the integration is structurally correct before you obtain a real Google Pay token.

```bash
ap2-pp-cli payment probe --envelope signed.json
# Expected output: classification=request_shape_ok → integration is structurally correct
```

Recommended workflow:

```bash
# 1. Sign the envelope (sandbox — no network call)
ucp-pp-cli checkout finalize --cart <cart_id> --json \
  | ap2-pp-cli mandate sign --envelope - --json > signed.json

# 2. Probe the live merchant with a stub token (validates request shape)
ap2-pp-cli payment probe --envelope signed.json
# classification=request_shape_ok → shape is correct, proceed

# 3. Authorize with a real Google Pay token
ap2-pp-cli payment authorize --envelope signed.json --live --token <gpay-token>
```

Classification outcomes:

| Classification | Meaning | Action |
|---|---|---|
| `request_shape_ok` | Merchant accepted shape, rejected stub token (GOOD) | Proceed with real token |
| `request_shape_bad` | Merchant rejected request structure | Check payment_mandate fields and signature |
| `agent_not_authorized` | Profile/delegation gate failed | Check `--profile-url` — default: https://www.igvita.com/ucp/profile.json |
| `merchant_unreachable` | 5xx or transport error | Retry or verify endpoint URL |
| `unknown` | No pattern matched | Inspect `response_body` in output |

Exit codes: `0` = request_shape_ok, `2` = shape/auth/unknown issue, `3` = merchant unreachable.

## Integration with ucp-pp-cli

ap2-pp-cli pairs with ucp-pp-cli to complete the full agentic-commerce flow against real merchants. The script `scripts/integration_bark.sh` runs the five-stage chain non-interactively (US-005 / openbrain-fmpb acceptance gate).

### Five-stage chain (verbatim)

```bash
# Stage 0: generate a fresh agent key
ap2-pp-cli keys generate --json

# Stage 1: search bark.co for a product
ucp-pp-cli search "dog rope toy" --merchant bark.co --json

# Stage 2: add to cart
ucp-pp-cli cart add --merchant bark.co \
  --variant-id <variant_id> --title "<title>" --price <cents> --qty 1 --json

# Stage 3: build the FinalizationEnvelope
ucp-pp-cli checkout finalize --cart <cart_id> --json

# Stage 4: sign all 3 mandates with the agent key
ucp-pp-cli checkout finalize --cart <cart_id> --json \
  | ap2-pp-cli mandate sign --envelope - --json

# Stage 5: sandbox-authorize the signed envelope
ucp-pp-cli checkout finalize --cart <cart_id> --json \
  | ap2-pp-cli mandate sign --envelope - --json \
  | ap2-pp-cli payment authorize --envelope - --sandbox --json
```

### Run non-interactively

```bash
cd library/ap2
mkdir -p build && go build -o build/ap2-pp-cli ./cmd/ap2-pp-cli
bash scripts/integration_bark.sh
```

Successful output:

```
INTEGRATION OK: <product title> @ $<price> → txn=sandbox-<uuid>
```

## Security

- **ECDSA-P256** — all mandate signatures use P-256 (secp256r1). Private keys are stored as PKCS#8 PEM with mode `0600`. Public keys are PKIX PEM with mode `0644`.
- **validateID path-traversal guards** — key ID inputs are validated before use as filesystem paths; path traversal attempts are rejected with a structured error.
- **Transaction records** — sandbox and live authorization results are written to `AP2_TXN_DIR` (default: platform config dir) with mode `0600`. These records contain the signed envelope and should be treated as sensitive.
- **No network calls in sandbox** — the default `--sandbox` mode builds the request payload but never connects to any merchant endpoint. Safe to run in CI and offline environments.

## Commands

### keys

Manage ECDSA-P256 agent keys stored in the platform config directory.

```bash
ap2-pp-cli keys generate                         # generate a new keypair
ap2-pp-cli keys list                             # list stored keys
ap2-pp-cli keys export --id agent-<uuid>         # export public key (PEM)
ap2-pp-cli keys export --id agent-<uuid> --format jwk  # export as JWK
```

### mandate

Sign and verify AP2 FinalizationEnvelopes.

```bash
ap2-pp-cli mandate sign --envelope envelope.json           # sign from file
ap2-pp-cli mandate sign --envelope -                       # sign from stdin
ap2-pp-cli mandate verify signed.json                      # verify all checks
ap2-pp-cli mandate verify -n signed.json                    # structural only
```

### payment

Authorize a signed envelope, probe live shape, and track transactions.

```bash
ap2-pp-cli payment authorize --envelope signed.json           # sandbox (default)
ap2-pp-cli payment authorize --envelope signed.json --live --token <tok>  # live
ap2-pp-cli payment probe --envelope signed.json               # validate shape against live merchant (stub token)
ap2-pp-cli payment status --transaction sandbox-<uuid>        # look up a txn
```

## Output Formats

```bash
# Human-readable (default in terminal)
ap2-pp-cli keys list

# JSON for scripting and agents
ap2-pp-cli keys list --json

# Filter to specific fields
ap2-pp-cli payment authorize --envelope signed.json --json --select status,transaction_id

# Dry run — show the would-be request without signing
ap2-pp-cli payment authorize --envelope signed.json --dry-run

# Agent mode — JSON + compact + no prompts in one flag
ap2-pp-cli mandate sign --envelope - --agent
```

## Agent Usage

ap2-pp-cli is designed for AI agent consumption:

- **Non-interactive** — never prompts, every input is a flag
- **Pipeable** — `--json` output to stdout, errors to stderr
- **Filterable** — `--select status,transaction_id` returns only fields you need
- **Previewable** — `--dry-run` shows the request without signing or sending
- **Agent-safe by default** — no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `1` operation failure, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
ap2-pp-cli doctor
```

Verifies key storage, configuration, and environment setup.

## Troubleshooting

**No keys found**
- Run `ap2-pp-cli keys generate` first. Keys are stored in the platform config dir; the `--config` flag or `AP2_KEYS_DIR` env var can override the location.

**Ambiguous key selection**
- More than one key exists and `--key-id` was not specified. Run `ap2-pp-cli keys list` and pass `--key-id agent-<uuid>`.

**Signature verification fails**
- The envelope was signed with a key whose `.pub` file is not in the default keystore. Pass `--keystore <path>` to point at the correct directory.

**`--live` requires `--token`**
- Live mode needs a Google Pay token. Use `--sandbox` (default) for testing without a real token.

**Envelope expiry**
- AP2 envelopes are short-lived. If `intent_mandate.expires_at` has passed, re-run `ucp-pp-cli checkout finalize` to get a fresh envelope.

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
