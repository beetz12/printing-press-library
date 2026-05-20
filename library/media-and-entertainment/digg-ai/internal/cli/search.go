// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Full-text search across synced stories (FTS5)",
		Example: `  digg-ai-pp-cli search "language model"
  digg-ai-pp-cli search "GPT" --limit 5 --json`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if dryRunOK(flags) {
				return nil
			}

			query := args[0]

			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}

			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.slug, s.topic, s.surface, s.rank, s.headline,
				       s.headline_short, s.summary, s.source_url, s.digg_url,
				       s.age_label, s.likes, s.bookmarks, s.endorser_count,
				       s.first_seen_at, s.last_seen_at
				FROM digg_stories s
				JOIN digg_stories_fts f ON s.cluster_id = f.cluster_id
				WHERE digg_stories_fts MATCH ?
				ORDER BY f.rank
				LIMIT ?`, query, limit)
			if err != nil {
				return fmt.Errorf("search query failed: %w", err)
			}
			defer rows.Close()

			results, err := scanStoryRows(rows)
			if err != nil {
				return err
			}

			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum number of results")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}

// storyRow is a flat struct for SQL scan results.
type storyRow struct {
	ClusterID     string `json:"cluster_id"`
	Slug          string `json:"slug"`
	Topic         string `json:"topic"`
	Surface       string `json:"surface"`
	Rank          *int   `json:"rank,omitempty"`
	Headline      string `json:"headline"`
	HeadlineShort string `json:"headline_short,omitempty"`
	Summary       string `json:"summary,omitempty"`
	SourceURL     string `json:"source_url,omitempty"`
	DiggURL       string `json:"digg_url"`
	AgeLabel      string `json:"age_label,omitempty"`
	Likes         int    `json:"likes"`
	Bookmarks     int    `json:"bookmarks"`
	EndorserCount int    `json:"endorser_count"`
	FirstSeenAt   string `json:"first_seen_at"`
	LastSeenAt    string `json:"last_seen_at"`
}

func scanStoryRows(rows *sql.Rows) ([]json.RawMessage, error) {
	var results []json.RawMessage
	for rows.Next() {
		var r storyRow
		var rank sql.NullInt64
		var headlineShort, summary, sourceURL, ageLabel sql.NullString
		err := rows.Scan(
			&r.ClusterID, &r.Slug, &r.Topic, &r.Surface, &rank,
			&r.Headline, &headlineShort, &summary, &sourceURL, &r.DiggURL,
			&ageLabel, &r.Likes, &r.Bookmarks, &r.EndorserCount,
			&r.FirstSeenAt, &r.LastSeenAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		if rank.Valid {
			v := int(rank.Int64)
			r.Rank = &v
		}
		if headlineShort.Valid {
			r.HeadlineShort = headlineShort.String
		}
		if summary.Valid {
			r.Summary = summary.String
		}
		if sourceURL.Valid {
			r.SourceURL = sourceURL.String
		}
		if ageLabel.Valid {
			r.AgeLabel = ageLabel.String
		}
		b, _ := json.Marshal(r)
		results = append(results, json.RawMessage(b))
	}
	return results, rows.Err()
}
