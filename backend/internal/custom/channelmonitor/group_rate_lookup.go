package channelmonitor

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/ent/user"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// EntGroupRateLookup 批量查询本站 API Key 当前关联的分组默认倍率。
type EntGroupRateLookup struct {
	client *dbent.Client
}

// NewEntGroupRateLookup 创建批量倍率查询器。
func NewEntGroupRateLookup(client *dbent.Client) *EntGroupRateLookup {
	return &EntGroupRateLookup{client: client}
}

// ListByAPIKeys 用单次批量查询返回 Key 到分组默认倍率的映射。
func (l *EntGroupRateLookup) ListByAPIKeys(ctx context.Context, keys []string) (map[string]GroupRate, error) {
	out := make(map[string]GroupRate, len(keys))
	if l == nil || l.client == nil || len(keys) == 0 {
		return out, nil
	}

	now := time.Now()
	rows, err := l.client.APIKey.Query().
		Where(
			apikey.KeyIn(keys...),
			apikey.DeletedAtIsNil(),
		).
		Select(
			apikey.FieldKey,
			apikey.FieldUserID,
			apikey.FieldGroupID,
			apikey.FieldStatus,
			apikey.FieldQuota,
			apikey.FieldQuotaUsed,
			apikey.FieldExpiresAt,
		).
		WithUser(func(query *dbent.UserQuery) {
			query.
				Where(user.DeletedAtIsNil()).
				Select(user.FieldStatus, user.FieldBalance).
				WithAllowedGroups(func(query *dbent.GroupQuery) {
					query.Select(group.FieldID)
				}).
				WithSubscriptions(func(query *dbent.UserSubscriptionQuery) {
					query.
						Where(
							usersubscription.DeletedAtIsNil(),
							usersubscription.StatusEQ(service.SubscriptionStatusActive),
							usersubscription.ExpiresAtGT(now),
						).
						Select(
							usersubscription.FieldUserID,
							usersubscription.FieldGroupID,
							usersubscription.FieldStartsAt,
							usersubscription.FieldExpiresAt,
							usersubscription.FieldStatus,
							usersubscription.FieldDailyWindowStart,
							usersubscription.FieldWeeklyWindowStart,
							usersubscription.FieldMonthlyWindowStart,
							usersubscription.FieldDailyUsageUsd,
							usersubscription.FieldWeeklyUsageUsd,
							usersubscription.FieldMonthlyUsageUsd,
						)
				})
		}).
		WithGroup(func(query *dbent.GroupQuery) {
			query.
				Where(group.DeletedAtIsNil()).
				Select(
					group.FieldPlatform,
					group.FieldRateMultiplier,
					group.FieldStatus,
					group.FieldIsExclusive,
					group.FieldSubscriptionType,
					group.FieldMinimumBalance,
					group.FieldDailyLimitUsd,
					group.FieldWeeklyLimitUsd,
					group.FieldMonthlyLimitUsd,
				)
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if !runtimeEligibleForGroupRate(row, now) {
			continue
		}
		currentGroup := row.Edges.Group
		out[row.Key] = GroupRate{
			Platform:       currentGroup.Platform,
			RateMultiplier: currentGroup.RateMultiplier,
		}
	}
	return out, nil
}
