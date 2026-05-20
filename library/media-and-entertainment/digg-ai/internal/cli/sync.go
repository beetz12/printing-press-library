// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

type syncResult struct {
	Resource string
	Count    int
	Err      error
	Warn     error
	Duration time.Duration
}

func newSyncCmd(flags *rootFlags) *cobra.Command {
	var topics []string
	var dbPath string
	var full bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync Digg AI stories to local SQLite for offline search and analysis",
		Long: `Fetch stories from digg.com and store them in a local SQLite database.
Supports multiple topics. Run periodically to build up snapshot history for
trending and velocity analysis.`,
		Example: `  # Sync the AI feed (default)
  digg-ai-pp-cli sync

  # Sync multiple topics
  digg-ai-pp-cli sync --topics ai,technology,science

  # Dry run: show what would be synced without writing
  digg-ai-pp-cli sync --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				fmt.Fprintf(cmd.OutOrStdout(), `{"dry_run":true,"topics":%s}`+"\n",
					mustJSON(topics))
				return nil
			}

			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}

			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening local database: %w", err)
			}
			defer db.Close()

			httpClient := &http.Client{Timeout: 30 * time.Second}
			now := time.Now().UTC()

			totalStories := 0
			newStories := 0
			newEndorsers := 0
			snapshots := 0

			for _, topic := range topics {
				path := "/" + topic
				htmlBody, err := digg.FetchPage(httpClient, path)
				if err != nil {
					if strict {
						return fmt.Errorf("fetching topic %q: %w", topic, err)
					}
					fmt.Fprintf(os.Stderr, "warning: failed to fetch topic %q: %v\n", topic, err)
					continue
				}

				stories, err := digg.ParseListing(htmlBody, topic)
				if err != nil {
					if strict {
						return fmt.Errorf("parsing topic %q: %w", topic, err)
					}
					fmt.Fprintf(os.Stderr, "warning: failed to parse topic %q: %v\n", topic, err)
					continue
				}

				ne, ns, nn, err := upsertStories(db, stories, now)
				if err != nil {
					if strict {
						return fmt.Errorf("upserting stories for topic %q: %w", topic, err)
					}
					fmt.Fprintf(os.Stderr, "warning: upsert error for topic %q: %v\n", topic, err)
					continue
				}
				totalStories += len(stories)
				newEndorsers += ne
				snapshots += ns
				newStories += nn
			}

			// Update digg_sync_state: push last→prev, write new last
			if err := updateDiggSyncState(db, now); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to update sync state: %v\n", err)
			}

			summary := map[string]any{
				"topics":        topics,
				"stories_total": totalStories,
				"stories_new":   newStories,
				"endorsers_new": newEndorsers,
				"snapshots":     snapshots,
				"synced_at":     now.Format(time.RFC3339),
			}
			return printJSONFiltered(cmd.OutOrStdout(), summary, flags)
		},
	}

	cmd.Flags().StringSliceVar(&topics, "topics", []string{"ai"}, "Comma-separated topic slugs to sync")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path (default: ~/.local/share/digg-ai-pp-cli/data.db)")
	cmd.Flags().BoolVar(&full, "full", false, "Full resync (reserved for future use)")
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero on any topic fetch failure")

	return cmd
}

// upsertStories inserts/updates stories, endorsers, endorsements, snapshots, and FTS.
// Returns (newEndorsers, snapshots, newStories, error).
func upsertStories(db *store.Store, stories []digg.Story, now time.Time) (int, int, int, error) {
	rawDB := db.DB()
	newEndorsers := 0
	snapshots := 0
	newStories := 0

	for _, s := range stories {
		// Pre-check existence so newStories counts true inserts only.
		// SQLite's UPSERT (ON CONFLICT DO UPDATE) returns RowsAffected=1
		// for both new inserts and conflict-triggered updates, so counting
		// via ra alone would always equal len(stories).
		var existed int
		if err := rawDB.QueryRow(
			`SELECT 1 FROM digg_stories WHERE cluster_id = ?`, s.ClusterID,
		).Scan(&existed); err != nil && err != sql.ErrNoRows {
			return newEndorsers, snapshots, newStories, fmt.Errorf("checking existence %s: %w", s.ClusterID, err)
		}
		isNew := existed == 0

		// UPSERT digg_stories
		if _, err := rawDB.Exec(`
			INSERT INTO digg_stories
				(cluster_id, slug, topic, surface, rank, headline, headline_short, summary,
				 source_url, digg_url, age_label, likes, bookmarks, endorser_count,
				 first_seen_at, last_seen_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(cluster_id) DO UPDATE SET
				slug=excluded.slug,
				topic=excluded.topic,
				surface=excluded.surface,
				rank=excluded.rank,
				headline=excluded.headline,
				headline_short=excluded.headline_short,
				summary=excluded.summary,
				source_url=excluded.source_url,
				digg_url=excluded.digg_url,
				age_label=excluded.age_label,
				likes=excluded.likes,
				bookmarks=excluded.bookmarks,
				endorser_count=excluded.endorser_count,
				last_seen_at=excluded.last_seen_at`,
			s.ClusterID, s.Slug, s.Topic, s.Surface, nullInt(s.Rank),
			s.Headline, s.HeadlineShort, s.Summary, s.SourceURL, s.DiggURL,
			s.AgeLabel, s.Likes, s.Bookmarks, s.EndorserCount, now, now,
		); err != nil {
			return newEndorsers, snapshots, newStories, fmt.Errorf("upserting story %s: %w", s.ClusterID, err)
		}
		if isNew {
			newStories++
		}

		// FTS5 virtual tables don't support INSERT OR IGNORE deduplication
		// (rowids are auto-incremented, no unique constraint to collide).
		// Delete any existing row for this cluster_id, then insert the
		// fresh content. Keeps digg_stories_fts 1:1 with digg_stories so
		// `search` returns one row per story regardless of sync count.
		if _, err := rawDB.Exec(
			`DELETE FROM digg_stories_fts WHERE cluster_id = ?`, s.ClusterID,
		); err != nil {
			return newEndorsers, snapshots, newStories, fmt.Errorf("fts delete %s: %w", s.ClusterID, err)
		}
		if _, err := rawDB.Exec(
			`INSERT INTO digg_stories_fts(cluster_id, headline, summary, source_url)
			 VALUES (?,?,?,?)`,
			s.ClusterID, s.Headline, s.Summary, s.SourceURL,
		); err != nil {
			return newEndorsers, snapshots, newStories, fmt.Errorf("fts insert %s: %w", s.ClusterID, err)
		}

		// Snapshot
		if _, err := rawDB.Exec(`
			INSERT OR IGNORE INTO digg_story_snapshots(cluster_id, snapshot_at, likes, bookmarks, rank)
			VALUES (?,?,?,?,?)`,
			s.ClusterID, now, s.Likes, s.Bookmarks, nullInt(s.Rank),
		); err == nil {
			snapshots++
		}

		// Endorsers
		for _, e := range s.Endorsers {
			if e.Handle == "" {
				continue
			}
			res, err := rawDB.Exec(`
				INSERT INTO digg_endorsers(handle, name, digg_url, x_url, avatar_url)
				VALUES (?,?,?,?,?)
				ON CONFLICT(handle) DO UPDATE SET
					name=excluded.name,
					digg_url=excluded.digg_url,
					x_url=excluded.x_url,
					avatar_url=excluded.avatar_url`,
				e.Handle, e.Name, e.DiggURL, e.XURL, e.AvatarURL,
			)
			if err == nil {
				if ra, _ := res.RowsAffected(); ra == 1 {
					newEndorsers++
				}
			}
			// Endorsement join
			_, _ = rawDB.Exec(`
				INSERT OR IGNORE INTO digg_story_endorsements(cluster_id, handle)
				VALUES (?,?)`,
				s.ClusterID, e.Handle,
			)
		}
	}

	return newEndorsers, snapshots, newStories, nil
}

// updateDiggSyncState rotates last_sync_at → prev_sync_at and writes now as last_sync_at.
func updateDiggSyncState(db *store.Store, now time.Time) error {
	rawDB := db.DB()
	var last sql.NullTime
	_ = rawDB.QueryRow(`SELECT last_sync_at FROM digg_sync_state WHERE id=1`).Scan(&last)
	_, err := rawDB.Exec(`
		INSERT INTO digg_sync_state(id, prev_sync_at, last_sync_at) VALUES(1,?,?)
		ON CONFLICT(id) DO UPDATE SET prev_sync_at=excluded.prev_sync_at, last_sync_at=excluded.last_sync_at`,
		last, now,
	)
	return err
}

// nullInt converts 0 to sql NULL to avoid polluting rank with spurious 0s.
func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// mustJSON marshals v to a JSON string, returning "null" on error.
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// The following stubs preserve compilation of sync-related helpers
// that the generator-emitted sync command used. They are no longer called
// but removing them would break any import paths that might reference them.
// Keeping them unexported and minimal.

func parseSinceDuration(s string) (time.Time, error) {
	re := regexp.MustCompile(`^(\d+)([dhwm])$`)
	matches := re.FindStringSubmatch(strings.TrimSpace(s))
	if matches == nil {
		return time.Time{}, fmt.Errorf("expected format like 7d, 24h, 1w, or 30m")
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return time.Time{}, err
	}
	now := time.Now()
	switch matches[2] {
	case "d":
		return now.Add(-time.Duration(n) * 24 * time.Hour), nil
	case "h":
		return now.Add(-time.Duration(n) * time.Hour), nil
	case "w":
		return now.Add(-time.Duration(n) * 7 * 24 * time.Hour), nil
	case "m":
		return now.Add(-time.Duration(n) * time.Minute), nil
	default:
		return time.Time{}, fmt.Errorf("unknown unit %q", matches[2])
	}
}
