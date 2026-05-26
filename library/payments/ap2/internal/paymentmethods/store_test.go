// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package paymentmethods

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempDir isolates each test to its own AP2_PM_DIR.
func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AP2_PM_DIR", dir)
	return dir
}

func TestAddGet_Roundtrip(t *testing.T) {
	withTempDir(t)

	pm := PaymentMethod{
		ID:       NewID(),
		Provider: "stripe",
		Token:    "pm_test_4242",
		Label:    "My Test Card",
	}
	if err := Add(pm); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := Get(pm.ID)
	if err != nil {
		t.Fatalf("Get(%q): %v", pm.ID, err)
	}
	if got.ID != pm.ID || got.Provider != pm.Provider || got.Token != pm.Token || got.Label != pm.Label {
		t.Fatalf("roundtrip mismatch: got %+v want %+v", got, pm)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt was not populated on Add")
	}
}

func TestFilePerms_0600(t *testing.T) {
	dir := withTempDir(t)

	pm := PaymentMethod{ID: NewID(), Provider: "raw", Token: "tok-xxx", Label: "raw"}
	if err := Add(pm); err != nil {
		t.Fatalf("Add: %v", err)
	}

	path := filepath.Join(dir, pm.ID+".json")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perms = %o, want 0600", perm)
	}

	dirFI, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat %s: %v", dir, err)
	}
	if perm := dirFI.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perms = %o, want 0700", perm)
	}
}

func TestGetDefault_SingleFallback(t *testing.T) {
	withTempDir(t)

	pm := PaymentMethod{ID: NewID(), Provider: "google-pay", Token: "tok-1", Label: "only"}
	if err := Add(pm); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := GetDefault()
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if got.ID != pm.ID {
		t.Fatalf("GetDefault returned %q, want %q", got.ID, pm.ID)
	}
}

func TestGetDefault_NoneStored(t *testing.T) {
	withTempDir(t)

	_, err := GetDefault()
	if err == nil {
		t.Fatal("expected error when no payment methods stored")
	}
	if !strings.Contains(err.Error(), "no payment methods stored") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRemove(t *testing.T) {
	withTempDir(t)

	pm := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_x", Label: "x"}
	if err := Add(pm); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := Remove(pm.ID); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Get(pm.ID); err == nil {
		t.Fatal("expected Get to fail after Remove")
	}
}

func TestValidateID_PathTraversal(t *testing.T) {
	invalid := []string{
		"../etc",
		"../../etc/passwd",
		"pm-../escape",
		"pm-not-a-uuid",
		"agent-12345678-1234-1234-1234-123456789012",
		"pm-",
		"",
	}
	for _, id := range invalid {
		t.Run(id, func(t *testing.T) {
			if err := validateID(id); err == nil {
				t.Errorf("validateID(%q) unexpectedly accepted", id)
			}
		})
	}

	valid := []string{
		"pm-12345678-1234-1234-1234-123456789012",
		"pm-00000000-0000-0000-0000-000000000000",
		"pm-ffffffff-ffff-ffff-ffff-ffffffffffff",
	}
	for _, id := range valid {
		t.Run("valid/"+id, func(t *testing.T) {
			if err := validateID(id); err != nil {
				t.Errorf("validateID(%q) returned unexpected error: %v", id, err)
			}
		})
	}

	// Confirm Get also rejects without ever touching the filesystem.
	withTempDir(t)
	if _, err := Get("../../etc/passwd"); err == nil {
		t.Fatal("Get accepted a path-traversal ID")
	}
}

func TestSetDefault(t *testing.T) {
	withTempDir(t)

	pm1 := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_a", Label: "a", Default: true, CreatedAt: time.Now().Add(-2 * time.Hour)}
	pm2 := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_b", Label: "b", CreatedAt: time.Now().Add(-1 * time.Hour)}
	if err := Add(pm1); err != nil {
		t.Fatalf("Add pm1: %v", err)
	}
	if err := Add(pm2); err != nil {
		t.Fatalf("Add pm2: %v", err)
	}

	if err := SetDefault(pm2.ID); err != nil {
		t.Fatalf("SetDefault(pm2): %v", err)
	}

	got, err := GetDefault()
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if got.ID != pm2.ID {
		t.Fatalf("default = %q, want %q", got.ID, pm2.ID)
	}

	// pm1.Default must have been cleared.
	loaded1, err := Get(pm1.ID)
	if err != nil {
		t.Fatalf("Get(pm1): %v", err)
	}
	if loaded1.Default {
		t.Fatal("pm1.Default was not cleared by SetDefault(pm2)")
	}
}

func TestList_SortedDesc(t *testing.T) {
	withTempDir(t)

	pm1 := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_a", Label: "a", CreatedAt: time.Now().Add(-3 * time.Hour)}
	pm2 := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_b", Label: "b", CreatedAt: time.Now().Add(-1 * time.Hour)}
	pm3 := PaymentMethod{ID: NewID(), Provider: "stripe", Token: "pm_c", Label: "c", CreatedAt: time.Now().Add(-2 * time.Hour)}
	for _, pm := range []PaymentMethod{pm1, pm2, pm3} {
		if err := Add(pm); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	pms, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pms) != 3 {
		t.Fatalf("len=%d, want 3", len(pms))
	}
	if pms[0].ID != pm2.ID || pms[1].ID != pm3.ID || pms[2].ID != pm1.ID {
		t.Fatalf("order wrong: %s,%s,%s", pms[0].ID, pms[1].ID, pms[2].ID)
	}
}
