package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupModelBlocklistAuthCacheInvalidationForwardMigration(t *testing.T) {
	content, err := FS.ReadFile("221_group_model_blocklist_auth_cache_invalidation.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION enqueue_group_auth_cache_invalidation()")
	require.Contains(t, sql, "OLD.models_list_config IS NOT DISTINCT FROM NEW.models_list_config")
	require.Contains(t, sql, "OLD.profit_control_enabled IS NOT DISTINCT FROM NEW.profit_control_enabled")
	require.Contains(t, sql, "INSERT INTO auth_cache_invalidation_outbox")
}
