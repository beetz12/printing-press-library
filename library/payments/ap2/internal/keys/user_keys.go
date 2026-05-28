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

// userIDPattern is the strict regex guard against path-traversal attacks for user keys.
// Matches ^user-<UUID>$ where UUID is lowercase hex with standard dashes.
var userIDPattern = regexp.MustCompile(`^user-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// userConfigDir is the function used to resolve the base user-keys config directory.
// Tests override this via SetUserConfigDir to avoid touching the real filesystem.
var userConfigDir = defaultUserConfigDir

// defaultUserConfigDir returns os.UserConfigDir()/ap2-pp-cli/user-keys, falling back
// to ~/.ap2-pp-cli/user-keys if UserConfigDir errors (mirrors keysDir pattern).
func defaultUserConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("resolving user config dir: %w (home fallback also failed: %v)", err, herr)
		}
		return filepath.Join(home, ".ap2-pp-cli", "user-keys"), nil
	}
	return filepath.Join(base, "ap2-pp-cli", "user-keys"), nil
}

// SetUserConfigDir overrides the base directory used for user key storage.
// Intended for tests only.
func SetUserConfigDir(dir string) {
	userConfigDir = func() (string, error) { return dir, nil }
}

// ResetUserConfigDir restores the default user config directory resolver.
func ResetUserConfigDir() {
	userConfigDir = defaultUserConfigDir
}

// userKeysDir returns the resolved user-keys directory, creating it if absent.
func userKeysDir() (string, error) {
	// Allow env override for integration testing / smoke tests.
	if env := os.Getenv("AP2_USER_KEYS_DIR"); env != "" {
		if err := os.MkdirAll(env, 0o700); err != nil {
			return "", fmt.Errorf("creating user keys dir %q: %w", env, err)
		}
		return env, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating user keys dir %q: %w", dir, err)
	}
	return dir, nil
}

// UserKey represents a stored user key (private + public).
type UserKey struct {
	UserID     string            // "user-<uuid>"
	PrivateKey *ecdsa.PrivateKey // nil when loaded via LoadUserPublic
	PublicKey  *ecdsa.PublicKey
	Path       string // private key file path
	CreatedAt  time.Time
}

// validateUserID rejects any string that isn't strictly
// ^user-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
// Path-traversal guard, mirrors validateAgentID.
func validateUserID(id string) error {
	if !userIDPattern.MatchString(id) {
		return fmt.Errorf("invalid user id %q: must match ^user-<uuid>$", id)
	}
	return nil
}

// GenerateUserKey creates a new ECDSA-P256 keypair, stores it under the user-keys
// directory as user-<uuid>.{pem,pub}, and returns the resulting UserKey.
// Private file is 0o600; public file is 0o644; directory is 0o700.
// Private encoding: PKCS#8 PEM block. Public encoding: PKIX PEM block.
func GenerateUserKey() (*UserKey, error) {
	dir, err := userKeysDir()
	if err != nil {
		return nil, err
	}

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating ECDSA-P256 user key: %w", err)
	}

	userID := "user-" + uuid.New().String()

	privDER, err := x509.MarshalPKCS8PrivateKey(privKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling user private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	pubDER, err := x509.MarshalPKIXPublicKey(&privKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling user public key: %w", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	privPath := filepath.Join(dir, userID+".pem")
	pubPath := filepath.Join(dir, userID+".pub")

	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		return nil, fmt.Errorf("writing user private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		_ = os.Remove(privPath)
		return nil, fmt.Errorf("writing user public key: %w", err)
	}

	fi, err := os.Stat(privPath)
	var createdAt time.Time
	if err == nil {
		createdAt = fi.ModTime()
	}

	return &UserKey{
		UserID:     userID,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
		Path:       privPath,
		CreatedAt:  createdAt,
	}, nil
}

// LoadUserPrivate reads user-<id>.pem (private key) and returns the parsed UserKey.
// Validates userID before any filepath.Join to prevent path traversal.
func LoadUserPrivate(id string) (*UserKey, error) {
	if err := validateUserID(id); err != nil {
		return nil, err
	}
	dir, err := userKeysDir()
	if err != nil {
		return nil, err
	}

	privPath := filepath.Join(dir, id+".pem")
	privData, err := os.ReadFile(privPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("user key %q not found — run 'ap2-pp-cli user-keys list' to see available keys", id)
		}
		return nil, fmt.Errorf("reading user private key: %w", err)
	}

	block, _ := pem.Decode(privData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in %s", privPath)
	}

	rawKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing user private key: %w", err)
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

	return &UserKey{
		UserID:     id,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
		Path:       privPath,
		CreatedAt:  createdAt,
	}, nil
}

// LoadUserPublic reads user-<id>.pub (public key only) and returns the parsed UserKey.
// PrivateKey will be nil on the returned UserKey.
// Validates userID before any filepath.Join to prevent path traversal.
func LoadUserPublic(id string) (*UserKey, error) {
	if err := validateUserID(id); err != nil {
		return nil, err
	}
	dir, err := userKeysDir()
	if err != nil {
		return nil, err
	}

	pubPath := filepath.Join(dir, id+".pub")
	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("user key %q not found — run 'ap2-pp-cli user-keys list' to see available keys", id)
		}
		return nil, fmt.Errorf("reading user public key: %w", err)
	}

	block, _ := pem.Decode(pubData)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM in %s", pubPath)
	}

	rawKey, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing user public key: %w", err)
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

	privPath := filepath.Join(dir, id+".pem")
	return &UserKey{
		UserID:    id,
		PublicKey: pubKey,
		Path:      privPath, // canonical path is still the .pem
		CreatedAt: createdAt,
	}, nil
}

// ListUserKeys enumerates all user keys present on disk (returns public side only).
// Result is sorted lexicographically by UserID for deterministic output.
func ListUserKeys() ([]*UserKey, error) {
	dir, err := userKeysDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*UserKey{}, nil
		}
		return nil, fmt.Errorf("listing user keys dir: %w", err)
	}

	var keys []*UserKey
	seen := map[string]bool{}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".pub") {
			continue
		}
		userID := strings.TrimSuffix(name, ".pub")
		if seen[userID] {
			continue
		}
		if err := validateUserID(userID); err != nil {
			continue
		}
		seen[userID] = true

		k, err := LoadUserPublic(userID)
		if err != nil {
			continue
		}
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return keys[i].UserID < keys[j].UserID
	})

	if keys == nil {
		keys = []*UserKey{}
	}
	return keys, nil
}

// ExportUserPEM returns the PEM-encoded user public key as a string.
func ExportUserPEM(id string) (string, error) {
	k, err := LoadUserPublic(id)
	if err != nil {
		return "", err
	}
	if k.PublicKey == nil {
		return "", fmt.Errorf("user key %q has no public key loaded", k.UserID)
	}
	der, err := x509.MarshalPKIXPublicKey(k.PublicKey)
	if err != nil {
		return "", fmt.Errorf("marshaling user public key to PKIX: %w", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

// ExportUserJWK returns the JWK as a map (kty:EC, crv:P-256, x/y base64url, kid:UserID).
// Per RFC 7518 §6.2. x and y are 32-byte big-endian, base64url-encoded without padding.
func ExportUserJWK(id string) (map[string]any, error) {
	k, err := LoadUserPublic(id)
	if err != nil {
		return nil, err
	}
	if k.PublicKey == nil {
		return nil, fmt.Errorf("user key %q has no public key loaded", k.UserID)
	}

	xBytes := make([]byte, 32)
	yBytes := make([]byte, 32)
	k.PublicKey.X.FillBytes(xBytes)
	k.PublicKey.Y.FillBytes(yBytes)

	jwk := map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"x":   base64.RawURLEncoding.EncodeToString(xBytes),
		"y":   base64.RawURLEncoding.EncodeToString(yBytes),
		"kid": k.UserID,
	}

	// Round-trip through json.Marshal so callers that want a JSON string can
	// re-marshal cheaply; the map shape itself is the canonical return.
	if _, err := json.Marshal(jwk); err != nil {
		return nil, fmt.Errorf("marshaling user JWK: %w", err)
	}
	return jwk, nil
}

// LoadPublicAny resolves a public key from either keystore.
// Checks agent keys first (agent-<uuid>), then user keys (user-<uuid>).
func LoadPublicAny(subject string) (*ecdsa.PublicKey, error) {
	if strings.HasPrefix(subject, "agent-") {
		k, err := LoadPublic(subject)
		if err != nil {
			return nil, err
		}
		return k.PublicKey, nil
	}
	if strings.HasPrefix(subject, "user-") {
		k, err := LoadUserPublic(subject)
		if err != nil {
			return nil, err
		}
		return k.PublicKey, nil
	}
	return nil, fmt.Errorf("unknown key subject prefix in %q: expected agent- or user-", subject)
}
