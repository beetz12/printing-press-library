package ap2

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// buildSignedEnvelope creates a fully-signed FinalizationEnvelope using the
// existing Build* helpers from mandate.go and inlines the signing step so
// this test has no dependency on sign.go (US-002).
func buildSignedEnvelope(t *testing.T, priv *ecdsa.PrivateKey) FinalizationEnvelope {
	t.Helper()
	subject := "test-agent"

	intentMandate := BuildIntentMandate(subject, IntentMandateBody{
		Description:    "Buy a durable dog rope toy under $30",
		MaxAmountCents: 3000,
		Currency:       "USD",
		ExpiresInHours: 24,
	})

	cart := &Cart{
		ID:       "cart-1",
		Merchant: "example.com",
		Currency: "USD",
		LineItems: []LineItem{
			{
				ID:       "li-1",
				Item:     Item{ID: "item-1", Title: "Dog Rope Toy", Price: 1999},
				Quantity: 1,
			},
		},
	}
	cartMandate := BuildCartMandate(subject, intentMandate.MandateID, cart, "tok_123", "https://example.com/checkout")

	// Extract subtotal from cart mandate body to build a consistent payment mandate.
	var cartBody CartMandateBody
	if err := json.Unmarshal(cartMandate.Body, &cartBody); err != nil {
		t.Fatalf("failed to unmarshal cart body: %v", err)
	}
	paymentMandate := BuildPaymentMandate(subject, cartMandate.MandateID, "com.google.pay", "tok_123", cartBody.Subtotal, "USD")

	// Sign each mandate: signing input is SHA-256 of the body_hash string.
	signMandate := func(m *AP2Mandate) {
		h := sha256.Sum256([]byte(m.BodyHash))
		sig, err := ecdsa.SignASN1(rand.Reader, priv, h[:])
		if err != nil {
			t.Fatalf("signing failed: %v", err)
		}
		m.Signature = base64.StdEncoding.EncodeToString(sig)
	}
	signMandate(&intentMandate)
	signMandate(&cartMandate)
	signMandate(&paymentMandate)

	return FinalizationEnvelope{
		Version:        "1.0",
		Subject:        subject,
		IntentMandate:  intentMandate,
		CartMandate:    cartMandate,
		PaymentMandate: paymentMandate,
		Merchant:       "example.com",
		CheckoutURL:    "https://example.com/checkout",
	}
}

func TestVerifyMandate_HappyPath(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	mandate := BuildIntentMandate("test-agent", IntentMandateBody{
		Description:    "test",
		MaxAmountCents: 1000,
		ExpiresInHours: 1,
	})
	h := sha256.Sum256([]byte(mandate.BodyHash))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, h[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	mandate.Signature = base64.StdEncoding.EncodeToString(sig)

	if err := VerifyMandate(&priv.PublicKey, mandate); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestVerifyMandate_BodyHashMismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	mandate := BuildIntentMandate("test-agent", IntentMandateBody{
		Description:    "test",
		MaxAmountCents: 1000,
		ExpiresInHours: 1,
	})
	// Tamper: set BodyHash to a wrong value (body stays unchanged).
	mandate.BodyHash = "0000000000000000000000000000000000000000000000000000000000000000"

	// Sign with the tampered BodyHash so signature check would pass, but body_hash check fires first.
	h := sha256.Sum256([]byte(mandate.BodyHash))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
	mandate.Signature = base64.StdEncoding.EncodeToString(sig)

	err := VerifyMandate(&priv.PublicKey, mandate)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected *VerifyError, got %T: %v", err, err)
	}
	if ve.Code != ErrBodyHashMismatch {
		t.Fatalf("expected ErrBodyHashMismatch, got %q", ve.Code)
	}
}

func TestVerifyMandate_SignatureInvalid(t *testing.T) {
	// Build with one key, sign with another.
	priv1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	priv2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	mandate := BuildIntentMandate("test-agent", IntentMandateBody{
		Description:    "test",
		MaxAmountCents: 1000,
		ExpiresInHours: 1,
	})
	// Sign with priv2 but verify against priv1's public key.
	h := sha256.Sum256([]byte(mandate.BodyHash))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv2, h[:])
	mandate.Signature = base64.StdEncoding.EncodeToString(sig)

	err := VerifyMandate(&priv1.PublicKey, mandate)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected *VerifyError, got %T: %v", err, err)
	}
	if ve.Code != ErrSignatureInvalid {
		t.Fatalf("expected ErrSignatureInvalid, got %q", ve.Code)
	}
}

