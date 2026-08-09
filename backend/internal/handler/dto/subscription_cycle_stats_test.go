package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionFromServiceIncludesCycleDisplayStatistics(t *testing.T) {
	sub := &service.UserSubscription{
		ID: 1, CycleUsageUSD: 12.345, ManualQuotaResetCount: 2,
	}

	userDTO := UserSubscriptionFromService(sub)
	adminDTO := UserSubscriptionFromServiceAdmin(sub)

	require.Equal(t, 12.345, userDTO.CycleUsageUSD)
	require.Equal(t, int64(2), userDTO.ManualQuotaResetCount)
	require.Equal(t, 12.345, adminDTO.CycleUsageUSD)
	require.Equal(t, int64(2), adminDTO.ManualQuotaResetCount)
}

func TestManualBulkResetEligibilityIsAdminOnlyMetadata(t *testing.T) {
	sub := &service.UserSubscription{
		ID:                           1,
		CycleSourceType:              "assignment",
		ManualBulkQuotaResetEnabled:  true,
		ManualBulkQuotaResetEditable: true,
	}

	adminDTO := UserSubscriptionFromServiceAdmin(sub)
	require.Equal(t, "assignment", adminDTO.CurrentCycleSourceType)
	require.True(t, adminDTO.ManualBulkQuotaResetEnabled)
	require.True(t, adminDTO.ManualBulkQuotaResetEditable)

	userJSON, err := json.Marshal(UserSubscriptionFromService(sub))
	require.NoError(t, err)
	require.NotContains(t, string(userJSON), "current_cycle_source_type")
	require.NotContains(t, string(userJSON), "manual_bulk_quota_reset")
}
