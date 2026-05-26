// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package keys

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

// withTempDir overrides the keys directory with a temp dir for the duration of the test.
func withTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	SetConfigDir(dir)
	t.Cleanup(ResetConfigDir)
}

func TestGenerate_RoundTrip(t *testing.T) {
	withTempDir(t)

	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if k.AgentID == "" {
		t.Fatal("AgentID is empty")
	}
	if k.PrivateKey == nil {
		t.Fatal("PrivateKey is nil after Generate")
	}
	if k.PublicKey == nil {
		t.Fatal("PublicKey is nil after Generate")
	}
	if k.Path == "" {
		t.Fatal("Path is empty")
	}

	// Load by ID and verify public keys match.
	loaded, err := Load(k.AgentID)
	if err != nil {
		t.Fatalf("Load(%q): %v", k.AgentID, err)
	}

	origPub, err := x509.MarshalPKIXPublicKey(k.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey (orig): %v", err)
	}
	loadedPub, err := x509.MarshalPKIXPublicKey(loaded.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey (loaded): %v", err)
	}
	if string(origPub) != string(loadedPub) {
		t.Fatal("public keys differ between Generate and Load")
	}
}

func TestList_Deterministic(t *testing.T) {
	withTempDir(t)

	// Generate 3 keys.
	var ids []string
	for i := 0; i < 3; i++ {
		k, err := Generate()
		if err != nil {
			t.Fatalf("Generate #%d: %v", i, err)
		}
		ids = append(ids, k.AgentID)
	}

	keys, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}

	// Verify sorted order.
	for i := 1; i < len(keys); i++ {
		if keys[i-1].AgentID >= keys[i].AgentID {
			t.Errorf("List not sorted: keys[%d]=%q >= keys[%d]=%q", i-1, keys[i-1].AgentID, i, keys[i].AgentID)
		}
	}

	// All generated IDs present.
	byID := map[string]bool{}
	for _, k := range keys {
		byID[k.AgentID] = true
	}
	for _, id := range ids {
		if !byID[id] {
			t.Errorf("generated id %q not found in List output", id)
		}
	}
}

func TestValidateAgentID(t *testing.T) {
	valid := []string{
		"agent-12345678-1234-1234-1234-123456789012",
		"agent-00000000-0000-0000-0000-000000000000",
		"agent-ffffffff-ffff-ffff-ffff-ffffffffffff",
		"agent-a1b2c3d4-e5f6-7890-abcd-ef1234567890",
	}
	for _, id := range valid {
		t.Run("valid/"+id, func(t *testing.T) {
			if err := validateAgentID(id); err != nil {
				t.Errorf("validateAgentID(%q) returned unexpected error: %v", id, err)
			}
		})
	}

	invalid := []string{
		"../../etc/passwd",
		"agent-../escape",
		"agent-",
		"",
		"AGENT-12345678-1234-1234-1234-123456789012", // uppercase
		"agent-12345678-1234-1234-1234-abcdef0000a",  // too short
		"agent-12345678-1234-1234-1234-1234567890123", // too long
		"agent-12345678-1234-1234-1234-12345678901g",  // non-hex char
		"12345678-1234-1234-1234-123456789012",        // missing "agent-" prefix
		"agent-",
		"agent-xyz",
	}
	for _, id := range invalid {
		t.Run("invalid/"+id, func(t *testing.T) {
			if err := validateAgentID(id); err == nil {
				t.Errorf("validateAgentID(%q) should have returned error, got nil", id)
			}
		})
	}
}

func TestExportJWK(t *testing.T) {
	withTempDir(t)

	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	jwkStr, err := k.ExportJWK()
	if err != nil {
		t.Fatalf("ExportJWK: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(jwkStr), &parsed); err != nil {
		t.Fatalf("ExportJWK output is not valid JSON: %v", err)
	}

	// kty=EC, crv=P-256
	if parsed["kty"] != "EC" {
		t.Errorf("kty: want EC, got %v", parsed["kty"])
	}
	if parsed["crv"] != "P-256" {
		t.Errorf("crv: want P-256, got %v", parsed["crv"])
	}

	// kid equals AgentID
	if parsed["kid"] != k.AgentID {
		t.Errorf("kid: want %q, got %v", k.AgentID, parsed["kid"])
	}

	// x and y are 32-byte base64url-encoded (no padding)
	for _, coord := range []string{"x", "y"} {
		val, ok := parsed[coord].(string)
		if !ok {
			t.Fatalf("JWK %q is not a string", coord)
		}
		// No padding characters allowed
		if strings.Contains(val, "=") {
			t.Errorf("JWK %q contains padding: %q", coord, val)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(val)
		if err != nil {
			t.Errorf("JWK %q is not valid base64url: %v", coord, err)
		}
		if len(decoded) != 32 {
			t.Errorf("JWK %q decoded length: want 32, got %d", coord, len(decoded))
		}
	}
}

func TestExportPEM(t *testing.T) {
	withTempDir(t)

	k, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	pemStr, err := k.ExportPEM()
	if err != nil {
		t.Fatalf("ExportPEM: %v", err)
	}

	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("ExportPEM output is not valid PEM")
	}
	if block.Type != "PUBLIC KEY" {
		t.Errorf("PEM block type: want %q, got %q", "PUBLIC KEY", block.Type)
	}

	// Parses back to the same public key.
	rawKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey: %v", err)
	}
	parsedPub, ok := rawKey.(*ecdsa.PublicKey)
	if !ok {
		t.Fatal("parsed key is not *ecdsa.PublicKey")
	}

	origDER, _ := x509.MarshalPKIXPublicKey(k.PublicKey)
	parsedDER, _ := x509.MarshalPKIXPublicKey(parsedPub)
	if string(origDER) != string(parsedDER) {
		t.Error("ExportPEM round-trip: public keys differ")
	}
}
