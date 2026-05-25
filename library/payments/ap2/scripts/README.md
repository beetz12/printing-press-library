# scripts/

Integration and utility scripts for ap2-pp-cli.

## integration_bark.sh

End-to-end acceptance test for the AP2 agentic-commerce flow against the live
bark.co merchant endpoint. This is the **US-005 / openbrain-fmpb v0.1 acceptance gate**.

### What it tests

Five stages chained together:

| Stage | Command | Assertion |
|-------|---------|-----------|
| 0 | `ap2-pp-cli keys generate` | Key written to isolated `$AP2_KEYS_DIR` |
| 1 | `ucp-pp-cli search "dog rope toy" --merchant bark.co` | ≥ 1 product returned |
| 2 | `ucp-pp-cli cart add ...` | `cart_id` captured from JSON output |
| 3 | `ucp-pp-cli checkout finalize --cart $cart_id` | `envelope_version == "1.0"` |
| 4 | `ap2-pp-cli mandate sign --envelope -` | All 3 mandate signatures non-empty |
| 5 | `ap2-pp-cli payment authorize --envelope - --sandbox` | `status == "sandbox_authorized"` AND `transaction_id` starts with `sandbox-` |

### Dependencies

- `jq` — JSON processor (`brew install jq`)
- `ucp-pp-cli` — v1.3.0+ on PATH (`~/go/bin/ucp-pp-cli`)
- `ap2-pp-cli` — built locally (default: `./build/ap2-pp-cli`) or on PATH

### Running

From the `library/ap2/` directory:

```bash
# Build first (if not already built)
mkdir -p build
go build -o build/ap2-pp-cli ./cmd/ap2-pp-cli

# Run the integration test
bash scripts/integration_bark.sh
```

Or with a custom ap2-pp-cli binary:

```bash
AP2_BIN=/path/to/ap2-pp-cli bash scripts/integration_bark.sh
```

### Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `AP2_BIN` | `./build/ap2-pp-cli` | Path to ap2-pp-cli binary |
| `AP2_KEYS_DIR` | auto-set to `mktemp -d` | Isolated key storage (auto-cleaned on exit) |
| `AP2_TXN_DIR` | auto-set to `mktemp -d` | Isolated transaction storage (auto-cleaned on exit) |

The script **never touches** `~/.config/ap2-pp-cli/` — all state goes in temp dirs
that are removed on exit.

### Expected output

```
Using ap2-pp-cli: ./build/ap2-pp-cli
Using ucp-pp-cli: /Users/dave/go/bin/ucp-pp-cli

[0/5] OK: generated agent key in /tmp/ap2keys.XXXXXX
[1/5] OK: search returned 10 hits, top: <product title> @ $<price>
[2/5] OK: cart_id=<uuid>
[3/5] OK: envelope built (subject=ucp-pp-cli-anonymous-agent)
[4/5] OK: all 3 mandates signed
[5/5] OK: sandbox_authorized txn=sandbox-<uuid>

INTEGRATION OK: <product title> @ $<price> → txn=sandbox-<uuid>
```

Exit code 0 = all stages passed. Any non-zero exit = stage N printed `[N/5] FAIL: ...` with diagnostic output before exiting.
