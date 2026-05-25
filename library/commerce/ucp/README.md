# UCP CLI

**A Go CLI for Google's Universal Commerce Protocol — talk to UCP merchants over REST or MCP, search across them in parallel, build carts, and prep checkout drafts that an AP2 CLI can authorize.**

UCP-pp-cli is the terminal-grade tool the official Python and JS SDKs don't ship. It speaks both REST and MCP transports so it works against real merchants like checkout.coffeecircle.com (Shopify-hosted, MCP-only) and the bundled mock. Local SQLite holds carts, checkout sessions, and a merchant directory, enabling cross-merchant search, capability diffs, and price-drift watch loops nothing else offers today.

Printed by [@beetz12](https://github.com/beetz12) (david).

## Real-Merchant Compatibility

v0.1 supports two interaction patterns against UCP merchants:

| Command | Real merchants (e.g. checkout.coffeecircle.com) | Bundled mock (`mock serve`) |
|---|---|---|
| `check <domain>` | ✅ Works against any merchant publishing `/.well-known/ucp` | ✅ |
| `search` / `cart` / `checkout prep` | ⚠️ Requires REST transport. Most public real merchants advertise MCP-only — those are deferred to v0.2 (MCP client + hosted agent profile + ECDSA-P256 signing) | ✅ Full end-to-end flow |

Known real merchants that publish a UCP manifest today: `checkout.coffeecircle.com` (Shopify-hosted, MCP-only). Etsy and Wayfair have UCP-powered checkout via Google AI Mode but their endpoints are gated to approved Google agents. v0.2 will add MCP transport client support to unlock real-merchant transactions.

## Install

The recommended path installs both the `ucp-pp-cli` binary and the `pp-ucp` agent skill (Claude Code, Codex, Cursor, Gemini CLI, GitHub Copilot, and other agents supported by the upstream [`skills`](https://github.com/vercel-labs/skills) CLI) in one shot:

```bash
npx -y @mvanhorn/printing-press-library install ucp
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press-library install ucp --cli-only
```

For skill only — installs the skill into the same agents as the default command above, but skips the CLI binary (use this to update or reinstall just the skill):

```bash
npx -y @mvanhorn/printing-press-library install ucp --skill-only
```

To constrain the skill install to one or more specific agents (repeatable — agent names match the [`skills`](https://github.com/vercel-labs/skills) CLI):

```bash
npx -y @mvanhorn/printing-press-library install ucp --agent claude-code
npx -y @mvanhorn/printing-press-library install ucp --agent claude-code --agent codex
```

### Without Node (Go fallback)

If `npx` isn't available (no Node, offline), install the CLI directly via Go (requires Go 1.26.3 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/commerce/ucp/cmd/ucp-pp-cli@latest
```

This installs the CLI only — no skill.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ucp-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-ucp --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-ucp --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-ucp skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-ucp. The skill defines how its required CLI can be installed.
```

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/ucp-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.
3. Fill in `API_TOKEN` when Claude Desktop prompts you.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


```bash
go install github.com/mvanhorn/printing-press-library/library/commerce/ucp/cmd/ucp-pp-mcp@latest
```

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "ucp": {
      "command": "ucp-pp-mcp",
      "env": {
        "API_TOKEN": "<your-key>"
      }
    }
  }
}
```

</details>

## Authentication

UCP has no global API key. Each merchant declares its own auth shape; the CLI identifies itself via the `UCP-Agent: profile="<url>"` header and signs requests with a per-agent ECDSA-P256 key. Run `ucp profile init` once to generate your profile. For account-linked flows, `ucp auth link <merchant>` walks an OAuth 2.0 flow per merchant (v1: print-mode — agent pastes the redirect URL back).

## Quick Start

Real shopping in 3 commands — no mock, no setup, no API key.

```bash
# List the dog/pet merchants seeded in the registry
ucp-pp-cli merchants list --rope-toys

# Search bark.co for rope toys (returns real products)
ucp-pp-cli search "rope toy" --merchant bark.co --limit 5 --json

# Add a real product to a local cart and prep a checkout draft
ucp-pp-cli cart add --merchant bark.co --sku BRK-001 --title "Corn Dog Tug" --price 999 --qty 1
ucp-pp-cli checkout prep --cart $(ucp-pp-cli cart list --json | jq -r '.[0].id') --json
```

The CLI ships with a seeded registry of 58 Grade-A UCP merchants (Shopify-hosted, verified 2026-05-24).
Browse with `ucp-pp-cli merchants list` or filter with `--category pet|fashion|beauty|...`.

## Unique Features

These capabilities aren't available in any other tool for this API.

### Cross-merchant discovery
- **`check`** — Fetch `/.well-known/ucp` for any domain and return a graded report covering schema validity, advertised transports, and capability coverage — works against any merchant publishing a UCP manifest, including MCP-only Shopify-hosted stores.

  _Lets an agent screen a domain for UCP viability without standing up an SDK or hand-rolling manifest parsing._

  ```bash
  ucp-pp-cli check checkout.coffeecircle.com --json
  ```

### Reachability mitigation
- **`mock serve`** — Spawn a UCP-compliant reference merchant locally (pure-Go, no external runtime) so `ucp check`, `search`, `cart`, and `checkout prep` flows work end-to-end without a third-party UCP merchant or extra language toolchains.

  _Lets an agent verify its UCP integration without coordinating with a live merchant or waiting for Google AI Mode approval._

  ```bash
  ucp-pp-cli mock serve --port 8080
  ```

## Recipes

### Boot a UCP test environment from scratch

```bash
ucp-pp-cli mock serve --port 8080  # run in background with & or a separate terminal
ucp-pp-cli profile init
ucp-pp-cli check 127.0.0.1:8080 --json
```

Three commands to a working UCP-merchant-agent pair — useful for tests and demos.

### End-to-end search-to-checkout against the mock merchant

```bash
ucp-pp-cli mock serve --port 8080  # run in background with & or a separate terminal
ucp-pp-cli search "coffee" --merchant 127.0.0.1:8080 --limit 5 --json
ucp-pp-cli cart add --merchant 127.0.0.1:8080 --sku coffee_001 --title "Coffee" --price 1500 --qty 2
ucp-pp-cli cart list --json
ucp-pp-cli checkout prep --cart <cart-id> --json
```

Full buy-side flow from product discovery to a checkout draft, entirely local.

### Validate a real UCP merchant manifest

```bash
ucp-pp-cli check checkout.coffeecircle.com --json
```

Fetches and validates the merchant's `/.well-known/ucp` manifest. Note: Shopify-hosted merchants like coffeecircle.com advertise MCP-only transport; REST calls will fail until v0.2 adds MCP transport client support.

## Usage

Run `ucp-pp-cli --help` for the full command reference and flag list.

## Commands

### checkout

Operations on checkout

- **`ucp-pp-cli checkout`** - POST /checkout


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
ucp-pp-cli checkout

# JSON for scripting and agents
ucp-pp-cli checkout --json

# Filter to specific fields
ucp-pp-cli checkout --json --select id,name,status

# Dry run — show the request without sending
ucp-pp-cli checkout --dry-run

# Agent mode — JSON + compact + no prompts in one flag
ucp-pp-cli checkout --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Explicit retries** - add `--idempotent` to create retries when a no-op success is acceptable
- **Confirmable** - `--yes` for explicit confirmation of destructive actions
- **Piped input** - write commands can accept structured input when their help lists `--stdin`
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `4` auth error, `5` API error, `7` rate limited, `10` config error.

## Health Check

```bash
ucp-pp-cli doctor
```

Verifies configuration, credentials, and connectivity to the API.

## Configuration

Config file: `~/.config/ucp-pp-cli/config.toml`

Static request headers can be configured under `headers`; per-command header overrides take precedence.

Environment variables:

| Name | Kind | Required | Description |
| --- | --- | --- | --- |
| `API_TOKEN` | per_call | Yes | Set to your API credential. |

## Troubleshooting
**Authentication errors (exit code 4)**
- Run `ucp-pp-cli doctor` to check credentials
- Verify the environment variable is set: `echo $API_TOKEN`
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific
- **ucp check returns HTTP 404** — The domain hosts a UCP docs site, not a UCP merchant. Confirm the merchant publishes `/.well-known/ucp`; many real-world UCP retailers (Etsy, Wayfair) are gated to approved Google/Microsoft agents and won't respond to unapproved profiles.
- **request fails with 'unsupported transport'** — The merchant only advertises MCP (e.g., coffeecircle.com). Re-run with `--transport mcp` or omit; the CLI picks the negotiated intersection automatically.
- **checkout prep fails with 'missing identity_linking'** — Run `ucp auth link <merchant>` first, or skip account-linking with `--guest` if the merchant allows guest checkout (declared in its manifest).
- **mock serve fails with 'address already in use'** — Port 8080 is taken. Pass `--port 9090` (or any free port) and update downstream commands to point at the new address.

## Sources & Inspiration

This CLI was built by studying these projects and resources:

- [**Universal-Commerce-Protocol/samples**](https://github.com/Universal-Commerce-Protocol/samples) — Python (206 stars)
- [**Universal-Commerce-Protocol/python-sdk**](https://github.com/Universal-Commerce-Protocol/python-sdk) — Python (76 stars)
- [**Universal-Commerce-Protocol/js-sdk**](https://github.com/Universal-Commerce-Protocol/js-sdk) — TypeScript (37 stars)
- [**dhananjay2021/ucp-go-sdk**](https://github.com/dhananjay2021/ucp-go-sdk) — Go (1 stars)
- [**OmnixHQ/ucp-client**](https://github.com/OmnixHQ/ucp-client) — TypeScript
- [**awesomeucp/ucp-doctor**](https://github.com/awesomeucp/ucp-doctor) — TypeScript
- [**davillafer/UCP-Compliance-Checker**](https://github.com/davillafer/UCP-Compliance-Checker) — TypeScript
- [**Upsonic/UCP-Agent**](https://github.com/Upsonic/UCP-Agent) — Python

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)

## Known Gaps (v1.1)

This v1.1 ships anonymous Shopify catalog search across 58 Grade-A merchants. The following are deferred; track in the GitHub backlog.

- **Native UCP MCP envelope for merchants whose /products.json is theme-overridden** — `coffeecircle.com` returns HTTP 500 on `/products.json` (headless theme). v1.2 will use `mark3labs/mcp-go` with a jsdelivr-hosted agent profile JSON to call `tools/call` directly.
- **Real-merchant checkout completion** — `checkout prep` emits a CheckoutDraft envelope but does not call Shopify's `/cart/add.js` or invoke AP2 mandate authorization yet (v1.2).
- **Cross-merchant parallel search** (T1)
- **Historical price/availability drift watch** (T2)
- **Multi-merchant cart optimizer** (T3)
- **Merchant capability diff** (T4) — single-merchant `ucp check` snapshots not yet persisted
- **AP2 mandate full preflight** (T5) — `checkout prep` emits a CheckoutDraft envelope; deeper AP2 schema validation lands with the AP2 CLI
- **Conformance test runner** (T6)
- **Spec-version schema diff + auto-migration** (T7)
- **Order-status watch loop + webhook receiver** (T8)
- **Identity linking OAuth flow** — `ucp auth link` not implemented
- **Discount + fulfillment extensions** — surfaced in `ucp check` but no dedicated commands
- **Webhook receiver** — no `ucp orders watch`

The bundled `ucp mock serve` provides a pure-Go reference merchant for end-to-end testing of the REST flow without external dependencies (hidden from `--help`; use `ucp-pp-cli mock serve --port N` explicitly).