func TestVerifyEnvelope_HappyPath(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	envelope := buildSignedEnvelope(t, priv)
	resolver := func(_ string) (*ecdsa.PublicKey, error) { return &priv.PublicKey, nil }

	if err := VerifyEnvelope(envelope, resolver); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
}

func TestVerifyEnvelope_ChainBroken(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	envelope := buildSignedEnvelope(t, priv)

	// Tamper: rewrite cart_mandate.body.intent_mandate_id.
	var cartBody CartMandateBody
	if err := json.Unmarshal(envelope.CartMandate.Body, &cartBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	cartBody.IntentRef = "intent_wrong-id"
	newBody, _ := json.Marshal(cartBody)
	envelope.CartMandate.Body = newBody
	// Recompute body hash and re-sign so chain check (not hash check) fires.
	sum := sha256.Sum256(newBody)
	envelope.CartMandate.BodyHash = encodeHex(sum[:])
	h := sha256.Sum256([]byte(envelope.CartMandate.BodyHash))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
	envelope.CartMandate.Signature = base64.StdEncoding.EncodeToString(sig)

	resolver := func(_ string) (*ecdsa.PublicKey, error) { return &priv.PublicKey, nil }
	err := VerifyEnvelope(envelope, resolver)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected *VerifyError, got %T: %v", err, err)
	}
	if ve.Code != ErrMandateChainBroken {
		t.Fatalf("expected ErrMandateChainBroken, got %q", ve.Code)
	}
}

func TestVerifyEnvelope_Expired(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	envelope := buildSignedEnvelope(t, priv)

	// Set intent mandate expires_at to 1 hour ago.
	envelope.IntentMandate.ExpiresAt = time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)

	resolver := func(_ string) (*ecdsa.PublicKey, error) { return &priv.PublicKey, nil }
	err := VerifyEnvelope(envelope, resolver)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected *VerifyError, got %T: %v", err, err)
	}
	if ve.Code != ErrExpiredMandate {
		t.Fatalf("expected ErrExpiredMandate, got %q", ve.Code)
	}
}

func TestVerifyEnvelope_AmountMismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	envelope := buildSignedEnvelope(t, priv)

	// Tamper: set payment amount to a different value than cart subtotal.
	var payBody PaymentMandateBody
	if err := json.Unmarshal(envelope.PaymentMandate.Body, &payBody); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	payBody.AmountCents = payBody.AmountCents + 1 // mismatch by 1 cent
	newBody, _ := json.Marshal(payBody)
	envelope.PaymentMandate.Body = newBody
	// Recompute body hash and re-sign so amount check (not hash check) fires.
	sum := sha256.Sum256(newBody)
	envelope.PaymentMandate.BodyHash = encodeHex(sum[:])
	h := sha256.Sum256([]byte(envelope.PaymentMandate.BodyHash))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, h[:])
	envelope.PaymentMandate.Signature = base64.StdEncoding.EncodeToString(sig)

	resolver := func(_ string) (*ecdsa.PublicKey, error) { return &priv.PublicKey, nil }
	err := VerifyEnvelope(envelope, resolver)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ve, ok := err.(*VerifyError)
	if !ok {
		t.Fatalf("expected *VerifyError, got %T: %v", err, err)
	}
	if ve.Code != ErrAmountMismatch {
		t.Fatalf("expected ErrAmountMismatch, got %q", ve.Code)
	}
}

// encodeHex is a local helper for tests to recompute body hashes.
func encodeHex(b []byte) string {
	const hextable = "0123456789abcdef"
	dst := make([]byte, len(b)*2)
	for i, v := range b {
		dst[i*2] = hextable[v>>4]
		dst[i*2+1] = hextable[v&0x0f]
	}
	return string(dst)
}
