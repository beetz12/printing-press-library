package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/paymentmethods"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/transport"
)

func newPaymentCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "payment",
		Short: "Authorize and track AP2 payments with a merchant's MCP complete_checkout endpoint",
		Long: `payment manages the final step of an AP2 agentic checkout:

  authorize  POST a signed FinalizationEnvelope to the merchant's complete_checkout endpoint
  probe      Validate request shape against a live merchant using a stub token (no money spent)
  status     Look up a recorded transaction by ID

Default mode is --sandbox: the request is built and shown without sending to the merchant.
Pass --live to make a real network call (requires --token).`,
	}
	cmd.AddCommand(newPaymentAuthorizeCmd(flags))
	cmd.AddCommand(newPaymentProbeCmd(flags))
	cmd.AddCommand(newPaymentStatusCmd(flags))
	return cmd
}

func newPaymentAuthorizeCmd(flags *rootFlags) *cobra.Command {
	var (
		envelopeFile   string
		googlePayToken string
		tokenFile      string
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
			// Security: in live mode we MUST derive from a signed source so a
			// tampered envelope.checkout_url cannot redirect the Google Pay token
			// to an attacker-controlled origin.  CartMandateBody.checkout_url is
			// covered by the ECDSA signature we just verified; the top-level
			// envelope.checkout_url field is unsigned and MUST NOT be used as
			// the routing destination for live payments.
			mcpURL := merchantMcpURL
			if mcpURL == "" {
				var cartBody ap2.CartMandateBody
				if jerr := json.Unmarshal(envelope.CartMandate.Body, &cartBody); jerr == nil && cartBody.CheckoutURL != "" {
					// Prefer the signed cart-mandate checkout_url.
					mcpURL = deriveMcpURLFromCheckout(cartBody.CheckoutURL)
					// Cross-check: warn if envelope.checkout_url disagrees with the signed value.
					if envelope.CheckoutURL != "" && envelope.CheckoutURL != cartBody.CheckoutURL {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: envelope.checkout_url (%s) differs from signed cart_mandate.checkout_url (%s); using signed value\n",
							envelope.CheckoutURL, cartBody.CheckoutURL)
					}
				} else if !isLive && envelope.CheckoutURL != "" {
					// Sandbox only: fall back to unsigned field (no real token sent).
					mcpURL = deriveMcpURLFromCheckout(envelope.CheckoutURL)
				}
				// In live mode with no cart checkout_url and no --merchant-mcp-url,
				// mcpURL stays ""; CompleteCheckout will return a clear error.
			}

			// Amount ceiling guard: verify payment.amount_cents <= intent.max_amount_cents.
			// Runs in both sandbox and live so the pre-flight catches over-limit
			// envelopes before they reach the merchant.
			if err := verifyAmountCeiling(envelope); err != nil {
				return err
			}

			// Resolve Google Pay token. Resolution order (back-compat):
			//   1. --token <value>         checked first; visible in `ps aux` — we warn
			//   2. --token-file <path>     reads bytes from file; not in process listing
			//   3. AP2_GPAY_TOKEN env var  not in process listing
			// Prefer --token-file or the env var in production to avoid process-listing exposure.
			resolvedToken := googlePayToken
			if resolvedToken != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "warning: --token exposes the payment token in process listings; prefer AP2_GPAY_TOKEN env or --token-file")
			} else if tokenFile != "" {
				b, ferr := os.ReadFile(tokenFile)
				if ferr != nil {
					return fmt.Errorf("reading --token-file %s: %w", tokenFile, ferr)
				}
				resolvedToken = strings.TrimSpace(string(b))
			} else {
				resolvedToken = os.Getenv("AP2_GPAY_TOKEN")
			}

			// If still no token and in live mode, try stored payment methods.
			// On error (no default set / none stored) we proceed with empty token —
			// the merchant will reject with a clear error.
			if resolvedToken == "" && isLive {
				if pm, pmerr := paymentmethods.GetDefault(); pmerr == nil {
					resolvedToken = pm.Token
					fmt.Fprintf(cmd.ErrOrStderr(), "info: using stored payment method %s (%s)\n", pm.ID, pm.Label)
				}
			}

			// Live payment confirmation prompt: only fires in live mode when neither
			// --yes nor --no-input is set (agent/automation paths skip it).
			if isLive && !flags.yes && !flags.noInput {
				var paymentBody ap2.PaymentMandateBody
				if perr := json.Unmarshal(envelope.PaymentMandate.Body, &paymentBody); perr == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "\n⚠️  LIVE PAYMENT\n")
					fmt.Fprintf(cmd.OutOrStdout(), "  Merchant:  %s\n", envelope.Merchant)
					fmt.Fprintf(cmd.OutOrStdout(), "  Amount:    %.2f %s\n",
						float64(paymentBody.AmountCents)/100, paymentBody.Currency)
					fmt.Fprintf(cmd.OutOrStdout(), "  Endpoint:  %s\n", mcpURL)
					fmt.Fprintf(cmd.OutOrStdout(), "\nAuthorize this payment? [y/N] ")

					var answer string
					if _, serr := fmt.Fscan(cmd.InOrStdin(), &answer); serr != nil || strings.ToLower(strings.TrimSpace(answer)) != "y" {
						return fmt.Errorf("payment cancelled by user")
					}
				}
			}

			opts := transport.CompleteOpts{
				MerchantMcpURL: mcpURL,
				GooglePayToken: resolvedToken,
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
	cmd.Flags().StringVar(&googlePayToken, "token", "", "Google Pay token for live mode (INSECURE: visible in process listings; prefer AP2_GPAY_TOKEN env or --token-file)")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "Path to a file whose contents are the Google Pay token (not visible in process listings)")
	cmd.Flags().BoolVar(&sandbox, "sandbox", false, "Sandbox mode: build the request but do NOT send it (this is the implicit default when --live is not passed)")
	cmd.Flags().BoolVar(&live, "live", false, "Live mode: POST to merchant's complete_checkout endpoint (requires --token)")
	cmd.Flags().StringVar(&merchantMcpURL, "merchant-mcp-url", "", "Merchant MCP endpoint URL (derived from envelope.checkout_url if omitted)")
	cmd.Flags().StringVar(&profileURL, "profile-url", "", "UCP agent profile URL (default: https://www.igvita.com/ucp/profile.json)")

	return cmd
}

