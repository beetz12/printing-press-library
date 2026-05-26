// Package ap2 implements AP2 (Agent Payments Protocol) mandate types and builders.
package ap2

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// ErrCode is the typed error code surface for verification failures.
type ErrCode string

const (
	ErrBodyHashMismatch   ErrCode = "body_hash_mismatch"
	ErrSignatureInvalid   ErrCode = "signature_invalid"
	ErrExpiredMandate     ErrCode = "expired_mandate"
	ErrMandateChainBroken ErrCode = "mandate_chain_broken"
	ErrSubjectKeyNotFound ErrCode = "subject_key_not_found"
	ErrAmountMismatch     ErrCode = "amount_mismatch" // payment.amount != cart.subtotal
)

// VerifyError carries a typed code, message, and the offending mandate ID.
type VerifyError struct {
	Code      ErrCode
	Message   string
	MandateID string
}

func (e *VerifyError) Error() string {
	if e.MandateID != "" {
		return fmt.Sprintf("%s [%s]: %s", e.Code, e.MandateID, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// VerifyMandate checks:
//  1. mandate.body_hash equals sha256(mandate.body) hex
//  2. mandate.signature decodes from base64 to ASN.1 DER
//  3. ecdsa.VerifyASN1(pub, sha256([]byte(mandate.body_hash)), sig) returns true
//
// Returns nil on success, *VerifyError on any failure.
// IMPORTANT: signing input is SHA-256 of the body_hash STRING (the hex-encoded body hash),
// not the raw body. Matches the contract in the handoff and US-002.
func VerifyMandate(pub *ecdsa.PublicKey, mandate AP2Mandate) error {
	// Step 1: recompute body hash from the canonical (compact) JSON body bytes.
	// Compact-normalize first so that pretty-printed and compact representations
	// of the same JSON value produce the same hash. This matches canonicalHash().
	bodyBytes, err := json.Marshal(json.RawMessage(mandate.Body))
	if err != nil {
		return &VerifyError{
			Code:      ErrBodyHashMismatch,
			Message:   fmt.Sprintf("failed to normalize body JSON: %v", err),
			MandateID: mandate.MandateID,
		}
	}
	sum := sha256.Sum256(bodyBytes)
	computed := hex.EncodeToString(sum[:])
	if computed != mandate.BodyHash {
		return &VerifyError{
			Code:      ErrBodyHashMismatch,
			Message:   fmt.Sprintf("computed %s, stored %s", computed, mandate.BodyHash),
			MandateID: mandate.MandateID,
		}
	}

	// Step 2 & 3: verify signature if public key provided.
	if pub != nil {
		if mandate.Signature == "" {
			return &VerifyError{
				Code:      ErrSignatureInvalid,
				Message:   "signature is empty",
				MandateID: mandate.MandateID,
			}
		}
		sig, err := base64.StdEncoding.DecodeString(mandate.Signature)
		if err != nil {
			return &VerifyError{
				Code:      ErrSignatureInvalid,
				Message:   fmt.Sprintf("base64 decode failed: %v", err),
				MandateID: mandate.MandateID,
			}
		}
		// Signing input is SHA-256 of the body_hash string.
		hashInput := sha256.Sum256([]byte(mandate.BodyHash))
		if !ecdsa.VerifyASN1(pub, hashInput[:], sig) {
			return &VerifyError{
				Code:      ErrSignatureInvalid,
				Message:   "ECDSA signature verification failed",
				MandateID: mandate.MandateID,
			}
		}
	}

	return nil
}

// VerifyEnvelope validates all 3 mandates AND the chain:
//   - cart_mandate.body.intent_mandate_id  == intent_mandate.mandate_id
//   - payment_mandate.body.cart_mandate_id == cart_mandate.mandate_id
//   - payment_mandate.body.amount_cents    == cart_mandate.body.subtotal_cents
//   - intent_mandate.expires_at must be in the future (if set)
//
// pubResolver maps a mandate.Subject -> public key. It is called once per
// mandate using that mandate's own Subject, so v0.2 envelopes with a
// user-signed intent (subject=user-<uuid>) plus agent-signed cart/payment
// (subject=agent-<uuid>) resolve to different keys per mandate. v0.1
// envelopes where envelope.Subject == every mandate.Subject still work:
// each mandate resolves to the same key. If pubResolver is nil, signature
// verification is skipped (structural-only).
func VerifyEnvelope(envelope FinalizationEnvelope, pubResolver func(subject string) (*ecdsa.PublicKey, error)) error {
	// Verify each mandate individually using its own subject for key resolution.
	for _, m := range []AP2Mandate{envelope.IntentMandate, envelope.CartMandate, envelope.PaymentMandate} {
		var pub *ecdsa.PublicKey
		if pubResolver != nil {
			subj := m.Subject
			if subj == "" {
				subj = envelope.Subject
			}
			var err error
			pub, err = pubResolver(subj)
			if err != nil {
				return &VerifyError{
					Code:      ErrSubjectKeyNotFound,
					Message:   fmt.Sprintf("pubResolver error for subject %q: %v", subj, err),
					MandateID: m.MandateID,
				}
			}
		}
		if err := VerifyMandate(pub, m); err != nil {
			return err
		}
	}

	// Check expiry on intent mandate (if set).
	// Fail closed: a malformed expires_at is treated as an error, not a bypass.
	// Previously the compound `err == nil && After(exp)` guard silently skipped
	// the expiry check when ExpiresAt was non-empty but unparseable — letting a
	// manipulated envelope with `expires_at: "not-a-date"` pass verification.
	if envelope.IntentMandate.ExpiresAt != "" {
		exp, err := time.Parse(time.RFC3339, envelope.IntentMandate.ExpiresAt)
		if err != nil {
			return &VerifyError{
				Code:      ErrExpiredMandate,
				Message:   fmt.Sprintf("intent mandate has malformed expires_at (expected RFC3339): %s", envelope.IntentMandate.ExpiresAt),
				MandateID: envelope.IntentMandate.MandateID,
			}
		}
		if time.Now().UTC().After(exp) {
			return &VerifyError{
				Code:      ErrExpiredMandate,
				Message:   fmt.Sprintf("intent mandate expired at %s", envelope.IntentMandate.ExpiresAt),
				MandateID: envelope.IntentMandate.MandateID,
			}
		}
	}

	// Extract cross-references from cart mandate body.
	var cartBody struct {
		IntentRef    string `json:"intent_mandate_id"`
		SubtotalCents int   `json:"subtotal_cents"`
	}
	if err := json.Unmarshal(envelope.CartMandate.Body, &cartBody); err != nil {
		return &VerifyError{
			Code:      ErrMandateChainBroken,
			Message:   fmt.Sprintf("failed to parse cart mandate body: %v", err),
			MandateID: envelope.CartMandate.MandateID,
		}
	}
	if cartBody.IntentRef != envelope.IntentMandate.MandateID {
		return &VerifyError{
			Code:      ErrMandateChainBroken,
			Message:   fmt.Sprintf("cart_mandate.body.intent_mandate_id %q != intent_mandate.mandate_id %q", cartBody.IntentRef, envelope.IntentMandate.MandateID),
			MandateID: envelope.CartMandate.MandateID,
		}
	}

	// Extract cross-references from payment mandate body.
	var paymentBody struct {
		CartRef     string `json:"cart_mandate_id"`
		AmountCents int    `json:"amount_cents"`
	}
	if err := json.Unmarshal(envelope.PaymentMandate.Body, &paymentBody); err != nil {
		return &VerifyError{
			Code:      ErrMandateChainBroken,
			Message:   fmt.Sprintf("failed to parse payment mandate body: %v", err),
			MandateID: envelope.PaymentMandate.MandateID,
		}
	}
	if paymentBody.CartRef != envelope.CartMandate.MandateID {
		return &VerifyError{
			Code:      ErrMandateChainBroken,
			Message:   fmt.Sprintf("payment_mandate.body.cart_mandate_id %q != cart_mandate.mandate_id %q", paymentBody.CartRef, envelope.CartMandate.MandateID),
			MandateID: envelope.PaymentMandate.MandateID,
		}
	}

	// Check amount consistency.
	if paymentBody.AmountCents != cartBody.SubtotalCents {
		return &VerifyError{
			Code:      ErrAmountMismatch,
			Message:   fmt.Sprintf("payment_mandate.body.amount_cents %d != cart_mandate.body.subtotal_cents %d", paymentBody.AmountCents, cartBody.SubtotalCents),
			MandateID: envelope.PaymentMandate.MandateID,
		}
	}

	return nil
}
