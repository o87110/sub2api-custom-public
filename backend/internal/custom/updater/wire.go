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

// NewVersionInfoClient provides a read-only service adapter for informational
// upstream release checks. Update and rollback downloads remain on ProvideService.
func NewVersionInfoClient(cfg *config.Config) service.GitHubReleaseClient {
	client := NewGitHubReleaseClientWithAuth(
		cfg.Update.ProxyURL,
		cfg.Security.ProxyFallback.AllowDirectOnError,
		os.Getenv("UPDATE_GITHUB_TOKEN"),
		os.Getenv("UPDATE_GITHUB_TOKEN_FILE"),
	)
	return newVersionInfoClientAdapter(client)
}

// CodexVersionSyncLifecycle keeps the official informational sync worker on
// the hardened Custom GitHub client without exposing the official updater.
type CodexVersionSyncLifecycle struct {
	service *service.OpenAICodexVersionSyncService
}

func NewCodexVersionSyncLifecycle(syncService *service.OpenAICodexVersionSyncService) *CodexVersionSyncLifecycle {
	return &CodexVersionSyncLifecycle{service: syncService}
}

func ProvideCodexVersionSyncLifecycle(
	settingRepo service.SettingRepository,
	settingService *service.SettingService,
	cfg *config.Config,
) *CodexVersionSyncLifecycle {
	client := NewVersionInfoClient(cfg)
	return NewCodexVersionSyncLifecycle(
		service.ProvideOpenAICodexVersionSyncService(settingRepo, settingService, client),
	)
}

func (lifecycle *CodexVersionSyncLifecycle) Stop() {
	if lifecycle == nil || lifecycle.service == nil {
		return
	}
	lifecycle.service.Stop()
}

// ProviderSet contains the custom updater providers.
var ProviderSet = wire.NewSet(ProvideUpdateCache, ProvideService, ProvideCodexVersionSyncLifecycle)
