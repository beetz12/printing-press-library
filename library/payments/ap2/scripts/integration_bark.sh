#!/usr/bin/env bash
# integration_bark.sh — end-to-end AP2 acceptance test against bark.co (sandbox)
#
# Chains: ucp-pp-cli search → cart add → checkout finalize → ap2 mandate sign → payment authorize
# This is the US-005 / openbrain-fmpb v0.1 ACCEPTANCE GATE.
# See scripts/README.md and library/ap2/README.md for context.
set -euo pipefail

# ---------------------------------------------------------------------------
# Dependency checks
# ---------------------------------------------------------------------------
for cmd in jq ucp-pp-cli; do
  command -v "$cmd" >/dev/null 2>&1 || { echo "ERROR: missing dependency: $cmd"; exit 1; }
done

AP2_BIN="${AP2_BIN:-./build/ap2-pp-cli}"
if [[ ! -x "$AP2_BIN" ]]; then
  AP2_BIN=$(command -v ap2-pp-cli 2>/dev/null) || { echo "ERROR: missing dependency: ap2-pp-cli (set AP2_BIN or add to PATH)"; exit 1; }
fi

echo "Using ap2-pp-cli: $AP2_BIN"
echo "Using ucp-pp-cli: $(command -v ucp-pp-cli)"
echo ""

# ---------------------------------------------------------------------------
# Isolated config dirs (so this test never pollutes ~/.config/ap2-pp-cli)
# ---------------------------------------------------------------------------
export AP2_KEYS_DIR
export AP2_TXN_DIR
AP2_KEYS_DIR=$(mktemp -d)
AP2_TXN_DIR=$(mktemp -d)
trap 'rm -rf "$AP2_KEYS_DIR" "$AP2_TXN_DIR"' EXIT

# ---------------------------------------------------------------------------
# Stage 0: Generate a fresh agent key, capture agent_id for signing
# ---------------------------------------------------------------------------
KEY_JSON=$("$AP2_BIN" keys generate --json 2>&1)
AGENT_ID=$(echo "$KEY_JSON" | jq -r '.agent_id // empty')
if [[ -z "$AGENT_ID" ]]; then
  echo "[0/5] FAIL: keys generate returned no agent_id"
  echo "$KEY_JSON"
  exit 1
fi
echo "[0/5] OK: generated agent key $AGENT_ID in $AP2_KEYS_DIR"

# ---------------------------------------------------------------------------
# Stage 1: Search bark.co for "dog rope toy"
# ---------------------------------------------------------------------------
SEARCH=$(ucp-pp-cli search "dog rope toy" --merchant bark.co --json 2>&1)
COUNT=$(echo "$SEARCH" | jq 'if type == "array" then length else 0 end')
if [[ "$COUNT" -lt 1 ]]; then
  echo "[1/5] FAIL: search returned no products"
  echo "$SEARCH" | head -20
  exit 1
fi

TITLE=$(echo "$SEARCH" | jq -r '.[0].title')
PRICE_CENTS=$(echo "$SEARCH" | jq -r '.[0].price')
VARIANT=$(echo "$SEARCH" | jq -r '.[0].variant_id')
# Format price as dollars for display
PRICE_DOLLARS=$(echo "scale=2; $PRICE_CENTS / 100" | bc)

echo "[1/5] OK: search returned $COUNT hits, top: $TITLE @ \$$PRICE_DOLLARS"

# ---------------------------------------------------------------------------
# Stage 2: Add to cart, capture cart_id
# ---------------------------------------------------------------------------
CART_ADD=$(ucp-pp-cli cart add \
  --merchant bark.co \
  --variant-id "$VARIANT" \
  --title "$TITLE" \
  --price "$PRICE_CENTS" \
  --qty 1 \
  --json 2>&1)

CART_ID=$(echo "$CART_ADD" | jq -r '.id // empty' 2>/dev/null)
if [[ -z "$CART_ID" ]]; then
  # Fallback: extract UUID from human-friendly output
  CART_ID=$(echo "$CART_ADD" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
fi
if [[ -z "$CART_ID" ]]; then
  echo "[2/5] FAIL: no cart_id in cart add output:"
  echo "$CART_ADD"
  exit 1
fi
echo "[2/5] OK: cart_id=$CART_ID"

# ---------------------------------------------------------------------------
# Stage 3: Checkout finalize → FinalizationEnvelope JSON
# ---------------------------------------------------------------------------
ENVELOPE=$(ucp-pp-cli checkout finalize --cart "$CART_ID" --json 2>&1)
ENVELOPE_VERSION=$(echo "$ENVELOPE" | jq -r '.envelope_version // empty' 2>/dev/null)
if [[ "$ENVELOPE_VERSION" != "1.0" ]]; then
  echo "[3/5] FAIL: expected envelope_version=1.0, got: $ENVELOPE_VERSION"
  echo "$ENVELOPE" | head -20
  exit 1
fi
SUBJECT=$(echo "$ENVELOPE" | jq -r '.subject // empty')
echo "[3/5] OK: envelope built (subject=$SUBJECT)"

# ---------------------------------------------------------------------------
# Stage 4: ap2 mandate sign — pipe envelope via stdin; auto-selects the 1 key
# ---------------------------------------------------------------------------
SIGNED=$(echo "$ENVELOPE" | "$AP2_BIN" mandate sign --envelope - --subject "$AGENT_ID" --json 2>&1)

INTENT_SIG=$(echo "$SIGNED" | jq -r '.intent_mandate.signature // empty' 2>/dev/null)
CART_SIG=$(echo "$SIGNED" | jq -r '.cart_mandate.signature // empty' 2>/dev/null)
PAYMENT_SIG=$(echo "$SIGNED" | jq -r '.payment_mandate.signature // empty' 2>/dev/null)

if [[ -z "$INTENT_SIG" ]]; then
  echo "[4/5] FAIL: intent_mandate.signature is empty"
  echo "$SIGNED" | head -20
  exit 1
fi
if [[ -z "$CART_SIG" ]]; then
  echo "[4/5] FAIL: cart_mandate.signature is empty"
  echo "$SIGNED" | head -20
  exit 1
fi
if [[ -z "$PAYMENT_SIG" ]]; then
  echo "[4/5] FAIL: payment_mandate.signature is empty"
  echo "$SIGNED" | head -20
  exit 1
fi
echo "[4/5] OK: all 3 mandates signed"

# ---------------------------------------------------------------------------
# Stage 5: ap2 payment authorize --sandbox → {status: sandbox_authorized}
# ---------------------------------------------------------------------------
AUTH=$(echo "$SIGNED" | "$AP2_BIN" payment authorize --envelope - --sandbox --json 2>&1)
STATUS=$(echo "$AUTH" | jq -r '.status // empty' 2>/dev/null)
TXN=$(echo "$AUTH" | jq -r '.transaction_id // empty' 2>/dev/null)

if [[ "$STATUS" != "sandbox_authorized" ]]; then
  echo "[5/5] FAIL: expected status=sandbox_authorized, got: $STATUS"
  echo "$AUTH"
  exit 1
fi
if [[ "$TXN" != sandbox-* ]]; then
  echo "[5/5] FAIL: expected transaction_id starting with sandbox-, got: $TXN"
  echo "$AUTH"
  exit 1
fi
echo "[5/5] OK: sandbox_authorized txn=$TXN"

echo ""
echo "INTEGRATION OK: $TITLE @ \$$PRICE_DOLLARS → txn=$TXN"
