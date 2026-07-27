//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestChannelMonitorRepositoryPersistsAndClearsGroupRateDisplay(t *testing.T) {
	tx := testEntTx(t)
	ctx := dbent.NewTxContext(context.Background(), tx)

	repo := NewChannelMonitorRepository(tx.Client(), integrationDB)
	override := 0.9
	monitor := &service.ChannelMonitor{
		Name:                     "group-rate-persistence",
		Provider:                 service.MonitorProviderOpenAI,
		APIMode:                  service.MonitorAPIModeChatCompletions,
		Endpoint:                 "https://api.example.com",
		APIKey:                   "encrypted-key",
		PrimaryModel:             "gpt-test",
		GroupName:                "custom label",
		GroupRateOverride:        &override,
		GroupRateDisplayTemplate: "{rate}优先用",
		Enabled:                  true,
		IntervalSeconds:          60,
		CreatedBy:                1,
	}

	require.NoError(t, repo.Create(ctx, monitor))
	stored, err := repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.Equal(t, "custom label", stored.GroupName)
	require.Equal(t, 0.9, *stored.GroupRateOverride)
	require.Equal(t, "{rate}优先用", stored.GroupRateDisplayTemplate)

	stored.GroupRateOverride = nil
	stored.GroupRateDisplayTemplate = ""
	require.NoError(t, repo.Update(ctx, stored))

	reloaded, err := repo.GetByID(ctx, monitor.ID)
	require.NoError(t, err)
	require.Nil(t, reloaded.GroupRateOverride)
	require.Empty(t, reloaded.GroupRateDisplayTemplate)
}
