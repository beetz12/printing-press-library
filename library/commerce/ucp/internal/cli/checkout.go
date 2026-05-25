package cli

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/store"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/transport"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/ucp"
)

func newCheckoutCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout",
		Short: "UCP checkout operations",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newCheckoutPrepCmd(flags))
	cmd.AddCommand(newCheckoutFinalizeCmd(flags))
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

			ap2Ready := len(missing) == 0 && len(cart.LineItems) > 0

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

func newCheckoutFinalizeCmd(flags *rootFlags) *cobra.Command {
	var cartID string
	var subject string
	var intentDescription string
	var maxAmountCents int

	cmd := &cobra.Command{
		Use:     "finalize",
		Short:   "Build a FinalizationEnvelope (Shopify cart-add + 3 AP2 mandates) for an external AP2 CLI to sign and complete",
		Example: `  ucp-pp-cli checkout finalize --cart <cart-id> --json | ap2-pp-cli sign-and-complete`,
		Annotations: map[string]string{"mcp:read-only": "false"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			ctx := cmd.Context()
			if cartID == "" {
				return usageErr(fmt.Errorf("--cart is required"))
			}
			cart, err := store.Load(cartID)
			if err != nil {
				return fmt.Errorf("load cart: %w", err)
			}
			if len(cart.LineItems) == 0 {
				return fmt.Errorf("cart %s has no line items", cartID)
			}
			if subject == "" {
				subject = "ucp-pp-cli-anonymous-agent"
			}
			if intentDescription == "" {
				intentDescription = fmt.Sprintf("Purchase %d item(s) from %s via UCP+AP2", len(cart.LineItems), cart.Merchant)
			}

			// 1. Call Shopify cart-add for the first line item.
			// For v1.2, we use the first line item's variant ID for the checkout_url construction;
			// multi-item live cart-add is v1.3.
			first := cart.LineItems[0]
			// Prefer the numeric Shopify variant ID stored at search time; fall back to SKU/ID
			// only for non-Shopify merchants (where VariantID will be zero).
			var variantID string
			if first.Item.VariantID != 0 {
				variantID = strconv.FormatInt(first.Item.VariantID, 10)
			} else {
				variantID = first.Item.SKU
			}
			if variantID == "" {
				variantID = first.Item.ID
			}
			if variantID == "" {
				return fmt.Errorf("first line item has no variant ID/SKU/ID — cannot call shopify cart-add")
			}
			addResult, err := transport.ShopifyCartAdd(ctx, cart.Merchant, variantID, first.Quantity)
			if err != nil {
				// Don't fail outright — emit the envelope without merchant_cart_token so
				// the AP2 CLI can decide whether to proceed. Document the failure.
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: shopify cart-add failed for %s: %v\n", cart.Merchant, err)
				addResult = &transport.ShopifyCartAddResult{
					CheckoutURL: fmt.Sprintf("https://%s/cart/%s:%d", cart.Merchant, variantID, first.Quantity),
				}
			}

			// 2. Compute subtotal locally.
			subtotal := 0
			for _, li := range cart.LineItems {
				subtotal += li.Item.Price * li.Quantity
			}

			// 3. Build the three mandates.
			maxAmt := maxAmountCents
			if subtotal*2 > maxAmt {
				maxAmt = subtotal * 2
			}
			intent := ucp.BuildIntentMandate(subject, ucp.IntentMandateBody{
				Description:      intentDescription,
				MaxAmountCents:   maxAmt,
				Currency:         cart.Currency,
				AllowedMerchants: []string{cart.Merchant},
				ExpiresInHours:   24,
			})
			cartMandate := ucp.BuildCartMandate(subject, intent.MandateID, cart, addResult.CartToken, addResult.CheckoutURL)
			// Payment handler default: com.google.pay (most common for Shopify UCP).
			payment := ucp.BuildPaymentMandate(subject, cartMandate.MandateID, "com.google.pay", addResult.CartToken, subtotal, cart.Currency)

			// 4. Emit envelope.
			envelope := ucp.FinalizationEnvelope{
				Version:           "1.0",
				Subject:           subject,
				IntentMandate:     intent,
				CartMandate:       cartMandate,
				PaymentMandate:    payment,
				Merchant:          cart.Merchant,
				MerchantCartToken: addResult.CartToken,
				CheckoutURL:       addResult.CheckoutURL,
				Instructions:      "Pipe this envelope to an external AP2 CLI (e.g. ap2-pp-cli) to sign the three mandates and POST checkout-sessions/{id}/complete with a real payment token. ucp-pp-cli v1.2 does not perform payment completion — that requires a real Google Pay token or equivalent.",
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(envelope)
		},
	}
	cmd.Flags().StringVar(&cartID, "cart", "", "Local cart ID (required)")
	cmd.Flags().StringVar(&subject, "subject", "", "Agent subject identifier (default: ucp-pp-cli-anonymous-agent)")
	cmd.Flags().StringVar(&intentDescription, "intent", "", "Human-readable purchase intent (default: auto-generated)")
	cmd.Flags().IntVar(&maxAmountCents, "max-cents", 0, "Maximum amount in cents the AP2 intent authorizes (default: 2x cart subtotal)")
	return cmd
}
