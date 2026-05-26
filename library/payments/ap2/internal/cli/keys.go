// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"ap2-pp-cli/internal/keys"
)

// newKeysCmd builds: ap2 keys {generate|list|export}
func newKeysCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage ECDSA-P256 agent keys",
		Long: `Generate, list, and export ECDSA-P256 agent keys used to sign AP2 mandates.

Keys are stored under the platform config directory:
  macOS:  ~/Library/Application Support/ap2-pp-cli/keys/
  Linux:  ~/.config/ap2-pp-cli/keys/

Each key pair consists of:
  agent-<uuid>.pem   private key (PKCS#8 PEM, mode 0600)
  agent-<uuid>.pub   public key  (PKIX PEM,   mode 0644)

  keys generate                          generate a new keypair
  keys list                              list all stored keys
  keys export --id <agent-id>            export a key (default: PEM)
  keys export --id <agent-id> --format jwk   export as JWK`,
		RunE: parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newKeysGenerateCmd(flags))
	cmd.AddCommand(newKeysListCmd(flags))
	cmd.AddCommand(newKeysExportCmd(flags))
	return cmd
}

// keyOutput is the JSON shape for generate and list output.
type keyOutput struct {
	AgentID      string `json:"agent_id"`
	PublicKeyPEM string `json:"public_key_pem,omitempty"`
	KeyPath      string `json:"key_path"`
	CreatedAt    string `json:"created_at,omitempty"`
}

func newKeysGenerateCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Generate a new ECDSA-P256 agent keypair",
		Example: `  ap2-pp-cli keys generate
  ap2-pp-cli keys generate --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			k, err := keys.Generate()
			if err != nil {
				return fmt.Errorf("generating agent key: %w", err)
			}
			pubPEM, err := k.ExportPEM()
			if err != nil {
				return fmt.Errorf("exporting public key: %w", err)
			}
			out := keyOutput{
				AgentID:      k.AgentID,
				PublicKeyPEM: pubPEM,
				KeyPath:      k.Path,
				CreatedAt:    k.CreatedAt.UTC().Format(time.RFC3339),
			}
			if flags.asJSON {
				return flags.printJSON(cmd, out)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "agent_id:   %s\n", out.AgentID)
			fmt.Fprintf(cmd.OutOrStdout(), "key_path:   %s\n", out.KeyPath)
			fmt.Fprintf(cmd.OutOrStdout(), "created_at: %s\n", out.CreatedAt)
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s", out.PublicKeyPEM)
			return nil
		},
	}
}

func newKeysListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all stored agent keys",
		Example: `  ap2-pp-cli keys list
  ap2-pp-cli keys list --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			ks, err := keys.List()
			if err != nil {
				return fmt.Errorf("listing agent keys: %w", err)
			}
			if flags.asJSON {
				out := make([]keyOutput, 0, len(ks))
				for _, k := range ks {
					out = append(out, keyOutput{
						AgentID:   k.AgentID,
						KeyPath:   k.Path,
						CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339),
					})
				}
				return flags.printJSON(cmd, out)
			}
			if len(ks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no agent keys found — run 'ap2-pp-cli keys generate' to create one")
				return nil
			}
			headers := []string{"AGENT_ID", "KEY_PATH", "CREATED_AT"}
			rows := make([][]string, 0, len(ks))
			for _, k := range ks {
				rows = append(rows, []string{
					k.AgentID,
					k.Path,
					k.CreatedAt.UTC().Format(time.RFC3339),
				})
			}
			return flags.printTable(cmd, headers, rows)
		},
	}
}

func newKeysExportCmd(flags *rootFlags) *cobra.Command {
	var agentID string
	var format string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an agent key's public component",
		Example: `  ap2-pp-cli keys export --id agent-<uuid>
  ap2-pp-cli keys export --id agent-<uuid> --format jwk
  ap2-pp-cli keys export --id agent-<uuid> --format pem --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if agentID == "" {
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

			k, err := keys.LoadPublic(agentID)
			if err != nil {
				return err
			}

			switch format {
			case "pem":
				pemStr, err := k.ExportPEM()
				if err != nil {
					return fmt.Errorf("exporting PEM: %w", err)
				}
				if flags.asJSON {
					return flags.printJSON(cmd, map[string]any{
						"agent_id":       k.AgentID,
						"format":         "pem",
						"public_key_pem": pemStr,
					})
				}
				fmt.Fprint(cmd.OutOrStdout(), pemStr)

			case "jwk":
				jwkStr, err := k.ExportJWK()
				if err != nil {
					return fmt.Errorf("exporting JWK: %w", err)
				}
				if flags.asJSON {
					// Parse the JWK back into a map so it nests cleanly inside the JSON envelope.
					var jwkMap map[string]any
					if err2 := json.Unmarshal([]byte(jwkStr), &jwkMap); err2 != nil {
						return fmt.Errorf("re-parsing JWK: %w", err2)
					}
					// Merge kid into the output (it's already in jwkMap but surface agent_id too).
					jwkMap["agent_id"] = k.AgentID
					return flags.printJSON(cmd, jwkMap)
				}
				fmt.Fprintln(cmd.OutOrStdout(), jwkStr)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&agentID, "id", "", "Agent ID (agent-<uuid>) of the key to export")
	cmd.Flags().StringVar(&format, "format", "pem", "Output format: pem or jwk")
	return cmd
}
