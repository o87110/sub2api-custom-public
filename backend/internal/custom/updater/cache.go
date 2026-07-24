package updater

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const updateCacheSchemaVersion = 2

type updateCache struct {
	rdb *redis.Client
	key string
}

func newUpdateCache(rdb *redis.Client) UpdateCache {
	repositoryNamespace := strings.ReplaceAll(customGitHubRepo, "/", ":")
	return &updateCache{
		rdb: rdb,
		key: fmt.Sprintf("update:custom:%s:v%d", repositoryNamespace, updateCacheSchemaVersion),
	}
}

func (c *updateCache) GetUpdateInfo(ctx context.Context) (string, error) {
	return c.rdb.Get(ctx, c.key).Result()
}

func (c *updateCache) SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error {
	return c.rdb.Set(ctx, c.key, data, ttl).Err()
}
