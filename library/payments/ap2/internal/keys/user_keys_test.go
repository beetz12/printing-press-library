// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package keys

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

// withTempUserDir overrides the user-keys directory with a temp dir for the duration of the test.
func withTempUserDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AP2_USER_KEYS_DIR", dir)
	SetUserConfigDir(dir)
	t.Cleanup(ResetUserConfigDir)
	return dir
}

func TestGenerateUserKey_FilePerms(t *testing.T) {
	// Use a non-existent subdir so MkdirAll creates it with the requested
	// 0o700 perms — t.TempDir() pre-creates with 0o755, which would mask the check.
	base := t.TempDir()
	dir := filepath.Join(base, "user-keys")
	t.Setenv("AP2_USER_KEYS_DIR", dir)
	SetUserConfigDir(dir)
	t.Cleanup(ResetUserConfigDir)

	k, err := GenerateUserKey()
	if err != nil {
		t.Fatalf("GenerateUserKey: %v", err)
	}

	// Directory: 0o700.
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("user keys dir perms: want 0700, got %#o", perm)
	}

	// Private key: 0o600.
	privInfo, err := os.Stat(k.Path)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if perm := privInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("private key perms: want 0600, got %#o", perm)
	}

	// Public key: 0o644.
	pubPath := filepath.Join(dir, k.UserID+".pub")
	pubInfo, err := os.Stat(pubPath)
	if err != nil {
		t.Fatalf("stat public key: %v", err)
	}
	if perm := pubInfo.Mode().Perm(); perm != 0o644 {
		t.Errorf("public key perms: want 0644, got %#o", perm)
	}
}

func TestLoadUserPublic_PathTraversal(t *testing.T) {
	withTempUserDir(t)

	invalid := []string{
		"../../etc/passwd",
		"user-../escape",
		"user-",
		"",
		"USER-12345678-1234-1234-1234-123456789012",       // uppercase
		"user-12345678-1234-1234-1234-12345678901",        // too short
		"user-12345678-1234-1234-1234-1234567890123",      // too long
		"user-12345678-1234-1234-1234-12345678901g",       // non-hex
		"agent-12345678-1234-1234-1234-123456789012",      // agent prefix, not user
		"12345678-1234-1234-1234-123456789012",            // missing prefix
	}
	for _, id := range invalid {
		t.Run("invalid/"+id, func(t *testing.T) {
			if _, err := LoadUserPublic(id); err == nil {
				t.Errorf("LoadUserPublic(%q) should have returned error, got nil", id)
			}
			if _, err := LoadUserPrivate(id); err == nil {
				t.Errorf("LoadUserPrivate(%q) should have returned error, got nil", id)
			}
		})
	}
}

func TestGenerateUserKey_Roundtrip(t *testing.T) {
	withTempUserDir(t)

	k, err := GenerateUserKey()
	if err != nil {
		t.Fatalf("GenerateUserKey: %v", err)
	}
	if k.UserID == "" {
		t.Fatal("UserID is empty")
	}
	if err := validateUserID(k.UserID); err != nil {
		t.Fatalf("generated UserID failed validation: %v", err)
	}
	if k.PrivateKey == nil {
		t.Fatal("PrivateKey is nil after GenerateUserKey")
	}
	if k.PublicKey == nil {
		t.Fatal("PublicKey is nil after GenerateUserKey")
	}

	// LoadUserPrivate roundtrip.
	loadedPriv, err := LoadUserPrivate(k.UserID)
	if err != nil {
		t.Fatalf("LoadUserPrivate: %v", err)
	}
	if loadedPriv.PrivateKey == nil {
		t.Fatal("LoadUserPrivate returned nil PrivateKey")
	}

	origPub, err := x509.MarshalPKIXPublicKey(k.PublicKey)
	if err != nil {
		t.Fatalf("marshal orig pub: %v", err)
	}
	loadedPub, err := x509.MarshalPKIXPublicKey(loadedPriv.PublicKey)
	if err != nil {
		t.Fatalf("marshal loaded pub: %v", err)
	}
	if string(origPub) != string(loadedPub) {
		t.Fatal("public keys differ between GenerateUserKey and LoadUserPrivate")
	}

	// LoadUserPublic roundtrip.
	loadedPubKey, err := LoadUserPublic(k.UserID)
	if err != nil {
		t.Fatalf("LoadUserPublic: %v", err)
	}
	if loadedPubKey.PrivateKey != nil {
		t.Error("LoadUserPublic should return nil PrivateKey")
	}
	loadedPub2, err := x509.MarshalPKIXPublicKey(loadedPubKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal loaded pub2: %v", err)
	}
	if string(origPub) != string(loadedPub2) {
		t.Fatal("LoadUserPublic returned different public key")
	}

	// ListUserKeys finds the new key.
	all, err := ListUserKeys()
	if err != nil {
		t.Fatalf("ListUserKeys: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("ListUserKeys: want 1 key, got %d", len(all))
	}
	if all[0].UserID != k.UserID {
		t.Errorf("ListUserKeys returned id %q, want %q", all[0].UserID, k.UserID)
	}
}

func TestLoadPublicAny_AgentKey(t *testing.T) {
	// Use SetConfigDir (agent keystore) and SetUserConfigDir (user keystore) on
	// disjoint temp dirs so the resolver demonstrably routes by prefix.
	agentDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("AP2_KEYS_DIR", agentDir)
	t.Setenv("AP2_USER_KEYS_DIR", userDir)
	SetConfigDir(agentDir)
	SetUserConfigDir(userDir)
	t.Cleanup(ResetConfigDir)
	t.Cleanup(ResetUserConfigDir)

	agentKey, err := Generate()
	if err != nil {
		t.Fatalf("Generate (agent): %v", err)
	}

	pub, err := LoadPublicAny(agentKey.AgentID)
	if err != nil {
		t.Fatalf("LoadPublicAny(agent): %v", err)
	}
	origPub, _ := x509.MarshalPKIXPublicKey(agentKey.PublicKey)
	gotPub, _ := x509.MarshalPKIXPublicKey(pub)
	if string(origPub) != string(gotPub) {
		t.Error("LoadPublicAny returned a different agent public key")
	}
}

func TestLoadPublicAny_UserKey(t *testing.T) {
	agentDir := t.TempDir()
	userDir := t.TempDir()
	t.Setenv("AP2_KEYS_DIR", agentDir)
	t.Setenv("AP2_USER_KEYS_DIR", userDir)
	SetConfigDir(agentDir)
	SetUserConfigDir(userDir)
	t.Cleanup(ResetConfigDir)
	t.Cleanup(ResetUserConfigDir)

	userKey, err := GenerateUserKey()
	if err != nil {
		t.Fatalf("GenerateUserKey: %v", err)
	}

	pub, err := LoadPublicAny(userKey.UserID)
	if err != nil {
		t.Fatalf("LoadPublicAny(user): %v", err)
	}
	origPub, _ := x509.MarshalPKIXPublicKey(userKey.PublicKey)
	gotPub, _ := x509.MarshalPKIXPublicKey(pub)
	if string(origPub) != string(gotPub) {
		t.Error("LoadPublicAny returned a different user public key")
	}

	// Unknown prefix → error.
	if _, err := LoadPublicAny("foo-12345678-1234-1234-1234-123456789012"); err == nil {
		t.Error("LoadPublicAny with unknown prefix should error")
	}
}
