// Package digg provides HTML parsers for digg.com listing and detail pages.
package digg

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Story represents a curated story from digg.com.
type Story struct {
	ClusterID     string     `json:"cluster_id"`
	Slug          string     `json:"slug"`
	Topic         string     `json:"topic"`
	Surface       string     `json:"surface"`
	Rank          int        `json:"rank,omitempty"`
	Headline      string     `json:"headline"`
	HeadlineShort string     `json:"headline_short,omitempty"`
	Summary       string     `json:"summary,omitempty"`
	SourceURL     string     `json:"source_url,omitempty"`
	DiggURL       string     `json:"digg_url"`
	AgeLabel      string     `json:"age_label,omitempty"`
	Likes         int        `json:"likes,omitempty"`
	Bookmarks     int        `json:"bookmarks,omitempty"`
	EndorserCount int        `json:"endorser_count,omitempty"`
	DatePublished string     `json:"date_published,omitempty"`
	DateModified  string     `json:"date_modified,omitempty"`
	Endorsers     []Endorser `json:"endorsers,omitempty"`
}

// Endorser represents a person who endorsed a story on digg.com.
type Endorser struct {
	Handle    string `json:"handle"`
	Name      string `json:"name"`
	DiggURL   string `json:"digg_url,omitempty"`
	XURL      string `json:"x_url,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// parseCount converts strings like "174", "1.2k", "5.3M", "174.4k" to ints.
func parseCount(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	lower := strings.ToLower(s)
	multiplier := 1.0
	if strings.HasSuffix(lower, "k") {
		multiplier = 1000
		lower = lower[:len(lower)-1]
	} else if strings.HasSuffix(lower, "m") {
		multiplier = 1_000_000
		lower = lower[:len(lower)-1]
	} else if strings.HasSuffix(lower, "b") {
		multiplier = 1_000_000_000
		lower = lower[:len(lower)-1]
	}
	f, err := strconv.ParseFloat(lower, 64)
	if err != nil {
		return 0
	}
	return int(f * multiplier)
}

// extractAttr extracts the value of a named attribute from an HTML tag string.
func extractAttr(tag, attr string) string {
	// Try double-quoted first
	re := regexp.MustCompile(attr + `="([^"]*)"`)
	if m := re.FindStringSubmatch(tag); len(m) > 1 {
		return m[1]
	}
	// Try single-quoted
	re2 := regexp.MustCompile(attr + `='([^']*)'`)
	if m := re2.FindStringSubmatch(tag); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractTagContent extracts text content between an opening and closing tag.
func extractTagContent(html, tag string) string {
	openRe := regexp.MustCompile(`(?i)<` + tag + `[^>]*>`)
	loc := openRe.FindStringIndex(html)
	if loc == nil {
		return ""
	}
	start := loc[1]
	closeTag := "</" + tag + ">"
	end := strings.Index(html[start:], closeTag)
	if end < 0 {
		return ""
	}
	return stripTags(html[start : start+end])
}

// stripTags removes HTML tags from a string.
func stripTags(s string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(s, ""))
}

// htmlDecode does minimal HTML entity decoding.
func htmlDecode(s string) string {
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return s
}

