package digg

import (
	"strings"
	"testing"
)

// listingFixture is a minimal HTML excerpt representing an AI listing page
// with one highlight article and two ranked story anchors.
const listingFixture = `<!DOCTYPE html>
<html>
<body>
<article data-story-row="true" data-cluster-id="abc123" data-story-surface="highlight" data-story-topic="ai" data-story-endorser-count="5" data-story-headline-short="Short headline">
  <h3>Big AI Breakthrough Story</h3>
  <a href="/ai/abc123"><img src="/thumb.jpg" /></a>
  <a href="/u/x/researcher1"><img alt="Alice Smith" src="https://cdn.digg.com/avatar1.jpg" /></a>
  <a href="/u/x/researcher2"><img alt="Bob Jones" src="https://cdn.digg.com/avatar2.jpg" /></a>
  <span data-testid="story-row-meta">14h <svg class="lucide-heart"></svg>1.2k <svg class="lucide-bookmark"></svg>42</span>
</article>
<article data-story-row="true" data-cluster-id="def456" data-story-surface="highlight" data-story-topic="ai" data-story-endorser-count="3" data-story-headline-short="Second story short">
  <h3>Another Important AI Story</h3>
  <a href="/ai/def456"></a>
  <a href="/u/x/airesearcher"><img alt="Carol White" src="https://cdn.digg.com/avatar3.jpg" /></a>
  <span data-testid="story-row-meta">2h <svg class="lucide-heart"></svg>500 <svg class="lucide-bookmark"></svg>10</span>
</article>
<article data-story-row="true" data-cluster-id="ghi789" data-story-surface="top" data-story-topic="ai" data-story-endorser-count="2" data-story-headline-short="">
  <h3>Top AI Story</h3>
  <a href="/ai/ghi789"></a>
</article>
<div>
  <a href="/ai/3vb8kiry?rank=1">
    <span class="font-semibold">Ranked Story One Headline</span>
    <span class="text-muted-foreground"> — Summary of ranked story one</span>
    <span data-testid="story-row-meta">6h <svg class="lucide-heart"></svg>174.4k <svg class="lucide-bookmark"></svg>88</span>
  </a>
  <a href="/ai/4xc9ljqp?rank=2">
    <span class="font-semibold">Ranked Story Two Headline</span>
    <span class="text-muted-foreground"> — Summary of ranked story two</span>
    <span data-testid="story-row-meta">12h <svg class="lucide-heart"></svg>5.3M <svg class="lucide-bookmark"></svg>200</span>
  </a>
</div>
</body>
</html>`

const detailFixture = `<!DOCTYPE html>
<html>
<head>
<link rel="canonical" href="https://digg.com/ai/0aeiof5t" />
<title>Breaking AI News | Digg</title>
<script type="application/ld+json">
{
  "@context": "https://schema.org",
  "@type": "NewsArticle",
  "headline": "Major AI Model Released Today",
  "description": "A new frontier model was released with unprecedented capabilities.",
  "url": "https://digg.com/ai/0aeiof5t",
  "datePublished": "2026-05-19T10:00:00Z",
  "dateModified": "2026-05-19T11:00:00Z",
  "author": [
    {
      "name": "Jane Researcher",
      "sameAs": ["https://x.com/janeresearcher", "https://digg.com/u/x/janeresearcher"]
    }
  ]
}
</script>
</head>
<body>
<article>
  <h1>Major AI Model Released Today</h1>
  <p>Check out this <a href="https://techcrunch.com/2026/05/19/ai-model">original coverage</a> on TechCrunch.</p>
</article>
</body>
</html>`

