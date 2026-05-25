package transport

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AP2_TXN_DIR", dir)

	want := &CompleteResult{
		Status:        "sandbox_authorized",
		TransactionID: "sandbox-12345678-1234-1234-1234-123456789abc",
		Merchant:      "bark.co",
		AmountCents:   799,
		Currency:      "USD",
		Mode:          "sandbox",
		WouldPostTo:   "https://bark.co/api/ucp/mcp",
		Request:       map[string]any{"jsonrpc": "2.0"},
		CreatedAt:     time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
	}

	if err := SaveTransaction(want); err != nil {
		t.Fatalf("SaveTransaction: %v", err)
	}

	got, err := LoadTransaction(want.TransactionID)
	if err != nil {
		t.Fatalf("LoadTransaction: %v", err)
	}

	// Compare via JSON marshal to handle time.Time normalization.
	wantJSON, _ := json.Marshal(want)
	gotJSON, _ := json.Marshal(got)
	if string(wantJSON) != string(gotJSON) {
		t.Errorf("round-trip mismatch:\n want: %s\n  got: %s", wantJSON, gotJSON)
	}
}

func TestSaveTransaction_FilePerms(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AP2_TXN_DIR", dir)

	result := &CompleteResult{
		TransactionID: "sandbox-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Mode:          "sandbox",
		CreatedAt:     time.Now(),
	}
	if err := SaveTransaction(result); err != nil {
		t.Fatalf("SaveTransaction: %v", err)
	}

	// File must be 0o600 (owner read/write only).
	info, err := os.Stat(dir + "/" + result.TransactionID + ".json")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("file perms = %o, want 0o600", mode)
	}
}

func TestValidateTxnID_PathTraversal(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id      string
		wantErr bool
	}{
		// Valid IDs.
		{"sandbox-12345678-1234-1234-1234-123456789abc", false},
		{"live-12345678-1234-1234-1234-123456789abc", false},
		{"agent-12345678-1234-1234-1234-123456789abc", false},
		// Path traversal attempts.
		{"../etc/passwd", true},
		{"sandbox-../escape", true},
		{"not-a-prefix-12345678-1234-1234-1234-123456789abc", true},
		// Wrong format.
		{"sandbox-notauuid", true},
		{"", true},
		{"sandbox-12345678-1234-1234-1234-123456789ABCD", true}, // uppercase hex
		{"sandbox-12345678-1234-1234-1234-123456789ab", true},   // too short
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			err := validateTxnID(tc.id)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateTxnID(%q) error=%v, wantErr=%v", tc.id, err, tc.wantErr)
			}
		})
	}
}

func TestList_DeterministicOrder(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AP2_TXN_DIR", dir)

	// Save 3 transactions with different CreatedAt values.
	txns := []*CompleteResult{
		{
			TransactionID: "sandbox-aaaaaaaa-0001-0001-0001-aaaaaaaaaaaa",
			Mode:          "sandbox",
			CreatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			TransactionID: "sandbox-bbbbbbbb-0002-0002-0002-bbbbbbbbbbbb",
			Mode:          "sandbox",
			CreatedAt:     time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			TransactionID: "sandbox-cccccccc-0003-0003-0003-cccccccccccc",
			Mode:          "sandbox",
			CreatedAt:     time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, txn := range txns {
		if err := SaveTransaction(txn); err != nil {
			t.Fatalf("SaveTransaction(%s): %v", txn.TransactionID, err)
		}
	}

	list, err := ListTransactions()
	if err != nil {
		t.Fatalf("ListTransactions: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 transactions, got %d", len(list))
	}

	// Expect descending order: Jan 3 > Jan 2 > Jan 1.
	want := []string{
		"sandbox-bbbbbbbb-0002-0002-0002-bbbbbbbbbbbb",
		"sandbox-cccccccc-0003-0003-0003-cccccccccccc",
		"sandbox-aaaaaaaa-0001-0001-0001-aaaaaaaaaaaa",
	}
	for i, r := range list {
		if r.TransactionID != want[i] {
			t.Errorf("list[%d] = %s, want %s", i, r.TransactionID, want[i])
		}
	}
}

// failingTransport is an http.RoundTripper that fails the test if called.
// Used by sandbox tests to assert no network call is made.
type failingTransport struct{ t *testing.T }

func (f failingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	f.t.Fatal("sandbox mode made a network call — this must not happen")
	return nil, nil
}
