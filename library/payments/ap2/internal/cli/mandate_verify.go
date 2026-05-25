package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
)

// defaultPubResolver resolves the public key for a given subject (agent ID)
// by calling keys.LoadPublic(subject). The subject set by mandate sign equals
// the key's AgentID, so this round-trips cleanly.
func defaultPubResolver(subject string) (*ecdsa.PublicKey, error) {
	k, err := keys.LoadPublic(subject)
	if err != nil {
		return nil, fmt.Errorf("agent key for subject %q not found (keystore: ~/.config/ap2-pp-cli/keys/) — run 'ap2-pp-cli keys generate' or pass --no-sig-check", subject)
	}
	return k.PublicKey, nil
}

func newMandateVerifyCmd(flags *rootFlags) *cobra.Command {
	var keystorePath string
	cmd := &cobra.Command{
		Use:   "verify [file]",
		Short: "Verify signature and chain integrity of a signed AP2 FinalizationEnvelope",
		Long: `verify reads a signed AP2 FinalizationEnvelope (JSON) from a file or stdin
and checks:
  1. body_hash integrity (SHA-256 of body bytes matches stored hash)
  2. ECDSA signature validity for each mandate
  3. cross-mandate chain references (intent→cart→payment)
  4. amount consistency (payment.amount_cents == cart.subtotal_cents)
  5. expiry (intent_mandate.expires_at must be in the future)

Exit codes:
  0  all checks passed
  1  verification failed (typed error code printed to stderr)
  2  usage error`,
		Example: `  # Verify from file
  ap2-pp-cli mandate verify envelope.json

  # Verify from stdin
  ucp-pp-cli checkout finalize | ap2-pp-cli mandate sign | ap2-pp-cli mandate verify

  # Structural-only check (no signature verification)
  ap2-pp-cli mandate verify --no-sig-check envelope.json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Read envelope from file arg or stdin.
			var data []byte
			var err error
			if len(args) == 1 {
				data, err = os.ReadFile(args[0])
				if err != nil {
					return fmt.Errorf("reading %s: %w", args[0], err)
				}
			} else {
				data, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
			}

			var envelope ap2.FinalizationEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				return fmt.Errorf("invalid envelope JSON: %w", err)
			}

			// Wire --keystore flag so the resolver loads from the user-supplied
			// directory rather than the default. Restore on return so concurrent
			// callers in the same process aren't affected.
			if keystorePath != "" {
				keys.SetConfigDir(keystorePath)
				defer keys.ResetConfigDir()
			}

			// Build resolver: nil = structural-only (skips signature checks).
			// defaultPubResolver calls keys.LoadPublic(subject) — requires a key in the keystore.
			var resolver func(string) (*ecdsa.PublicKey, error)
			noSigCheck, _ := cmd.Flags().GetBool("no-sig-check")
			if !noSigCheck {
				resolver = defaultPubResolver
			}
			// nil resolver = structural-only (VerifyEnvelope skips signature checks).

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
					return fmt.Errorf("verify failed: %s", ve.Code)
				}
				return err
			}

			if flags.asJSON {
				return flags.printJSON(cmd, map[string]any{
					"ok":      true,
					"subject": envelope.Subject,
					"version": envelope.Version,
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s envelope ok (subject: %s)\n", green("✓"), envelope.Subject)
			return nil
		},
	}
	cmd.Flags().BoolP("no-sig-check", "n", false, "Skip ECDSA signature verification (structural checks only)")
	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (default: ~/.config/ap2-pp-cli/keys; or AP2_KEYS_DIR env)")
	return cmd
}
