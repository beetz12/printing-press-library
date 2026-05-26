// Copyright 2026 beetz12. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"fmt"

	"ap2-pp-cli/internal/ap2"
)

// verifyAmountCeiling checks that the payment mandate's amount does not exceed
// the intent mandate's authorized ceiling. Returns a clear error if exceeded.
// A MaxAmountCents of 0 means "no ceiling" — the check is skipped.
func verifyAmountCeiling(envelope ap2.FinalizationEnvelope) error {
	// Parse intent body to get max_amount_cents and currency.
	var intentBody ap2.IntentMandateBody
	if err := json.Unmarshal(envelope.IntentMandate.Body, &intentBody); err != nil {
		return fmt.Errorf("parsing intent mandate body: %w", err)
	}

	// Parse payment body to get amount_cents and currency.
	var paymentBody ap2.PaymentMandateBody
	if err := json.Unmarshal(envelope.PaymentMandate.Body, &paymentBody); err != nil {
		return fmt.Errorf("parsing payment mandate body: %w", err)
	}

	// Currency must match (when both sides specify one).
	if intentBody.Currency != "" && paymentBody.Currency != "" && intentBody.Currency != paymentBody.Currency {
		return fmt.Errorf("currency mismatch: intent authorized %s but payment is %s",
			intentBody.Currency, paymentBody.Currency)
	}

	// Amount must not exceed ceiling. MaxAmountCents=0 means "no ceiling".
	if intentBody.MaxAmountCents > 0 && paymentBody.AmountCents > intentBody.MaxAmountCents {
		return fmt.Errorf("payment amount %d cents exceeds intent mandate ceiling of %d cents (%.2f vs %.2f %s)",
			paymentBody.AmountCents, intentBody.MaxAmountCents,
			float64(paymentBody.AmountCents)/100, float64(intentBody.MaxAmountCents)/100,
			intentBody.Currency)
	}

	return nil
}
