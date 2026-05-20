// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

func newCooccurCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "cooccur <term1> <term2>",
		Short: "Stories where both terms appear (FTS5 AND query)",
		Example: `  digg-ai-pp-cli cooccur "language model" "safety"
  digg-ai-pp-cli cooccur "OpenAI" "GPT"`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				return fmt.Errorf("cooccur requires two terms (got %d); see --help", len(args))
			}
			if dryRunOK(flags) {
				return nil
			}

			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			// FTS5 MATCH with AND
			ftsQuery := fmt.Sprintf("%q AND %q", args[0], args[1])

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.slug, s.topic, s.surface, s.rank, s.headline,
				       s.headline_short, s.summary, s.source_url, s.digg_url,
				       s.age_label, s.likes, s.bookmarks, s.endorser_count,
				       s.first_seen_at, s.last_seen_at
				FROM digg_stories s
				WHERE s.cluster_id IN (
					SELECT cluster_id FROM digg_stories_fts WHERE digg_stories_fts MATCH ?
				)
				ORDER BY s.last_seen_at DESC
				LIMIT ?`, ftsQuery, limit)
			if err != nil {
				return fmt.Errorf("cooccur query: %w", err)
			}
			defer rows.Close()

			results, err := scanStoryRows(rows)
			if err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}
