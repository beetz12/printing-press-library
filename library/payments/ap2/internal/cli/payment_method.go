// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"ap2-pp-cli/internal/paymentmethods"
)

// newPaymentMethodCmd builds: ap2 payment-method {add|list|remove|set-default}
func newPaymentMethodCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payment-method",
		Short: "Manage local payment methods used by `payment authorize`",
		Long: `payment-method stores payment tokens locally so 'payment authorize --live'
can resolve a token without having to pass --token every time.

Storage layout (mode 0600 inside a 0700 directory):
  macOS:  ~/Library/Application Support/ap2-pp-cli/payment-methods/
  Linux:  ~/.config/ap2-pp-cli/payment-methods/
  Override: AP2_PM_DIR env var

Each method has a pm-<uuid> ID, a provider tag, a token, and a label.

  payment-method add     --token <tok> [--provider google-pay|stripe|raw] [--label "My Visa"] [--default]
  payment-method list
  payment-method remove  --id <pm-uuid>
  payment-method set-default --id <pm-uuid>`,
		RunE: parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newPaymentMethodAddCmd(flags))
	cmd.AddCommand(newPaymentMethodListCmd(flags))
	cmd.AddCommand(newPaymentMethodRemoveCmd(flags))
	cmd.AddCommand(newPaymentMethodSetDefaultCmd(flags))
	return cmd
}

func newPaymentMethodAddCmd(flags *rootFlags) *cobra.Command {
	var (
		token    string
		provider string
		label    string
		makeDef  bool
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a payment method to the local store",
		Example: `  ap2-pp-cli payment-method add --token pm_test_4242 --provider stripe --label "My Visa 4242"
  ap2-pp-cli payment-method add --token gpay-token-xyz --provider google-pay --default`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return usageErr(fmt.Errorf("--token is required"))
			}
			switch provider {
			case "google-pay", "stripe", "raw":
				// valid
			default:
				return usageErr(fmt.Errorf("invalid --provider %q: must be google-pay, stripe, or raw", provider))
			}
			if dryRunOK(flags) {
				return nil
			}

			pm := paymentmethods.PaymentMethod{
				ID:        paymentmethods.NewID(),
				Provider:  provider,
				Token:     token,
				Label:     label,
				Default:   makeDef,
				CreatedAt: time.Now().UTC(),
			}
			if err := paymentmethods.Add(pm); err != nil {
				return fmt.Errorf("adding payment method: %w", err)
			}
			// If --default, clear the flag on every other method.
			if makeDef {
				if err := paymentmethods.SetDefault(pm.ID); err != nil {
					return fmt.Errorf("marking default: %w", err)
				}
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"id":         pm.ID,
					"provider":   pm.Provider,
					"label":      pm.Label,
					"default":    pm.Default,
					"created_at": pm.CreatedAt.Format(time.RFC3339),
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s (%s)\n", pm.ID, pm.Provider)
			return nil
		},
	}

	cmd.Flags().StringVar(&token, "token", "", "Payment token to store (Google Pay token, Stripe pm_xxx, or raw)")
	cmd.Flags().StringVar(&provider, "provider", "raw", "Payment provider: google-pay, stripe, or raw")
	cmd.Flags().StringVar(&label, "label", "", "User-friendly label (e.g. \"My Visa 4242\")")
	cmd.Flags().BoolVar(&makeDef, "default", false, "Mark as the default payment method (clears flag on others)")
	return cmd
}

func newPaymentMethodListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List stored payment methods",
		Example: `  ap2-pp-cli payment-method list
  ap2-pp-cli payment-method list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			pms, err := paymentmethods.List()
			if err != nil {
				return fmt.Errorf("listing payment methods: %w", err)
			}
			if flags.asJSON {
				out := make([]map[string]any, 0, len(pms))
				for _, pm := range pms {
					out = append(out, map[string]any{
						"id":         pm.ID,
						"provider":   pm.Provider,
						"label":      pm.Label,
						"default":    pm.Default,
						"created_at": pm.CreatedAt.Format(time.RFC3339),
					})
				}
				return flags.printJSON(cmd, out)
			}
			if len(pms) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no payment methods stored — run 'ap2-pp-cli payment-method add' to add one")
				return nil
			}
			headers := []string{"ID", "PROVIDER", "LABEL", "DEFAULT", "CREATED_AT"}
			rows := make([][]string, 0, len(pms))
			for _, pm := range pms {
				def := ""
				if pm.Default {
					def = "*"
				}
				rows = append(rows, []string{
					pm.ID,
					pm.Provider,
					pm.Label,
					def,
					pm.CreatedAt.Format(time.RFC3339),
				})
			}
			return flags.printTable(cmd, headers, rows)
		},
	}
}

func newPaymentMethodRemoveCmd(flags *rootFlags) *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a payment method from the local store",
		Example: `  ap2-pp-cli payment-method remove --id pm-12345678-1234-1234-1234-123456789012`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return usageErr(fmt.Errorf("--id is required"))
			}
			if dryRunOK(flags) {
				return nil
			}
			if err := paymentmethods.Remove(id); err != nil {
				return fmt.Errorf("removing payment method: %w", err)
			}
			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{"id": id, "removed": true})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Payment method ID (pm-<uuid>) to remove")
	return cmd
}

func newPaymentMethodSetDefaultCmd(flags *rootFlags) *cobra.Command {
	var id string

	cmd := &cobra.Command{
		Use:   "set-default",
		Short: "Mark a payment method as the default (clears flag on others)",
		Example: `  ap2-pp-cli payment-method set-default --id pm-12345678-1234-1234-1234-123456789012`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return usageErr(fmt.Errorf("--id is required"))
			}
			if dryRunOK(flags) {
				return nil
			}
			if err := paymentmethods.SetDefault(id); err != nil {
				return fmt.Errorf("setting default: %w", err)
			}
			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{"id": id, "default": true})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set default: %s\n", id)
			return nil
		},
	}

	cmd.Flags().StringVar(&id, "id", "", "Payment method ID (pm-<uuid>) to mark as default")
	return cmd
}
