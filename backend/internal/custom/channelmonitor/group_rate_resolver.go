package channelmonitor

import (
	"context"
	"log/slog"

	"github.com/Wei-Shaw/sub2api/internal/custom/channelmonitor/ratedisplay"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type enabledMonitorLister interface {
	ListEnabledMonitors(ctx context.Context) ([]*service.ChannelMonitor, error)
}

type groupRateLookup interface {
	ListByAPIKeys(ctx context.Context, keys []string) (map[string]GroupRate, error)
}

// GroupRate 是本地 API Key 当前所属分组的默认倍率快照。
type GroupRate struct {
	Platform       string
	RateMultiplier float64
}

// GroupRateResolver 根据监控保存的本站 API Key 动态解析分组默认倍率。
type GroupRateResolver struct {
	monitors enabledMonitorLister
	lookup   groupRateLookup
}

// NewGroupRateResolver 创建渠道监控分组倍率解析器。
func NewGroupRateResolver(
	monitorService *service.ChannelMonitorService,
	lookup *EntGroupRateLookup,
) *GroupRateResolver {
	return newGroupRateResolver(monitorService, lookup)
}

func newGroupRateResolver(
	monitors enabledMonitorLister,
	lookup groupRateLookup,
) *GroupRateResolver {
	return &GroupRateResolver{
		monitors: monitors,
		lookup:   lookup,
	}
}

// Resolve 返回 monitor ID 到分组默认倍率的映射。
//
// 外部 Key、失效 Key、无分组、解密失败或平台不匹配时不返回对应项，
// 由用户视图继续展示原有备用分组标签。
func (r *GroupRateResolver) Resolve(ctx context.Context) map[int64]float64 {
	out := make(map[int64]float64)
	if r == nil || r.monitors == nil {
		return out
	}

	monitors, err := r.monitors.ListEnabledMonitors(ctx)
	if err != nil {
		slog.Warn("custom channel monitor: list enabled monitors for group rates failed", "error", err)
		return out
	}

	for _, monitor := range monitors {
		if rate, ok := validMonitorGroupRateOverride(monitor); ok {
			out[monitor.ID] = rate
		}
	}
	if r.lookup == nil {
		return out
	}

	keys := uniqueMonitorAPIKeys(monitors)
	if len(keys) == 0 {
		return out
	}

	rates, err := r.lookup.ListByAPIKeys(ctx, keys)
	if err != nil {
		slog.Warn("custom channel monitor: batch resolve group rates failed", "error", err)
		return out
	}

	for _, monitor := range monitors {
		if monitor == nil || monitor.APIKey == "" || monitor.APIKeyDecryptFailed {
			continue
		}
		if _, overridden := validMonitorGroupRateOverride(monitor); overridden {
			continue
		}
		rate, ok := rates[monitor.APIKey]
		if !ok || rate.Platform != monitor.Provider {
			continue
		}
		out[monitor.ID] = rate.RateMultiplier
	}
	return out
}

func validMonitorGroupRateOverride(monitor *service.ChannelMonitor) (float64, bool) {
	if monitor == nil || monitor.GroupRateOverride == nil ||
		!ratedisplay.ValidOverride(monitor.GroupRateOverride) {
		return 0, false
	}
	return *monitor.GroupRateOverride, true
}

func uniqueMonitorAPIKeys(monitors []*service.ChannelMonitor) []string {
	keys := make([]string, 0, len(monitors))
	seen := make(map[string]struct{}, len(monitors))
	for _, monitor := range monitors {
		if monitor == nil || monitor.APIKey == "" || monitor.APIKeyDecryptFailed {
			continue
		}
		if _, overridden := validMonitorGroupRateOverride(monitor); overridden {
			continue
		}
		if _, ok := seen[monitor.APIKey]; ok {
			continue
		}
		seen[monitor.APIKey] = struct{}{}
		keys = append(keys, monitor.APIKey)
	}
	return keys
}
