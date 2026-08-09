package subscriptionquota

import "time"

const (
	CycleStatusCurrent   = "current"
	CycleStatusPending   = "pending"
	CycleStatusCompleted = "completed"
	CycleStatusCancelled = "cancelled"

	CycleSourceAssignment = "assignment"
	CycleSourcePayment    = "payment"
	CycleSourceRedeem     = "redeem"
	CycleSourceLegacy     = "legacy"
)

// NeedsAdvance reports whether a renewed subscription has crossed its current
// entitlement-cycle boundary. It deliberately ignores quota-window anchors:
// administrators may reset those anchors without starting a new subscription cycle.
func NeedsAdvance(currentCycleEndsAt, expiresAt, now time.Time) bool {
	return !currentCycleEndsAt.IsZero() &&
		!now.Before(currentCycleEndsAt) &&
		expiresAt.After(currentCycleEndsAt)
}

// NormalizeSource keeps internal cycle rows deterministic without exposing
// purchase or redeem implementation details to the subscription domain model.
func NormalizeSource(source string) string {
	switch source {
	case CycleSourcePayment, CycleSourceRedeem, CycleSourceLegacy:
		return source
	default:
		return CycleSourceAssignment
	}
}

// NormalizeManualBulkQuotaResetEligibility limits per-cycle eligibility to
// sources that are administered directly instead of through a payment plan.
func NormalizeManualBulkQuotaResetEligibility(source string, enabled bool) bool {
	normalizedSource := NormalizeSource(source)
	return enabled && (normalizedSource == CycleSourceAssignment || normalizedSource == CycleSourceLegacy)
}
