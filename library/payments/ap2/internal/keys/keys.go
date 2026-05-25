// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package keys

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// agentIDPattern is the strict regex guard against path-traversal attacks.
// Matches ^agent-<UUID>$ where UUID is lowercase hex with standard dashes.
var agentIDPattern = regexp.MustCompile(`^agent-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// configDir is the function used to resolve the base config directory.
// Tests override this via SetConfigDir to avoid touching the real filesystem.
var configDir = defaultConfigDir

// defaultConfigDir returns os.UserConfigDir()/ap2-pp-cli/keys, falling back
// to ~/.ap2-pp-cli/keys if UserConfigDir errors (mirrors store.go pattern).
func defaultConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("resolving config dir: %w (home fallback also failed: %v)", err, herr)
		}
		return filepath.Join(home, ".ap2-pp-cli", "keys"), nil
	}
	return filepath.Join(base, "ap2-pp-cli", "keys"), nil
}

// SetConfigDir overrides the base directory used for key storage.
// Intended for tests only.
func SetConfigDir(dir string) {
	configDir = func() (string, error) { return dir, nil }
}

// ResetConfigDir restores the default config directory resolver.
func ResetConfigDir() {
	configDir = defaultConfigDir
}

// keysDir returns the resolved keys directory, creating it if absent.
func keysDir() (string, error) {
	// Allow env override for integration testing / smoke tests.
	if env := os.Getenv("AP2_KEYS_DIR"); env != "" {
		if err := os.MkdirAll(env, 0o700); err != nil {
			return "", fmt.Errorf("creating keys dir %q: %w", env, err)
		}
		return env, nil
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating keys dir %q: %w", dir, err)
	}
	return dir, nil
}

// Key represents a stored agent key (private + public).
type Key struct {
	AgentID    string             // "agent-<uuid>"
	PrivateKey *ecdsa.PrivateKey  // nil when loaded via LoadPublic
	PublicKey  *ecdsa.PublicKey
	Path       string    // private key file path
	CreatedAt  time.Time
}

// validateAgentID rejects any string that isn't strictly
// ^agent-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
// This is the Greptile path-traversal guard from ucp PR #822.
func validateAgentID(id string) error {
	if !agentIDPattern.MatchString(id) {
		return fmt.Errorf("invalid agent id %q: must match ^agent-<uuid>$", id)
	}
	return nil
}

// Generate creates a new ECDSA-P256 keypair, stores it under the keys
// directory as agent-<uuid>.{pem,pub}, and returns the resulting Key.
// Private file is 0o600; public file is 0o644; directory is 0o700.
// Private encoding: PKCS#8 PEM block. Public encoding: PKIX PEM block.
func Generate() (*Key, error) {
	dir, err := keysDir()
	if err != nil {
		return nil, err
	}

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating ECDSA-P256 key: %w", err)
	}

	agentID := "agent-" + uuid.New().String()

	// Encode private key as PKCS#8 PEM.
	privDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	// Encode public key as PKIX PEM.
	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	privPath := filepath.Join(dir, agentID+".pem")
	pubPath := filepath.Join(dir, agentID+".pub")

	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("writing private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		// Clean up private key if public write fails.
		_ = os.Remove(privPath)
		return nil, fmt.Errorf("writing public key: %w", err)
	}

	fi, err := os.Stat(privPath)
	var createdAt time.Time
	if err == nil {
		createdAt = fi.ModTime()
	}

	return &Key{
		AgentID:    agentID,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
		Path:       privPath,
		CreatedAt:  createdAt,
	}, nil
}

// Load reads agent-<id>.pem (private key) and returns the parsed Key.
// Validates agentID before any filepath.Join to prevent path traversal.
func Load(agentID string) (*Key, error) {
	if err := validateAgentID(agentID); err != nil {
		return nil, err
	}
	dir, err := keysDir()
	if err != nil {
		return nil, err
	}

	privPath := filepath.Join(dir, agentID+".pem")
	privData, err := os.ReadFile(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent key %q not found — run 'ap2-pp-cli keys list' to see available keys", agentID)
		}
		return nil, fmt.Errorf("reading private key: %w", err)
	}

	block, _ := pem.Decode(privData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in %s", privPath)
	}

	rawKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	privKey, ok := rawKey.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key at %s is not an ECDSA key", privPath)
	}

	fi, err := os.Stat(privPath)
	var createdAt time.Time
	if err == nil {
		createdAt = fi.ModTime()
	}

	return &Key{
		AgentID:    agentID,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
		Path:       privPath,
		CreatedAt:  createdAt,
	}, nil
}

// LoadPublic reads agent-<id>.pub (public key only) and returns the parsed Key.
// PrivateKey will be nil on the returned Key.
// Validates agentID before any filepath.Join to prevent path traversal.
func LoadPublic(agentID string) (*Key, error) {
	if err := validateAgentID(agentID); err != nil {
		return nil, err
	}
	dir, err := keysDir()
	if err != nil {
		return nil, err
	}

	pubPath := filepath.Join(dir, agentID+".pub")
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agent key %q not found — run 'ap2-pp-cli keys list' to see available keys", agentID)
		}
		return nil, fmt.Errorf("reading public key: %w", err)
	}

	block, _ := pem.Decode(pubData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in %s", pubPath)
	}

	rawKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing public key: %w", err)
	}
	pubKey, ok := rawKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key at %s is not an ECDSA public key", pubPath)
	}

	fi, err := os.Stat(pubPath)
	var createdAt time.Time
	if err == nil {
		createdAt = fi.ModTime()
	}

	privPath := filepath.Join(dir, agentID+".pem")
	return &Key{
		AgentID:   agentID,
		PublicKey: pubKey,
		Path:      privPath, // canonical path is still the .pem
		CreatedAt: createdAt,
	}, nil
}

// List enumerates all agent keys present on disk (returns public side only).
// Result is sorted lexicographically by AgentID for deterministic output.
func List() ([]*Key, error) {
	dir, err := keysDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Key{}, nil
		}
		return nil, fmt.Errorf("listing keys dir: %w", err)
	}

	var keys []*Key
	seen := map[string]bool{}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".pub") {
			continue
		}
		agentID := strings.TrimSuffix(name, ".pub")
		if seen[agentID] {
			continue
		}
		if err := validateAgentID(agentID); err != nil {
			// Skip files that don't match the agent ID pattern.
			continue
		}
		seen[agentID] = true

		k, err := LoadPublic(agentID)
		if err != nil {
			// Skip unreadable keys rather than aborting the whole list.
			continue
		}
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i].AgentID < keys[j].AgentID
	})

	if keys == nil {
		keys = []*Key{}
	}
	return keys, nil
}

// ExportPEM returns the PEM-encoded public key as a string.
func (k *Key) ExportPEM() (string, error) {
	if k.PublicKey == nil {
		return "", fmt.Errorf("key %q has no public key loaded", k.AgentID)
	}
	der, err := x509.MarshalPKIXPublicKey(k.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshaling public key to PKIX: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// jwkPublic is the JSON structure for an EC JWK (RFC 7518 §6.2).
type jwkPublic struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	Kid string `json:"kid"`
}

// ExportJWK returns the JSON-encoded JWK (kty:EC, crv:P-256, x/y base64url, kid:AgentID).
// Per RFC 7518 §6.2. x and y are 32-byte big-endian, base64url-encoded without padding.
func (k *Key) ExportJWK() (string, error) {
	if k.PublicKey == nil {
		return "", fmt.Errorf("key %q has no public key loaded", k.AgentID)
	}

	// P-256 coordinates are 32 bytes each. Pad to 32 bytes with leading zeros.
	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	k.PublicKey.X.FillBytes(xBytes)
	k.PublicKey.Y.FillBytes(yBytes)

	jwk := jwkPublic{
		Kty: "EC",
		Crv: "P-256",
		X:   base64.RawURLEncoding.EncodeToString(xBytes),
		Y:   base64.RawURLEncoding.EncodeToString(yBytes),
		Kid: k.AgentID,
	}

	data, err := json.Marshal(jwk)
	if err != nil {
		return "", fmt.Errorf("marshaling JWK: %w", err)
	}
	return string(data), nil
}
