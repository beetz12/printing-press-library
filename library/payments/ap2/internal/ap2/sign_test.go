package ap2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"testing"
)

func mustGenerateKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	return priv
}

func buildTestEnvelope(t *testing.T, subject string) FinalizationEnvelope {
	t.Helper()
	intentBody := IntentMandateBody{
		Description:    "Buy a durable dog rope toy under $30",
		MaxAmountCents: 3000,
		Currency:       "USD",
		ExpiresInHours: 24,
	}
	intent := BuildIntentMandate(subject, intentBody)

	cart := &Cart{
		Merchant:  "bark.co",
		Currency:  "USD",
		LineItems: []LineItem{{ID: "li-1", Item: Item{ID: "prod-1", Title: "Dog Rope Toy", Price: 799}, Quantity: 1}},
	}
	cartMandate := BuildCartMandate(subject, intent.MandateID, cart, "tok-abc", "https://bark.co/checkout")
	paymentMandate := BuildPaymentMandate(subject, cartMandate.MandateID, "com.google.pay", "tok-abc", 799, "USD")

	return FinalizationEnvelope{
		Version:        "1.0",
		Subject:        subject,
		IntentMandate:  intent,
		CartMandate:    cartMandate,
		PaymentMandate: paymentMandate,
		Merchant:       "bark.co",
		CheckoutURL:    "https://bark.co/checkout",
	}
}

// TestSignMandate_RoundTrip: sign a single mandate and verify it with the matching public key.
func TestSignMandate_RoundTrip(t *testing.T) {
	priv := mustGenerateKey(t)
	intentBody := IntentMandateBody{
		Description:    "Buy a durable dog rope toy under $30",
		MaxAmountCents: 3000,
		Currency:       "USD",
		ExpiresInHours: 24,
	}
	mandate := BuildIntentMandate("test-agent", intentBody)

	if err := SignMandate(priv, &mandate); err != nil {
		t.Fatalf("SignMandate: %v", err)
	}
	if mandate.Signature == "" {
		t.Fatal("expected non-empty signature after SignMandate")
	}

	if err := VerifyMandate(&priv.PublicKey, mandate); err != nil {
		t.Fatalf("VerifyMandate after sign: %v", err)
	}
}

// TestSignMandate_BodyTamperRejected: tamper body without recomputing BodyHash → sign rejects.
func TestSignMandate_BodyTamperRejected(t *testing.T) {
	priv := mustGenerateKey(t)
	intentBody := IntentMandateBody{
		Description:    "Buy a durable dog rope toy under $30",
		MaxAmountCents: 3000,
		Currency:       "USD",
		ExpiresInHours: 24,
	}
	mandate := BuildIntentMandate("test-agent", intentBody)

	// Tamper one byte of the body without recomputing BodyHash.
	tampered := make([]byte, len(mandate.Body))
	copy(tampered, mandate.Body)
	// Flip a byte deep in the JSON (avoid first/last byte to keep valid JSON structure).
	if len(tampered) > 5 {
		tampered[5] ^= 0x01
	}
	mandate.Body = json.RawMessage(tampered)

	err := SignMandate(priv, &mandate)
	if err == nil {
		t.Fatal("expected error for tampered body, got nil")
	}
	if !containsSubstring(err.Error(), "body hash mismatch") {
		t.Fatalf("expected 'body hash mismatch' in error, got: %v", err)
	}
}

// TestSignEnvelope_RoundTrip: sign full envelope, verify it with VerifyEnvelope.
func TestSignEnvelope_RoundTrip(t *testing.T) {
	priv := mustGenerateKey(t)
	envelope := buildTestEnvelope(t, "test-agent")

	if err := SignEnvelope(priv, &envelope); err != nil {
		t.Fatalf("SignEnvelope: %v", err)
	}

	resolver := func(_ string) (*ecdsa.PublicKey, error) {
		return &priv.PublicKey, nil
	}

	if err := VerifyEnvelope(envelope, resolver); err != nil {
		t.Fatalf("VerifyEnvelope after SignEnvelope: %v", err)
	}
}

// TestSignMandate_NotDeterministic documents that ECDSA signatures are randomized.
// Same input produces different valid signatures — we test that each verifies, not that they match.
//
// Note: ECDSA signatures are randomized; same input produces different valid sigs.
// We only test that each sig verifies. RFC 6979 deterministic mode would require an
// explicit library choice; Go's crypto/ecdsa uses crypto/rand by default.
func TestSignMandate_NotDeterministic(t *testing.T) {
	priv := mustGenerateKey(t)
	intentBody := IntentMandateBody{
		Description:    "Buy a durable dog rope toy under $30",
		MaxAmountCents: 3000,
		Currency:       "USD",
		ExpiresInHours: 24,
	}

	m1 := BuildIntentMandate("test-agent", intentBody)
	m2 := BuildIntentMandate("test-agent", intentBody)

	if err := SignMandate(priv, &m1); err != nil {
		t.Fatalf("sign m1: %v", err)
	}
	if err := SignMandate(priv, &m2); err != nil {
		t.Fatalf("sign m2: %v", err)
	}

	// Both must verify even though the mandate IDs and timestamps differ.
	if err := VerifyMandate(&priv.PublicKey, m1); err != nil {
		t.Fatalf("verify m1: %v", err)
	}
	if err := VerifyMandate(&priv.PublicKey, m2); err != nil {
		t.Fatalf("verify m2: %v", err)
	}
}

// TestSignEnvelope_VerifyAcrossMandates: sign envelope, verify each mandate individually.
func TestSignEnvelope_VerifyAcrossMandates(t *testing.T) {
	priv := mustGenerateKey(t)
	envelope := buildTestEnvelope(t, "test-agent")

	if err := SignEnvelope(priv, &envelope); err != nil {
		t.Fatalf("SignEnvelope: %v", err)
	}

	for _, m := range []struct {
		name    string
		mandate AP2Mandate
	}{
		{"intent", envelope.IntentMandate},
		{"cart", envelope.CartMandate},
		{"payment", envelope.PaymentMandate},
	} {
		if err := VerifyMandate(&priv.PublicKey, m.mandate); err != nil {
			t.Fatalf("VerifyMandate(%s): %v", m.name, err)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
