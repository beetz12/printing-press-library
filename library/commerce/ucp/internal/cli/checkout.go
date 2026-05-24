package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/store"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/ucp"
)

func newCheckoutCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout",
		Short: "UCP checkout operations",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newCheckoutPrepCmd(flags))
	return cmd
}

func newCheckoutPrepCmd(flags *rootFlags) *cobra.Command {
	var cartID string

	cmd := &cobra.Command{
		Use:     "prep",
		Short:   "Build an AP2-ready CheckoutDraft envelope from a local cart",
		Example: `  ucp-pp-cli checkout prep --cart <cart-id> --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if cartID == "" {
				return usageErr(fmt.Errorf("--cart is required"))
			}
			ctx := cmd.Context()

			cart, err := store.Load(cartID)
			if err != nil {
				return fmt.Errorf("load cart: %w", err)
			}

			c, err := ucp.NewMerchantClient(ctx, cart.Merchant)
			if err != nil {
				return fmt.Errorf("connect to merchant %s: %w", cart.Merchant, err)
			}

			sessionJSON, err := c.CreateCheckoutSession(ctx, cart)
			if err != nil {
				return fmt.Errorf("create checkout session: %w", err)
			}

			// Pick the lexicographically first payment handler for a stable, reproducible result.
			negotiated := ""
			for k := range c.Manifest.UCP.PaymentHandlers {
				if negotiated == "" || k < negotiated {
					negotiated = k
				}
			}

			// Compute missing fields
			var missing []string
			if cart.Buyer == nil || cart.Buyer.Email == "" {
				missing = append(missing, "buyer.email")
			}

			ap2Ready := len(missing) == 0 && len(cart.LineItems) > 0 && negotiated != ""

			draft := ucp.CheckoutDraft{
				CartID:            cart.ID,
				Merchant:          cart.Merchant,
				MerchantDomain:    cart.Merchant,
				NegotiatedPayment: negotiated,
				CheckoutSession:   json.RawMessage(sessionJSON),
				MissingFields:     missing,
				AP2Ready:          ap2Ready,
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(draft)
		},
	}
	cmd.Flags().StringVar(&cartID, "cart", "", "Cart ID (required)")
	return cmd
}
