// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/paymentmethods"
)

// isFirstRun returns true if neither agent keys nor user keys exist yet.
// This is the natural marker — once setup completes, keys exist and the
// wizard won't re-trigger.
func isFirstRun() bool {
	agentKeys, err := keys.List()
	if err == nil && len(agentKeys) > 0 {
		return false
	}
	userKeys, err := keys.ListUserKeys()
	if err == nil && len(userKeys) > 0 {
		return false
	}
	return true
}

// shouldRunSetup returns true if the first-run wizard should be shown for this
// command. Skip conditions:
//   - not a first run (keys already exist)
//   - command is in the skip list (setup, version, which, help, __complete, doctor)
//   - noInput or agent mode (non-interactive)
//   - AP2_SKIP_SETUP=1 env var
func shouldRunSetup(cmd *cobra.Command, flags *rootFlags) bool {
	if !isFirstRun() {
		return false
	}
	if flags.noInput || flags.agent {
		return false
	}
	if os.Getenv("AP2_SKIP_SETUP") == "1" {
		return false
	}
	name := cmd.Name()
	for _, skip := range []string{"setup", "version", "which", "__complete", "help", "doctor", "completion"} {
		if name == skip {
			return false
		}
	}
	if cmd.Parent() != nil {
		parentName := cmd.Parent().Name()
		for _, skip := range []string{"setup", "__complete", "completion"} {
			if parentName == skip {
				return false
			}
		}
	}
	return true
}

// runFirstRunSetup runs the interactive first-run setup wizard. It generates an
// agent key, a user key, and optionally stores a payment method. On completion,
// prints a summary and returns nil so the original command continues.
func runFirstRunSetup(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "╔══════════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "║  Welcome to ap2-pp-cli!  First-time setup (< 30s)   ║")
	fmt.Fprintln(w, "╚══════════════════════════════════════════════════════╝")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "ap2-pp-cli lets your AI agent make purchases on your behalf")
	fmt.Fprintln(w, "using the AP2 (Agent Payments Protocol). Two keys are needed:")
	fmt.Fprintln(w, "  • Agent key  — signs the cart and payment mandates (agent identity)")
	fmt.Fprintln(w, "  • User key   — signs the intent mandate  (proves YOU authorized the agent)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Both are ECDSA-P256 keys stored locally at 0o600. No data leaves your machine.")
	fmt.Fprintln(w, "")

	// Step 1: generate agent key
	fmt.Fprintf(w, "Step 1/3  Generating agent key... ")
	agentKey, err := keys.Generate()
	if err != nil {
		return fmt.Errorf("generating agent key: %w", err)
	}
	fmt.Fprintf(w, "✓  %s\n", agentKey.AgentID)

	// Step 2: generate user key
	fmt.Fprintf(w, "Step 2/3  Generating user key...  ")
	userKey, err := keys.GenerateUserKey()
	if err != nil {
		return fmt.Errorf("generating user key: %w", err)
	}
	fmt.Fprintf(w, "✓  %s\n", userKey.UserID)

	// Step 3: payment method (optional)
	fmt.Fprintln(w, "Step 3/3  Payment method setup")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  To make real purchases your agent needs a payment token.")
	fmt.Fprintln(w, "  Options:")
	fmt.Fprintln(w, "    a) Enter a Google Pay token     (from your Google Pay account)")
	fmt.Fprintln(w, "    b) Enter a Stripe payment method ID  (pm_xxx from Stripe dashboard)")
	fmt.Fprintln(w, "    s) Skip for now (sandbox/probe mode still works)")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "  Choice [a/b/s]: ")

	reader := bufio.NewReader(cmd.InOrStdin())
	choice, _ := reader.ReadString('\n')
	choice = strings.ToLower(strings.TrimSpace(choice))

	switch choice {
	case "a", "b":
		provider := "google-pay"
		if choice == "b" {
			provider = "stripe"
		}
		fmt.Fprintf(w, "  Token: ")
		token, _ := reader.ReadString('\n')
		token = strings.TrimSpace(token)
		if token != "" {
			pm := paymentmethods.PaymentMethod{
				ID:        paymentmethods.NewID(),
				Provider:  provider,
				Token:     token,
				Label:     "Default (" + provider + ")",
				Default:   true,
				CreatedAt: time.Now(),
			}
			if aerr := paymentmethods.Add(pm); aerr != nil {
				fmt.Fprintf(w, "  warning: could not store payment method: %v\n", aerr)
			} else {
				fmt.Fprintf(w, "  ✓ Stored as %s (default)\n", pm.ID)
			}
		}
	default:
		fmt.Fprintln(w, "  Skipped. Add one later: ap2 payment-method add --token <t> --provider google-pay")
	}

	// Summary
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────")
	fmt.Fprintln(w, "Setup complete! Your configuration:")
	fmt.Fprintf(w, "  Agent key:  %s\n", agentKey.AgentID)
	fmt.Fprintf(w, "  User key:   %s\n", userKey.UserID)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Quick start:")
	fmt.Fprintln(w, "  # Authorize what your agent can buy (user-side):")
	fmt.Fprintln(w, "  ap2 intent grant --description 'Buy a book under $30' --max-cents 3000 --hours 24 > intent.json")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  # Test your setup (sandbox — no real money):")
	fmt.Fprintln(w, "  ap2 payment authorize --envelope signed.json")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "  Re-run setup any time: ap2 setup")
	fmt.Fprintln(w, "─────────────────────────────────────────────────────────")
	fmt.Fprintln(w, "")
	return nil
}

// newSetupCmd returns the explicit `ap2 setup` command. Unlike the auto-wizard,
// this always runs regardless of whether keys exist when --force is supplied.
func newSetupCmd(flags *rootFlags) *cobra.Command {
	_ = flags
	var force bool
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Interactive first-time setup wizard (generates keys, stores payment method)",
		Long: `setup walks you through generating your agent key, user key, and optionally
storing a payment method token. Safe to re-run — existing keys are not overwritten.

Use --force to generate a brand-new set of keys even if keys already exist.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force && !isFirstRun() {
				agentKeyList, _ := keys.List()
				userKeyList, _ := keys.ListUserKeys()
				pmList, _ := paymentmethods.List()
				w := cmd.OutOrStdout()
				fmt.Fprintln(w, "ap2-pp-cli is already set up:")
				fmt.Fprintf(w, "  Agent keys:    %d key(s) in ~/.config/ap2-pp-cli/keys/\n", len(agentKeyList))
				fmt.Fprintf(w, "  User keys:     %d key(s) in ~/.config/ap2-pp-cli/user-keys/\n", len(userKeyList))
				fmt.Fprintf(w, "  Payment methods: %d stored\n", len(pmList))
				fmt.Fprintln(w, "")
				fmt.Fprintln(w, "Run 'ap2 setup --force' to re-run the wizard and generate new keys.")
				fmt.Fprintln(w, "Run 'ap2 payment-method add' to add a payment method.")
				fmt.Fprintln(w, "Run 'ap2 doctor' for a full health check.")
				return nil
			}
			return runFirstRunSetup(cmd)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Re-run the wizard even if keys already exist")
	return cmd
}
