# AP2 PP CLI — Ship Check
**run_id:** 20260525-051800-hand-scaffold
**date:** 2026-05-25

## Verification gates passed

### Build
```
go mod tidy        ✓ no changes
go build ./...     ✓ zero errors
go vet ./...       ✓ zero warnings
```

### Binary smoke tests
```
ap2-pp-cli version       ✓ prints "ap2-pp-cli v0.1.0"
ap2-pp-cli --help        ✓ renders usage with all top-level commands
ap2-pp-cli doctor --json ✓ returns JSON with auth_source, config_path fields
```

### Fix verifications
- Fix 4 (txnstore): error message now reads `must contain only alphanumerics, hyphens, and be prefixed sandbox- or live-` matching the actual regex `^(sandbox|live)-<uuid>$`
- Fix 5 (config): `API_TOKEN` env var fallback removed; only `AP2_*` vars accepted; `API_TOKEN=stolen "$CLI" doctor --json` does NOT populate auth credentials

### Manifest check
```
manifest.json server.mcp_config.env declares 5 entries:
  AP2_BASE_URL, AP2_CONFIG, AP2_GPAY_TOKEN, AP2_KEYS_DIR, AP2_TXN_DIR ✓
```

### CI annotations addressed
- Verify gate: .manuscripts/ present ✓
- Verify gate: .printing-press-patches.json present ✓
- MCPB manifest contract: all 5 env vars declared ✓
- Greptile P1 txnstore.go:61 ✓
- Greptile P1 config.go:58 ✓
