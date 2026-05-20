---
name: pp-digg-ai
description: "AI news from digg.com/ai with offline state, endorser pivots, and velocity tracking no website surface gives you. Trigger phrases: `what's the latest AI news on digg`, `digg AI brief`, `show me what AI researchers are sharing`, `latest from digg.com/ai`, `morning AI digest from digg`, `use digg-ai`, `run digg-ai`."
author: "david"
license: "Apache-2.0"
argument-hint: "<command> [args] | install cli|mcp"
allowed-tools: "Read Bash"
metadata:
  openclaw:
    requires:
      bins:
        - digg-ai-pp-cli
---

# Digg AI — Printing Press CLI

## Prerequisites: Install the CLI

This skill drives the `digg-ai-pp-cli` binary. **You must verify the CLI is installed before invoking any command from this skill.** If it is missing, install it first:

1. Install via the Printing Press installer:
   ```bash
   npx -y @mvanhorn/printing-press install digg-ai --cli-only
   ```
2. Verify: `digg-ai-pp-cli --version`
3. Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on `$PATH`.

If the `npx` install fails (no Node, offline, etc.), fall back to a direct Go install (requires Go 1.26.3 or newer):

```bash
go install github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/cmd/digg-ai-pp-cli@latest
```

If `--version` reports "command not found" after install, the install step did not put the binary on `$PATH`. Do not proceed with skill commands until verification succeeds.

digg-ai pulls the curated AI feed from digg.com/ai and persists it locally. Once it's in SQLite you can pivot by endorser, diff against last sync, see which stories are climbing fastest, and pipe everything into agent flows as clean JSON — none of which the site itself supports.

## When to Use This CLI

Reach for digg-ai when you want curated AI news in scriptable form. It is the right pick for daily AI briefings, watching specific researchers' picks, or feeding a downstream agent that needs structured story data. It is not the right pick if you want raw RSS from every news site — use a generic aggregator. Digg's value is the curation; this CLI's value is making that curation programmable.

## When Not to Use This CLI

Do not activate this CLI for requests that require creating, updating, deleting, publishing, commenting, upvoting, inviting, ordering, sending messages, booking, purchasing, or changing remote state. This printed CLI exposes read-only commands for inspection, export, sync, and analysis.

## Unique Capabilities

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

## Command Reference

**stories** — Curated AI news stories from digg.com/ai

- `digg-ai-pp-cli stories get` — Fetch full detail for a single AI story by its cluster ID. Returns headline, description, source URL, publish/modify...
- `digg-ai-pp-cli stories list` — List the latest AI stories curated on digg.com/ai. Returns a ranked feed of stories with headline, summary, age,...

**topic** — Curated story feeds for other Digg topics (technology, science, world, politics, business, sports, entertainment, news)

