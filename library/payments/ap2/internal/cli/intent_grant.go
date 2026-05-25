// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
)

// newIntentCmd returns the parent "intent" command.
// Registers newIntentGrantCmd (v0.2: user signs an IntentMandate).
func newIntentCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "intent",
		Short: "Build and sign AP2 IntentMandates with a user key",
		Long: `intent — tools for the user-authority half of the AP2 trust chain.

In v0.2, the user signs an IntentMandate to authorize an agent. The agent then
signs the CartMandate and PaymentMandate. Merchants verify the chain end-to-end.

Subcommands:
  grant   Build and sign a new IntentMandate with a user key`,
		RunE: parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newIntentGrantCmd(flags))
	return cmd
}

// newIntentGrantCmd: ap2 intent grant --description "..." --max-cents N --currency USD --merchant <m> --hours H [--key-id user-<uuid>] [--output <path>]
func newIntentGrantCmd(flags *rootFlags) *cobra.Command {
	var description string
	var maxCents int
	var currency string
	var merchant string
	var hours int
	var keyID string
	var output string

	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Sign an IntentMandate with a user key (grants authority to an agent)",
		Long: `grant builds an AP2 IntentMandate that describes what purchase a user has
authorized, then signs it with a user ECDSA-P256 key. The signed mandate is the
"user authority" half of the AP2 trust chain — it pairs with an agent-signed
CartMandate + PaymentMandate to give merchants a verifiable user → agent chain.

Key selection:
  - --key-id <user-id>   use this specific user key
  - (no flag)            auto-select if exactly one user key exists; error otherwise

Output:
  - --output <file>      write signed mandate JSON to this path
  - (no flag)            write to stdout

Exit codes:
  0  signed intent mandate written
  1  signing error
  2  usage error (no user keys, ambiguous key, bad input)`,
		Example: `  ap2-pp-cli intent grant --description "Buy a dog rope toy under $30" \
    --max-cents 3000 --currency USD --merchant bark.co --hours 24

  ap2-pp-cli intent grant --description "Order groceries" --max-cents 15000 \
    --merchant instacart.com --output signed_intent.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			if description == "" {
				return usageErr(fmt.Errorf("--description is required"))
			}
			if maxCents <= 0 {
				return usageErr(fmt.Errorf("--max-cents must be > 0"))
			}

			// Resolve user key.
			var key *keys.UserKey
			var err error
			if keyID != "" {
				key, err = keys.LoadUserPrivate(keyID)
				if err != nil {
					return fmt.Errorf("loading user key %q: %w", keyID, err)
				}
			} else {
				all, lerr := keys.ListUserKeys()
				if lerr != nil {
					return fmt.Errorf("listing user keys: %w", lerr)
				}
				switch len(all) {
				case 0:
					return usageErr(fmt.Errorf("no user keys found — run 'ap2-pp-cli user-keys generate'"))
				case 1:
					key, err = keys.LoadUserPrivate(all[0].UserID)
					if err != nil {
						return fmt.Errorf("loading user key %q: %w", all[0].UserID, err)
					}
				default:
					ids := make([]string, len(all))
					for i, k := range all {
						ids[i] = k.UserID
					}
					return usageErr(fmt.Errorf("multiple user keys found — specify --key-id <id>: %s", strings.Join(ids, ", ")))
				}
			}

			// Build mandate.
			body := ap2.IntentMandateBody{
				Description:    description,
				MaxAmountCents: maxCents,
				Currency:       currency,
				ExpiresInHours: hours,
			}
			if merchant != "" {
				body.AllowedMerchants = []string{merchant}
			}

			mandate := ap2.BuildIntentMandate(key.UserID, body)

			// Sign with user private key.
			if err := ap2.SignMandate(key.PrivateKey, &mandate); err != nil {
				return fmt.Errorf("signing intent mandate: %w", err)
			}

			// Marshal and emit.
			if flags.asJSON {
				if output != "" {
					data, mErr := json.MarshalIndent(mandate, "", "  ")
					if mErr != nil {
						return fmt.Errorf("marshaling intent mandate: %w", mErr)
					}
					if err := os.WriteFile(output, data, 0o644); err != nil {
						return fmt.Errorf("writing %s: %w", output, err)
					}
					return flags.printJSON(cmd, map[string]any{
						"ok":         true,
						"mandate_id": mandate.MandateID,
						"subject":    mandate.Subject,
						"output":     output,
					})
				}
				return flags.printJSON(cmd, mandate)
			}

			data, mErr := json.MarshalIndent(mandate, "", "  ")
			if mErr != nil {
				return fmt.Errorf("marshaling intent mandate: %w", mErr)
			}
			if output != "" {
				if err := os.WriteFile(output, data, 0o644); err != nil {
					return fmt.Errorf("writing %s: %w", output, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s signed intent mandate written to %s (subject: %s, id: %s)\n",
					green("✓"), output, mandate.Subject, mandate.MandateID)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "Human-readable description of the authorized purchase (required)")
	cmd.Flags().IntVar(&maxCents, "max-cents", 0, "Maximum amount in cents the agent may spend (required, > 0)")
	cmd.Flags().StringVar(&currency, "currency", "USD", "ISO 4217 currency code")
	cmd.Flags().StringVar(&merchant, "merchant", "", "Allowed merchant (empty = any)")
	cmd.Flags().IntVar(&hours, "hours", 24, "Hours until the intent mandate expires")
	cmd.Flags().StringVar(&keyID, "key-id", "", "User key ID to sign with (e.g. user-<uuid>); auto-selected if omitted and only one key exists")
	cmd.Flags().StringVar(&output, "output", "", "Write signed mandate to this file path (default: stdout)")
	return cmd
}