// ParseListing extracts stories from a /ai or /{topic} HTML page.
// topic is the topic slug used to populate Story.Topic.
func ParseListing(html, topic string) ([]Story, error) {
	seen := map[string]bool{}
	var stories []Story

	// Surface 1: highlight articles with data-story-row="true"
	articleRe := regexp.MustCompile(`(?s)<article\s[^>]*data-story-row="true"[^>]*>.*?</article>`)
	articles := articleRe.FindAllString(html, -1)
	for _, art := range articles {
		s := parseHighlightArticle(art, topic)
		if s == nil || s.DiggURL == "" {
			continue
		}
		key := s.DiggURL
		if seen[key] {
			continue
		}
		seen[key] = true
		stories = append(stories, *s)
	}

	// Surface 2: ranked stories — anchors with ?rank= parameter
	// Pattern: href="/topic/slug?rank=N"
	rankedRe := regexp.MustCompile(`(?s)<a\s[^>]*href="(/[a-zA-Z0-9_-]+/([a-zA-Z0-9_-]+)\?rank=(\d+))"[^>]*>.*?</a>`)
	rankedMatches := rankedRe.FindAllStringSubmatch(html, -1)

	// Build a wider context map for ranked stories: find surrounding block
	// by searching for each match position in the raw html
	for _, m := range rankedMatches {
		fullPath := m[1] // e.g. /ai/3vb8kiry?rank=3
		slug := m[2]
		rankStr := m[3]

		// Build a digg URL without the rank param
		pathParts := strings.SplitN(fullPath, "?", 2)
		cleanPath := pathParts[0] // /ai/3vb8kiry

		// Determine topic from path
		pathSegs := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")
		storyTopic := topic
		if len(pathSegs) >= 1 && pathSegs[0] != "" {
			storyTopic = pathSegs[0]
		}

		diggURL := "https://digg.com" + cleanPath
		if seen[diggURL] {
			continue
		}

		rank, _ := strconv.Atoi(rankStr)

		// Find the anchor's surrounding block to extract headline/summary/meta.
		// Search for the opening <a ... href="..."> tag start to capture the
		// full anchor element and whatever follows it (meta block, counts).
		anchorIdx := strings.Index(html, `href="`+fullPath+`"`)
		if anchorIdx < 0 {
			anchorIdx = strings.Index(html, `href='`+fullPath+`'`)
		}
		if anchorIdx < 0 {
			continue
		}

		// Walk back to find the opening < of the anchor tag
		openTag := anchorIdx
		for openTag > 0 && html[openTag] != '<' {
			openTag--
		}

		// Extract a forward window starting at the anchor's opening tag
		// so that headline/summary/meta all appear AFTER the anchor start.
		end := openTag + 3000
		if end > len(html) {
			end = len(html)
		}
		window := html[openTag:end]

		headline := extractRankedHeadline(window)
		summary := extractRankedSummary(window)
		ageLabel := extractAgeLabel(window)
		likes := extractLikes(window)
		bookmarks := extractBookmarks(window)

		s := Story{
			ClusterID: slug,
			Slug:      slug,
			Topic:     storyTopic,
			Surface:   "ranked",
			Rank:      rank,
			Headline:  htmlDecode(headline),
			Summary:   htmlDecode(summary),
			DiggURL:   diggURL,
			AgeLabel:  ageLabel,
			Likes:     likes,
			Bookmarks: bookmarks,
		}
		seen[diggURL] = true
		stories = append(stories, s)
	}

	return stories, nil
}

