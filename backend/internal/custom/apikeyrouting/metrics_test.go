package apikeyrouting

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoutingMetricsSnapshotTracksFailoverWithoutSensitiveLabels(t *testing.T) {
	before := SnapshotMetrics()
	RecordStickyHit()
	RecordStickyInvalidated()
	RecordGroupAttempt(2)
	RecordExhausted()
	RecordRedisFailOpen()

	after := SnapshotMetrics()
	require.Equal(t, before.StickyHits+1, after.StickyHits)
	require.Equal(t, before.StickyInvalidated+1, after.StickyInvalidated)
	require.Equal(t, before.GroupAttempts+1, after.GroupAttempts)
	require.Equal(t, before.CrossGroup+1, after.CrossGroup)
	require.Equal(t, before.Exhausted+1, after.Exhausted)
	require.Equal(t, before.RedisFailOpen+1, after.RedisFailOpen)
	require.Equal(t, before.FailoverDepth+2, after.FailoverDepth)
}

func TestMaybeLogMetricsUsesBoundedCadence(t *testing.T) {
	before := metricsSnapshotLogCounter.Load()
	for i := 0; i < metricsSnapshotLogInterval; i++ {
		MaybeLogMetrics(context.Background())
	}
	require.Equal(t, before+metricsSnapshotLogInterval, metricsSnapshotLogCounter.Load())
}
