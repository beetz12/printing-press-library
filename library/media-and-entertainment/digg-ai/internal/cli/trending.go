// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

type trendingEntry struct {
	ClusterID   string `json:"cluster_id"`
	Headline    string `json:"headline"`
	LikesStart  int    `json:"likes_start"`
	LikesEnd    int    `json:"likes_end"`
	LikesGrowth int    `json:"likes_growth"`
	Topic       string `json:"topic"`
	DiggURL     string `json:"digg_url"`
}

func newTrendingCmd(flags *rootFlags) *cobra.Command {
	var hours int
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "trending",
		Short: "Top stories sorted by likes growth over a window",
		Example: `  digg-ai-pp-cli trending
  digg-ai-pp-cli trending --hours 48 --limit 10`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if hours <= 0 {
				return fmt.Errorf("--hours must be positive (got %d)", hours)
			}
			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			// HAVING MAX>=MIN keeps stories whose likes count dropped (after
			// a correction or Digg removal) out of the "top growth" list —
			// otherwise they'd surface with negative growth. Mirrors the
			// monotonic-growth filter in velocity.go.
			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.headline, s.topic, s.digg_url,
				       MIN(sn.likes) AS likes_start,
				       MAX(sn.likes) AS likes_end,
				       MAX(sn.likes) - MIN(sn.likes) AS likes_growth
				FROM digg_story_snapshots sn
				JOIN digg_stories s ON s.cluster_id = sn.cluster_id
				WHERE sn.snapshot_at >= datetime('now', ? || ' hours')
				GROUP BY sn.cluster_id
				HAVING COUNT(*) >= 2 AND MAX(sn.likes) >= MIN(sn.likes)
				ORDER BY likes_growth DESC
				LIMIT ?`,
				fmt.Sprintf("-%d", hours), limit,
			)
			if err != nil {
				return fmt.Errorf("trending query: %w", err)
			}
			defer rows.Close()

			var results []json.RawMessage
			for rows.Next() {
				var e trendingEntry
				if err := rows.Scan(&e.ClusterID, &e.Headline, &e.Topic, &e.DiggURL,
					&e.LikesStart, &e.LikesEnd, &e.LikesGrowth); err != nil {
					return fmt.Errorf("scanning: %w", err)
				}
				b, _ := json.Marshal(e)
				results = append(results, json.RawMessage(b))
			}
			if err := rows.Err(); err != nil {
				return err
			}
			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}

	cmd.Flags().IntVar(&hours, "hours", 24, "Time window in hours")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}
