# digg-ai CLI Brief

## API Identity
- Domain: digg.com/ai — Digg's curated AI news section ("AI news, before it trends"). Operated by Basic Intelligence (basic_in_ on X).
- Users: AI researchers, engineers, founders, and AI-watchers who want a digest of what AI insiders on X are pointing at — before mainstream coverage hits.
- Data profile: Curated story feed. Each story aggregates an X post from a notable "endorser" (researcher/journalist/founder) plus optional staff commentary. Pure HTML, no public REST/JSON API, no auth required.

## Reachability Risk
- **Low**. `probe-reachability` returned `standard_http` (200 via stdlib HTTP, confidence 0.95, 427ms). Same-origin asset CDN. No Cloudflare/WAF/DataDome challenge. No login or rate limit visible at this depth.

## Top Workflows
1. **"What's hot in AI right now"** — pull the top N ranked stories from `/ai` with headline, summary, age, likes, bookmarks. The most-frequent reason anyone visits Digg.
2. **"Tell me about story X"** — fetch full detail for a story by cluster-id/slug: headline, description, source URL (usually an X post), endorsers (the people who shared it), published/modified times.
3. **"What did <person> share this week"** — pivot the feed by endorser. Digg's whole thesis is "watch the people who move first" — surfacing one endorser's curated firehose is a primary use case the website doesn't let you do directly.
4. **"What's new since I last looked"** — diff against the last sync. Inbox-style "show me only the ones I haven't seen".
5. **"Morning AI brief"** — top-10 digest with author, summary, source URL. Designed to be piped into a wider morning briefing flow.

## Table Stakes
- List + filter by topic (Digg has /ai, /technology, /science, /world, /politics, /business, /sports, /entertainment, /news)
- Detail view for a single story
- JSON output for piping
- Local store so repeat fetches are cheap and historical data accumulates
- Search across the local corpus

## Data Layer
- **Primary entities:**
  - `Story` — cluster_id (UUID, PK), slug, topic, surface (highlight|top|in-case-you-missed-it), rank, headline, headline_short, summary, source_url, age_label, likes, bookmarks, endorser_count, fetched_at, first_seen_at, last_seen_at
  - `Endorser` — handle (PK), name, digg_url, x_url, avatar_url
  - `StoryEndorsement` — (cluster_id, handle) M2M
  - `StorySnapshot` — (cluster_id, snapshot_at, likes, bookmarks, rank) for trend / velocity calculations
- **Sync cursor:** No native cursor. Sync = fetch `/ai` (and optionally other topics), upsert by cluster_id, append a StorySnapshot row per fetched story.
- **FTS/search:** FTS5 over (headline, headline_short, summary) and over endorser names/handles.

## Codebase Intelligence
- No public SDK or wrapper exists. Digg deprecated its public API in 2010s and the current /ai relaunch (Basic Intelligence, 2025+) is a fresh Next.js App Router build. Confirmed by absence of `__NEXT_DATA__` (RSC streaming via `self.__next_f.push`).
- Stories embedded as `<article data-story-row="true">` (highlight surface) or as listing rows with `data-testid="story-row-meta"` (ranked surface, ranks 1–30).
- Rich `data-*` attributes on each story: `data-cluster-id`, `data-story-id`, `data-story-rank`, `data-story-surface`, `data-story-row-surface`, `data-story-topic`, `data-story-endorser-count`, `data-story-headline-short`.
- Story detail pages emit clean JSON-LD `NewsArticle`: headline, description, url, datePublished, dateModified, author[] (with x.com sameAs), image. This is the gold-standard parse target.
- Endorsers link to `/u/x/<handle>` with avatar `<img>` carrying `alt="<full name>"`.
- HTML page is ~3.4MB (lots of inlined CSS/JS), but the structured fields we want are present in the first ~50KB.

## User Vision
- David wants the latest AI news from digg.com/ai. He uses a personal-knowledge stack (OpenBrain, AI David, work-system) and consumes morning briefings. Output should be agent-piping-friendly (JSON, `--select`) and quick (`digg-ai latest --json | jq`).

## Product Thesis
- **Name:** `digg-ai`
- **Why it should exist:** Digg's /ai relaunch is the highest-signal-per-minute AI news feed in 2025–2026 because it curates what *researchers themselves* boost on X. There is no CLI, MCP, or programmatic wrapper for it. `digg-ai` is the first agent-native way to consume it: scriptable, JSON-clean, locally indexed, and able to pivot the feed by person (which the site itself doesn't expose). The local SQLite snapshot history unlocks trend/velocity analysis that the site doesn't show either.

## Build Priorities
1. Spec → generator skeleton with store + sync + search + MCP wiring
2. Hand-built HTML parsers: `/ai` listing (highlight + ranked), `/{topic}` listing, `/ai/{slug}` detail (JSON-LD primary, HTML fallback)
3. Core commands: `latest`, `topic`, `show`, `topics`, `sync`, `search`, `sql`, `doctor`
4. Transcendence: `trending` (sort by likes growth), `endorsers top|feed`, `brief` (morning digest), `diff` (since-last-sync), `velocity`
5. README/SKILL/MCP narrative + agent-friendly examples
