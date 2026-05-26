// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/keys"
)

// newUserKeysCmd builds: ap2 user-keys {generate|list|export}
func newUserKeysCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user-keys",
		Short: "Manage ECDSA-P256 user keys (for IntentMandate signing)",
		Long: `Generate, list, and export ECDSA-P256 user keys used to sign AP2 IntentMandates.

User keys are distinct from agent keys: the user signs the IntentMandate to
authorize an agent; the agent then signs CartMandate and PaymentMandate. This
gives merchants a verifiable trust chain (user → agent → payment).

Keys are stored under the platform config directory:
  macOS:  ~/Library/Application Support/ap2-pp-cli/user-keys/
  Linux:  ~/.config/ap2-pp-cli/user-keys/
  (override with AP2_USER_KEYS_DIR)

Each key pair consists of:
  user-<uuid>.pem   private key (PKCS#8 PEM, mode 0600)
  user-<uuid>.pub   public key  (PKIX PEM,   mode 0644)

  user-keys generate                          generate a new keypair
  user-keys list                              list all stored keys
  user-keys export --id <user-id>             export a key (default: PEM)
  user-keys export --id <user-id> --format jwk   export as JWK`,
		RunE: parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newUserKeysGenerateCmd(flags))
	cmd.AddCommand(newUserKeysListCmd(flags))
	cmd.AddCommand(newUserKeysExportCmd(flags))
	return cmd
}

// userKeyOutput is the JSON shape for generate and list output.
type userKeyOutput struct {
	UserID       string `json:"user_id"`
	PublicKeyPEM string `json:"public_key_pem,omitempty"`
	KeyPath      string `json:"key_path"`
	CreatedAt    string `json:"created_at,omitempty"`
}

func newUserKeysGenerateCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generate a new ECDSA-P256 user keypair",
		Example: `  ap2-pp-cli user-keys generate
  ap2-pp-cli user-keys generate --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			k, err := keys.GenerateUserKey()
			if err != nil {
				return fmt.Errorf("generating user key: %w", err)
			}
			pubPEM, err := keys.ExportUserPEM(k.UserID)
			if err != nil {
				return fmt.Errorf("exporting user public key: %w", err)
			}
			out := userKeyOutput{
				UserID:       k.UserID,
				PublicKeyPEM: pubPEM,
				KeyPath:      k.Path,
				CreatedAt:    k.CreatedAt.UTC().Format(time.RFC3339),
			}
			if flags.asJSON {
				return flags.printJSON(cmd, out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "user_id:    %s\n", out.UserID)
			fmt.Fprintf(cmd.OutOrStdout(), "key_path:   %s\n", out.KeyPath)
			fmt.Fprintf(cmd.OutOrStdout(), "created_at: %s\n", out.CreatedAt)
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s", out.PublicKeyPEM)
			return nil
		},
	}
}

func newUserKeysListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all stored user keys",
		Example: `  ap2-pp-cli user-keys list
  ap2-pp-cli user-keys list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			ks, err := keys.ListUserKeys()
			if err != nil {
				return fmt.Errorf("listing user keys: %w", err)
			}
			if flags.asJSON {
				out := make([]userKeyOutput, 0, len(ks))
				for _, k := range ks {
					out = append(out, userKeyOutput{
						UserID:    k.UserID,
						KeyPath:   k.Path,
						CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339),
					})
				}
				return flags.printJSON(cmd, out)
			}
			if len(ks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no user keys found — run 'ap2-pp-cli user-keys generate' to create one")
				return nil
			}
			headers := []string{"USER_ID", "KEY_PATH", "CREATED_AT"}
			rows := make([][]string, 0, len(ks))
			for _, k := range ks {
				rows = append(rows, []string{
					k.UserID,
					k.Path,
					k.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
			return flags.printTable(cmd, headers, rows)
		},
	}
}

func newUserKeysExportCmd(flags *rootFlags) *cobra.Command {
	var userID string
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a user key's public component",
		Example: `  ap2-pp-cli user-keys export --id user-<uuid>
  ap2-pp-cli user-keys export --id user-<uuid> --format jwk
  ap2-pp-cli user-keys export --id user-<uuid> --format pem --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if userID == "" {
				return usageErr(fmt.Errorf("--id is required"))
			}
			if dryRunOK(flags) {
				return nil
			}

			switch format {
			case "pem", "jwk":
				// valid
			default:
				return usageErr(fmt.Errorf("invalid --format %q: must be pem or jwk", format))
			}

			switch format {
			case "pem":
				pemStr, err := keys.ExportUserPEM(userID)
				if err != nil {
					return err
				}
				if flags.asJSON {
					return flags.printJSON(cmd, map[string]any{
						"user_id":        userID,
						"format":         "pem",
						"public_key_pem": pemStr,
					})
				}
				fmt.Fprint(cmd.OutOrStdout(), pemStr)

			case "jwk":
				jwk, err := keys.ExportUserJWK(userID)
				if err != nil {
					return err
				}
				if flags.asJSON {
					jwk["user_id"] = userID
					return flags.printJSON(cmd, jwk)
				}
				data, mErr := json.Marshal(jwk)
				if mErr != nil {
					return fmt.Errorf("marshaling JWK: %w", mErr)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(data))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&userID, "id", "", "User ID (user-<uuid>) of the key to export")
	cmd.Flags().StringVar(&format, "format", "pem", "Output format: pem or jwk")
	return cmd
}
