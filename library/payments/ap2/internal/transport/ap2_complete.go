package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"
	"ap2-pp-cli/internal/ap2"
)

// DefaultProfileURL is the default UCP agent profile served at igvita.com.
// Per the UCP lesson: this goes in params.arguments.meta["ucp-agent"].profile
// of the JSON-RPC body — NEVER in an HTTP header.
const DefaultProfileURL = "https://www.igvita.com/ucp/profile.json"

// CompleteOpts configures a CompleteCheckout call.
type CompleteOpts struct {
	// MerchantMcpURL is the full MCP endpoint URL, e.g.
	// https://bark-food.myshopify.com/api/ucp/mcp. If empty, derived
	// from envelope.CheckoutURL host as https://<host>/api/ucp/mcp.
	MerchantMcpURL string

	// GooglePayToken is the real Google Pay token for --live mode.
	// In sandbox mode this is ignored; a stub string is sent instead.
	GooglePayToken string

	// ProfileURL is the UCP agent profile URL. Empty → DefaultProfileURL.
	ProfileURL string

	// Sandbox controls whether to make the actual network call.
	// true  → build the JSON-RPC body but do NOT POST it.
	// false → POST to MerchantMcpURL and parse the response.
	Sandbox bool

	// HTTPClient is the HTTP client used for live mode.
	// nil → http.DefaultClient with a 30-second timeout.
	HTTPClient *http.Client
}

// CompleteResult is returned by CompleteCheckout on success.
type CompleteResult struct {
	Status        string         `json:"status"`           // "sandbox_authorized" | "authorized" | "failed"
	TransactionID string         `json:"transaction_id"`
	Merchant      string         `json:"merchant"`
	AmountCents   int            `json:"amount_cents"`
	Currency      string         `json:"currency"`
	Mode          string         `json:"mode"`              // "sandbox" | "live"
	WouldPostTo   string         `json:"would_post_to,omitempty"` // mcp_url (sandbox only)
	Request       map[string]any `json:"request,omitempty"`       // the JSON-RPC body (sandbox only)
	Response      map[string]any `json:"response,omitempty"`      // merchant response (live only)
	Error         string         `json:"error,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

// CompleteCheckout consumes a SIGNED FinalizationEnvelope and either:
//   - sandbox=true  → builds the JSON-RPC complete_checkout body, returns it WITHOUT making the HTTP call
//   - sandbox=false → POSTs the body to opts.MerchantMcpURL and parses the response
//
// Pre-flight: callers must run ap2.VerifyEnvelope before calling CompleteCheckout.
// CompleteCheckout does NOT re-verify (separation of concerns; the cli command runs verify first).
func CompleteCheckout(ctx context.Context, envelope ap2.FinalizationEnvelope, opts CompleteOpts) (*CompleteResult, error) {
	profileURL := opts.ProfileURL
	if profileURL == "" {
		profileURL = DefaultProfileURL
	}

	mcpURL := opts.MerchantMcpURL
	if mcpURL == "" {
		mcpURL = deriveMcpURL(envelope.CheckoutURL)
	}

	// Parse amount and currency from payment_mandate body.
	amountCents, currency := parsePaymentMandateBody(envelope.PaymentMandate)

	// Build JSON-RPC envelope. NOTE: meta.ucp-agent.profile goes in the BODY,
	// not in any HTTP header. Header-based profile is a dead end (returns 422).
	// In sandbox mode use a stub so no real token is ever required.
	// In live mode use the resolved token (may be "" — the merchant will
	// reject with a clear error rather than receiving a misleading stub value).
	paymentToken := opts.GooglePayToken
	if opts.Sandbox {
		paymentToken = "sandbox-stub"
	}

	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "complete_checkout",
			"arguments": map[string]any{
				"meta": map[string]any{
					"ucp-agent":       map[string]any{"profile": profileURL},
					"idempotency-key": uuid.NewString(),
				},
				"cart_token":      envelope.MerchantCartToken,
				"payment_mandate": envelope.PaymentMandate,
				"cart_mandate":    envelope.CartMandate,
				"intent_mandate":  envelope.IntentMandate,
				"payment_token":   paymentToken,
			},
		},
	}

	if opts.Sandbox {
		return &CompleteResult{
			Status:        "sandbox_authorized",
			TransactionID: "sandbox-" + uuid.NewString(),
			Merchant:      envelope.Merchant,
			AmountCents:   amountCents,
			Currency:      currency,
			Mode:          "sandbox",
			WouldPostTo:   mcpURL,
			Request:       body,
			CreatedAt:     time.Now(),
		}, nil
	}

	// Live mode: POST to merchant MCP endpoint.
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON-RPC body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ap2-pp-cli/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST %s: %w", mcpURL, err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		result := &CompleteResult{
			Status:        "failed",
			TransactionID: "live-" + uuid.NewString(),
			Merchant:      envelope.Merchant,
			AmountCents:   amountCents,
			Currency:      currency,
			Mode:          "live",
			Error:         fmt.Sprintf("merchant MCP returned HTTP %d: %s", resp.StatusCode, truncate(string(respBytes), 300)),
			CreatedAt:     time.Now(),
		}
		return result, fmt.Errorf("merchant MCP returned HTTP %d", resp.StatusCode)
	}

	var rpcResp map[string]any
	if err := json.Unmarshal(respBytes, &rpcResp); err != nil {
		return nil, fmt.Errorf("parsing merchant response: %w", err)
	}

	txnID := "live-" + uuid.NewString()
	status := "authorized"

	// Try to extract status from response content.
	if result, ok := rpcResp["result"].(map[string]any); ok {
		if content, ok := result["content"].([]any); ok && len(content) > 0 {
			if item, ok := content[0].(map[string]any); ok {
				if text, ok := item["text"].(string); ok {
					var inner map[string]any
					if json.Unmarshal([]byte(text), &inner) == nil {
						if s, ok := inner["status"].(string); ok {
							status = s
						}
						if id, ok := inner["transaction_id"].(string); ok {
							txnID = id
						}
					}
				}
			}
		}
	}

	return &CompleteResult{
		Status:        status,
		TransactionID: txnID,
		Merchant:      envelope.Merchant,
		AmountCents:   amountCents,
		Currency:      currency,
		Mode:          "live",
		Response:      rpcResp,
		CreatedAt:     time.Now(),
	}, nil
}

// deriveMcpURL builds the MCP endpoint URL from the checkout URL host.
// "https://bark.co/checkout" → "https://bark.co/api/ucp/mcp"
func deriveMcpURL(checkoutURL string) string {
	u, err := url.Parse(checkoutURL)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://" + u.Host + "/api/ucp/mcp"
}

// parsePaymentMandateBody extracts amount_cents and currency from the payment mandate body.
func parsePaymentMandateBody(mandate ap2.AP2Mandate) (int, string) {
	var body struct {
		AmountCents int    `json:"amount_cents"`
		Currency    string `json:"currency"`
	}
	if err := json.Unmarshal(mandate.Body, &body); err != nil {
		return 0, "USD"
	}
	if body.Currency == "" {
		body.Currency = "USD"
	}
	return body.AmountCents, body.Currency
}

// truncate caps a string at n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

