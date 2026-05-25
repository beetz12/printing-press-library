// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

// Package paymentmethods implements a local store for payment methods used by
// the ap2-pp-cli payment authorize flow. Tokens are persisted as JSON files
// under ~/.config/ap2-pp-cli/payment-methods/ (or AP2_PM_DIR), one file per
// method, mode 0o600 inside a 0o700 directory.
package paymentmethods

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
)

// pmIDPattern is the strict regex guard against path-traversal attacks.
// Matches ^pm-<UUID>$ where UUID is lowercase hex with standard dashes.
var pmIDPattern = regexp.MustCompile(`^pm-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// pmDir is the function used to resolve the base payment-methods directory.
// Tests override this via SetPMDir to avoid touching the real filesystem.
var pmDir = defaultPMDir

// defaultPMDir returns os.UserConfigDir()/ap2-pp-cli/payment-methods, falling
// back to ~/.ap2-pp-cli/payment-methods if UserConfigDir errors. Mirrors the
// keys package pattern.
func defaultPMDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("resolving config dir: %w (home fallback also failed: %v)", err, herr)
		}
		return filepath.Join(home, ".ap2-pp-cli", "payment-methods"), nil
	}
	return filepath.Join(base, "ap2-pp-cli", "payment-methods"), nil
}

// SetPMDir overrides the base directory used for payment-method storage.
// Intended for tests only.
func SetPMDir(dir string) {
	pmDir = func() (string, error) { return dir, nil }
}

// ResetPMDir restores the default payment-methods directory resolver.
func ResetPMDir() {
	pmDir = defaultPMDir
}

// resolveDir returns the payment-methods directory, creating it if absent.
// AP2_PM_DIR env var, if set, wins over both SetPMDir and the default resolver.
// The returned directory is always chmod'd to 0o700 to guarantee the storage
// contract regardless of the umask in effect when the parent created it (this
// matters in tests where t.TempDir() returns a 0o755 directory).
func resolveDir() (string, error) {
	var dir string
	if env := os.Getenv("AP2_PM_DIR"); env != "" {
		dir = env
	} else {
		d, err := pmDir()
		if err != nil {
			return "", err
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating payment-methods dir %q: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("chmod payment-methods dir %q: %w", dir, err)
	}
	return dir, nil
}

// PaymentMethod is a single stored payment method.
type PaymentMethod struct {
	ID        string    `json:"id"`         // "pm-<uuid>"
	Provider  string    `json:"provider"`   // "google-pay" | "stripe" | "raw"
	Token     string    `json:"token"`      // the actual payment token or Stripe pm_xxx
	Label     string    `json:"label"`      // user-friendly name e.g. "My Visa 4242"
	Default   bool      `json:"default"`    // auto-selected when --payment-method not set
	CreatedAt time.Time `json:"created_at"`
}

// validateID rejects any string that isn't strictly
// ^pm-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$
// This is the path-traversal guard mirroring the keys package.
func validateID(id string) error {
	if !pmIDPattern.MatchString(id) {
		return fmt.Errorf("invalid payment method id %q: must match ^pm-<uuid>$", id)
	}
	return nil
}

// NewID returns a freshly generated pm-<uuid> identifier.
func NewID() string {
	return "pm-" + uuid.New().String()
}

// Add persists pm as <ID>.json at 0o600 under the payment-methods directory.
// Returns an error if pm.ID is missing or malformed.
func Add(pm PaymentMethod) error {
	if err := validateID(pm.ID); err != nil {
		return err
	}
	dir, err := resolveDir()
	if err != nil {
		return err
	}
	if pm.CreatedAt.IsZero() {
		pm.CreatedAt = time.Now().UTC()
	}
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling payment method: %w", err)
	}
	path := filepath.Join(dir, pm.ID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing payment method: %w", err)
	}
	return nil
}

// List returns every stored payment method, sorted by CreatedAt descending.
func List() ([]*PaymentMethod, error) {
	dir, err := resolveDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*PaymentMethod{}, nil
		}
		return nil, fmt.Errorf("listing payment-methods dir: %w", err)
	}
	out := make([]*PaymentMethod, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		id := name[:len(name)-len(".json")]
		if err := validateID(id); err != nil {
			// Skip files that don't match the pm-<uuid> pattern.
			continue
		}
		pm, err := Get(id)
		if err != nil {
			// Skip unreadable entries rather than aborting the whole list.
			continue
		}
		out = append(out, pm)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Get loads a payment method by ID. Validates ID before any filepath.Join to
// prevent path traversal.
func Get(id string) (*PaymentMethod, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	dir, err := resolveDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("payment method %q not found — run 'ap2-pp-cli payment-method list' to see available methods", id)
		}
		return nil, fmt.Errorf("reading payment method: %w", err)
	}
	var pm PaymentMethod
	if err := json.Unmarshal(data, &pm); err != nil {
		return nil, fmt.Errorf("parsing payment method %q: %w", id, err)
	}
	return &pm, nil
}

// Remove deletes the payment method file. Validates ID first.
func Remove(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	dir, err := resolveDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, id+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("payment method %q not found", id)
		}
		return fmt.Errorf("removing payment method: %w", err)
	}
	return nil
}

// GetDefault returns the user-selected default payment method. If no method is
// flagged default but exactly one is stored, that single method is returned as
// an implicit default. Returns a clear error when no methods are stored.
func GetDefault() (*PaymentMethod, error) {
	pms, err := List()
	if err != nil {
		return nil, err
	}
	if len(pms) == 0 {
		return nil, fmt.Errorf("no payment methods stored — run 'ap2 payment-method add'")
	}
	for _, pm := range pms {
		if pm.Default {
			return pm, nil
		}
	}
	if len(pms) == 1 {
		return pms[0], nil
	}
	return nil, fmt.Errorf("no default payment method set — run 'ap2 payment-method set-default --id <pm-uuid>'")
}

// SetDefault marks one payment method as default and clears the flag on every
// other stored method. Returns an error if id is missing or malformed.
func SetDefault(id string) error {
	if err := validateID(id); err != nil {
		return err
	}
	target, err := Get(id)
	if err != nil {
		return err
	}
	pms, err := List()
	if err != nil {
		return err
	}
	// Clear default on everything else.
	for _, pm := range pms {
		if pm.ID == id {
			continue
		}
		if pm.Default {
			pm.Default = false
			if err := Add(*pm); err != nil {
				return fmt.Errorf("clearing default on %q: %w", pm.ID, err)
			}
		}
	}
	target.Default = true
	return Add(*target)
}
