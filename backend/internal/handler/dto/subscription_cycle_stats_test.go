package dto

import (
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
