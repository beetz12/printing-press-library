package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mvanhorn/printing-press-library/library/payments/ap2/internal/ap2"
)

// TestProbe_RequestShapeOK_InvalidToken asserts that a 422 with "invalid_payment_token"
// classifies as ProbeRequestShapeOK — the GOOD outcome.
func TestProbe_RequestShapeOK_InvalidToken(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_payment_token","message":"The payment token is not valid"}}`))
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-probe")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	if result.Classification != ProbeRequestShapeOK {
		t.Errorf("Classification = %q, want %q", result.Classification, ProbeRequestShapeOK)
	}
	if result.HTTPStatus != http.StatusUnprocessableEntity {
		t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusUnprocessableEntity)
	}
}

// TestProbe_RequestShapeBad asserts that a 422 with "invalid_request" classifies as
// ProbeRequestShapeBad.
func TestProbe_RequestShapeBad(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"missing required field"}}`))
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-probe")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	if result.Classification != ProbeRequestShapeBad {
		t.Errorf("Classification = %q, want %q", result.Classification, ProbeRequestShapeBad)
	}
}

// TestProbe_AgentNotAuthorized asserts that a 422 with "invalid_profile_url" classifies
// as ProbeAgentNotAuthorized — same pattern as bark.co's profile gate error.
func TestProbe_AgentNotAuthorized(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_profile_url","message":"Missing profile uri"}}`))
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-probe")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	if result.Classification != ProbeAgentNotAuthorized {
		t.Errorf("Classification = %q, want %q", result.Classification, ProbeAgentNotAuthorized)
	}
}

// TestProbe_MerchantUnreachable asserts that a 500 classifies as ProbeMerchantUnreachable.
func TestProbe_MerchantUnreachable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"internal_server_error"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-probe")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	if result.Classification != ProbeMerchantUnreachable {
		t.Errorf("Classification = %q, want %q", result.Classification, ProbeMerchantUnreachable)
	}
	if result.HTTPStatus != http.StatusInternalServerError {
		t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusInternalServerError)
	}
}

// TestProbe_UnknownAccepted asserts that a 200 (stub token accepted) classifies as
// ProbeUnknown with a recommendation about verifying the test environment.
func TestProbe_UnknownAccepted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":{"content":[{"type":"text","text":"{\"status\":\"authorized\"}"}]}}`))
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-probe")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}
	if result.Classification != ProbeUnknown {
		t.Errorf("Classification = %q, want %q", result.Classification, ProbeUnknown)
	}
	if result.HTTPStatus != http.StatusOK {
		t.Errorf("HTTPStatus = %d, want %d", result.HTTPStatus, http.StatusOK)
	}
	// Recommendation must warn about test environment.
	if !containsAny(result.Recommendation, "test environment", "stub token") {
		t.Errorf("Recommendation should mention test environment; got: %s", result.Recommendation)
	}
}

// TestProbe_RequestShapeIncludesPaymentMandate asserts that the RequestShape sent to the
// merchant includes params.arguments.payment_mandate.
func TestProbe_RequestShapeIncludesPaymentMandate(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture and echo back the request body so we can inspect it.
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":{"code":"invalid_payment_token"}}`))
	}))
	defer srv.Close()

	priv := mustGenerateKey(t)
	envelope := buildSignedEnvelope(t, priv, "probe-agent", 1999, "tok-shape")

	result, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: srv.URL,
		HTTPClient:     srv.Client(),
	})
	if err != nil {
		t.Fatalf("Probe returned unexpected error: %v", err)
	}

	// Navigate: params.arguments.payment_mandate
	params, ok := result.RequestShape["params"].(map[string]any)
	if !ok {
		t.Fatal("RequestShape.params must be map[string]any")
	}
	arguments, ok := params["arguments"].(map[string]any)
	if !ok {
		t.Fatal("RequestShape.params.arguments must be map[string]any")
	}
	if _, ok := arguments["payment_mandate"]; !ok {
		t.Error("RequestShape.params.arguments.payment_mandate must be present")
	}

	// Also assert stub token is used (not a real token).
	tokenVal, _ := arguments["payment_token"].(string)
	if tokenVal != DefaultProbeTokenStub {
		t.Errorf("payment_token = %q, want stub %q", tokenVal, DefaultProbeTokenStub)
	}
}

// TestProbe_NoMcpURL_Error asserts that an empty MerchantMcpURL with no derivable URL
// from the envelope causes a pre-request error return.
func TestProbe_NoMcpURL_Error(t *testing.T) {
	t.Parallel()

	// Build a minimal envelope with no CheckoutURL (so deriveMcpURL returns empty).
	envelope := ap2.FinalizationEnvelope{
		Version:  "1.0",
		Subject:  "probe-agent",
		Merchant: "test.merchant.io",
		// No CheckoutURL — deriveMcpURL will return "".
	}

	_, err := Probe(context.Background(), envelope, ProbeOpts{
		MerchantMcpURL: "", // empty — should cause pre-request error
	})
	if err == nil {
		t.Fatal("expected pre-request error for missing MCP URL, got nil")
	}
}
