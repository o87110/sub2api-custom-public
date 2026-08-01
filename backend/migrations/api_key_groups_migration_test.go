package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyGroupsMigrationDefinesOrderedAssignmentsAndBackfill(t *testing.T) {
	content, err := FS.ReadFile("193_add_api_key_groups.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS api_key_groups")
	require.Contains(t, sql, "PRIMARY KEY (api_key_id, group_id)")
	require.Contains(t, sql, "UNIQUE (api_key_id, priority)")
	require.Contains(t, sql, "CHECK (priority >= 0)")
	require.Contains(t, sql, "REFERENCES api_keys(id) ON DELETE CASCADE")
	require.Contains(t, sql, "REFERENCES groups(id) ON DELETE CASCADE")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_api_key_groups_group_id ON api_key_groups(group_id)")
	require.Contains(t, sql, "INSERT INTO api_key_groups (api_key_id, group_id, priority) SELECT id, group_id, 0 FROM api_keys WHERE group_id IS NOT NULL")
}

func TestAPIKeyGroupsMigrationPersistsBatchGroupAndInvalidatesEveryCandidate(t *testing.T) {
	content, err := FS.ReadFile("193_add_api_key_groups.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_batch_image_jobs_group_id ON batch_image_jobs(group_id)")
	require.Contains(t, sql, "EXISTS ( SELECT 1 FROM api_key_groups AS akg WHERE akg.api_key_id = k.id AND akg.group_id = target_group_id )")
	require.Contains(t, sql, "CREATE TRIGGER trg_api_key_groups_auth_cache_invalidation")
	require.Contains(t, sql, "CREATE TRIGGER trg_user_group_rates_auth_cache_invalidation")
	require.Contains(t, sql, "CREATE TRIGGER trg_user_subscriptions_auth_cache_invalidation")
	require.Contains(t, sql, "enqueue_user_subscription_auth_cache_invalidation")
	require.Contains(t, sql, "encode(sha256(convert_to(k.key, 'UTF8')), 'hex')")
	require.NotContains(t, sql, "INSERT INTO auth_cache_invalidation_outbox (cache_key) SELECT k.key")
}
