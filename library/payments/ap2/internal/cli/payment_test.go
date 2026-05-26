// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"ap2-pp-cli/internal/ap2"
)

// buildEnvelope constructs a minimal FinalizationEnvelope with the given
// intent ceiling and payment amount (both cents) for guard tests.
func buildEnvelope(t *testing.T, intentMax, paymentAmount int, intentCurrency, paymentCurrency string) ap2.FinalizationEnvelope {
	t.Helper()

	intentBody := ap2.IntentMandateBody{
		Description:    "test intent",
		MaxAmountCents: intentMax,
		Currency:       intentCurrency,
		ExpiresInHours: 1,
	}
	intentJSON, err := json.Marshal(intentBody)
	if err != nil {
		t.Fatalf("marshal intent body: %v", err)
	}

	paymentBody := ap2.PaymentMandateBody{
		PaymentHandler: "com.google.pay",
		AmountCents:    paymentAmount,
		Currency:       paymentCurrency,
		CartRef:        "cart_test",
	}
	paymentJSON, err := json.Marshal(paymentBody)
	if err != nil {
		t.Fatalf("marshal payment body: %v", err)
	}

	return ap2.FinalizationEnvelope{
		Version:        "1.0",
		Subject:        "test-agent",
		IntentMandate:  ap2.AP2Mandate{Type: "intent", Body: intentJSON},
		PaymentMandate: ap2.AP2Mandate{Type: "payment", Body: paymentJSON},
	}
}

func TestVerifyAmountCeiling_OK(t *testing.T) {
	env := buildEnvelope(t, 5000, 4999, "USD", "USD")
	if err := verifyAmountCeiling(env); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyAmountCeiling_Exceeded(t *testing.T) {
	env := buildEnvelope(t, 5000, 5001, "USD", "USD")
	err := verifyAmountCeiling(env)
	if err == nil {
		t.Fatal("expected ceiling-exceeded error")
	}
	if !strings.Contains(err.Error(), "exceeds intent mandate ceiling") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestVerifyAmountCeiling_CurrencyMismatch(t *testing.T) {
	env := buildEnvelope(t, 5000, 4000, "USD", "EUR")
	err := verifyAmountCeiling(env)
	if err == nil {
		t.Fatal("expected currency-mismatch error")
	}
	if !strings.Contains(err.Error(), "currency mismatch") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestVerifyAmountCeiling_ZeroCeiling(t *testing.T) {
	// MaxAmountCents=0 means "no ceiling" — large payment must pass.
	env := buildEnvelope(t, 0, 9_999_999, "USD", "USD")
	if err := verifyAmountCeiling(env); err != nil {
		t.Fatalf("unexpected error with zero ceiling: %v", err)
	}
}

func TestVerifyAmountCeiling_BadIntentBody(t *testing.T) {
	env := buildEnvelope(t, 1000, 500, "USD", "USD")
	env.IntentMandate.Body = json.RawMessage("not json")
	err := verifyAmountCeiling(env)
	if err == nil {
		t.Fatal("expected parse error on invalid intent body")
	}
	if !strings.Contains(err.Error(), "parsing intent mandate body") {
		t.Fatalf("unexpected error message: %v", err)
	}
}
