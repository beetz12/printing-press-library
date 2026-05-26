// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"ap2-pp-cli/internal/keys"
	"ap2-pp-cli/internal/paymentmethods"
)

// isolateKeyDirs points all three key/payment stores at fresh per-test temp
// directories so the wizard's filesystem writes don't leak into the real user
// config or across tests.
func isolateKeyDirs(t *testing.T) {
	t.Helper()
	t.Setenv("AP2_KEYS_DIR", t.TempDir())
	t.Setenv("AP2_USER_KEYS_DIR", t.TempDir())
	t.Setenv("AP2_PM_DIR", t.TempDir())
}

func TestIsFirstRun_NoKeys(t *testing.T) {
	isolateKeyDirs(t)
	if !isFirstRun() {
		t.Errorf("isFirstRun() = false, want true when no keys exist")
	}
}

func TestIsFirstRun_HasAgentKey(t *testing.T) {
	isolateKeyDirs(t)
	if _, err := keys.Generate(); err != nil {
		t.Fatalf("generating agent key: %v", err)
	}
	if isFirstRun() {
		t.Errorf("isFirstRun() = true, want false after agent key generated")
	}
}

func TestIsFirstRun_HasUserKey(t *testing.T) {
	isolateKeyDirs(t)
	if _, err := keys.GenerateUserKey(); err != nil {
		t.Fatalf("generating user key: %v", err)
	}
	if isFirstRun() {
		t.Errorf("isFirstRun() = true, want false after user key generated")
	}
}

func TestShouldRunSetup_NoInput(t *testing.T) {
	isolateKeyDirs(t)
	flags := &rootFlags{noInput: true}
	cmd := &cobra.Command{Use: "intent"}
	if shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup with noInput=true should return false")
	}
}

func TestShouldRunSetup_Agent(t *testing.T) {
	isolateKeyDirs(t)
	flags := &rootFlags{agent: true}
	cmd := &cobra.Command{Use: "intent"}
	if shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup with agent=true should return false")
	}
}

func TestShouldRunSetup_EnvSkip(t *testing.T) {
	isolateKeyDirs(t)
	t.Setenv("AP2_SKIP_SETUP", "1")
	flags := &rootFlags{}
	cmd := &cobra.Command{Use: "intent"}
	if shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup with AP2_SKIP_SETUP=1 should return false")
	}
}

func TestShouldRunSetup_SetupCommand(t *testing.T) {
	isolateKeyDirs(t)
	flags := &rootFlags{}
	cmd := &cobra.Command{Use: "setup"}
	if shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup for the 'setup' command itself should return false")
	}
}

func TestShouldRunSetup_SkipCommands(t *testing.T) {
	isolateKeyDirs(t)
	flags := &rootFlags{}
	for _, name := range []string{"setup", "version", "which", "__complete", "help", "doctor", "completion"} {
		cmd := &cobra.Command{Use: name}
		if shouldRunSetup(cmd, flags) {
			t.Errorf("shouldRunSetup for %q should return false", name)
		}
	}
}

func TestShouldRunSetup_FirstRunNonSkipCommand(t *testing.T) {
	isolateKeyDirs(t)
	flags := &rootFlags{}
	cmd := &cobra.Command{Use: "intent"}
	if !shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup on first run for non-skip command should return true")
	}
}

func TestShouldRunSetup_NotFirstRun(t *testing.T) {
	isolateKeyDirs(t)
	if _, err := keys.Generate(); err != nil {
		t.Fatalf("generating agent key: %v", err)
	}
	flags := &rootFlags{}
	cmd := &cobra.Command{Use: "intent"}
	if shouldRunSetup(cmd, flags) {
		t.Errorf("shouldRunSetup when keys already exist should return false")
	}
}

func TestRunFirstRunSetup_SkipsPayment(t *testing.T) {
	isolateKeyDirs(t)
	cmd := &cobra.Command{Use: "setup"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("s\n"))

	if err := runFirstRunSetup(cmd); err != nil {
		t.Fatalf("runFirstRunSetup returned error: %v", err)
	}

	agentKeyList, err := keys.List()
	if err != nil {
		t.Fatalf("listing agent keys: %v", err)
	}
	if len(agentKeyList) != 1 {
		t.Errorf("expected 1 agent key, got %d", len(agentKeyList))
	}

	userKeyList, err := keys.ListUserKeys()
	if err != nil {
		t.Fatalf("listing user keys: %v", err)
	}
	if len(userKeyList) != 1 {
		t.Errorf("expected 1 user key, got %d", len(userKeyList))
	}

	pmList, err := paymentmethods.List()
	if err != nil {
		t.Fatalf("listing payment methods: %v", err)
	}
	if len(pmList) != 0 {
		t.Errorf("expected 0 payment methods after skip, got %d", len(pmList))
	}

	output := out.String()
	if !strings.Contains(output, "Setup complete!") {
		t.Errorf("expected 'Setup complete!' in output, got: %s", output)
	}
	if !strings.Contains(output, "Skipped.") {
		t.Errorf("expected 'Skipped.' in output (skip branch), got: %s", output)
	}
}

func TestRunFirstRunSetup_StoresPaymentMethod(t *testing.T) {
	isolateKeyDirs(t)
	cmd := &cobra.Command{Use: "setup"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("a\npm_test_token_12345\n"))

	if err := runFirstRunSetup(cmd); err != nil {
		t.Fatalf("runFirstRunSetup returned error: %v", err)
	}

	pmList, err := paymentmethods.List()
	if err != nil {
		t.Fatalf("listing payment methods: %v", err)
	}
	if len(pmList) != 1 {
		t.Fatalf("expected 1 payment method, got %d", len(pmList))
	}
	if pmList[0].Provider != "google-pay" {
		t.Errorf("expected provider google-pay, got %s", pmList[0].Provider)
	}
	if pmList[0].Token != "pm_test_token_12345" {
		t.Errorf("expected token pm_test_token_12345, got %s", pmList[0].Token)
	}
	if !pmList[0].Default {
		t.Errorf("payment method should be marked default")
	}
}