func newPaymentProbeCmd(flags *rootFlags) *cobra.Command {
	var (
		envelopeFile   string
		merchantMcpURL string
		tokenStub      string
		profileURL     string
	)

	cmd := &cobra.Command{
		Use:   "probe",
		Short: "Validate request shape against a live merchant using a stub token (no money spent)",
		Long: `probe reads a signed AP2 FinalizationEnvelope and sends a complete_checkout
request to the merchant's MCP endpoint with a deliberately-invalid stub token.

The expected GOOD outcome is classification=request_shape_ok — the merchant
rejected our stub token (not our request structure). This proves the integration
is structurally correct without spending any money.

Other classifications indicate actionable problems:
  request_shape_bad     Merchant rejected the request structure itself
  agent_not_authorized  Profile/delegation gate failed
  merchant_unreachable  5xx or transport error
  unknown               Doesn't match any known pattern

Exit codes:
  0  request_shape_ok (integration structurally correct)
  2  request_shape_bad | agent_not_authorized | unknown
  3  merchant_unreachable`,
		Example: `  # Probe a signed envelope against the live merchant
  ap2-pp-cli payment probe --envelope signed.json

  # Probe with explicit merchant URL
  ap2-pp-cli payment probe --envelope signed.json --merchant-mcp-url https://bark.co/api/ucp/mcp

  # Read envelope from stdin
  cat signed.json | ap2-pp-cli payment probe --envelope -`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Verify-first: abort on invalid signature.
			resolver := func(subject string) (*ecdsa.PublicKey, error) {
				k, err := keys.LoadPublic(subject)
				if err != nil {
					return nil, err
				}
				return k.PublicKey, nil
			}
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

			// Amount ceiling guard: catch over-limit envelopes pre-flight so
			// probe gives the same authorize-ready signal as authorize itself.
			if err := verifyAmountCeiling(envelope); err != nil {
				return err
			}

			opts := transport.ProbeOpts{
				MerchantMcpURL: merchantMcpURL,
				TokenStub:      tokenStub,
				ProfileURL:     profileURL,
			}

			result, err := transport.Probe(cmd.Context(), envelope, opts)
			if err != nil {
				// Pre-request failure (e.g. no MCP URL).
				return fmt.Errorf("probe: %w", err)
			}

			if err := flags.printJSON(cmd, result); err != nil {
				return err
			}

			// Map classification to exit code.
			switch result.Classification {
			case transport.ProbeRequestShapeOK:
				return nil // exit 0
			case transport.ProbeMerchantUnreachable:
				return &cliError{code: 3, err: fmt.Errorf("merchant unreachable: %s", result.MerchantError)}
			default:
				return &cliError{code: 2, err: fmt.Errorf("probe classification: %s", result.Classification)}
			}
		},
	}

	cmd.Flags().StringVar(&envelopeFile, "envelope", "-", "Path to signed FinalizationEnvelope JSON file, or - for stdin")
	cmd.Flags().StringVar(&merchantMcpURL, "merchant-mcp-url", "", "Merchant MCP endpoint URL (derived from envelope.checkout_url if omitted)")
	cmd.Flags().StringVar(&tokenStub, "token-stub", "", "Stub token to send (default: stub-invalid-token-for-probe)")
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
