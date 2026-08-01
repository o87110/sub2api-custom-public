package apikeyrouting

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisGroupBindingCASAndHashedIdentity(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	const (
		apiKeyID   = int64(42)
		protocol   = "openai_responses"
		sessionRaw = "raw-session-identifier-must-not-leak"
	)
	firstBinding := newVersionedBinding(11)
	updated, err := CompareAndSetRedisGroupBinding(
		ctx, client, apiKeyID, protocol, sessionRaw, service.APIKeyGroupBinding{}, firstBinding, time.Hour)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, firstBinding, mustLoadGroupBinding(t, client, ctx, apiKeyID, protocol, sessionRaw))

	keys := redisServer.Keys()
	require.Len(t, keys, 1)
	require.False(t, strings.Contains(keys[0], sessionRaw))
	require.False(t, strings.Contains(keys[0], protocol))
	value, err := redisServer.Get(keys[0])
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(value, "11:"))
	require.False(t, strings.Contains(value, sessionRaw))
	require.Equal(t, time.Hour, redisServer.TTL(keys[0]))

	wrongBinding := newVersionedBinding(99)
	updated, err = CompareAndSetRedisGroupBinding(
		ctx, client, apiKeyID, protocol, sessionRaw, wrongBinding, newVersionedBinding(12), time.Hour)
	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, firstBinding, mustLoadGroupBinding(t, client, ctx, apiKeyID, protocol, sessionRaw))

	redisServer.FastForward(20 * time.Minute)
	secondBinding := newVersionedBinding(12)
	updated, err = CompareAndSetRedisGroupBinding(
		ctx, client, apiKeyID, protocol, sessionRaw, firstBinding, secondBinding, time.Hour)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, time.Hour, redisServer.TTL(keys[0]), "成功 CAS 应刷新滑动 TTL")

	deleted, err := CompareAndDeleteRedisGroupBinding(
		ctx, client, apiKeyID, protocol, sessionRaw, firstBinding)
	require.NoError(t, err)
	require.False(t, deleted)
	deleted, err = CompareAndDeleteRedisGroupBinding(
		ctx, client, apiKeyID, protocol, sessionRaw, secondBinding)
	require.NoError(t, err)
	require.True(t, deleted)
	require.Zero(t, mustLoadGroupBinding(t, client, ctx, apiKeyID, protocol, sessionRaw))
}

func mustLoadGroupBinding(
	t *testing.T,
	client *redis.Client,
	ctx context.Context,
	apiKeyID int64,
	protocol, sessionHash string,
) service.APIKeyGroupBinding {
	t.Helper()
	binding, err := LoadRedisGroupBinding(ctx, client, apiKeyID, protocol, sessionHash)
	require.NoError(t, err)
	return binding
}
