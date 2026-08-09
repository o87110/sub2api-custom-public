package subscriptionquota

import (
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestTermCycleSnapshotCASIncludesManualBulkResetEligibility(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	cycle := &dbent.UserSubscriptionCycle{
		ID:                          1,
		StartsAt:                    now,
		EndsAt:                      now.Add(24 * time.Hour),
		Status:                      CycleStatusCurrent,
		SourceType:                  CycleSourceAssignment,
		ManualBulkQuotaResetEnabled: true,
	}
	snapshot := termCycleSnapshot(cycle)
	require.True(t, cycleMatchesSnapshot(cycle, snapshot))
	require.True(t, snapshot.ManualBulkQuotaResetEnabled)

	cycle.ManualBulkQuotaResetEnabled = false
	require.False(t, cycleMatchesSnapshot(cycle, snapshot))
}
