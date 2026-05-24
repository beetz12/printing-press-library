package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

type merchantEntry struct {
	Domain       string `json:"domain"`
	Status       string `json:"status"`
	LastChecked  string `json:"last_checked,omitempty"`
	Capabilities int    `json:"capabilities,omitempty"`
}

var seededMerchants = []merchantEntry{
	{Domain: "checkout.coffeecircle.com", Status: "live", LastChecked: "2026-05-24"},
	{Domain: "ucp.dev", Status: "docs-only", LastChecked: "2026-05-24"},
}

func newMerchantsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "merchants",
		Short: "UCP merchant directory",
		RunE:  parentNoSubcommandRunE(flags),
	}
	cmd.AddCommand(newMerchantsListCmd(flags))
	return cmd
}

func newMerchantsListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List known UCP merchants (seeded directory plus any cached locally)",
		Example: `  ucp-pp-cli merchants list --json`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			merchants := append([]merchantEntry{}, seededMerchants...)

			// Merge from cache file if present
			cached := loadCachedMerchants()
			merchants = append(merchants, cached...)

			if flags.asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(merchants)
			}

			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, "DOMAIN\tSTATUS\tLAST-CHECKED\tCAPABILITIES")
			for _, m := range merchants {
				capStr := ""
				if m.Capabilities > 0 {
					capStr = fmt.Sprintf("%d", m.Capabilities)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Domain, m.Status, m.LastChecked, capStr)
			}
			return tw.Flush()
		},
	}
}

func loadCachedMerchants() []merchantEntry {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	path := filepath.Join(base, "ucp-pp-cli", "merchants.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entries []merchantEntry
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	return entries
}
