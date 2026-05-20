// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"net/http"
	"time"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/spf13/cobra"
)

func newLatestCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var topics []string

	cmd := &cobra.Command{
		Use:   "latest",
		Short: "Fetch the latest AI stories from digg.com",
		Example: `  digg-ai-pp-cli latest
  digg-ai-pp-cli latest --limit 10 --topics ai,technology`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			httpClient := &http.Client{Timeout: 30 * time.Second}
			var all []digg.Story
			seen := map[string]bool{}

			for _, topic := range topics {
				htmlBody, err := digg.FetchPage(httpClient, "/"+topic)
				if err != nil {
					return fmt.Errorf("fetching topic %q: %w", topic, err)
				}
				stories, err := digg.ParseListing(htmlBody, topic)
				if err != nil {
					return fmt.Errorf("parsing topic %q: %w", topic, err)
				}
				for _, s := range stories {
					if !seen[s.DiggURL] {
						seen[s.DiggURL] = true
						all = append(all, s)
					}
				}
			}

			if limit > 0 && len(all) > limit {
				all = all[:limit]
			}

			return printJSONFiltered(cmd.OutOrStdout(), all, flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of stories to return")
	cmd.Flags().StringSliceVar(&topics, "topics", []string{"ai"}, "Comma-separated topic slugs to fetch")

	return cmd
}
