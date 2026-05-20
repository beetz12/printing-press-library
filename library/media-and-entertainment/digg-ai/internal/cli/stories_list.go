// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/spf13/cobra"
)

func newStoriesListCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var topic string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the latest AI stories curated on digg.com/ai. Returns a ranked feed of stories with headline, summary, age, endorsers.",
		Example: `  digg-ai-pp-cli stories list
  digg-ai-pp-cli stories list --limit 10 --topic technology`,
		Annotations: map[string]string{
			"pp:endpoint":   "stories.list",
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			path := "/" + topic
			htmlBody, err := digg.FetchPage(nil, path)
			if err != nil {
				return classifyAPIError(err, flags)
			}

			stories, err := digg.ParseListing(htmlBody, topic)
			if err != nil {
				return fmt.Errorf("parsing listing: %w", err)
			}

			if limit > 0 && len(stories) > limit {
				stories = stories[:limit]
			}

			return printJSONFiltered(cmd.OutOrStdout(), stories, flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 30, "Maximum number of stories to return")
	cmd.Flags().StringVar(&topic, "topic", "ai", "Topic slug (ai, technology, science, world, ...)")

	return cmd
}
