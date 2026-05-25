// Package ap2 implements AP2 (Agent Payments Protocol) mandate types and builders.
package ap2

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SignMandate signs a single mandate in place: fills mandate.Signature.
// Signing input: SHA-256 of the mandate.BodyHash STRING bytes (the hex-encoded body hash).
// Signature: base64.StdEncoding of ecdsa.SignASN1(rand.Reader, priv, hash[:]).
// Pre-check: if mandate.Body is non-empty, recompute SHA-256 of body bytes and verify
// it matches mandate.BodyHash hex; reject with a clear error on mismatch.
// This prevents signing a tampered body.
func SignMandate(priv *ecdsa.PrivateKey, mandate *AP2Mandate) error {
	// Pre-check: verify body hash integrity if body is present.
	// Compact-normalize the body JSON before hashing so that pretty-printed
	// and compact representations produce the same hash (matches canonicalHash()).
	if len(mandate.Body) > 0 {
		normalized, nerr := json.Marshal(json.RawMessage(mandate.Body))
		if nerr != nil {
			return fmt.Errorf("normalizing body for mandate %s: %w", mandate.MandateID, nerr)
		}
		sum := sha256.Sum256(normalized)
		computed := hex.EncodeToString(sum[:])
		if computed != mandate.BodyHash {
			return fmt.Errorf("body hash mismatch for mandate %s: computed %s, stored %s",
				mandate.MandateID, computed, mandate.BodyHash)
		}
	}

	// Signing input is SHA-256 of the body_hash string (the hex-encoded body hash).
	hashInput := sha256.Sum256([]byte(mandate.BodyHash))
	sig, err := ecdsa.SignASN1(rand.Reader, priv, hashInput[:])
	if err != nil {
		return fmt.Errorf("signing mandate %s: %w", mandate.MandateID, err)
	}

	mandate.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// SignEnvelope signs all 3 mandates (intent, cart, payment) in-place with the same key.
// Does NOT modify envelope.Subject (caller is responsible for setting subject before sign,
// since subject identifies which key the resolver will use during verify).
func SignEnvelope(priv *ecdsa.PrivateKey, envelope *FinalizationEnvelope) error {
	if err := SignMandate(priv, &envelope.IntentMandate); err != nil {
		return fmt.Errorf("signing intent mandate: %w", err)
	}
	if err := SignMandate(priv, &envelope.CartMandate); err != nil {
		return fmt.Errorf("signing cart mandate: %w", err)
	}
	if err := SignMandate(priv, &envelope.PaymentMandate); err != nil {
		return fmt.Errorf("signing payment mandate: %w", err)
	}
	return nil
}
