// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/store"
	"github.com/spf13/cobra"
)

// dangerousKeywords rejects non-read SQL.
var dangerousKeywords = []string{
	"insert", "update", "delete", "drop", "create", "alter", "attach",
}

func newSQLCmd(flags *rootFlags) *cobra.Command {
	var dbPath string

	cmd := &cobra.Command{
		Use:     "sql <query>",
		Short:   "Run a read-only SELECT query against the local store",
		Example: `  digg-ai-pp-cli sql "SELECT cluster_id, headline, likes FROM digg_stories ORDER BY likes DESC LIMIT 5"`,
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

			query := strings.TrimSpace(args[0])
			upper := strings.ToUpper(query)

			if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
				return fmt.Errorf("only SELECT queries are allowed")
			}
			for _, kw := range dangerousKeywords {
				// Check as word boundary to avoid false positives
				if containsWord(upper, strings.ToUpper(kw)) {
					return fmt.Errorf("query contains disallowed keyword %q", kw)
				}
			}

			if dbPath == "" {
				dbPath = defaultDBPath("digg-ai-pp-cli")
			}

			db, err := store.OpenWithContext(cmd.Context(), dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer db.Close()

			rows, err := db.DB().QueryContext(cmd.Context(), query)
			if err != nil {
				return fmt.Errorf("query error: %w", err)
			}
			defer rows.Close()

			cols, err := rows.Columns()
			if err != nil {
				return fmt.Errorf("getting columns: %w", err)
			}

			var results []json.RawMessage
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := rows.Scan(ptrs...); err != nil {
					return fmt.Errorf("scanning: %w", err)
				}
				obj := make(map[string]any, len(cols))
				for i, col := range cols {
					v := vals[i]
					// Convert []byte to string for readability
					if b, ok := v.([]byte); ok {
						v = string(b)
					}
					obj[col] = v
				}
				b, _ := json.Marshal(obj)
				results = append(results, json.RawMessage(b))
			}
			if err := rows.Err(); err != nil {
				return fmt.Errorf("row iteration: %w", err)
			}

			return printJSONFiltered(cmd.OutOrStdout(), results, flags)
		},
	}

	cmd.Flags().StringVar(&dbPath, "db", "", "Database path")

	return cmd
}

// containsWord checks if word appears as a SQL keyword (space/paren bounded).
func containsWord(haystack, word string) bool {
	idx := strings.Index(haystack, word)
	for idx >= 0 {
		before := idx == 0 || !isAlphaNum(rune(haystack[idx-1]))
		after := idx+len(word) >= len(haystack) || !isAlphaNum(rune(haystack[idx+len(word)]))
		if before && after {
			return true
		}
		next := strings.Index(haystack[idx+1:], word)
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return false
}

func isAlphaNum(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
}

// Ensure sql import is used.
var _ = sql.ErrNoRows