// parseHighlightArticle parses a single highlight-surface <article> block.
func parseHighlightArticle(art, defaultTopic string) *Story {
	// Extract data attributes from the opening article tag
	tagEnd := strings.Index(art, ">")
	if tagEnd < 0 {
		tagEnd = len(art)
	}
	openTag := art[:tagEnd]

	clusterID := extractAttr(openTag, "data-cluster-id")
	surface := extractAttr(openTag, "data-story-surface")
	if surface == "" {
		surface = "highlight"
	}
	topicAttr := extractAttr(openTag, "data-story-topic")
	if topicAttr == "" {
		topicAttr = defaultTopic
	}
	headlineShort := htmlDecode(extractAttr(openTag, "data-story-headline-short"))
	endorserCountStr := extractAttr(openTag, "data-story-endorser-count")
	endorserCount, _ := strconv.Atoi(endorserCountStr)

	// Extract headline from <h3>
	headline := htmlDecode(extractTagContent(art, "h3"))

	// Extract primary anchor href to build digg URL
	anchorRe := regexp.MustCompile(`<a\s[^>]*href="(/[^"]+)"`)
	anchorMatches := anchorRe.FindAllStringSubmatch(art, -1)
	diggPath := ""
	for _, am := range anchorMatches {
		p := am[1]
		// Skip external links, social, nav
		if strings.HasPrefix(p, "/"+topicAttr+"/") && !strings.Contains(p, "?") {
			diggPath = p
			break
		}
	}
	if diggPath == "" && len(anchorMatches) > 0 {
		diggPath = anchorMatches[0][1]
	}

	diggURL := ""
	if diggPath != "" {
		diggURL = "https://digg.com" + diggPath
	}

	// Extract endorser handles/names from avatar links. Image attribute order
	// is not guaranteed (src may come before or after alt), so capture the
	// anchor + nested <img> block, then pull attributes independently.
	var endorsers []Endorser
	seenHandles := map[string]bool{}
	endorserBlockRe := regexp.MustCompile(`<a\s[^>]*href="/u/x/([^/"]+)"[^>]*>([\s\S]*?)</a>`)
	imgAltRe := regexp.MustCompile(`<img\s[^>]*?\balt="([^"]*)"`)
	imgSrcRe := regexp.MustCompile(`<img\s[^>]*?\bsrc="([^"]*)"`)
	for _, em := range endorserBlockRe.FindAllStringSubmatch(art, -1) {
		handleRaw := em[1]
		handle := strings.ToLower(handleRaw)
		if seenHandles[handle] {
			continue
		}
		seenHandles[handle] = true
		block := em[2]
		var name, avatarURL string
		if m := imgAltRe.FindStringSubmatch(block); m != nil {
			name = htmlDecode(m[1])
		}
		if m := imgSrcRe.FindStringSubmatch(block); m != nil {
			avatarURL = m[1]
		}
		// Skip if no name found (likely a non-endorser link to a user page,
		// e.g., bare anchor without avatar img).
		if name == "" {
			continue
		}
		endorsers = append(endorsers, Endorser{
			Handle:    handle,
			Name:      name,
			DiggURL:   "https://digg.com/u/x/" + handleRaw,
			XURL:      "https://x.com/" + handleRaw,
			AvatarURL: avatarURL,
		})
	}

	// Extract age label
	ageLabel := extractAgeLabel(art)
	likes := extractLikes(art)
	bookmarks := extractBookmarks(art)

	return &Story{
		ClusterID:     clusterID,
		Slug:          clusterID,
		Topic:         topicAttr,
		Surface:       surface,
		HeadlineShort: headlineShort,
		Headline:      headline,
		DiggURL:       diggURL,
		EndorserCount: endorserCount,
		AgeLabel:      ageLabel,
		Likes:         likes,
		Bookmarks:     bookmarks,
		Endorsers:     endorsers,
	}
}

// extractRankedHeadline finds the headline span inside a ranked story block.
func extractRankedHeadline(window string) string {
	// Look for <span class="font-semibold">...</span>
	re := regexp.MustCompile(`(?s)<span\s[^>]*class="[^"]*font-semibold[^"]*"[^>]*>(.*?)</span>`)
	if m := re.FindStringSubmatch(window); len(m) > 1 {
		return stripTags(m[1])
	}
	// Fallback: any h3 content
	return extractTagContent(window, "h3")
}

// extractRankedSummary finds the summary text in a ranked story block.
func extractRankedSummary(window string) string {
	// Look for <span class="...text-muted-foreground..."> — summary</span>
	re := regexp.MustCompile(`(?s)<span\s[^>]*class="[^"]*text-muted-foreground[^"]*"[^>]*>(.*?)</span>`)
	if m := re.FindStringSubmatch(window); len(m) > 1 {
		s := strings.TrimSpace(stripTags(m[1]))
		s = strings.TrimPrefix(s, "—")
		s = strings.TrimPrefix(s, "-")
		return strings.TrimSpace(s)
	}
	return ""
}

// extractAgeLabel finds an age label like "14h", "2d" in a block.
func extractAgeLabel(block string) string {
	// Look for data-testid="story-row-meta" block
	metaRe := regexp.MustCompile(`(?s)data-testid="story-row-meta"[^>]*>(.*?)</`)
	if m := metaRe.FindStringSubmatch(block); len(m) > 1 {
		text := stripTags(m[1])
		ageRe := regexp.MustCompile(`\b(\d+[hd])\b`)
		if am := ageRe.FindStringSubmatch(text); len(am) > 1 {
			return am[1]
		}
	}
	// Fallback: scan for age pattern anywhere
	ageRe := regexp.MustCompile(`\b(\d+[hd])\b`)
	if m := ageRe.FindStringSubmatch(block); len(m) > 1 {
		return m[1]
	}
	return ""
}

