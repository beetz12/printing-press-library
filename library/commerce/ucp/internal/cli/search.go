package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/registry"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/transport"
	"github.com/mvanhorn/printing-press-library/library/commerce/ucp/internal/ucp"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	var merchant string
	var limit int
	var allPet bool

	cmd := &cobra.Command{
		Use:     "search <query>",
		Short:   "Search a UCP merchant's catalog",
		Example: `  ucp-pp-cli search "rope toy" --merchant bark.co --limit 5 --json`,
		Args:    cobra.ExactArgs(1),
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			query := args[0]
			ctx := cmd.Context()

			// --all-pet: fan out across all rope-toy pet merchants.
			if allPet {
				var petDomains []string
				for _, m := range registry.Default() {
					if m.HasRopeToys {
						petDomains = append(petDomains, m.Domain)
					}
				}
				var allHits []ucp.SearchHit
				for _, domain := range petDomains {
					hits, err := transport.ShopifyProductsSearch(ctx, domain, query, limit)
					if err != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s: %v\n", domain, err)
						continue
					}
					allHits = append(allHits, hits...)
				}
				return printSearchHits(cmd, flags, allHits)
			}

			if merchant == "" {
				return usageErr(fmt.Errorf("--merchant is required (or use --all-pet to search all pet stores)"))
			}

			// Determine transport: try manifest first; fall through to products.json for Shopify.
			useShopify := true
			manifestCtx := ctx
			m, err := ucp.FetchManifest(manifestCtx, merchant)
			if err == nil {
				// Check if any service endpoint points to myshopify.com (Shopify UCP pattern).
				useShopify = isShopifyMerchant(m)
			}
			// If manifest fetch fails (e.g. localhost mock that doesn't advertise /.well-known/ucp
			// or a network error), fall through to products.json attempt only for known Shopify domains.
			// For localhost/127.0.0.1 targets (CI fixture), always use the REST client.
			if strings.HasPrefix(merchant, "127.0.0.1") || strings.HasPrefix(merchant, "localhost") {
				useShopify = false
			}

			var hits []ucp.SearchHit
			if useShopify {
				hits, err = transport.ShopifyProductsSearch(ctx, merchant, query, limit)
				if err != nil {
					return fmt.Errorf("shopify catalog search: %w", err)
				}
			} else {
				// Legacy REST path (mock merchant or non-Shopify REST merchant).
				c, cerr := ucp.NewMerchantClient(ctx, merchant)
				if cerr != nil {
					return fmt.Errorf("connect to merchant %s: %w", merchant, cerr)
				}
				hits, err = c.Search(ctx, query, limit)
				if err != nil {
					return fmt.Errorf("search: %w", err)
				}
			}

			return printSearchHits(cmd, flags, hits)
		},
	}

	cmd.Flags().StringVar(&merchant, "merchant", "", "Merchant domain (e.g. bark.co)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Max results to return")
	cmd.Flags().BoolVar(&allPet, "all-pet", false, "Fan out query across all rope-toy pet merchants (bark.co, ruffwear.com, sitstay.com)")
	return cmd
}

// isShopifyMerchant returns true if the manifest advertises a Shopify-hosted MCP endpoint.
func isShopifyMerchant(m *ucp.Manifest) bool {
	for _, svcs := range m.UCP.Services {
		for _, s := range svcs {
			if strings.Contains(s.Endpoint, ".myshopify.com") {
				return true
			}
		}
	}
	// Also treat "embedded" transport as Shopify (Shopify-hosted merchants often omit endpoint URL).
	for _, svcs := range m.UCP.Services {
		for _, s := range svcs {
			if s.Transport == "embedded" || s.Transport == "mcp" {
				return true
			}
		}
	}
	return false
}

func printSearchHits(cmd *cobra.Command, flags *rootFlags, hits []ucp.SearchHit) error {
	if flags.asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(hits)
	}

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
}
