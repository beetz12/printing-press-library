package transport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// txnIDPattern accepts sandbox-<uuid> or live-<uuid> prefixes. These are the
// only prefixes CompleteCheckout generates; older drafts included agent- but
// no code path produces it, so the pattern stays minimal to avoid signaling
// that agent-<uuid> is a valid txn id.
var txnIDPattern = regexp.MustCompile(`^(sandbox|live)-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// txnDir is the function used to resolve the transactions directory.
// Tests override this via SetTxnDir to use a tmpdir without touching real state.
var txnDir = defaultTxnDir

func defaultTxnDir() (string, error) {
	// Allow env override for integration testing / smoke tests.
	if env := os.Getenv("AP2_TXN_DIR"); env != "" {
		if err := os.MkdirAll(env, 0o700); err != nil {
			return "", fmt.Errorf("creating txn dir %q: %w", env, err)
		}
		return env, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("resolving config dir: %w (home fallback also failed: %v)", err, herr)
		}
		return filepath.Join(home, ".ap2-pp-cli", "transactions"), nil
	}
	return filepath.Join(base, "ap2-pp-cli", "transactions"), nil
}

// transactionsDir returns the resolved transactions directory, creating it if absent.
func transactionsDir() (string, error) {
	dir, err := txnDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating transactions dir %q: %w", dir, err)
	}
	return dir, nil
}

// validateTxnID rejects anything that isn't a safe transaction ID.
// Accepted: sandbox-<uuid>, live-<uuid>, agent-<uuid>.
// Rejects path traversal attempts like "../etc/passwd" or "sandbox-../escape".
func validateTxnID(id string) error {
	if !txnIDPattern.MatchString(id) {
		return fmt.Errorf("invalid transaction id %q: must match ^(sandbox|live|agent)-<uuid>$", id)
	}
	return nil
}

// SetTxnDir overrides the base directory used for transaction storage.
// Intended for tests only.
func SetTxnDir(dir string) {
	txnDir = func() (string, error) { return dir, nil }
}

// ResetTxnDir restores the default transaction directory resolver.
func ResetTxnDir() {
	txnDir = defaultTxnDir
}

// SaveTransaction persists a CompleteResult as <txnID>.json with 0o600 perms.
func SaveTransaction(result *CompleteResult) error {
	if err := validateTxnID(result.TransactionID); err != nil {
		return err
	}
	dir, err := transactionsDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling transaction %s: %w", result.TransactionID, err)
	}

	path := filepath.Join(dir, result.TransactionID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing transaction %s: %w", result.TransactionID, err)
	}
	return nil
}

// LoadTransaction reads and parses a single transaction by ID.
func LoadTransaction(txnID string) (*CompleteResult, error) {
	if err := validateTxnID(txnID); err != nil {
		return nil, err
	}
	dir, err := transactionsDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, txnID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("transaction %q not found", txnID)
		}
		return nil, fmt.Errorf("reading transaction %s: %w", txnID, err)
	}

	var result CompleteResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parsing transaction %s: %w", txnID, err)
	}
	return &result, nil
}

// ListTransactions returns all saved transactions sorted by CreatedAt descending.
func ListTransactions() ([]*CompleteResult, error) {
	dir, err := transactionsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*CompleteResult{}, nil
		}
		return nil, fmt.Errorf("listing transactions dir: %w", err)
	}

	var results []*CompleteResult
	for _, e := range entries {
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		txnID := name[:len(name)-len(".json")]
		if err := validateTxnID(txnID); err != nil {
			// Skip files that don't match the transaction ID pattern.
			continue
		}
		r, err := LoadTransaction(txnID)
		if err != nil {
			// Skip unreadable/corrupt entries rather than aborting the whole list.
			continue
		}
		results = append(results, r)
	}

	// Sort by CreatedAt descending (newest first).
	sort.Slice(results, func(i, j int) bool {
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	if results == nil {
		results = []*CompleteResult{}
	}
	return results, nil
}
