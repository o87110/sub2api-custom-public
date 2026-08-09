package subscriptionquota

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNeedsAdvanceOnlyAfterCurrentBoundaryWithRemainingEntitlement(t *testing.T) {
	boundary := time.Date(2026, 1, 31, 12, 0, 0, 0, time.UTC)

	require.False(t, NeedsAdvance(boundary, boundary.Add(30*24*time.Hour), boundary.Add(-time.Nanosecond)))
	require.True(t, NeedsAdvance(boundary, boundary.Add(30*24*time.Hour), boundary))
	require.False(t, NeedsAdvance(boundary, boundary, boundary))
	require.False(t, NeedsAdvance(time.Time{}, boundary.Add(30*24*time.Hour), boundary))
}

func TestNormalizeManualBulkQuotaResetEligibilityOnlyAllowsManualSources(t *testing.T) {
	require.True(t, NormalizeManualBulkQuotaResetEligibility(CycleSourceAssignment, true))
	require.True(t, NormalizeManualBulkQuotaResetEligibility(CycleSourceLegacy, true))
	require.False(t, NormalizeManualBulkQuotaResetEligibility(CycleSourcePayment, true))
	require.False(t, NormalizeManualBulkQuotaResetEligibility(CycleSourceRedeem, true))
	require.False(t, NormalizeManualBulkQuotaResetEligibility(CycleSourceAssignment, false))
}
