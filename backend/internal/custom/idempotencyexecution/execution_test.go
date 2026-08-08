package idempotencyexecution

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewSeparatesScopeAndActor(t *testing.T) {
	claimedAt := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	expiresAt := claimedAt.Add(24 * time.Hour)
	keyHash := strings.Repeat("a", 64)
	single, err := New("admin.subscriptions.reset_quota", "admin:1", keyHash, claimedAt, expiresAt)
	require.NoError(t, err)
	bulk, err := New("admin.subscriptions.bulk_reset_quota", "admin:1", keyHash, claimedAt, expiresAt)
	require.NoError(t, err)
	otherActor, err := New("admin.subscriptions.reset_quota", "admin:2", keyHash, claimedAt, expiresAt)
	require.NoError(t, err)

	require.NotEqual(t, single.OperationKeyHash, bulk.OperationKeyHash)
	require.NotEqual(t, single.OperationKeyHash, otherActor.OperationKeyHash)
	require.Equal(t, single.IdempotencyKeyHash, bulk.IdempotencyKeyHash)
	require.Equal(t, single.IdempotencyKeyHash, otherActor.IdempotencyKeyHash)
}