// extractLikes scans a block for a likes count near a heart icon.
// Finds the SVG closing tag (</svg>) after the heart class, then the first
// text node that looks like a count immediately following it.
func extractLikes(block string) int {
	return extractCountAfterIcon(block, "lucide-heart")
}

// extractBookmarks scans a block for a bookmark count near a bookmark icon.
func extractBookmarks(block string) int {
	return extractCountAfterIcon(block, "lucide-bookmark")
}

// extractCountAfterIcon finds an icon by class name, then extracts the count
// that immediately follows the closing </svg> tag.
func extractCountAfterIcon(block, iconClass string) int {
	iconIdx := strings.Index(block, iconClass)
	if iconIdx < 0 {
		return 0
	}
	// Find the closing </svg> after the icon
	rest := block[iconIdx:]
	svgEnd := strings.Index(rest, "</svg>")
	if svgEnd < 0 {
		// No closing tag — try self-closing
		svgEnd = strings.Index(rest, "/>")
		if svgEnd < 0 {
			return 0
		}
		rest = rest[svgEnd+2:]
	} else {
		rest = rest[svgEnd+6:]
	}
	// rest now starts just after the icon's closing tag
	// Take a small window and look for a count-like text node
	if len(rest) > 100 {
		rest = rest[:100]
	}
	// Match: optional whitespace, then digits/decimal/suffix before next tag
	re := regexp.MustCompile(`^\s*([\d.,]+[kKmMbB]?)`)
	if m := re.FindStringSubmatch(rest); len(m) > 1 {
		v := strings.TrimSpace(m[1])
		if v != "" {
			return parseCount(v)
		}
	}
	// Fallback: find first number in a text node >N within the window
	re2 := regexp.MustCompile(`>(\s*[\d.,]+[kKmMbB]?\s*)<`)
	matches := re2.FindAllStringSubmatch(rest, -1)
	for _, m := range matches {
		v := strings.TrimSpace(m[1])
		if v != "" && v != "0" {
			return parseCount(v)
		}
	}
	return 0
}

