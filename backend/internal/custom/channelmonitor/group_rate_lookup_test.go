package channelmonitor

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newGroupRateLookupTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:group_rate_lookup?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestEntGroupRateLookupListByAPIKeysReturnsOnlyActiveKeys(t *testing.T) {
	ctx := context.Background()
	client := newGroupRateLookupTestClient(t)

	user, err := client.User.Create().
		SetEmail("group-rate-lookup@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetBalance(10).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	currentGroup, err := client.Group.Create().
		SetName("OpenAI 18%").
		SetPlatform("openai").
		SetRateMultiplier(0.18).
		Save(ctx)
	require.NoError(t, err)

	createKey := func(userID int64, key, status string, groupID *int64, deletedAt *time.Time) *dbent.APIKey {
		t.Helper()
		builder := client.APIKey.Create().
			SetUserID(userID).
			SetName(key).
			SetKey(key).
			SetStatus(status)
		if groupID != nil {
			builder.SetGroupID(*groupID)
		}
		if deletedAt != nil {
			builder.SetDeletedAt(*deletedAt)
		}
		created, createErr := builder.Save(ctx)
		require.NoError(t, createErr)
		return created
	}

	groupID := currentGroup.ID
	deletedAt := time.Now()
	createKey(user.ID, "sk-active", service.StatusAPIKeyActive, &groupID, nil)
	createKey(user.ID, "sk-disabled", service.StatusAPIKeyDisabled, &groupID, nil)
	createKey(user.ID, "sk-quota-exhausted", service.StatusAPIKeyQuotaExhausted, &groupID, nil)
	createKey(user.ID, "sk-expired", service.StatusAPIKeyExpired, &groupID, nil)
	createKey(user.ID, "sk-deleted", service.StatusAPIKeyActive, &groupID, &deletedAt)
	createKey(user.ID, "sk-no-group", service.StatusAPIKeyActive, nil, nil)

	dynamicExpired := createKey(user.ID, "sk-dynamic-expired", service.StatusAPIKeyActive, &groupID, nil)
	_, err = client.APIKey.UpdateOneID(dynamicExpired.ID).SetExpiresAt(time.Now().Add(-time.Minute)).Save(ctx)
	require.NoError(t, err)
	dynamicQuota := createKey(user.ID, "sk-dynamic-quota", service.StatusAPIKeyActive, &groupID, nil)
	_, err = client.APIKey.UpdateOneID(dynamicQuota.ID).SetQuota(10).SetQuotaUsed(10).Save(ctx)
	require.NoError(t, err)

	inactiveUser, err := client.User.Create().
		SetEmail("group-rate-inactive-user@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetBalance(10).
		SetStatus(service.StatusDisabled).
		Save(ctx)
	require.NoError(t, err)
	createKey(inactiveUser.ID, "sk-inactive-user", service.StatusAPIKeyActive, &groupID, nil)

	disabledGroup, err := client.Group.Create().
		SetName("Disabled").
		SetPlatform("openai").
		SetRateMultiplier(0.25).
		SetStatus(service.StatusDisabled).
		Save(ctx)
	require.NoError(t, err)
	createKey(user.ID, "sk-disabled-group", service.StatusAPIKeyActive, &disabledGroup.ID, nil)

	exclusiveGroup, err := client.Group.Create().
		SetName("Exclusive").
		SetPlatform("openai").
		SetRateMultiplier(0.3).
		SetIsExclusive(true).
		Save(ctx)
	require.NoError(t, err)
	exclusiveDeniedUser, err := client.User.Create().
		SetEmail("group-rate-exclusive-denied@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetBalance(10).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	createKey(exclusiveDeniedUser.ID, "sk-exclusive-denied", service.StatusAPIKeyActive, &exclusiveGroup.ID, nil)
	_, err = client.User.UpdateOneID(user.ID).AddAllowedGroupIDs(exclusiveGroup.ID).Save(ctx)
	require.NoError(t, err)
	createKey(user.ID, "sk-exclusive-allowed", service.StatusAPIKeyActive, &exclusiveGroup.ID, nil)

	minimumBalanceGroup, err := client.Group.Create().
		SetName("Minimum balance").
		SetPlatform("openai").
		SetRateMultiplier(0.4).
		SetMinimumBalance(10).
		Save(ctx)
	require.NoError(t, err)
	createKey(user.ID, "sk-minimum-balance", service.StatusAPIKeyActive, &minimumBalanceGroup.ID, nil)

	zeroBalanceUser, err := client.User.Create().
		SetEmail("group-rate-zero-balance@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	createKey(zeroBalanceUser.ID, "sk-zero-balance", service.StatusAPIKeyActive, &groupID, nil)

	subscriptionLimit := 10.0
	subscriptionGroup, err := client.Group.Create().
		SetName("Subscription").
		SetPlatform("openai").
		SetRateMultiplier(0.5).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetDailyLimitUsd(subscriptionLimit).
		SetWeeklyLimitUsd(subscriptionLimit).
		SetMonthlyLimitUsd(subscriptionLimit).
		Save(ctx)
	require.NoError(t, err)
	subscriptionMissingUser, err := client.User.Create().
		SetEmail("group-rate-subscription-missing@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	createKey(subscriptionMissingUser.ID, "sk-subscription-missing", service.StatusAPIKeyActive, &subscriptionGroup.ID, nil)
	_, err = client.UserSubscription.Create().
		SetUserID(zeroBalanceUser.ID).
		SetGroupID(subscriptionGroup.ID).
		SetStartsAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		Save(ctx)
	require.NoError(t, err)
	createKey(zeroBalanceUser.ID, "sk-subscription-active", service.StatusAPIKeyActive, &subscriptionGroup.ID, nil)
	subscriptionOverLimitUser, err := client.User.Create().
		SetEmail("group-rate-subscription-over-limit@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	currentWindowStart := time.Now().Add(-time.Hour)
	_, err = client.UserSubscription.Create().
		SetUserID(subscriptionOverLimitUser.ID).
		SetGroupID(subscriptionGroup.ID).
		SetStartsAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetDailyWindowStart(currentWindowStart).
		SetDailyUsageUsd(subscriptionLimit + 1).
		Save(ctx)
	require.NoError(t, err)
	createKey(subscriptionOverLimitUser.ID, "sk-subscription-over-limit", service.StatusAPIKeyActive, &subscriptionGroup.ID, nil)

	rates, err := NewEntGroupRateLookup(client).ListByAPIKeys(ctx, []string{
		"sk-active",
		"sk-disabled",
		"sk-quota-exhausted",
		"sk-expired",
		"sk-deleted",
		"sk-no-group",
		"sk-dynamic-expired",
		"sk-dynamic-quota",
		"sk-inactive-user",
		"sk-disabled-group",
		"sk-exclusive-denied",
		"sk-exclusive-allowed",
		"sk-minimum-balance",
		"sk-zero-balance",
		"sk-subscription-missing",
		"sk-subscription-active",
		"sk-subscription-over-limit",
		"sk-unknown",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]GroupRate{
		"sk-active": {
			Platform:       "openai",
			RateMultiplier: 0.18,
		},
		"sk-exclusive-allowed": {
			Platform:       "openai",
			RateMultiplier: 0.3,
		},
		"sk-subscription-active": {
			Platform:       "openai",
			RateMultiplier: 0.5,
		},
	}, rates)
}

func TestSubscriptionWithinRuntimeLimits(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	limit := 10.0
	currentGroup := &dbent.Group{
		ID:              7,
		DailyLimitUsd:   &limit,
		WeeklyLimitUsd:  &limit,
		MonthlyLimitUsd: &limit,
	}
	currentWindowStart := now.Add(-time.Hour)
	newSubscription := func() *dbent.UserSubscription {
		return &dbent.UserSubscription{
			GroupID:            currentGroup.ID,
			StartsAt:           now.Add(-48 * time.Hour),
			ExpiresAt:          now.Add(48 * time.Hour),
			Status:             service.SubscriptionStatusActive,
			DailyWindowStart:   &currentWindowStart,
			WeeklyWindowStart:  &currentWindowStart,
			MonthlyWindowStart: &currentWindowStart,
			DailyUsageUsd:      limit - 1,
			WeeklyUsageUsd:     limit - 1,
			MonthlyUsageUsd:    limit - 1,
		}
	}

	tests := []struct {
		name   string
		mutate func(*dbent.UserSubscription)
		want   bool
	}{
		{name: "below every limit", want: true},
		{
			name: "exactly at the limit matches auth preflight",
			mutate: func(subscription *dbent.UserSubscription) {
				subscription.DailyUsageUsd = limit
				subscription.WeeklyUsageUsd = limit
				subscription.MonthlyUsageUsd = limit
			},
			want: true,
		},
		{
			name: "daily limit exceeded",
			mutate: func(subscription *dbent.UserSubscription) {
				subscription.DailyUsageUsd = limit + 1
			},
			want: false,
		},
		{
			name: "weekly limit exceeded",
			mutate: func(subscription *dbent.UserSubscription) {
				subscription.WeeklyUsageUsd = limit + 1
			},
			want: false,
		},
		{
			name: "monthly limit exceeded",
			mutate: func(subscription *dbent.UserSubscription) {
				subscription.MonthlyUsageUsd = limit + 1
			},
			want: false,
		},
		{
			name: "expired recurring windows use reset projection",
			mutate: func(subscription *dbent.UserSubscription) {
				staleDailyWindowStart := now.Add(-25 * time.Hour)
				staleWeeklyWindowStart := now.Add(-8 * 24 * time.Hour)
				staleMonthlyWindowStart := now.Add(-31 * 24 * time.Hour)
				subscription.DailyWindowStart = &staleDailyWindowStart
				subscription.WeeklyWindowStart = &staleWeeklyWindowStart
				subscription.MonthlyWindowStart = &staleMonthlyWindowStart
				subscription.DailyUsageUsd = limit + 1
				subscription.WeeklyUsageUsd = limit + 1
				subscription.MonthlyUsageUsd = limit + 1
			},
			want: true,
		},
		{
			name: "one-time daily quota does not reset",
			mutate: func(subscription *dbent.UserSubscription) {
				staleWindowStart := now.Add(-25 * time.Hour)
				subscription.StartsAt = now.Add(-23 * time.Hour)
				subscription.ExpiresAt = subscription.StartsAt.Add(24 * time.Hour)
				subscription.DailyWindowStart = &staleWindowStart
				subscription.DailyUsageUsd = limit + 1
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscription := newSubscription()
			if tt.mutate != nil {
				tt.mutate(subscription)
			}
			require.Equal(t, tt.want, subscriptionWithinRuntimeLimits(subscription, currentGroup, now))
		})
	}
}
