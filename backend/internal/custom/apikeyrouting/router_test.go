package apikeyrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestStateStickyFailoverOrder(t *testing.T) {
	key := &service.APIKey{
		GroupIDs: []int64{1, 2, 3},
		Groups: []service.Group{
			{ID: 1, Name: "A"},
			{ID: 2, Name: "B"},
			{ID: 3, Name: "C"},
		},
		User: &service.User{ID: 9},
	}
	state := NewState(key, 2)

	first, ok := state.Next()
	require.True(t, ok)
	require.Equal(t, int64(2), *first.GroupID)

	second, ok := state.Next()
	require.True(t, ok)
	require.Equal(t, int64(1), *second.GroupID)

	third, ok := state.Next()
	require.True(t, ok)
	require.Equal(t, int64(3), *third.GroupID)

	_, ok = state.Next()
	require.False(t, ok)
	require.Equal(t, 3, state.AttemptedCount())
}

func TestAPIKeyForGroupDoesNotMutateSource(t *testing.T) {
	override := 17
	key := &service.APIKey{
		GroupID:  int64Ptr(1),
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
		Group:    &service.Group{ID: 1},
		User:     &service.User{ID: 5},
		UserGroupRPMOverrides: map[int64]*int{
			2: &override,
		},
	}

	routed, ok := APIKeyForGroup(key, 2)
	require.True(t, ok)
	require.Equal(t, int64(2), *routed.GroupID)
	require.Equal(t, 17, *routed.User.UserGroupRPMOverride)
	require.Equal(t, int64(1), *key.GroupID)
	require.Nil(t, key.User.UserGroupRPMOverride)
}

func TestAPIKeyForGroupDoesNotInheritPrimaryRPMOverride(t *testing.T) {
	primaryOverride := 17
	key := &service.APIKey{
		GroupID:  int64Ptr(1),
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
		Group:    &service.Group{ID: 1},
		User: &service.User{
			ID:                           5,
			UserGroupRPMOverride:         &primaryOverride,
			UserGroupRPMOverrideResolved: true,
		},
		UserGroupRPMOverrides: map[int64]*int{
			1: &primaryOverride,
		},
	}

	routed, ok := APIKeyForGroup(key, 2)
	require.True(t, ok)
	require.Nil(t, routed.User.UserGroupRPMOverride)
	require.False(t, routed.User.UserGroupRPMOverrideResolved)
	require.Equal(t, 17, *key.User.UserGroupRPMOverride)
}

func TestAPIKeyForGroupDeepClonesCandidateGroupConfig(t *testing.T) {
	dailyLimit := 12.0
	fallbackID := int64(3)
	lastLogin := time.Now().Add(-time.Hour)
	lastUsedIP := "192.0.2.1"
	expiresAt := time.Now().Add(time.Hour)
	key := &service.APIKey{
		GroupID:  int64Ptr(1),
		GroupIDs: []int64{1, 2},
		Groups: []service.Group{
			{ID: 1},
			{
				ID:              2,
				DailyLimitUSD:   &dailyLimit,
				FallbackGroupID: &fallbackID,
				MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{
					ExactModelMappings: map[string]string{"claude-sonnet": "gpt-5"},
				},
				ModelsListConfig: service.GroupModelsListConfig{
					Enabled: true,
					Models:  []string{"gpt-5"},
				},
			},
		},
		Group: &service.Group{ID: 1},
		User: &service.User{
			ID:          5,
			LastLoginAt: &lastLogin,
			GroupRates:  map[int64]float64{2: 0.5},
		},
		LastUsedIP: &lastUsedIP,
		ExpiresAt:  &expiresAt,
	}

	routed, ok := APIKeyForGroup(key, 2)
	require.True(t, ok)
	routed.Group.MessagesDispatchModelConfig.ExactModelMappings["claude-sonnet"] = "changed"
	routed.Group.ModelsListConfig.Models[0] = "changed"
	routed.Groups[1].MessagesDispatchModelConfig.ExactModelMappings["claude-sonnet"] = "changed-again"
	routed.Groups[1].ModelsListConfig.Models[0] = "changed-again"
	*routed.Group.DailyLimitUSD = 99
	*routed.Group.FallbackGroupID = 99
	routed.User.GroupRates[2] = 2
	*routed.User.LastLoginAt = time.Time{}
	*routed.LastUsedIP = "198.51.100.1"
	*routed.ExpiresAt = time.Time{}

	require.Equal(t, "gpt-5", key.Groups[1].MessagesDispatchModelConfig.ExactModelMappings["claude-sonnet"])
	require.Equal(t, "gpt-5", key.Groups[1].ModelsListConfig.Models[0])
	require.Equal(t, 12.0, *key.Groups[1].DailyLimitUSD)
	require.Equal(t, int64(3), *key.Groups[1].FallbackGroupID)
	require.Equal(t, 0.5, key.User.GroupRates[2])
	require.Equal(t, lastLogin, *key.User.LastLoginAt)
	require.Equal(t, "192.0.2.1", *key.LastUsedIP)
	require.Equal(t, expiresAt, *key.ExpiresAt)
}

