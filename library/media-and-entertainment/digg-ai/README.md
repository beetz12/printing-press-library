# Digg AI CLI

**AI news from digg.com/ai with offline state, endorser pivots, and velocity tracking no website surface gives you.**

digg-ai pulls the curated AI feed from digg.com/ai and persists it locally. Once it's in SQLite you can pivot by endorser, diff against last sync, see which stories are climbing fastest, and pipe everything into agent flows as clean JSON — none of which the site itself supports.

Learn more at [Digg AI](https://digg.com).

## Install

The recommended path installs both the `digg-ai-pp-cli` binary and the `pp-digg-ai` agent skill in one shot:

```bash
npx -y @mvanhorn/printing-press install digg-ai
```

For CLI only (no skill):

```bash
npx -y @mvanhorn/printing-press install digg-ai --cli-only
```


### Without Node

The generated install path is category-agnostic until this CLI is published. If `npx` is not available before publish, install Node or use the category-specific Go fallback from the public-library entry after publish.

### Pre-built binary

Download a pre-built binary for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/digg-ai-current). On macOS, clear the Gatekeeper quarantine: `xattr -d com.apple.quarantine <binary>`. On Unix, mark it executable: `chmod +x <binary>`.

<!-- pp-hermes-install-anchor -->
## Install for Hermes

From the Hermes CLI:

```bash
hermes skills install mvanhorn/printing-press-library/cli-skills/pp-digg-ai --force
```

Inside a Hermes chat session:

```bash
/skills install mvanhorn/printing-press-library/cli-skills/pp-digg-ai --force
```

## Install for OpenClaw

Tell your OpenClaw agent (copy this):

```
Install the pp-digg-ai skill from https://github.com/mvanhorn/printing-press-library/tree/main/cli-skills/pp-digg-ai. The skill defines how its required CLI can be installed.
```

## Quick Start

```bash
# Top 10 AI stories right now, agent-ready JSON.
digg-ai latest --limit 10 --json


# Persist the current feed to the local store so trending/diff/velocity have data to work on.
digg-ai sync


# Pre-formatted morning digest ready to paste into a daily briefing.
digg-ai brief --limit 10


# Who Digg leans on the most over the last month.
digg-ai endorsers top --days 30 --limit 20 --json


# Only what is new since you last synced.
digg-ai diff --json

```

## Unique Features

These capabilities aren't available in any other tool for this API.

### Digest-shaped output
- **`brief`** — Pre-formatted top-N AI digest with headline, author, summary, and source URL — designed to pipe into a wider morning briefing.

  _Reach for this when an agent needs a ready-to-render AI news block, not a raw feed._

  ```bash
  digg-ai brief --limit 10 --json
  ```

### Local state that compounds
- **`diff`** — Show only stories that are new since the last sync, in cluster-id-stable form.

  _Pick this when you only care about what changed, not the whole feed._

  ```bash
  digg-ai diff --json
  ```
- **`endorsers feed`** — Pull every story endorsed by a given person across all sync history.

  _Use this to subscribe to a single researcher's curation, not Digg's editorial mix._

  ```bash
  digg-ai endorsers feed lateinteraction --json
  ```
- **`endorsers top`** — Rank endorsers by how often they appear in the synced corpus over a window.

  _Use to find the highest-signal people Digg leans on._

  ```bash
  digg-ai endorsers top --days 30 --limit 20 --json
  ```
- **`velocity`** — Stories whose likes count grew the fastest over the last N hours, ranked.

  _Catches something going viral inside Digg's curated world before it tops the page._

  ```bash
  digg-ai velocity --hours 6 --json
  ```
- **`trending`** — Top stories sorted by likes growth (not absolute likes) over a window.

  _Better than 'latest' once the local store has a day of snapshots behind it._

  ```bash
  digg-ai trending --hours 24 --json
  ```
- **`endorsers overlap`** — Stories endorsed by two specific people, showing the 'everyone is sharing this' signal.

  _Use when you want to find the consensus picks across a pair of researchers._

  ```bash
  digg-ai endorsers overlap karpathy lateinteraction --json
  ```

### Agent-native plumbing
- **`cooccur`** — Stories where two terms both appear in any field, ordered by recency.

  _Find the stories where competing actors show up together — a fast contradiction-spotter._

  ```bash
  digg-ai cooccur anthropic openai --json
  ```

## Usage

Run `digg-ai-pp-cli --help` for the full command reference and flag list.

## Commands

### stories

Curated AI news stories from digg.com/ai

