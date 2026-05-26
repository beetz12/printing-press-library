package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ap2-pp-cli/internal/ap2"
)

// mustGenerateKey creates an ephemeral ECDSA-P256 key for tests.
func mustGenerateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating ECDSA key: %v", err)
	}
	return priv
}

// buildSignedEnvelope builds a minimal FinalizationEnvelope and signs it.
func buildSignedEnvelope(t *testing.T, priv *ecdsa.PrivateKey, subject string, amountCents int, cartToken string) ap2.FinalizationEnvelope {
	t.Helper()

	intentBody := ap2.IntentMandateBody{
		Description:    "Buy a durable dog rope toy",
		MaxAmountCents: amountCents + 500,
		Currency:       "USD",
		ExpiresInHours: 24,
	}
	intent := ap2.BuildIntentMandate(subject, intentBody)

	cart := &ap2.Cart{
		Merchant:  "test.merchant.io",
		Currency:  "USD",
		LineItems: []ap2.LineItem{{ID: "li-1", Item: ap2.Item{ID: "prod-1", Title: "Dog Rope Toy", Price: amountCents}, Quantity: 1}},
	}
	cartMandate := ap2.BuildCartMandate(subject, intent.MandateID, cart, cartToken, "https://test.merchant.io/checkout")
	paymentMandate := ap2.BuildPaymentMandate(subject, cartMandate.MandateID, "com.google.pay", cartToken, amountCents, "USD")

	envelope := ap2.FinalizationEnvelope{
		Version:           "1.0",
		Subject:           subject,
		IntentMandate:     intent,
		CartMandate:       cartMandate,
		PaymentMandate:    paymentMandate,
		Merchant:          "test.merchant.io",
		MerchantCartToken: cartToken,
		CheckoutURL:       "https://test.merchant.io/checkout",
	}

	if err := ap2.SignEnvelope(priv, &envelope); err != nil {
		t.Fatalf("SignEnvelope: %v", err)
	}
	return envelope
}

// TestCompleteCheckout_SandboxNoNetwork asserts sandbox mode makes NO network call.
// The failingTransport fails the test if any HTTP request is attempted.
func TestCompleteCheckout_SandboxNoNetwork(t *testing.T) {
	t.Parallel()
	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "test-agent", 1799, "tok-test")

	result, err := CompleteCheckout(context.Background(), envelope, CompleteOpts{
		MerchantMcpURL: "https://should-not-be-reached.example.com/api/ucp/mcp",
		Sandbox:        true,
		HTTPClient:     &http.Client{Transport: failingTransport{t}},
	})
	if err != nil {
		t.Fatalf("CompleteCheckout sandbox: unexpected error: %v", err)
	}
	if result.Status != "sandbox_authorized" {
		t.Errorf("status = %q, want %q", result.Status, "sandbox_authorized")
	}
	if result.Request == nil {
		t.Error("sandbox result.Request must be non-nil")
	}
	if result.Response != nil {
		t.Error("sandbox result.Response must be nil (no network call made)")
	}
	if result.Mode != "sandbox" {
		t.Errorf("mode = %q, want %q", result.Mode, "sandbox")
	}
}

