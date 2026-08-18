package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserMonitorViewToItemIncludesCustomGroupRateMultiplier(t *testing.T) {
	rate := 0.18
	item := userMonitorViewToItem(&service.UserMonitorView{
		ID:                       7,
		Name:                     "monitor",
		Provider:                 "openai",
		GroupName:                "custom label",
		GroupRateDisplayTemplate: "{rate}优先用",
		PrimaryModel:             "gpt-5",
	}, &rate, false)

	require.NotNil(t, item.GroupRateMultiplier)
	require.Equal(t, 0.18, *item.GroupRateMultiplier)
	require.Equal(t, "custom label", item.GroupName)
	require.Equal(t, "{rate}优先用", item.GroupRateDisplayTemplate)
}

func TestUserMonitorViewToItemPreservesZeroAndMissingGroupRates(t *testing.T) {
	zero := 0.0
	withZero := userMonitorViewToItem(&service.UserMonitorView{ID: 8}, &zero, false)
	withoutRate := userMonitorViewToItem(&service.UserMonitorView{ID: 9}, nil, false)

	require.NotNil(t, withZero.GroupRateMultiplier)
	require.Zero(t, *withZero.GroupRateMultiplier)
	require.Nil(t, withoutRate.GroupRateMultiplier)
}
