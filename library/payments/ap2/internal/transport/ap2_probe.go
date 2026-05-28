package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
)

// DefaultProbeTokenStub is the token sent to the merchant during a probe.
// It is intentionally invalid so the merchant will reject it for "invalid token",
// which is the GOOD outcome (proves request shape is accepted).
const DefaultProbeTokenStub = "stub-invalid-token-for-probe"

// ProbeClassification is the typed outcome of a probe call.
type ProbeClassification string

const (
	// ProbeRequestShapeOK means the merchant accepted our request shape but rejected
	// the stub token — this is the GOOD outcome. Real token needed for end-to-end.
	ProbeRequestShapeOK ProbeClassification = "request_shape_ok"

	// ProbeRequestShapeBad means the merchant rejected the request structure itself
	// (missing fields, wrong format, etc.).
	ProbeRequestShapeBad ProbeClassification = "request_shape_bad"

	// ProbeAgentNotAuthorized means the profile/delegation check failed.
	ProbeAgentNotAuthorized ProbeClassification = "agent_not_authorized"

	// ProbeMerchantUnreachable means the merchant returned 5xx or the transport failed.
	ProbeMerchantUnreachable ProbeClassification = "merchant_unreachable"

	// ProbeUnknown means the response didn't match any known pattern.
	ProbeUnknown ProbeClassification = "unknown"
)

// ProbeOpts configures a Probe call.
type ProbeOpts struct {
	// MerchantMcpURL is the full MCP endpoint URL, e.g.
	// https://bark.co/api/ucp/mcp. If empty, derived from envelope.CheckoutURL host.
	MerchantMcpURL string

	// TokenStub is the deliberately-invalid token sent to the merchant.
	// Empty → DefaultProbeTokenStub.
	TokenStub string

	// ProfileURL is the UCP agent profile URL.
	// Empty → DefaultProfileURL.
	ProfileURL string

	// HTTPClient is the HTTP client used for the probe POST.
	// nil → http.DefaultClient with 30-second timeout.
	HTTPClient *http.Client
}

// ProbeResult is returned by Probe with the classification and full diagnostic info.
type ProbeResult struct {
	Classification ProbeClassification `json:"classification"`
	HTTPStatus     int                 `json:"http_status"`
	MerchantError  string              `json:"merchant_error,omitempty"` // raw error text from merchant
	RequestShape   map[string]any      `json:"request_shape"`
	ResponseBody   string              `json:"response_body,omitempty"` // truncated to 1KB
	Recommendation string              `json:"recommendation"`
}

// Probe consumes a SIGNED envelope, fires a live POST with an invalid stub token,
// reads the merchant response, and classifies the result.
//
// Always returns a non-nil ProbeResult (even on transport errors).
// Returns an error only for pre-request failures (empty MCP URL, nil envelope fields).
// "Merchant rejected" is NOT an error — it populates Classification accordingly.
//
// Callers must run ap2.VerifyEnvelope before calling Probe.
// Probe does NOT re-verify (separation of concerns; cli command runs verify first).
func Probe(ctx context.Context, envelope ap2.FinalizationEnvelope, opts ProbeOpts) (*ProbeResult, error) {
	profileURL := opts.ProfileURL
	if profileURL == "" {
		profileURL = DefaultProfileURL
	}

	mcpURL := opts.MerchantMcpURL
	if mcpURL == "" {
		// Prefer the signed CartMandateBody.CheckoutURL so a tampered top-level
		// envelope.checkout_url cannot redirect the probe payload to an attacker
		// endpoint. Falls back to the unsigned field only when the cart body
		// cannot be parsed (should not happen on a verified envelope).
		var cartBody ap2.CartMandateBody
		if jerr := json.Unmarshal(envelope.CartMandate.Body, &cartBody); jerr == nil && cartBody.CheckoutURL != "" {
			mcpURL = deriveMcpURL(cartBody.CheckoutURL)
		} else {
			mcpURL = deriveMcpURL(envelope.CheckoutURL)
		}
	}
	if mcpURL == "" {
		return nil, fmt.Errorf("no merchant MCP URL: set --merchant-mcp-url or ensure envelope.checkout_url is set")
	}

	tokenStub := opts.TokenStub
	if tokenStub == "" {
		tokenStub = DefaultProbeTokenStub
	}

	// Build the complete_checkout JSON-RPC body — same shape as CompleteCheckout,
	// but always uses the stub token regardless of sandbox/live flag.
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
				"payment_token":   tokenStub,
			},
		},
	}

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
		return &ProbeResult{
			Classification: ProbeMerchantUnreachable,
			HTTPStatus:     0,
			MerchantError:  err.Error(),
			RequestShape:   body,
			Recommendation: "Merchant unreachable (transport error). Retry later or verify endpoint URL.",
		}, nil
	}
	defer resp.Body.Close()

	const maxRespBody = 1 << 10 // 1KB
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, maxRespBody))
	respStr := string(respBytes)

	classification, recommendation := classify(resp.StatusCode, respStr)

	return &ProbeResult{
		Classification: classification,
		HTTPStatus:     resp.StatusCode,
		MerchantError:  extractMerchantError(respStr),
		RequestShape:   body,
		ResponseBody:   respStr,
		Recommendation: recommendation,
	}, nil
}

// classify determines the ProbeClassification from the HTTP status and response body.
func classify(status int, body string) (ProbeClassification, string) {
	lower := strings.ToLower(body)

	switch {
	case status >= 500:
		return ProbeMerchantUnreachable,
			fmt.Sprintf("Merchant unreachable (HTTP %d). Retry later or verify endpoint URL.", status)

	case status >= 400 && status < 500:
		switch {
		case containsAny(lower, "invalid_payment_token", "invalid_token", "payment_token_invalid"):
			return ProbeRequestShapeOK,
				"Request shape is correct. Real Google Pay token needed for end-to-end."

		case containsAny(lower, "agent_not_authorized", "missing_profile", "invalid_profile_url"):
			return ProbeAgentNotAuthorized,
				"Profile gate failed. Check meta.ucp-agent.profile URL — see https://www.igvita.com/ucp/profile.json."

		case containsAny(lower, "invalid_request", "validation_failed", "malformed"):
			return ProbeRequestShapeBad,
				"Merchant rejected request shape. Check: payment_mandate body fields, signature format, body_hash."

		default:
			return ProbeUnknown,
				"Response didn't match any known pattern. Inspect ResponseBody for details."
		}

	case status >= 200 && status < 300:
		return ProbeUnknown,
			"Merchant accepted stub token — verify your test environment. Response didn't match any known pattern. Inspect ResponseBody for details."

	default:
		return ProbeUnknown,
			"Response didn't match any known pattern. Inspect ResponseBody for details."
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// extractMerchantError attempts to pull a short error string from the response body.
// Returns raw body snippet if JSON parsing fails.
func extractMerchantError(body string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return truncate(body, 200)
	}
	// Try {"error": {"code": "...", "message": "..."}} shape.
	if errObj, ok := parsed["error"].(map[string]any); ok {
		code, _ := errObj["code"].(string)
		msg, _ := errObj["message"].(string)
		if code != "" && msg != "" {
			return fmt.Sprintf("%s: %s", code, msg)
		}
		if code != "" {
			return code
		}
		if msg != "" {
			return msg
		}
	}
	// Try {"error": "string"} shape.
	if errStr, ok := parsed["error"].(string); ok {
		return errStr
	}
	return truncate(body, 200)
}