func TestParseListing_ProducesStories(t *testing.T) {
	stories, err := ParseListing(listingFixture, "ai")
	if err != nil {
		t.Fatalf("ParseListing error: %v", err)
	}
	if len(stories) < 4 {
		t.Fatalf("expected >= 4 stories, got %d", len(stories))
	}

	// Verify first highlight story
	var highlight *Story
	for i := range stories {
		if stories[i].ClusterID == "abc123" {
			highlight = &stories[i]
			break
		}
	}
	if highlight == nil {
		t.Fatal("expected story with cluster_id abc123")
	}
	if highlight.Surface != "highlight" {
		t.Errorf("surface = %q, want highlight", highlight.Surface)
	}
	if highlight.Topic != "ai" {
		t.Errorf("topic = %q, want ai", highlight.Topic)
	}
	if !strings.Contains(highlight.Headline, "AI Breakthrough") {
		t.Errorf("headline = %q, expected to contain 'AI Breakthrough'", highlight.Headline)
	}
	if highlight.EndorserCount != 5 {
		t.Errorf("endorser_count = %d, want 5", highlight.EndorserCount)
	}
	if len(highlight.Endorsers) < 2 {
		t.Errorf("endorsers = %d, want >= 2", len(highlight.Endorsers))
	}

	// Verify ranked story exists
	var ranked *Story
	for i := range stories {
		if stories[i].Slug == "3vb8kiry" {
			ranked = &stories[i]
			break
		}
	}
	if ranked == nil {
		t.Fatal("expected ranked story with slug 3vb8kiry")
	}
	if ranked.Surface != "ranked" {
		t.Errorf("ranked surface = %q, want ranked", ranked.Surface)
	}
	if ranked.Rank != 1 {
		t.Errorf("rank = %d, want 1", ranked.Rank)
	}
	if ranked.Likes != 174400 {
		t.Errorf("likes = %d, want 174400", ranked.Likes)
	}
}

func TestParseDetail_ProducesHeadlineAndAuthor(t *testing.T) {
	story, err := ParseDetail(detailFixture, "0aeiof5t")
	if err != nil {
		t.Fatalf("ParseDetail error: %v", err)
	}
	if story == nil {
		t.Fatal("got nil story")
	}
	if story.Headline == "" {
		t.Error("expected non-empty headline")
	}
	if !strings.Contains(story.Headline, "Major AI Model") {
		t.Errorf("headline = %q, expected to contain 'Major AI Model'", story.Headline)
	}
	if len(story.Endorsers) < 1 {
		t.Errorf("expected >= 1 endorser, got %d", len(story.Endorsers))
	}
	if story.Endorsers[0].Handle != "janeresearcher" {
		t.Errorf("handle = %q, want janeresearcher", story.Endorsers[0].Handle)
	}
	if story.DatePublished == "" {
		t.Error("expected non-empty date_published")
	}
	if story.SourceURL == "" {
		t.Error("expected non-empty source_url")
	}
}

func TestParseCount(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"174", 174},
		{"1.2k", 1200},
		{"5.3M", 5_300_000},
		{"174.4k", 174_400},
		{"500", 500},
		{"0", 0},
		{"", 0},
		{"2.5B", 2_500_000_000},
	}
	for _, tt := range tests {
		got := parseCount(tt.input)
		if got != tt.want {
			t.Errorf("parseCount(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseListing_DeduplicatesURLs(t *testing.T) {
	// Same anchor repeated twice (image + headline pattern)
	html := `<article data-story-row="true" data-cluster-id="dup01" data-story-surface="highlight" data-story-topic="ai" data-story-endorser-count="0">
  <h3>Dedup Story</h3>
  <a href="/ai/dup01"><img src="/a.jpg"/></a>
  <a href="/ai/dup01">Read more</a>
</article>`
	stories, err := ParseListing(html, "ai")
	if err != nil {
		t.Fatalf("ParseListing error: %v", err)
	}
	count := 0
	for _, s := range stories {
		if s.ClusterID == "dup01" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("dedup: got %d entries for dup01, want 1", count)
	}
}

func TestParseDetail_FallbackHeadline(t *testing.T) {
	html := `<html><head><title>Fallback Title | Digg</title></head><body><h1>Fallback Title</h1></body></html>`
	story, err := ParseDetail(html, "testslug")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if story.Headline == "" {
		t.Error("expected non-empty fallback headline")
	}
}
