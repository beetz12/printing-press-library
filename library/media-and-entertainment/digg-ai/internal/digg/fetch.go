// Package digg provides HTML parsers and a fetcher for digg.com pages.
package digg

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mvanhorn/printing-press-library/library/media-and-entertainment/digg-ai/internal/cliutil"
)

const baseURL = "https://digg.com"

// FetchPage fetches an HTML page from digg.com using the given path.
// Uses a browser-like User-Agent to avoid bot detection.
//
// Rate-limit handling: surfaces HTTP 429 as a typed *cliutil.RateLimitError
// so callers can distinguish throttling from other transport errors
// (empty-on-throttle would corrupt downstream sync state).
func FetchPage(httpClient *http.Client, path string) (string, error) {
	return FetchPageWithLimiter(httpClient, nil, path)
}

// FetchPageWithLimiter is FetchPage with an explicit AdaptiveLimiter for
// callers that sync multiple pages in series. Pass nil to disable pacing.
func FetchPageWithLimiter(httpClient *http.Client, limiter *cliutil.AdaptiveLimiter, path string) (string, error) {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	limiter.Wait()
	url := baseURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		limiter.OnRateLimit()
		body, _ := io.ReadAll(resp.Body)
		return "", &cliutil.RateLimitError{
			URL:        url,
			RetryAfter: cliutil.RetryAfter(resp),
			Body:       string(body),
		}
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, url)
	}

	limiter.OnSuccess()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading body from %s: %w", url, err)
	}
	return string(body), nil
}
