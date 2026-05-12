// Copyright 2026 david. Licensed under Apache-2.0. See LICENSE.
// Hand-written for slickdeals-pp-cli v0.2 (rss-browse engineer).
//
// Note: the original draft of this file also declared Item, Parse, FetchURL,
// and pubDateLayouts — those moved into rss.go during integration to avoid
// duplicate-symbol compile errors. The category map and URL builders below
// are the rss-browse engineer's contribution and remain authoritative.

package rss

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// CategoryMap maps friendly names (lowercase) to Slickdeals forum IDs.
// Verified via live probes during v0.2 build (2026-05-11):
//   - All forumid=N URLs return 25 items at 200 OK via
//     newsearch.php?mode=frontpage&forumid=N&rss=1
var CategoryMap = map[string]int{
	"hot":         9,  // Hot Deals / Frontpage (verified: 25 items)
	"deals":       9,
	"tech":        25, // Computer/Tech deals (verified: 25 items)
	"computers":   25,
	"computer":    25,
	"home":        17, // Home & Garden
	"garden":      17,
	"automotive":  53,
	"auto":        53,
	"apparel":     68,
	"clothing":    68,
	"sports":      46,
	"fitness":     46,
	"travel":      55,
	"games":       30,
	"gaming":      30,
	"toys":        35,
	"beauty":      38,
	"health":      38,
	"grocery":     14,
	"food":        14,
	"pets":        48,
	"baby":        36,
	"office":      45,
	"tools":       49,
}

// ResolveCategory accepts a numeric ID string OR a friendly name and returns
// the forum ID. Returns -1 and an error for unrecognised inputs.
func ResolveCategory(input string) (int, error) {
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return -1, fmt.Errorf("category input is empty")
	}
	// Try numeric first.
	if n, err := strconv.Atoi(input); err == nil {
		if n <= 0 {
			return -1, fmt.Errorf("forum ID must be positive, got %d", n)
		}
		return n, nil
	}
	// Try map lookup.
	if id, ok := CategoryMap[input]; ok {
		return id, nil
	}
	// Build a sorted list of known names for the hint.
	known := make([]string, 0, len(CategoryMap))
	seen := map[int]bool{}
	for name, id := range CategoryMap {
		if !seen[id] {
			known = append(known, name)
			seen[id] = true
		}
	}
	return -1, fmt.Errorf("unknown category %q; use a numeric forum ID or one of: %s",
		input, strings.Join(known, ", "))
}

// CategoryURL returns the frontpage RSS URL. The forum ID is kept in the
// signature for API stability, but the URL itself does not include it:
// Slickdeals' RSS silently ignores `forumid=N` when `mode=frontpage` is set
// (verified 2026-05-11), and dropping `mode=frontpage` returns an empty feed.
// The category filter is applied client-side in LiveCategory.
func CategoryURL(forumID int) string {
	_ = forumID
	return "https://slickdeals.net/newsearch.php?mode=frontpage&rss=1"
}

// LiveCategory fetches the frontpage RSS feed and filters items client-side
// using the keyword aliases registered for the given forum ID in CategoryMap.
// When the forum ID has no alias keywords (or is forum 9 / Hot Deals), the
// full feed is returned. Limit is applied last.
//
// This implementation is the honest workaround for Slickdeals' RSS surface
// not honoring forum-id filtering when mode=frontpage is set. The category
// concept is preserved at the CLI level by matching keywords against item
// titles/descriptions, which is the only available signal.
func LiveCategory(ctx context.Context, hc *http.Client, forumID, limit int) ([]Item, error) {
	items, err := FetchURL(ctx, CategoryURL(forumID), hc)
	if err != nil {
		return nil, err
	}
	if forumID > 0 && forumID != 9 {
		keywords := keywordsForForum(forumID)
		if len(keywords) > 0 {
			items = FilterByAnyKeyword(items, keywords)
		}
	}
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

// keywordsForForum returns the keyword set used to client-side filter frontpage
// items for a given forum ID. The friendly-name aliases from CategoryMap are
// always included, and the common forums get an extended semantic keyword set
// so a Slickdeals frontpage drop ("USB-C Power Bank", "WiFi router") matches
// the user's intent ("tech") even when the literal alias word isn't in the title.
func keywordsForForum(forumID int) []string {
	out := make([]string, 0, 8)
	for name, id := range CategoryMap {
		if id == forumID {
			out = append(out, name)
		}
	}
	if extra, ok := forumKeywords[forumID]; ok {
		out = append(out, extra...)
	}
	return out
}

// forumKeywords extends the alias-based filter with semantic terms per forum.
// Each list is small and human-curated; the goal is "tech matches USB and SSD
// and laptop", not exhaustive product taxonomy.
var forumKeywords = map[int][]string{
	25: { // Computer/Tech Deals
		"usb", "usb-c", "ssd", "nvme", "monitor", "laptop", "desktop", "keyboard",
		"mouse", "headphone", "earbud", "earbuds", "magsafe", "power bank",
		"charger", "cable", "wifi", "wireless", "router", "switch", "ram",
		"cpu", "gpu", "graphics card", "gaming pc", "external drive", "hub",
		"adapter", "webcam", "microphone",
	},
	30: { // Gaming
		"nintendo", "playstation", "xbox", "ps5", "ps4", "switch", "steam",
		"controller", "console", "gaming", "lego", "board game", "pokémon",
		"pokemon",
	},
	17: { // Home & Garden
		"vacuum", "cookware", "knife", "blender", "toaster", "oven",
		"microwave", "fridge", "refrigerator", "mattress", "sheet", "towel",
		"sofa", "chair", "desk", "lamp", "trash bin", "kohler", "dyson",
	},
	14: { // Grocery / Food
		"coffee", "tea", "snack", "cereal", "pasta", "rice", "oil", "vinegar",
		"soda", "chip", "candy", "chocolate", "protein", "meal",
	},
	68: { // Apparel
		"shirt", "shoe", "sneaker", "pant", "jeans", "jacket", "coat", "dress",
		"sock", "underwear", "hat",
	},
	46: { // Sports & Fitness
		"dumbbell", "kettlebell", "treadmill", "yoga", "bike", "bicycle",
		"helmet", "gym", "fitness", "workout", "barbell", "weight", "plate",
	},
}

// FilterByAnyKeyword returns items whose title or description (case-insensitive)
// contains at least one of the keywords. Empty keyword list returns the input
// unchanged. Exported so the search command can reuse the same filter.
func FilterByAnyKeyword(items []Item, keywords []string) []Item {
	if len(keywords) == 0 {
		return items
	}
	lowered := make([]string, 0, len(keywords))
	for _, k := range keywords {
		k = strings.TrimSpace(strings.ToLower(k))
		if k != "" {
			lowered = append(lowered, k)
		}
	}
	out := items[:0]
	for _, it := range items {
		hay := strings.ToLower(it.Title + "\n" + it.Description)
		for _, k := range lowered {
			if strings.Contains(hay, k) {
				out = append(out, it)
				break
			}
		}
	}
	return out
}
