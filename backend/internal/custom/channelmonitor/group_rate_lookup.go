package channelmonitor

import (
	"context"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikey"
	"github.com/Wei-Shaw/sub2api/ent/group"
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

	rows, err := l.client.APIKey.Query().
		Where(
			apikey.KeyIn(keys...),
			apikey.DeletedAtIsNil(),
			apikey.StatusEQ(service.StatusAPIKeyActive),
		).
		Select(apikey.FieldKey, apikey.FieldGroupID).
		WithGroup(func(query *dbent.GroupQuery) {
			query.
				Where(group.DeletedAtIsNil()).
				Select(group.FieldPlatform, group.FieldRateMultiplier)
		}).
		All(ctx)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		currentGroup := row.Edges.Group
		if currentGroup == nil {
			continue
		}
		out[row.Key] = GroupRate{
			Platform:       currentGroup.Platform,
			RateMultiplier: currentGroup.RateMultiplier,
		}
	}
	return out, nil
}
