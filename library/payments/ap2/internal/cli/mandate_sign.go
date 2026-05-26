package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"ap2-pp-cli/internal/ap2"
	"ap2-pp-cli/internal/keys"
)

// newMandateSignCmd: ap2 mandate sign --envelope <file-or-->  [--key-id <agent-id>] [--subject <subject>]
//
// Behavior:
//   - Read envelope JSON from --envelope arg (file path) or "-" (stdin)
//   - Resolve which key to sign with:
//   - if --key-id given, keys.Load(--key-id)
//   - else: keys.List(); if exactly 1, use it; if 0, error "no agent keys found — run 'ap2-pp-cli keys generate'" with exit code 2; if >1, error "multiple keys — specify --key-id <id>: <list>" with exit code 2
//   - If --subject given, set envelope.Subject = --subject; else if envelope.Subject is empty, set it to the key's AgentID
//   - --user-intent <file>: load user-signed IntentMandate from <file>, splice into envelope, sign only cart+payment
//   - --no-user-intent: v0.1 behavior — sign all 3 mandates with agent key (back-compat)
//   - (no flag, intent already signed): sign only cart+payment (preserves user signature)
//   - (no flag, intent unsigned): v0.1 fallback — sign all 3 with agent key
//   - Marshal signed envelope to stdout (pretty-printed unless --compact)
func newMandateSignCmd(flags *rootFlags) *cobra.Command {
	var envelopeArg string
	var keyID string
	var subject string
	var userIntentFile string
	var noUserIntent bool

	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Sign an unsigned AP2 FinalizationEnvelope with an agent key",
		Long: `sign reads an unsigned AP2 FinalizationEnvelope (JSON) from a file or stdin,
signs the cart and payment mandates with the specified ECDSA-P256 agent key,
and writes the signed envelope to stdout.

In v0.2, the IntentMandate is signed by a user key (see 'ap2-pp-cli intent grant').
Pass --user-intent <file> to splice that user-signed mandate into the envelope
before agent signing — the user's signature on intent is preserved untouched.

Key selection:
  - --key-id <agent-id>  use this specific key
  - (no flag)            auto-select if exactly one key exists; error if 0 or >1

Subject:
  - --subject <s>        set envelope.Subject to this value
  - (no flag)            if envelope.Subject is empty, default to the key's AgentID

User intent (v0.2):
  - --user-intent <file>      load user-signed IntentMandate JSON, splice into envelope
  - --no-user-intent          v0.1 back-compat: sign all 3 mandates with agent key
  - (neither, intent signed)  sign only cart + payment (preserves user signature)
  - (neither, intent unsigned) v0.1 fallback: sign all 3 with agent key

Exit codes:
  0  signed envelope written to stdout
  1  signing error
  2  usage error (no key, ambiguous key, bad input)`,
		Example: `  ap2-pp-cli mandate sign --envelope envelope.json
  ucp-pp-cli checkout finalize --cart $C --json | ap2-pp-cli mandate sign --envelope -
  ap2-pp-cli mandate sign --envelope envelope.json --user-intent signed_intent.json
  ap2-pp-cli mandate sign --envelope envelope.json --key-id agent-<uuid> --subject my-agent-v1`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			// Read envelope.
			var data []byte
			var err error
			switch {
			case envelopeArg == "" || envelopeArg == "-":
				data, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("reading stdin: %w", err)
				}
			default:
				data, err = os.ReadFile(envelopeArg)
				if err != nil {
					return fmt.Errorf("reading %s: %w", envelopeArg, err)
				}
			}

			var envelope ap2.FinalizationEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				return usageErr(fmt.Errorf("invalid envelope JSON: %w", err))
			}

			// Splice user-signed intent if requested.
			if userIntentFile != "" {
				uiData, uierr := os.ReadFile(userIntentFile)
				if uierr != nil {
					return fmt.Errorf("reading user intent file %s: %w", userIntentFile, uierr)
				}
				var userIntent ap2.AP2Mandate
				if uErr := json.Unmarshal(uiData, &userIntent); uErr != nil {
					return usageErr(fmt.Errorf("invalid user intent JSON: %w", uErr))
				}
				envelope.IntentMandate = userIntent
			}

			// Resolve key.
			var key *keys.Key
			if keyID != "" {
				key, err = keys.Load(keyID)
				if err != nil {
					return fmt.Errorf("loading key %q: %w", keyID, err)
				}
			} else {
				all, lerr := keys.List()
				if lerr != nil {
					return fmt.Errorf("listing keys: %w", lerr)
				}
				switch len(all) {
				case 0:
					return usageErr(fmt.Errorf("no agent keys found — run 'ap2-pp-cli keys generate'"))
				case 1:
					// Load the private key for the single entry (List returns public-only keys).
					key, err = keys.Load(all[0].AgentID)
					if err != nil {
						return fmt.Errorf("loading key %q: %w", all[0].AgentID, err)
					}
				default:
					ids := make([]string, len(all))
					for i, k := range all {
						ids[i] = k.AgentID
					}
					return usageErr(fmt.Errorf("multiple keys found — specify --key-id <id>: %s", strings.Join(ids, ", ")))
				}
			}

			// Set subject.
			if subject != "" {
				envelope.Subject = subject
			} else if envelope.Subject == "" {
				envelope.Subject = key.AgentID
			}

			// Decide signing path:
			//   - --user-intent given  → sign only cart + payment (intent already user-signed)
			//   - --no-user-intent     → sign all 3 with agent key (v0.1 back-compat)
			//   - intent already signed → sign only cart + payment (preserve existing sig)
			//   - otherwise            → sign all 3 (v0.1 fallback)
			signCartAndPaymentOnly := userIntentFile != "" ||
				(!noUserIntent && envelope.IntentMandate.Signature != "")

			if signCartAndPaymentOnly {
				if err := ap2.SignCartAndPayment(key.PrivateKey, &envelope); err != nil {
					return fmt.Errorf("signing envelope: %w", err)
				}
			} else {
				if err := ap2.SignEnvelope(key.PrivateKey, &envelope); err != nil {
					return fmt.Errorf("signing envelope: %w", err)
				}
			}

			// Output.
			if flags.asJSON {
				return flags.printJSON(cmd, envelope)
			}

			enc := json.NewEncoder(cmd.OutOrStdout())
			if !flags.compact {
				enc.SetIndent("", "  ")
			}
			return enc.Encode(envelope)
		},
	}

	cmd.Flags().StringVar(&envelopeArg, "envelope", "-", `Envelope file path, or "-" to read from stdin`)
	cmd.Flags().StringVar(&keyID, "key-id", "", "Agent key ID to sign with (e.g. agent-<uuid>); auto-selected if omitted and only one key exists")
	cmd.Flags().StringVar(&subject, "subject", "", "Override envelope.Subject (defaults to the signing key's AgentID if envelope.Subject is empty)")
	cmd.Flags().StringVar(&userIntentFile, "user-intent", "", "Path to a user-signed IntentMandate JSON (from 'ap2 intent grant')")
	cmd.Flags().BoolVar(&noUserIntent, "no-user-intent", false, "Sign all 3 mandates with agent key (v0.1 back-compat; skips user authority chain)")
	return cmd
}
