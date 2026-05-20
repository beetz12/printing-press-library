// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

func newDiffCmd(flags *rootFlags) *cobra.Command {
	var dbPath string

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Stories added since the previous sync (new since last run)",
		Example: `  digg-ai-pp-cli diff
  digg-ai-pp-cli diff --json`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
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

			// Read previous sync timestamp
			var prevSync sql.NullTime
			_ = db.DB().QueryRowContext(cmd.Context(),
				`SELECT prev_sync_at FROM digg_sync_state WHERE id=1`,
			).Scan(&prevSync)

			if !prevSync.Valid {
				return fmt.Errorf("no previous sync found; run 'sync' at least twice to compare")
			}

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT cluster_id, slug, topic, surface, rank, headline,
				       headline_short, summary, source_url, digg_url,
				       age_label, likes, bookmarks, endorser_count,
				       first_seen_at, last_seen_at
				FROM digg_stories
				WHERE first_seen_at > ?
				ORDER BY first_seen_at DESC`, prevSync.Time)
			if err != nil {
				return fmt.Errorf("querying diff: %w", err)
			}
			defer rows.Close()

			results, err := scanStoryRows(rows)
			if err != nil {
				return err
			}

			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}