- `digg-ai-pp-cli topic <topic>` — List the latest stories for a given Digg topic. AI is the primary feed; other topics (technology, science, world,...


### Finding the right command

When you know what you want to do but not which command does it, ask the CLI directly:

```bash
digg-ai-pp-cli which "<capability in your own words>"
```

`which` resolves a natural-language capability query to the best matching command from this CLI's curated feature index. Exit code `0` means at least one match; exit code `2` means no confident match — fall back to `--help` or use a narrower query.

## Recipes


### Top headlines as plain lines

```bash
digg-ai latest --limit 10 --json --select stories.headline | jq -r '.[]'
```

Strip the structured output down to just the headlines.

### Morning digest with sources

```bash
digg-ai brief --limit 10 --json --select stories.headline,stories.source_url,stories.authors
```

Headline plus source URL and authors, ready to render as a list.

### What's new today

```bash
digg-ai sync && digg-ai diff --json
```

Sync, then show only stories added since the previous sync.

### Watch one researcher

```bash
digg-ai endorsers feed lateinteraction --limit 20 --json
```

All stories endorsed by Omar Khattab across the local corpus.

### Velocity climbers in last 6h

```bash
digg-ai velocity --hours 6 --limit 10 --json
```

Stories whose likes count grew fastest in the last 6 hours.

## Auth Setup

No authentication required.

Run `digg-ai-pp-cli doctor` to verify setup.

## Agent Mode

Add `--agent` to any command. Expands to: `--json --compact --no-input --no-color --yes`.

- **Pipeable** — JSON on stdout, errors on stderr
- **Filterable** — `--select` keeps a subset of fields. Dotted paths descend into nested structures; arrays traverse element-wise. Critical for keeping context small on verbose APIs:

  ```bash
  digg-ai-pp-cli stories list --agent --select id,name,status
  ```
- **Previewable** — `--dry-run` shows the request without sending
- **Offline-friendly** — sync/search commands can use the local SQLite store when available
- **Non-interactive** — never prompts, every input is a flag
- **Read-only** — do not use this CLI for create, update, delete, publish, comment, upvote, invite, order, send, or other mutating requests

### Response envelope

Commands that read from the local store or the API wrap output in a provenance envelope:

```json
{
  "meta": {"source": "live" | "local", "synced_at": "...", "reason": "..."},
  "results": <data>
}
```

Parse `.results` for data and `.meta.source` to know whether it's live or local. A human-readable `N results (live)` summary is printed to stderr only when stdout is a terminal — piped/agent consumers get pure JSON on stdout.

## Agent Feedback

When you (or the agent) notice something off about this CLI, record it:

```
digg-ai-pp-cli feedback "the --since flag is inclusive but docs say exclusive"
digg-ai-pp-cli feedback --stdin < notes.txt
digg-ai-pp-cli feedback list --json --limit 10
```

Entries are stored locally at `~/.digg-ai-pp-cli/feedback.jsonl`. They are never POSTed unless `DIGG_AI_FEEDBACK_ENDPOINT` is set AND either `--send` is passed or `DIGG_AI_FEEDBACK_AUTO_SEND=true`. Default behavior is local-only.

Write what *surprised* you, not a bug report. Short, specific, one line: that is the part that compounds.

## Output Delivery

Every command accepts `--deliver <sink>`. The output goes to the named sink in addition to (or instead of) stdout, so agents can route command results without hand-piping. Three sinks are supported:

| Sink | Effect |
|------|--------|
| `stdout` | Default; write to stdout only |
| `file:<path>` | Atomically write output to `<path>` (tmp + rename) |
| `webhook:<url>` | POST the output body to the URL (`application/json` or `application/x-ndjson` when `--compact`) |

Unknown schemes are refused with a structured error naming the supported set. Webhook failures return non-zero and log the URL + HTTP status on stderr.

## Named Profiles

A profile is a saved set of flag values, reused across invocations. Use it when a scheduled agent calls the same command every run with the same configuration - HeyGen's "Beacon" pattern.

```
digg-ai-pp-cli profile save briefing --json
digg-ai-pp-cli --profile briefing stories list
digg-ai-pp-cli profile list --json
digg-ai-pp-cli profile show briefing
digg-ai-pp-cli profile delete briefing --yes
```

Explicit flags always win over profile values; profile values win over defaults. `agent-context` lists all available profiles under `available_profiles` so introspecting agents discover them at runtime.

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage error (wrong arguments) |
| 3 | Resource not found |
| 5 | API error (upstream issue) |
| 7 | Rate limited (wait and retry) |
| 10 | Config error |

## Argument Parsing

Parse `$ARGUMENTS`:

1. **Empty, `help`, or `--help`** → show `digg-ai-pp-cli --help` output
2. **Starts with `install`** → ends with `mcp` → MCP installation; otherwise → see Prerequisites above
3. **Anything else** → Direct Use (execute as CLI command with `--agent`)

## MCP Server Installation

Install the MCP binary from this CLI's published public-library entry or pre-built release, then register it:

```bash
claude mcp add digg-ai-pp-mcp -- digg-ai-pp-mcp
```

Verify: `claude mcp list`

## Direct Use

1. Check if installed: `which digg-ai-pp-cli`
   If not found, offer to install (see Prerequisites at the top of this skill).
2. Match the user query to the best command from the Unique Capabilities and Command Reference above.
3. Execute with the `--agent` flag:
   ```bash
   digg-ai-pp-cli <command> [subcommand] [args] --agent
   ```
4. If ambiguous, drill into subcommand help: `digg-ai-pp-cli <command> --help`.
