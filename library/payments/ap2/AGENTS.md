# AP2 Printed CLI Agent Guide

This directory is the `ap2-pp-cli` printed CLI — the signing and authorization counterpart to `ucp-pp-cli`. It was hand-scaffolded from the ucp tree by lifting shared infrastructure (cliutil, MCP cobratree, deliver sinks, profile, doctor, feedback, agent-context) and adding ap2-specific surface on top. Treat systemic infrastructure fixes as upstream Printing Press fixes first. Keep local edits narrow and document why a generated-tree patch belongs here.

## Local Operating Contract

Start by asking the generated CLI for current runtime truth:

```bash
ap2-pp-cli doctor --json
ap2-pp-cli agent-context --pretty
```

Use runtime discovery instead of relying on a copied command list:

```bash
ap2-pp-cli which "<capability>" --json
ap2-pp-cli <command> --help
```

Add `--agent` to command invocations for JSON, compact output, non-interactive defaults, no color, and confirmation-safe scripting:

```bash
ap2-pp-cli <command> --agent
```

Before running an unfamiliar command that may mutate remote state, inspect its help and prefer a dry run:

```bash
ap2-pp-cli <command> --help
ap2-pp-cli <command> --dry-run --agent
```

Use `--yes --no-input` only after the target, arguments, and side effects are clear.

## What ap2-pp-cli ships in v0.1

Three command trees:

- **keys** — `generate`, `list`, `export` — manage ECDSA-P256 agent keys stored at `~/.config/ap2-pp-cli/keys/` (override with `AP2_KEYS_DIR`)
- **mandate** — `sign`, `verify` — sign and verify AP2 FinalizationEnvelopes emitted by `ucp-pp-cli checkout finalize`
- **payment** — `authorize`, `probe`, `status` — sandbox-default authorization, live-shape validation with stub token, transaction status lookup

The pairing model: `ucp-pp-cli` builds carts and emits a `FinalizationEnvelope`; `ap2-pp-cli mandate sign` signs all three mandates with an agent's ECDSA-P256 key; `ap2-pp-cli payment authorize --sandbox` validates the request shape without spending money; `ap2-pp-cli payment authorize --live --token-file <path>` actually POSTs to the merchant's `complete_checkout` MCP endpoint.

Token handling: prefer `AP2_GPAY_TOKEN` env or `--token-file` over `--token` (which is visible in `ps aux` and `/proc/<pid>/cmdline`).

For install, auth, examples, and longer product guidance, read `README.md` and `SKILL.md`. This file intentionally stays small so repo-local agents get invariant local guidance without duplicating the generated docs.

## Local Customizations

If you modify this CLI beyond the hand-scaffold baseline, record each customization in a `.printing-press-patches.json` at this CLI's root (parallel to `.printing-press.json`) so the change isn't lost on the next regen and is visible to the next reader.

Minimum shape:

```json
{
  "schema_version": 1,
  "applied_at": "YYYY-MM-DD",
  "base_run_id": "<copy from .printing-press.json>",
  "base_printing_press_version": "<copy from .printing-press.json>",
  "patches": [
    {
      "id": "short-identifier",
      "summary": "What changed (one sentence).",
      "reason": "Why this customization was needed (one or two sentences).",
      "files": ["internal/cli/foo.go"],
      "validated_outcome": "Optional: non-obvious test result that confirms the fix."
    }
  ]
}
```

Use `deferred_to_upstream` when a local patch is a temporary bridge for a missing public API endpoint, an unofficial-host workaround, a live response-shape drift, or behavior the Printing Press should eventually generate correctly. Search `mvanhorn/cli-printing-press` issues first; reuse a matching issue or open one, then set `upstream_issue` so the next regen knows what must supersede the patch:

```json
{
  "id": "temporary-bridge",
  "summary": "What changed (one sentence).",
  "reason": "Why this customization was needed (one or two sentences).",
  "files": ["internal/cli/foo.go"],
  "validated_outcome": "Optional: non-obvious test result that confirms the fix.",
  "deferred_to_upstream": [
    {
      "feature": "Generator behavior or upstream API capability that should eventually supersede this patch",
      "reason": "Why the local patch is temporary or API-specific"
    }
  ],
  "upstream_issue": "https://github.com/mvanhorn/cli-printing-press/issues/<n>"
}
```

This file is an **index of customizations**, not a second copy of the diff. Diffs live in `git`; the manifest is what tells the next agent (or regeneration tooling) what was customized and why. Keep `summary` and `reason` short — if you find yourself writing tables of field renames or code transformations, that detail belongs in the commit message, not here.

Inline `// PATCH:` source comments are optional. If you find them helpful as a navigation aid (`grep -rn 'PATCH' .` surfaces customized sites), feel free to add them — but they aren't required and aren't enforced by any CI.