func TestAPIKeyForGroupDeepClonesCandidateSubscription(t *testing.T) {
	now := time.Now()
	dailyStart := now.Add(-time.Hour)
	key := &service.APIKey{
		GroupID:  int64Ptr(1),
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 1}, {ID: 2}},
		Group:    &service.Group{ID: 1},
		User:     &service.User{ID: 5},
		GroupSubscriptions: map[int64]*service.UserSubscription{
			2: {ID: 77, GroupID: 2, Status: service.SubscriptionStatusActive, DailyWindowStart: &dailyStart},
		},
		GroupSubscriptionsResolved: map[int64]bool{2: true},
	}

	routed, ok := APIKeyForGroup(key, 2)
	require.True(t, ok)
	require.True(t, routed.GroupSubscriptionsResolved[2])
	routed.GroupSubscriptions[2].Status = service.SubscriptionStatusExpired
	*routed.GroupSubscriptions[2].DailyWindowStart = time.Time{}

	require.Equal(t, service.SubscriptionStatusActive, key.GroupSubscriptions[2].Status)
	require.Equal(t, dailyStart, *key.GroupSubscriptions[2].DailyWindowStart)
}

func TestStateNextSkipsMissingSnapshotGroupAndContinues(t *testing.T) {
	apiKey := &service.APIKey{
		GroupIDs: []int64{1, 2},
		Groups:   []service.Group{{ID: 2, Name: "available"}},
	}
	state := NewState(apiKey, 0)

	candidate, ok := state.Next()
	require.True(t, ok)
	require.NotNil(t, candidate)
	require.NotNil(t, candidate.GroupID)
	require.Equal(t, int64(2), *candidate.GroupID)
	require.Equal(t, 2, state.AttemptedCount())
}

func TestStateNextSkipsMissingStickySnapshotGroupAndContinues(t *testing.T) {
	apiKey := &service.APIKey{
		GroupIDs: []int64{1, 2, 3},
		Groups:   []service.Group{{ID: 1, Name: "primary"}, {ID: 3, Name: "available"}},
	}
	state := NewState(apiKey, 2)

	candidate, ok := state.Next()
	require.True(t, ok)
	require.NotNil(t, candidate)
	require.NotNil(t, candidate.GroupID)
	require.Equal(t, int64(1), *candidate.GroupID)
	require.Equal(t, 2, state.AttemptedCount())

	candidate, ok = state.Next()
	require.True(t, ok)
	require.Equal(t, int64(3), *candidate.GroupID)
	require.Equal(t, 3, state.AttemptedCount())
}

func TestShouldCrossGroup(t *testing.T) {
	require.True(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, false))
	require.True(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}, false))
	require.True(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{StatusCode: http.StatusRequestTimeout}, false))
	require.True(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{}, false))
	require.True(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{
		StatusCode: http.StatusUnauthorized,
		Stage:      service.GatewayFailureStageAccountAuth,
		Scope:      service.GatewayFailureScopeAccount,
	}, false))
	require.False(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{StatusCode: http.StatusBadRequest}, false))
	require.False(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{
		StatusCode: http.StatusServiceUnavailable,
		Scope:      service.GatewayFailureScopeRequest,
	}, false))
	require.False(t, ShouldCrossGroup(context.Background(), &service.UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable}, true))
	require.False(t, ShouldCrossGroup(context.Background(), errors.New("not classified"), false))
}

func TestShouldCrossBatchImageGroup(t *testing.T) {
	require.True(t, ShouldCrossBatchImageGroup(context.Background(), service.ErrBatchImageNoAccountAvailable))
	require.True(t, ShouldCrossBatchImageGroup(context.Background(), service.ErrBatchImageProviderSubmitFailed))
	require.True(t, ShouldCrossBatchImageGroup(context.Background(), service.ErrBatchImageProviderMissingAPIKey))
	require.False(t, ShouldCrossBatchImageGroup(context.Background(), service.ErrBatchImageProviderInvalidInput))
	require.False(t, ShouldCrossBatchImageGroup(context.Background(), service.ErrBatchImageQueueFailed))

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	require.False(t, ShouldCrossBatchImageGroup(canceled, service.ErrBatchImageProviderSubmitFailed))
}
