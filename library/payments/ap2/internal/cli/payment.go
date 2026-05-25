package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/transport"
)

func newPaymentCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payment",
		Short: "Authorize and track AP2 payments with a merchant's MCP complete_checkout endpoint",
		Long: `payment manages the final step of an AP2 agentic checkout:

  authorize  POST a signed FinalizationEnvelope to the merchant's complete_checkout endpoint
  status     Look up a recorded transaction by ID

Default mode is --sandbox: the request is built and shown without sending to the merchant.
Pass --live to make a real network call (requires --token).`,
	}
	cmd.AddCommand(newPaymentAuthorizeCmd(flags))
	cmd.AddCommand(newPaymentStatusCmd(flags))
	return cmd
}

func newPaymentAuthorizeCmd(flags *rootFlags) *cobra.Command {
	var (
		envelopeFile  string
		googlePayToken string
		sandbox        bool
		live           bool
		merchantMcpURL string
		profileURL     string
	)

	cmd := &cobra.Command{
		Use:   "authorize",
		Short: "Authorize a signed AP2 FinalizationEnvelope with the merchant (sandbox default)",
		Long: `authorize reads a signed AP2 FinalizationEnvelope and calls the merchant's
complete_checkout MCP endpoint. Default mode is --sandbox, which builds and
prints the would-be request without making any network call.

Exit codes:
  0  authorized (sandbox_authorized in sandbox mode)
  1  verification or authorization failure
  2  usage error`,
		Example: `  # Sandbox authorization (default — no network call)
  ap2-pp-cli payment authorize --envelope signed.json

  # Live authorization (real money — requires --token)
  ap2-pp-cli payment authorize --envelope signed.json --live --token gpay-token-here

  # Read envelope from stdin
  cat signed.json | ap2-pp-cli payment authorize --envelope -`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate mutually exclusive --sandbox / --live.
			if sandbox && live {
				return usageErr(fmt.Errorf("--sandbox and --live are mutually exclusive"))
			}
			isLive := live && !sandbox

			// Live mode requires --token.
			if isLive && googlePayToken == "" {
				return usageErr(fmt.Errorf("live mode requires --token <google_pay_token>"))
			}

			// Read envelope from file or stdin.
			var data []byte
			var err error
			if envelopeFile == "" || envelopeFile == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
			} else {
				data, err = os.ReadFile(envelopeFile)
				if err != nil {
					return fmt.Errorf("reading %s: %w", envelopeFile, err)
				}
			}

			var envelope ap2.FinalizationEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				return fmt.Errorf("invalid envelope JSON: %w", err)
			}

			// Build resolver from internal/keys, matching mandate_verify.go pattern.
			resolver := func(subject string) (*ecdsa.PublicKey, error) {
				k, err := keys.LoadPublic(subject)
				if err != nil {
					return nil, err
				}
				return k.PublicKey, nil
			}

			// Verify-first: abort on any verify error.
			if err := ap2.VerifyEnvelope(envelope, resolver); err != nil {
				var ve *ap2.VerifyError
				if errors.As(err, &ve) {
					if flags.asJSON {
						return flags.printJSON(cmd, map[string]any{
							"ok":         false,
							"error_code": string(ve.Code),
							"message":    ve.Message,
							"mandate_id": ve.MandateID,
						})
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "verify failed [%s]", ve.Code)
					if ve.MandateID != "" {
						fmt.Fprintf(cmd.ErrOrStderr(), " (mandate: %s)", ve.MandateID)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), ": %s\n", ve.Message)
					return usageErr(fmt.Errorf("verify failed: %s", ve.Code))
				}
				return err
			}

			// Derive MCP URL if not provided.
			mcpURL := merchantMcpURL
			if mcpURL == "" && envelope.CheckoutURL != "" {
				mcpURL = deriveMcpURLFromCheckout(envelope.CheckoutURL)
			}

			opts := transport.CompleteOpts{
				MerchantMcpURL: mcpURL,
				GooglePayToken: googlePayToken,
				ProfileURL:     profileURL,
				Sandbox:        !isLive,
			}

			result, err := transport.CompleteCheckout(cmd.Context(), envelope, opts)
			if err != nil {
				if result != nil {
					_ = transport.SaveTransaction(result)
					_ = flags.printJSON(cmd, result)
				}
				return fmt.Errorf("complete_checkout: %w", err)
			}

			// Save transaction record.
			if serr := transport.SaveTransaction(result); serr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: failed to save transaction: %v\n", serr)
			}

			return flags.printJSON(cmd, result)
		},
	}

	cmd.Flags().StringVar(&envelopeFile, "envelope", "-", "Path to signed FinalizationEnvelope JSON file, or - for stdin")
	cmd.Flags().StringVar(&googlePayToken, "token", "", "Google Pay token for live mode")
	cmd.Flags().BoolVar(&sandbox, "sandbox", true, "Sandbox mode: build the request but do NOT send it (default)")
	cmd.Flags().BoolVar(&live, "live", false, "Live mode: POST to merchant's complete_checkout endpoint (requires --token)")
	cmd.Flags().StringVar(&merchantMcpURL, "merchant-mcp-url", "", "Merchant MCP endpoint URL (derived from envelope.checkout_url if omitted)")
	cmd.Flags().StringVar(&profileURL, "profile-url", "", "UCP agent profile URL (default: https://www.igvita.com/ucp/profile.json)")

	return cmd
}

func newPaymentStatusCmd(flags *rootFlags) *cobra.Command {
	var txnID string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Look up a recorded transaction by ID",
		Example: `  ap2-pp-cli payment status --transaction sandbox-12345678-1234-1234-1234-123456789abc`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if txnID == "" {
				return usageErr(fmt.Errorf("--transaction is required"))
			}

			result, err := transport.LoadTransaction(txnID)
			if err != nil {
				return fmt.Errorf("loading transaction: %w", err)
			}

			return flags.printJSON(cmd, result)
		},
	}

	cmd.Flags().StringVar(&txnID, "transaction", "", "Transaction ID to look up (e.g. sandbox-<uuid>)")

	return cmd
}

// deriveMcpURLFromCheckout builds the MCP endpoint URL from the checkout URL.
// "https://bark-food.myshopify.com/checkout" → "https://bark-food.myshopify.com/api/ucp/mcp"
func deriveMcpURLFromCheckout(checkoutURL string) string {
	u, err := url.Parse(checkoutURL)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host + "/api/ucp/mcp"
}
