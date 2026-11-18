//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// 官方同用户批量结算仍须在原事务中累加二改周期用量；重放和失败批次不能重复累加。
func TestCustomUsageBillingBatchPreservesCycleUsageAndRollback(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	fixture := newBatchBillingFixture(t, client, 10)
	repo := NewUsageBillingRepository(client, integrationDB)
	commands := fixture.commands("custom-cycle-"+uuid.NewString(), []batchBillingStep{
		{key: 'A', account: 'P', subscription: 1.25},
		{key: 'B', account: 'Q', subscription: 2.5},
	})

	assertUsage := func(expected float64) {
		t.Helper()
		var daily, weekly, monthly, cycle float64
		require.NoError(t, integrationDB.QueryRowContext(ctx,
			`SELECT daily_usage_usd, weekly_usage_usd, monthly_usage_usd, cycle_usage_usd
			 FROM user_subscriptions WHERE id = $1`, fixture.subscriptionID,
		).Scan(&daily, &weekly, &monthly, &cycle))
		for _, usage := range []float64{daily, weekly, monthly, cycle} {
			require.InDelta(t, expected, usage, 1e-8)
		}
	}

	outcomes, err := repo.ApplyBatch(ctx, commands)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	for _, outcome := range outcomes {
		require.NoError(t, outcome.Err)
		require.NotNil(t, outcome.Result)
		require.True(t, outcome.Result.Applied)
	}
	assertUsage(3.75)

	outcomes, err = repo.ApplyBatch(ctx, commands)
	require.NoError(t, err)
	require.Len(t, outcomes, 2)
	for _, outcome := range outcomes {
		require.NoError(t, outcome.Err)
		require.NotNil(t, outcome.Result)
		require.False(t, outcome.Result.Applied)
	}
	assertUsage(3.75)

	failing := fixture.commands("custom-cycle-rollback-"+uuid.NewString(), []batchBillingStep{
		{key: 'A', account: 'P', subscription: 4},
		{key: 'B', account: 'Q', subscription: 8},
	})
	missingSubscriptionID := int64(9223372036854775807)
	failing[1].SubscriptionID = &missingSubscriptionID
	_, err = repo.ApplyBatch(ctx, failing)
	require.Error(t, err)
	assertUsage(3.75)
	var dedupRows int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id IN ($1, $2)`,
		failing[0].RequestID, failing[1].RequestID,
	).Scan(&dedupRows))
	require.Zero(t, dedupRows)
}
