package updater

import (
	"os"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
)

// ProvideService wires the custom Release source and authenticated GitHub client.
func ProvideService(cache UpdateCache, buildInfo service.BuildInfo, cfg *config.Config) *UpdateService {
	client := NewGitHubReleaseClientWithAuth(
		cfg.Update.ProxyURL,
		cfg.Security.ProxyFallback.AllowDirectOnError,
		os.Getenv("UPDATE_GITHUB_TOKEN"),
		os.Getenv("UPDATE_GITHUB_TOKEN_FILE"),
	)
	return NewUpdateService(cache, client, buildInfo.Version, buildInfo.BuildType)
}

func ProvideUpdateCache(rdb *redis.Client) UpdateCache {
	return newUpdateCache(rdb)
}

// ProviderSet contains the custom updater providers.
var ProviderSet = wire.NewSet(ProvideUpdateCache, ProvideService)