// TestCompleteCheckout_SandboxRequestShape asserts the built request body has
// the correct structure, especially the profile URL placement and cart_token.
func TestCompleteCheckout_SandboxRequestShape(t *testing.T) {
	t.Parallel()
	priv := mustGenerateKey(t)
	cartToken := "test-token-xyz"
	envelope := buildSignedEnvelope(t, priv, "test-agent", 999, cartToken)

	result, err := CompleteCheckout(context.Background(), envelope, CompleteOpts{
		Sandbox:    true,
		HTTPClient: &http.Client{Transport: failingTransport{t}},
	})
	if err != nil {
		t.Fatalf("CompleteCheckout: %v", err)
	}

	body := result.Request
	if body == nil {
		t.Fatal("Request map must not be nil")
	}

	// Navigate: params.arguments.meta["ucp-agent"].profile
	params, ok := body["params"].(map[string]any)
	if !ok {
		t.Fatal("params must be map[string]any")
	}
	arguments, ok := params["arguments"].(map[string]any)
	if !ok {
		t.Fatal("params.arguments must be map[string]any")
	}
	meta, ok := arguments["meta"].(map[string]any)
	if !ok {
		t.Fatal("params.arguments.meta must be map[string]any")
	}
	ucpAgent, ok := meta["ucp-agent"].(map[string]any)
	if !ok {
		t.Fatal("params.arguments.meta['ucp-agent'] must be map[string]any")
	}
	profile, ok := ucpAgent["profile"].(string)
	if !ok || profile == "" {
		t.Fatalf("params.arguments.meta['ucp-agent'].profile must be a non-empty string, got %v", ucpAgent["profile"])
	}
	if profile != DefaultProfileURL {
		t.Errorf("profile = %q, want %q", profile, DefaultProfileURL)
	}

	// cart_token must match envelope.MerchantCartToken.
	cartTokenGot, ok := arguments["cart_token"].(string)
	if !ok || cartTokenGot != cartToken {
		t.Errorf("cart_token = %q, want %q", cartTokenGot, cartToken)
	}

	// payment_mandate must be present (non-nil).
	if _, ok := arguments["payment_mandate"]; !ok {
		t.Error("payment_mandate must be present in arguments")
	}

	// Method must be tools/call with name=complete_checkout.
	if body["method"] != "tools/call" {
		t.Errorf("method = %v, want 'tools/call'", body["method"])
	}
	name, ok := params["name"].(string)
	if !ok || name != "complete_checkout" {
		t.Errorf("params.name = %q, want %q", name, "complete_checkout")
	}
}

// TestCompleteCheckout_LiveSuccess tests the live path with a mock server
// returning 200 and a valid MCP response.
func TestCompleteCheckout_LiveSuccess(t *testing.T) {
	t.Parallel()

	merchantResp := map[string]any{
		"result": map[string]any{
			"content": []any{
				map[string]any{
					"type": "text",
					"text": `{"status":"authorized","transaction_id":"merchant-xyz"}`,
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(merchantResp)
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "test-agent", 1500, "tok-live")

	result, err := CompleteCheckout(context.Background(), envelope, CompleteOpts{
		MerchantMcpURL: srv.URL,
		GooglePayToken: "gpay-real-token",
		Sandbox:        false,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("CompleteCheckout live: unexpected error: %v", err)
	}
	if result.Status != "authorized" {
		t.Errorf("status = %q, want %q", result.Status, "authorized")
	}
	if result.Mode != "live" {
		t.Errorf("mode = %q, want %q", result.Mode, "live")
	}
}

// TestCompleteCheckout_LiveError tests that a 422 from the merchant returns an error.
func TestCompleteCheckout_LiveError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unprocessable_entity"}`, http.StatusUnprocessableEntity)
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "test-agent", 999, "tok-err")

	_, err := CompleteCheckout(context.Background(), envelope, CompleteOpts{
		MerchantMcpURL: srv.URL,
		Sandbox:        false,
		HTTPClient:     srv.Client(),
	})
	if err == nil {
		t.Fatal("expected error for HTTP 422, got nil")
	}
}

// TestCompleteCheckout_AmountFromPayment tests that AmountCents is correctly
// parsed from the payment_mandate body.
func TestCompleteCheckout_AmountFromPayment(t *testing.T) {
	t.Parallel()
	priv := mustGenerateKey(t)
	wantAmount := 1799
	envelope := buildSignedEnvelope(t, priv, "test-agent", wantAmount, "tok-amount")

	result, err := CompleteCheckout(context.Background(), envelope, CompleteOpts{
		Sandbox:    true,
		HTTPClient: &http.Client{Transport: failingTransport{t}},
	})
	if err != nil {
		t.Fatalf("CompleteCheckout: %v", err)
	}
	if result.AmountCents != wantAmount {
		t.Errorf("AmountCents = %d, want %d", result.AmountCents, wantAmount)
	}
}
