// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

func newEndorsersCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "endorsers",
		Short: "Explore endorsers: top endorsers, feed by handle, overlap between two endorsers",
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
	}

	cmd.AddCommand(newEndorsersTopCmd(flags))
	cmd.AddCommand(newEndorsersFeedCmd(flags))
	cmd.AddCommand(newEndorsersOverlapCmd(flags))

	return cmd
}

// endorsers top

type endorserTopEntry struct {
	Handle string `json:"handle"`
	Name   string `json:"name"`
	Count  int    `json:"count"`
}

func newEndorsersTopCmd(flags *rootFlags) *cobra.Command {
	var days int
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "top",
		Short: "Rank endorsers by how often they appear in the synced corpus",
		Example: `  digg-ai-pp-cli endorsers top
  digg-ai-pp-cli endorsers top --days 7 --limit 20`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			if days <= 0 {
				return fmt.Errorf("--days must be positive (got %d)", days)
			}
			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT e.handle, COALESCE(en.name, e.handle) AS name, COUNT(*) AS cnt
				FROM digg_story_endorsements e
				JOIN digg_endorsers en ON e.handle = en.handle
				JOIN digg_stories s ON e.cluster_id = s.cluster_id
				WHERE s.last_seen_at > datetime('now', ? || ' days')
				GROUP BY e.handle
				ORDER BY cnt DESC
				LIMIT ?`,
				fmt.Sprintf("-%d", days), limit,
			)
			if err != nil {
				return fmt.Errorf("endorsers top query: %w", err)
			}
			defer rows.Close()

			var results []json.RawMessage
			for rows.Next() {
				var e endorserTopEntry
				if err := rows.Scan(&e.Handle, &e.Name, &e.Count); err != nil {
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

	cmd.Flags().IntVar(&days, "days", 30, "Window in days")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum results")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")
	return cmd
}

// endorsers feed

func newEndorsersFeedCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "feed <handle>",
		Short: "Stories endorsed by a given X handle",
		Example: `  digg-ai-pp-cli endorsers feed researcher1
  digg-ai-pp-cli endorsers feed lateinteraction --limit 20`,
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
			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}
			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			handle := args[0]

			// Confirm the handle is in the synced corpus before querying stories.
			// "Unknown handle" and "known handle with zero stories" need to be
			// distinguishable: the former is user error (typo / sentinel), the
			// latter is a real but empty result.
			var known int
			if err := db.DB().QueryRowContext(cmd.Context(),
				`SELECT COUNT(*) FROM digg_endorsers WHERE handle = ?`, handle,
			).Scan(&known); err != nil {
				return fmt.Errorf("checking endorser: %w", err)
			}
			if known == 0 {
				return fmt.Errorf("no endorser known with handle %q in local corpus; try `digg-ai-pp-cli endorsers top` to see who is indexed, or `digg-ai-pp-cli sync` to widen coverage", handle)
			}

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.slug, s.topic, s.surface, s.rank, s.headline,
				       s.headline_short, s.summary, s.source_url, s.digg_url,
				       s.age_label, s.likes, s.bookmarks, s.endorser_count,
				       s.first_seen_at, s.last_seen_at
				FROM digg_stories s
				JOIN digg_story_endorsements e ON s.cluster_id = e.cluster_id
				WHERE e.handle = ?
				ORDER BY s.last_seen_at DESC
				LIMIT ?`, handle, limit)
			if err != nil {
				return fmt.Errorf("feed query: %w", err)
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
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum results")
	return cmd
}

// endorsers overlap

func newEndorsersOverlapCmd(flags *rootFlags) *cobra.Command {
	var dbPath string
	var limit int

	cmd := &cobra.Command{
		Use:   "overlap <handle1> <handle2>",
		Short: "Stories endorsed by both handles",
		Example: `  digg-ai-pp-cli endorsers overlap researcher1 researcher2
  digg-ai-pp-cli endorsers overlap karpathy lateinteraction --limit 10`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			if len(args) < 2 {
				return fmt.Errorf("overlap requires two handles (got %d); see --help", len(args))
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

			h1, h2 := args[0], args[1]

			rows, err := db.DB().QueryContext(cmd.Context(), `
				SELECT s.cluster_id, s.slug, s.topic, s.surface, s.rank, s.headline,
				       s.headline_short, s.summary, s.source_url, s.digg_url,
				       s.age_label, s.likes, s.bookmarks, s.endorser_count,
				       s.first_seen_at, s.last_seen_at
				FROM digg_stories s
				WHERE s.cluster_id IN (
					SELECT e1.cluster_id FROM digg_story_endorsements e1
					WHERE e1.handle = ?
					INTERSECT
					SELECT e2.cluster_id FROM digg_story_endorsements e2
					WHERE e2.handle = ?
				)
				ORDER BY s.last_seen_at DESC
				LIMIT ?`, h1, h2, limit)
			if err != nil {
				return fmt.Errorf("overlap query: %w", err)
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
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum results")
	return cmd
}
