// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/spf13/cobra"
)

func newStoriesGetCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <cluster_id>",
		Short: "Fetch full detail for a single AI story by its cluster ID.",
		Example: `  digg-ai-pp-cli stories get 550e8400-e29b-41d4-a716-446655440000
  digg-ai-pp-cli stories get 3vb8kiry`,
		Annotations: map[string]string{
			"pp:endpoint":   "stories.get",
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			clusterID := args[0]
			path := "/ai/" + clusterID
			htmlBody, err := digg.FetchPage(nil, path)
			if err != nil {
				return classifyAPIError(err, flags)
			}

			story, err := digg.ParseDetail(htmlBody, clusterID)
			if err != nil {
				return fmt.Errorf("parsing detail: %w", err)
			}

			return printJSONFiltered(cmd.OutOrStdout(), story, flags)
		},
	}

	return cmd
}
