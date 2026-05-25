---
name: ap2-pp-cli
description: "A Go CLI for the AP2 (Agent Payments Protocol) — sign AP2 FinalizationEnvelopes from ucp-pp-cli and authorize payments with merchant MCP endpoints. Trigger phrases: `sign this envelope`, `ap2 mandate`, `authorize payment`, `ap2 keys`, `sandbox checkout`."
author: "david"
license: "Apache-2.0"
category: payments
tags: [ap2, agent-payments-protocol, ecdsa, mandate-signing]
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - ap2-pp-cli
    install:
      - kind: go
        bins: [ap2-pp-cli]
        module: github.com/mvanhorn/printing-press-library/library/payments/ap2/cmd/ap2-pp-cli
---

# AP2 — Printing Press CLI

## Overview

ap2-pp-cli is the signing and authorization half of an agentic commerce flow built on Google's AP2 (Agent Payments Protocol). It pairs with ucp-pp-cli: ucp-pp-cli searches merchants, builds carts, and emits a `FinalizationEnvelope`; ap2-pp-cli signs that envelope with a local ECDSA-P256 key and posts it to the merchant's `complete_checkout` endpoint.

No external auth service, no API key, no third-party dependency for v0.1 sandbox flows. All keys are stored locally under the platform config directory.

## Prerequisites: Install the CLI

This skill drives the `ap2-pp-cli` binary. **Verify the CLI is installed before invoking any command.** If missing, install:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press-library install ap2 --cli-only
   ```
2. Verify: `ap2-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

