# digg-ai Absorb Manifest

## Source landscape
No existing CLI, MCP server, Claude plugin, npm wrapper, or PyPI package targets digg.com/ai. The 2025+ Basic Intelligence relaunch has no public API. Closest competitors are generic news-aggregator CLIs (newsboat, rss2email) and AI-news scrapers (TLDR AI, AINews scrapers). Reverse-engineered RSS attempts return HTML (Digg dropped real RSS). So the absorb table is short — there's almost nothing to mirror — but the transcendence table is where the differentiation lives.

## Absorbed (match-or-beat every feature any related tool has)
| # | Feature | Best Source | Our Implementation | Added Value |
|---|---------|-------------|--------------------|-------------|
| 1 | List latest items | newsboat list, rss2email digest | `digg-ai latest --limit N --json` | Structured JSON, --select dotted paths, --csv, --compact |
| 2 | Filter by category/topic | newsboat tag filter | `digg-ai topic <name>` and `--topic` on `latest` | Topics enumerated; auto-completes; tab-completable |
| 3 | Show single item detail | newsboat show, lynx + RSS link | `digg-ai show <cluster-id>` | JSON-LD parsed, authors, source URL, publish/modify times |
| 4 | Search across saved items | newsboat search, sqlite over OPML | `digg-ai search <query>` + FTS5 | Offline, regex, ranked |
| 5 | Background sync | newsboat reload, rss2email cron | `digg-ai sync [--topics ai,technology,...]` | Upsert by cluster-id, snapshot row per fetch |
| 6 | Raw SQL access | sqlite3 on OPML db | `digg-ai sql "SELECT ..."` | Read-only, parameterized, composable with jq |
| 7 | Health check | newsboat -E | `digg-ai doctor` | Reachability + corpus age + sync freshness |
| 8 | Open in browser | newsboat o | `digg-ai open <id>` (print URL by default; `--launch` to open) | Verify-friendly default; respects PRINTING_PRESS_VERIFY |
| 9 | Trigger phrases / agent surface | None — newsboat is TUI-only | MCP server + SKILL.md | Agent-callable; cobratree-walked tools |
| 10 | Schedule polling | rss2email cron + custom recipe | `digg-ai sync` returns counts; agent crons it | Idempotent, --dry-run, --since cursor |

## Transcendence (only possible with our approach)
| # | Feature | Command | Why Only We Can Do This | Score |
|---|---------|---------|--------------------------|-------|
| 1 | Endorser pivot (firehose for one person) | `digg-ai endorsers feed <handle>` | Site indexes by topic, never by endorser; requires local cross-time join of StoryEndorsement | 9 |
| 2 | Top endorsers leaderboard | `digg-ai endorsers top --days 30` | Aggregation over time the website doesn't surface | 8 |
| 3 | Trending by velocity (likes growth) | `digg-ai trending --hours 24` | Needs multiple StorySnapshot rows; site only shows current count | 9 |
| 4 | Since-last-sync diff | `digg-ai diff` | Stateless site can't answer "what's new since I last looked" | 8 |
| 5 | Morning AI brief | `digg-ai brief --limit 10` | Pre-formatted digest combining headline + authors + source URL; agents pipe into broader briefings | 8 |
| 6 | Velocity climbers | `digg-ai velocity --hours 6` | Stories whose likes count grew fastest in last N hours; only possible from snapshot history | 7 |
| 7 | Cooccurrence search | `digg-ai cooccur "<a>" "<b>"` | Stories where both terms appear in any field, ordered by recency | 6 |
| 8 | Endorser overlap | `digg-ai endorsers overlap @a @b` | Stories endorsed by BOTH people — quickly finds the "everyone is sharing this" signal | 7 |

All transcendence rows ship as real implementations (not stubs). They are the differentiator.

## Stubs / known gaps
None planned. Endorser-feed pivot depends on having sync history of enough breadth; a fresh install will show fewer endorser rows than a steady-state corpus, but the command works against any non-empty store — this is documented in `## Known Gaps` if it surfaces during dogfood.
