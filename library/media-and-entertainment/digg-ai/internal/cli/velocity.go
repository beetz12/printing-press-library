// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

type velocityEntry struct {
	ClusterID    string  `json:"cluster_id"`
	Headline     string  `json:"headline"`
	LikesStart   int     `json:"likes_start"`
	LikesEnd     int     `json:"likes_end"`
	LikesGrowth  int     `json:"likes_growth"`
	LikesPerHour float64 `json:"likes_per_hour"`
	Topic        string  `json:"topic"`
	DiggURL      string  `json:"digg_url"`
}

func newVelocityCmd(flags *rootFlags) *cobra.Command {
	var hours int
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "velocity",
		Short: "Stories with fastest monotonic likes growth (per-hour rate)",
		Example: `  digg-ai-pp-cli velocity
  digg-ai-pp-cli velocity --hours 6 --limit 10`,
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

			// COALESCE the per-hour rate to 0 so SQLite never returns NULL into a Go float64.
			// julianday() returns NULL for unparseable timestamps; without COALESCE the
			// row scan would fail. Keeping the HAVING clause means stories with a single
			// snapshot are filtered out before this computation, but COALESCE remains
			// belt-and-suspenders for any pathological timestamp format.
			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.headline, s.topic, s.digg_url,
				       COALESCE(MIN(sn.likes), 0) AS likes_start,
				       COALESCE(MAX(sn.likes), 0) AS likes_end,
				       COALESCE(MAX(sn.likes) - MIN(sn.likes), 0) AS likes_growth,
				       COALESCE(
				           CAST(MAX(sn.likes) - MIN(sn.likes) AS REAL) /
				             MAX(1.0, (julianday(MAX(sn.snapshot_at)) - julianday(MIN(sn.snapshot_at))) * 24.0),
				           0.0
				       ) AS likes_per_hour
				FROM digg_story_snapshots sn
				JOIN digg_stories s ON s.cluster_id = sn.cluster_id
				WHERE sn.snapshot_at >= datetime('now', ? || ' hours')
				GROUP BY sn.cluster_id
				HAVING COUNT(*) >= 2 AND MAX(sn.likes) >= MIN(sn.likes)
				ORDER BY likes_per_hour DESC
				LIMIT ?`,
				fmt.Sprintf("-%d", hours), limit,
			)
			if err != nil {
				return fmt.Errorf("velocity query: %w", err)
			}
			defer rows.Close()

			var results []json.RawMessage
			for rows.Next() {
				var e velocityEntry
				if err := rows.Scan(&e.ClusterID, &e.Headline, &e.Topic, &e.DiggURL,
					&e.LikesStart, &e.LikesEnd, &e.LikesGrowth, &e.LikesPerHour); err != nil {
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

	cmd.Flags().IntVar(&hours, "hours", 6, "Time window in hours")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}
