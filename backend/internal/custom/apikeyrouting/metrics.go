package apikeyrouting

import (
	"context"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const metricsSnapshotLogInterval = 1024

// MetricsSnapshot 提供多分组路由的低开销进程内指标快照。
// 这些计数不包含 API Key、原始会话标识或其他敏感值。
type MetricsSnapshot struct {
	StickyHits        uint64
	StickyInvalidated uint64
	GroupAttempts     uint64
	CrossGroup        uint64
	Exhausted         uint64
	RedisFailOpen     uint64
	FailoverDepth     uint64
}

var routingMetrics struct {
	stickyHits        atomic.Uint64
	stickyInvalidated atomic.Uint64
	groupAttempts     atomic.Uint64
	crossGroup        atomic.Uint64
	exhausted         atomic.Uint64
	redisFailOpen     atomic.Uint64
	failoverDepth     atomic.Uint64
}

var metricsSnapshotLogCounter atomic.Uint64

func RecordStickyHit() {
	routingMetrics.stickyHits.Add(1)
}

func RecordStickyInvalidated() {
	routingMetrics.stickyInvalidated.Add(1)
}

func RecordGroupAttempt(depth int) {
	routingMetrics.groupAttempts.Add(1)
	if depth > 0 {
		routingMetrics.crossGroup.Add(1)
		routingMetrics.failoverDepth.Add(uint64(depth))
	}
}

func RecordExhausted() {
	routingMetrics.exhausted.Add(1)
}

func RecordRedisFailOpen() {
	routingMetrics.redisFailOpen.Add(1)
}

func SnapshotMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		StickyHits:        routingMetrics.stickyHits.Load(),
		StickyInvalidated: routingMetrics.stickyInvalidated.Load(),
		GroupAttempts:     routingMetrics.groupAttempts.Load(),
		CrossGroup:        routingMetrics.crossGroup.Load(),
		Exhausted:         routingMetrics.exhausted.Load(),
		RedisFailOpen:     routingMetrics.redisFailOpen.Load(),
		FailoverDepth:     routingMetrics.failoverDepth.Load(),
	}
}

// MaybeLogMetrics emits a bounded structured snapshot so process-local routing
// counters are observable without adding a new public endpoint or sensitive
// labels. Callers invoke it once per actual group attempt.
func MaybeLogMetrics(ctx context.Context) {
	sequence := metricsSnapshotLogCounter.Add(1)
	if sequence%metricsSnapshotLogInterval != 0 {
		return
	}
	snapshot := SnapshotMetrics()
	logger.FromContext(ctx).Named("handler.api_key_group_routing").Info(
		"gateway.group_route_metrics",
		zap.Uint64("sticky_hits", snapshot.StickyHits),
		zap.Uint64("sticky_invalidated", snapshot.StickyInvalidated),
		zap.Uint64("group_attempts", snapshot.GroupAttempts),
		zap.Uint64("cross_group", snapshot.CrossGroup),
		zap.Uint64("exhausted", snapshot.Exhausted),
		zap.Uint64("redis_fail_open", snapshot.RedisFailOpen),
		zap.Uint64("failover_depth_total", snapshot.FailoverDepth),
	)
}