// ParseDetail extracts a single story from a /ai/{slug} HTML page.
// Uses JSON-LD primary, HTML fallback for fields JSON-LD doesn't cover.
func ParseDetail(html, slugOrClusterID string) (*Story, error) {
	// Determine topic from slugOrClusterID if it contains a slash
	topic := "ai"
	slug := slugOrClusterID

	// Extract topic from URL embedded in page if possible
	// Look for canonical link or og:url
	canonRe := regexp.MustCompile(`(?i)<link\s[^>]*rel="canonical"[^>]*href="([^"]+)"`)
	if m := canonRe.FindStringSubmatch(html); len(m) > 1 {
		parts := strings.Split(strings.TrimPrefix(m[1], "https://digg.com/"), "/")
		if len(parts) >= 2 {
			topic = parts[0]
			slug = parts[1]
		}
	}

	s := &Story{
		ClusterID: slug,
		Slug:      slug,
		Topic:     topic,
		DiggURL:   fmt.Sprintf("https://digg.com/%s/%s", topic, slug),
	}

	// Primary: parse JSON-LD blocks
	ldRe := regexp.MustCompile(`(?s)<script\s[^>]*type="application/ld\+json"[^>]*>(.*?)</script>`)
	ldMatches := ldRe.FindAllStringSubmatch(html, -1)
	for _, lm := range ldMatches {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(lm[1]), &obj); err != nil {
			continue
		}
		typeRaw, hasType := obj["@type"]
		if !hasType {
			continue
		}
		var typeStr string
		if err := json.Unmarshal(typeRaw, &typeStr); err != nil {
			continue
		}
		if typeStr != "NewsArticle" && typeStr != "Article" {
			continue
		}

		if raw, ok := obj["headline"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				s.Headline = v
			}
		}
		if raw, ok := obj["description"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				s.Summary = v
			}
		}
		if raw, ok := obj["url"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil && v != "" {
				s.DiggURL = v
			}
		}
		if raw, ok := obj["datePublished"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				s.DatePublished = v
			}
		}
		if raw, ok := obj["dateModified"]; ok {
			var v string
			if json.Unmarshal(raw, &v) == nil {
				s.DateModified = v
			}
		}

		// Parse authors
		if raw, ok := obj["author"]; ok {
			var authors []map[string]json.RawMessage
			if json.Unmarshal(raw, &authors) == nil {
				for _, a := range authors {
					var name string
					if nr, ok := a["name"]; ok {
						_ = json.Unmarshal(nr, &name)
					}
					handle := ""
					xURL := ""
					// sameAs is an array of URLs
					if sr, ok := a["sameAs"]; ok {
						var sameAs []string
						if json.Unmarshal(sr, &sameAs) == nil {
							for _, sa := range sameAs {
								if strings.Contains(sa, "x.com/") || strings.Contains(sa, "twitter.com/") {
									xURL = sa
									parts := strings.Split(strings.TrimRight(sa, "/"), "/")
									if len(parts) > 0 {
										handle = strings.ToLower(parts[len(parts)-1])
									}
								}
							}
						} else {
							// sameAs might be a single string
							var single string
							if json.Unmarshal(sr, &single) == nil {
								if strings.Contains(single, "x.com/") || strings.Contains(single, "twitter.com/") {
									xURL = single
									parts := strings.Split(strings.TrimRight(single, "/"), "/")
									if len(parts) > 0 {
										handle = strings.ToLower(parts[len(parts)-1])
									}
								}
							}
						}
					}
					s.Endorsers = append(s.Endorsers, Endorser{
						Handle:  handle,
						Name:    name,
						XURL:    xURL,
						DiggURL: "https://digg.com/u/x/" + handle,
					})
				}
			}
		}
		break // use first NewsArticle
	}

	// Fallback headline from <h1> or <title>
	if s.Headline == "" {
		if h := extractTagContent(html, "h1"); h != "" {
			s.Headline = htmlDecode(h)
		} else {
			title := extractTagContent(html, "title")
			title = strings.TrimSuffix(title, " | Digg")
			s.Headline = htmlDecode(title)
		}
	}

	// Source URL: first external non-digg href in the article body
	s.SourceURL = extractSourceURL(html)

	return s, nil
}

// extractSourceURL finds the canonical external source link for a story.
//
// Digg's story pages preload avatar/og-image assets via `<link rel="preload"
// as="image" href="...">` tags, which would otherwise match before the real
// source link. Two-pass strategy:
//  1. Prefer x.com/twitter.com URLs — most Digg AI stories source from a
//     single X post and that's the canonical reference.
//  2. Otherwise, return the first external href that isn't a CDN, infra
//     host, or static image asset.
func extractSourceURL(html string) string {
	skipDomains := []string{
		"digg.com", "clerk.", "vercel-storage", "vercel.app",
		"googletagmanager", "gstatic", "fonts.googleapis",
		"cdn.", "cloudflare", "analytics", "pbs.twimg.com",
	}
	imageExts := []string{".jpg", ".jpeg", ".png", ".webp", ".svg", ".gif", ".ico", ".avif"}
	hrefRe := regexp.MustCompile(`href="(https?://[^"]+)"`)
	matches := hrefRe.FindAllStringSubmatch(html, -1)

	// Pass 1: prefer x.com / twitter.com source links (excluding the publisher's
	// own X handle which can appear in the page footer).
	for _, m := range matches {
		u := m[1]
		ul := strings.ToLower(u)
		if (strings.Contains(ul, "x.com/") || strings.Contains(ul, "twitter.com/")) &&
			!strings.Contains(ul, "x.com/basic_in_") &&
			!strings.Contains(ul, "x.com/intent") &&
			!strings.Contains(ul, "x.com/share") {
			return u
		}
	}

	// Pass 2: first external non-asset link.
	for _, m := range matches {
		u := m[1]
		ul := strings.ToLower(u)
		skip := false
		for _, d := range skipDomains {
			if strings.Contains(ul, d) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for _, ext := range imageExts {
			if strings.HasSuffix(ul, ext) {
				skip = true
				break
			}
		}
		if !skip {
			return u
		}
	}
	return ""
}