- **`digg-ai-pp-cli stories get`** - Fetch full detail for a single AI story by its cluster ID. Returns headline, description, source URL, publish/modify times, and the list of endorsers (researchers and journalists who shared it).
- **`digg-ai-pp-cli stories list`** - List the latest AI stories curated on digg.com/ai. Returns a ranked feed of stories with headline, summary, age, likes, bookmarks, endorser count, and source link.

### topic

Curated story feeds for other Digg topics (technology, science, world, politics, business, sports, entertainment, news)

- **`digg-ai-pp-cli topic list`** - List the latest stories for a given Digg topic. AI is the primary feed; other topics (technology, science, world, politics, business, sports, entertainment, news) work the same way but are secondary.


## Output Formats

```bash
# Human-readable table (default in terminal, JSON when piped)
digg-ai-pp-cli stories list

# JSON for scripting and agents
digg-ai-pp-cli stories list --json

# Filter to specific fields
digg-ai-pp-cli stories list --json --select id,name,status

# Dry run — show the request without sending
digg-ai-pp-cli stories list --dry-run

# Agent mode — JSON + compact + no prompts in one flag
digg-ai-pp-cli stories list --agent
```

## Agent Usage

This CLI is designed for AI agent consumption:

- **Non-interactive** - never prompts, every input is a flag
- **Pipeable** - `--json` output to stdout, errors to stderr
- **Filterable** - `--select id,name` returns only fields you need
- **Previewable** - `--dry-run` shows the request without sending
- **Read-only by default** - this CLI does not create, update, delete, publish, send, or mutate remote resources
- **Offline-friendly** - sync/search commands can use the local SQLite store when available
- **Agent-safe by default** - no colors or formatting unless `--human-friendly` is set

Exit codes: `0` success, `2` usage error, `3` not found, `5` API error, `7` rate limited, `10` config error.

## Use with Claude Code

Install the focused skill — it auto-installs the CLI on first invocation:

```bash
npx skills add mvanhorn/printing-press-library/cli-skills/pp-digg-ai -g
```

Then invoke `/pp-digg-ai <query>` in Claude Code. The skill is the most efficient path — Claude Code drives the CLI directly without an MCP server in the middle.

<details>
<summary>Use as an MCP server in Claude Code (advanced)</summary>

If you'd rather register this CLI as an MCP server in Claude Code, install the MCP binary first:


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Then register it:

```bash
claude mcp add digg-ai digg-ai-pp-mcp
```

</details>

## Use with Claude Desktop

This CLI ships an [MCPB](https://github.com/modelcontextprotocol/mcpb) bundle — Claude Desktop's standard format for one-click MCP extension installs (no JSON config required).

To install:

1. Download the `.mcpb` for your platform from the [latest release](https://github.com/mvanhorn/printing-press-library/releases/tag/digg-ai-current).
2. Double-click the `.mcpb` file. Claude Desktop opens and walks you through the install.

Requires Claude Desktop 1.0.0 or later. Pre-built bundles ship for macOS Apple Silicon (`darwin-arm64`) and Windows (`amd64`, `arm64`); for other platforms, use the manual config below.

<details>
<summary>Manual JSON config (advanced)</summary>

If you can't use the MCPB bundle (older Claude Desktop, unsupported platform), install the MCP binary and configure it manually.


Install the MCP binary from this CLI's published public-library entry or pre-built release.

Add to your Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "digg-ai": {
      "command": "digg-ai-pp-mcp"
    }
  }
}
```

</details>

## Health Check

```bash
digg-ai-pp-cli doctor
```

Verifies configuration and connectivity to the API.

## Configuration

Config file: `~/.config/digg-ai-pp-cli/config.toml`

Static request headers can be configured under `headers`; per-command header overrides take precedence.

## Troubleshooting
**Not found errors (exit code 3)**
- Check the resource ID is correct
- Run the `list` command to see available items

### API-specific

- **Empty results from trending or velocity** — Run `digg-ai sync` at least twice with time between runs — these commands need ≥2 snapshots per story.
- **endorsers feed <handle> returns nothing** — The endorser appears in stories only after a sync covers a window where they endorsed. Run `digg-ai sync --topics ai,technology,science` to widen the corpus.
- **doctor reports unreachable** — Digg returns 200 over plain HTTPS for /ai today; check `curl -I https://digg.com/ai` from the same network before suspecting the CLI.

---

Generated by [CLI Printing Press](https://github.com/mvanhorn/cli-printing-press)
