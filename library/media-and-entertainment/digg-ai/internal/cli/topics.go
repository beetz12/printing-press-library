// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"github.com/spf13/cobra"
)

type topicInfo struct {
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

var allTopics = []topicInfo{
	{"ai", "Artificial intelligence news and research"},
	{"technology", "Technology news and industry updates"},
	{"science", "Science discoveries and research"},
	{"world", "World news and international affairs"},
	{"politics", "Political news and analysis"},
	{"business", "Business, finance, and economy"},
	{"sports", "Sports news and highlights"},
	{"entertainment", "Entertainment, culture, and media"},
	{"news", "Breaking news and top stories"},
}

func newTopicsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "topics",
		Short: "List all known Digg topic slugs with descriptions",
		Example: `  digg-ai-pp-cli topics
  digg-ai-pp-cli topics --json`,
		Annotations: map[string]string{
			"mcp:read-only": "true",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if dryRunOK(flags) {
				return nil
			}
			return printJSONFiltered(cmd.OutOrStdout(), allTopics, flags)
		},
	}
	return cmd
}