Go fallback (requires Go 1.26.3 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/payments/ap2/cmd/ap2-pp-cli@latest
```

## Authentication

AP2 keys are **local** — no external auth required for v0.1 sandbox flows.

- Keys are stored as PKCS#8 PEM files under the platform config directory:
  - macOS: `~/Library/Application Support/ap2-pp-cli/keys/`
  - Linux: `~/.config/ap2-pp-cli/keys/`
- Each keypair: `agent-<uuid>.pem` (private, mode 0600) + `agent-<uuid>.pub` (public, mode 0644)
- For live payment flows (`--live`), a Google Pay token is required via `--token`; ap2 itself does not manage GPay tokens

## Quickstart

```bash
# 1. Generate a local ECDSA-P256 agent key
ap2-pp-cli keys generate

# 2. Sign an envelope from ucp-pp-cli
ucp-pp-cli checkout finalize --cart <cart_id> --json \
  | ap2-pp-cli mandate sign --envelope - --json

# 3. Verify the signed envelope
ap2-pp-cli mandate verify signed_envelope.json

# 4. Sandbox-authorize the signed envelope (no network call)
ap2-pp-cli payment authorize --envelope signed.json --sandbox
```

Full bark.co integration (5-stage chain):

```bash
cd library/ap2 && bash scripts/integration_bark.sh
```

## Commands

### keys — Manage ECDSA-P256 agent keys

Manage ECDSA-P256 agent keys used to sign AP2 mandates.

- **`ap2-pp-cli keys generate`** — Generate a new ECDSA-P256 agent keypair

  Flags: _(none beyond global flags)_

  ```bash
  ap2-pp-cli keys generate
  ap2-pp-cli keys generate --json
  ```

- **`ap2-pp-cli keys list`** — List all stored agent keys

  Flags: _(none beyond global flags)_

  ```bash
  ap2-pp-cli keys list
  ap2-pp-cli keys list --json
  ```

- **`ap2-pp-cli keys export`** — Export an agent key's public component

  Flags:
  - `--id string` — Agent ID (`agent-<uuid>`) of the key to export
  - `--format string` — Output format: `pem` or `jwk` (default `"pem"`)

  ```bash
  ap2-pp-cli keys export --id agent-<uuid>
  ap2-pp-cli keys export --id agent-<uuid> --format jwk
  ap2-pp-cli keys export --id agent-<uuid> --format pem --json
  ```

### mandate — Sign and verify AP2 mandate envelopes

Tools for AP2 FinalizationEnvelopes emitted by ucp-pp-cli.

- **`ap2-pp-cli mandate sign`** — Sign an unsigned AP2 FinalizationEnvelope with an agent key

  Signs all three mandates (intent, cart, payment) with the specified ECDSA-P256 key. Key auto-selected if exactly one exists.

  Flags:
  - `--envelope string` — Envelope file path, or `"-"` to read from stdin (default `"-"`)
  - `--key-id string` — Agent key ID (`agent-<uuid>`); auto-selected if omitted and only one key exists
  - `--subject string` — Override `envelope.Subject` (defaults to signing key's AgentID if envelope.Subject is empty)

  Exit codes: `0` signed, `1` signing error, `2` usage error

  ```bash
  ap2-pp-cli mandate sign --envelope envelope.json
  ucp-pp-cli checkout finalize --cart "$C" --json | ap2-pp-cli mandate sign --envelope -
  ap2-pp-cli mandate sign --envelope envelope.json --key-id agent-<uuid> --subject my-agent-v1
  ```

- **`ap2-pp-cli mandate verify`** — Verify signature and chain integrity of a signed AP2 FinalizationEnvelope

  Checks: body_hash integrity, ECDSA signature validity for each mandate, cross-mandate chain references (intent→cart→payment), amount consistency, and expiry.

  Flags:
  - `[file]` — positional: envelope file path (or use stdin)
  - `--keystore string` — Path to keystore directory (default: `~/.ap2/keys`)
  - `-n, --no-sig-check` — Skip ECDSA signature verification (structural checks only)

  Exit codes: `0` all checks passed, `1` verification failed, `2` usage error

  ```bash
  ap2-pp-cli mandate verify envelope.json
  ucp-pp-cli checkout finalize | ap2-pp-cli mandate sign | ap2-pp-cli mandate verify
  ap2-pp-cli mandate verify -n envelope.json
  ```

### payment — Authorize and track AP2 payments

Manages the final step of an AP2 agentic checkout.

- **`ap2-pp-cli payment authorize`** — Authorize a signed AP2 FinalizationEnvelope with the merchant (sandbox default)

  Default mode is `--sandbox`: builds and prints the would-be request without making a network call. Pass `--live` for a real call (requires `--token`).

  Flags:
  - `--envelope string` — Path to signed FinalizationEnvelope JSON file, or `-` for stdin (default `"-"`)
  - `--sandbox` — Sandbox mode: build the request but do NOT send it (default `true`)
  - `--live` — Live mode: POST to merchant's `complete_checkout` endpoint (requires `--token`)
  - `--token string` — Google Pay token for live mode
  - `--merchant-mcp-url string` — Merchant MCP endpoint URL (derived from `envelope.checkout_url` if omitted)
  - `--profile-url string` — UCP agent profile URL (default: `https://www.igvita.com/ucp/profile.json`)

  Exit codes: `0` authorized (`sandbox_authorized` in sandbox mode), `1` failure, `2` usage error

  ```bash
  # Sandbox authorization (default — no network call)
  ap2-pp-cli payment authorize --envelope signed.json

  # Live authorization (real money — requires --token)
  ap2-pp-cli payment authorize --envelope signed.json --live --token gpay-token-here

  # Read envelope from stdin
  cat signed.json | ap2-pp-cli payment authorize --envelope -
  ```

- **`ap2-pp-cli payment status`** — Look up a recorded transaction by ID

  Flags:
  - `--transaction string` — Transaction ID to look up (e.g. `sandbox-<uuid>`)

  ```bash
  ap2-pp-cli payment status --transaction sandbox-12345678-1234-1234-1234-123456789abc
  ```

## Examples

### End-to-end against bark.co (sandbox, 5 stages)

```bash
# Generate key (once)
ap2-pp-cli keys generate

# Search → cart → finalize → sign → authorize
SEARCH=$(ucp-pp-cli search "dog rope toy" --merchant bark.co --json)
VARIANT=$(echo "$SEARCH" | jq -r '.[0].variant_id')
TITLE=$(echo "$SEARCH" | jq -r '.[0].title')
PRICE=$(echo "$SEARCH" | jq -r '.[0].price')

CART_ID=$(ucp-pp-cli cart add --merchant bark.co \
  --variant-id "$VARIANT" --title "$TITLE" --price "$PRICE" --qty 1 --json \
  | jq -r '.id')

ucp-pp-cli checkout finalize --cart "$CART_ID" --json \
  | ap2-pp-cli mandate sign --envelope - --json \
  | ap2-pp-cli payment authorize --envelope - --sandbox --json
```

Successful output: `{"status":"sandbox_authorized","transaction_id":"sandbox-<uuid>","..."}`

### Non-interactive acceptance gate (US-005)

```bash
cd library/ap2
mkdir -p build && go build -o build/ap2-pp-cli ./cmd/ap2-pp-cli
bash scripts/integration_bark.sh
# Expect: INTEGRATION OK: <product> @ $<price> → txn=sandbox-<uuid>
```

### Verify a signed envelope from file

```bash
ap2-pp-cli mandate sign --envelope envelope.json --json > signed.json
ap2-pp-cli mandate verify signed.json
```

### Export a key as JWK for use in another system

```bash
ap2-pp-cli keys list --json | jq -r '.[0].agent_id'
# → agent-abc123...
ap2-pp-cli keys export --id agent-abc123 --format jwk
```

## Troubleshooting

**No keys found (exit code 2)**
- Run `ap2-pp-cli keys generate` first
- Check `AP2_KEYS_DIR` env var if using a custom keystore path

**Ambiguous key error**
- More than one key exists and `--key-id` was not specified
- Run `ap2-pp-cli keys list` to see key IDs, then pass `--key-id agent-<uuid>`

**Signature verification fails (exit code 1)**
- The envelope was signed with a key not in the default keystore
- Pass `--keystore <path>` pointing to the directory containing the signing key's `.pub` file
- Or use `--no-sig-check` for structural-only validation

**`payment authorize --live` fails**
- `--live` requires `--token` with a valid Google Pay token
- Sandbox mode (`--sandbox`, the default) needs no token and makes no network call

**Envelope expiry error**
- `intent_mandate.expires_at` is in the past
- Re-run `ucp-pp-cli checkout finalize` to get a fresh envelope; envelopes are short-lived

## Agent Mode

Add `--agent` to any command. Expands to `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select id,status,transaction_id` returns only fields you need
- **Previewable** — `--dry-run` shows the request without sending
- **Non-interactive** — never prompts, every input is a flag

## Output Delivery

Every command accepts `--deliver <sink>`:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` |
| `webhook:<url>` | POST the output body to the URL |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Operation failure (signing, verification, or authorization error) |
| 2 | Usage error (wrong arguments, missing required flag) |
| 3 | Resource not found |
| 4 | Authentication required |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `ap2-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

1. Install the MCP server:
   ```bash
   go install github.com/mvanhorn/printing-press-library/library/payments/ap2/cmd/ap2-pp-mcp@latest
   ```
2. Register with Claude Code:
   ```bash
   claude mcp add ap2-pp-mcp -- ap2-pp-mcp
   ```
3. Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which ap2-pp-cli`
   If not found, install (see Prerequisites above).
2. Match the user query to the best command from the Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   ap2-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `ap2-pp-cli <command> --help`
