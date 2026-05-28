package cli

import (
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
)

// defaultPubResolver resolves the public key for a given subject (agent ID)
// by calling keys.LoadPublic(subject). The subject set by mandate sign equals
// the key's AgentID, so this round-trips cleanly. Used when --no-user-intent-check
// is set (v0.1 envelopes where all 3 mandates were signed by the same agent key).
func defaultPubResolver(subject string) (*ecdsa.PublicKey, error) {
	k, err := keys.LoadPublic(subject)
	if err != nil {
		return nil, fmt.Errorf("%w — run 'ap2-pp-cli keys generate' or pass --no-sig-check", err)
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
  6. user authority chain (intent_mandate.subject must be a user-<uuid> key)
     — pass --no-user-intent-check for v0.1 envelopes signed entirely by agent

Exit codes:
  0  all checks passed
  1  verification failed (typed error code printed to stderr)
  2  usage error`,
		Example: `  # Verify from file
  ap2-pp-cli mandate verify envelope.json

  # Verify from stdin
  ucp-pp-cli checkout finalize | ap2-pp-cli mandate sign | ap2-pp-cli mandate verify

  # Structural-only check (no signature verification)
  ap2-pp-cli mandate verify --no-sig-check envelope.json

  # v0.1 envelope (all 3 mandates signed by agent)
  ap2-pp-cli mandate verify --no-user-intent-check envelope.json`,
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

			noSigCheck, _ := cmd.Flags().GetBool("no-sig-check")
			noUserIntentCheck, _ := cmd.Flags().GetBool("no-user-intent-check")

			// Build resolver: nil = structural-only (skips signature checks).
			//   - --no-user-intent-check  → defaultPubResolver (v0.1: agent-only keystore)
			//   - otherwise               → keys.LoadPublicAny (v0.2: mixed user/agent keys)
			var resolver func(string) (*ecdsa.PublicKey, error)
			if !noSigCheck {
				if noUserIntentCheck {
					resolver = defaultPubResolver
				} else {
					resolver = func(subject string) (*ecdsa.PublicKey, error) {
						pub, err := keys.LoadPublicAny(subject)
						if err != nil {
							return nil, fmt.Errorf("%w — run 'ap2-pp-cli keys generate' or 'ap2-pp-cli user-keys generate'", err)
						}
						return pub, nil
					}
				}
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
					return fmt.Errorf("verify failed: %s", ve.Code)
				}
				return err
			}

			// Trust chain check: intent MUST be signed by a user key (user-<uuid>),
			// not an agent key. Fail hard with exit 1 so the shell-level outcome
			// matches the JSON {ok:false} envelope; previously the JSON path
			// said ok:false but returned exit 0, silently misleading scripted
			// callers. --no-user-intent-check is the explicit v0.1 back-compat
			// opt-out for envelopes whose intent was signed by the agent
			// rather than the user.
			if !noUserIntentCheck {
				intentSubject := envelope.IntentMandate.Subject
				if !strings.HasPrefix(intentSubject, "user-") {
					msg := fmt.Sprintf("intent mandate subject %q is not a user key (user-<uuid>); pass --no-user-intent-check for v0.1 back-compat", intentSubject)
					if flags.asJSON {
						if perr := flags.printJSON(cmd, map[string]any{
							"ok":         false,
							"error_code": "no_user_intent",
							"message":    msg,
							"mandate_id": envelope.IntentMandate.MandateID,
						}); perr != nil {
							return perr
						}
						return fmt.Errorf("verify failed: no_user_intent")
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "verify failed [no_user_intent] (mandate: %s): %s\n", envelope.IntentMandate.MandateID, msg)
					return fmt.Errorf("verify failed: no_user_intent")
				}
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
	cmd.Flags().BoolP("no-user-intent-check", "u", false, "Skip user-intent trust chain check (v0.1 back-compat: allows agent-signed intent)")
	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (default: ~/.config/ap2-pp-cli/keys; or AP2_KEYS_DIR env)")
	return cmd
}
