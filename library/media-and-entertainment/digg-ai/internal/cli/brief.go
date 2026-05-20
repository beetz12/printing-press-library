// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/digg"
	"github.com/spf13/cobra"
)

type briefStory struct {
	Rank      int      `json:"rank"`
	Headline  string   `json:"headline"`
	Summary   string   `json:"summary,omitempty"`
	SourceURL string   `json:"source_url,omitempty"`
	Age       string   `json:"age,omitempty"`
	Likes     int      `json:"likes,omitempty"`
	Endorsers []string `json:"endorsers,omitempty"`
}

func newBriefCmd(flags *rootFlags) *cobra.Command {
	var limit int
	var dbPath string

	cmd := &cobra.Command{
		Use:   "brief",
		Short: "Pre-formatted top-N AI digest for piping into a morning briefing",
		Example: `  digg-ai-pp-cli brief
  digg-ai-pp-cli brief --limit 5 --json`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}

			var stories []digg.Story

			// Try local store first if requested, otherwise live
			if flags.dataSource == "local" {
				_ = dbPath // reserved for future use
				return fmt.Errorf("--data-source=local not yet implemented for brief; run sync first then use search")
			}

			httpClient := &http.Client{Timeout: 30 * time.Second}
			htmlBody, err := digg.FetchPage(httpClient, "/ai")
			if err != nil {
				return classifyAPIError(err, flags)
			}
			stories, err = digg.ParseListing(htmlBody, "ai")
			if err != nil {
				return fmt.Errorf("parsing listing: %w", err)
			}

			if limit > 0 && len(stories) > limit {
				stories = stories[:limit]
			}

			var briefs []briefStory
			for i, s := range stories {
				var endorserStrs []string
				for _, e := range s.Endorsers {
					if e.Name != "" && e.Handle != "" {
						endorserStrs = append(endorserStrs, fmt.Sprintf("%s (@%s)", e.Name, e.Handle))
					} else if e.Name != "" {
						endorserStrs = append(endorserStrs, e.Name)
					}
				}
				briefs = append(briefs, briefStory{
					Rank:      i + 1,
					Headline:  s.Headline,
					Summary:   s.Summary,
					SourceURL: s.SourceURL,
					Age:       s.AgeLabel,
					Likes:     s.Likes,
					Endorsers: endorserStrs,
				})
			}

			// Use raw JSON marshal so printJSONFiltered can apply --select etc.
			raw, _ := json.Marshal(briefs)
			return printOutputWithFlags(cmd.OutOrStdout(), json.RawMessage(raw), flags)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 10, "Number of stories to include")
	cmd.Flags().StringVar(&dbPath, "db", "", "Database path (for --data-source=local)")

	return cmd
}
