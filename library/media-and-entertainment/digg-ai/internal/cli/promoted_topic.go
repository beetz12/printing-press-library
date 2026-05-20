// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/spf13/cobra"
)

var knownTopics = []string{
	"ai", "technology", "science", "world", "politics",
	"business", "sports", "entertainment", "news",
}

func newTopicPromotedCmd(flags *rootFlags) *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "topic <topic>",
		Short: "List the latest stories for a given Digg topic.",
		Long:  "List the latest stories for a given Digg topic. AI is the primary feed; other topics: technology, science, world, politics, business, sports, entertainment, news.",
		Example: `  digg-ai-pp-cli topic ai
  digg-ai-pp-cli topic technology --limit 20`,
		Annotations: map[string]string{
			"pp:endpoint":   "topic.list",
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			topic := args[0]

			// Validate topic
			valid := false
			for _, t := range knownTopics {
				if t == topic {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("unknown topic %q; available topics: %s", topic, strings.Join(knownTopics, ", "))
			}

			path := "/" + topic
			htmlBody, err := digg.FetchPage(nil, path)
			if err != nil {
				return classifyAPIError(err, flags)
			}

			stories, err := digg.ParseListing(htmlBody, topic)
			if err != nil {
				return fmt.Errorf("parsing topic listing: %w", err)
			}

			if limit > 0 && len(stories) > limit {
				stories = stories[:limit]
			}

			return printJSONFiltered(cmd.OutOrStdout(), stories, flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 30, "Maximum number of stories to return")

	return cmd
}
