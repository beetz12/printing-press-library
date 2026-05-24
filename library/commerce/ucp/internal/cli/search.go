package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/ucp"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	var merchant string
	var limit int

	cmd := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search a single UCP merchant's catalog via the REST transport",
		Example: `  ucp-pp-cli search "coffee" --merchant 127.0.0.1:8080 --limit 5 --json`,
		Args:    cobra.ExactArgs(1),
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if merchant == "" {
				return usageErr(fmt.Errorf("--merchant is required"))
			}
			query := args[0]
			ctx := cmd.Context()

			c, err := ucp.NewMerchantClient(ctx, merchant)
			if err != nil {
				return fmt.Errorf("connect to merchant %s: %w", merchant, err)
			}

			hits, err := c.Search(ctx, query, limit)
			if err != nil {
				return fmt.Errorf("search: %w", err)
			}

			if flags.asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(hits)
			}

			// Human table
			if len(hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No results.")
				return nil
			}
			tw := newTabWriter(cmd.OutOrStdout())
			fmt.Fprintln(tw, "TITLE\tPRICE\tSKU\tURL")
			for _, h := range hits {
				priceStr := fmt.Sprintf("%d¢", h.Price)
				if h.Price > 0 {
					priceStr = fmt.Sprintf("$%.2f", float64(h.Price)/100)
				}
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Title, priceStr, h.SKU, h.URL)
			}
			return tw.Flush()
		},
	}

	cmd.Flags().StringVar(&merchant, "merchant", "", "Merchant domain (required)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results to return")
	return cmd
}
