package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionCycleUsageMigrationPreservesHistoryAndAddsCycleState(t *testing.T) {
	content, err := FS.ReadFile("196_subscription_cycle_usage.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "cycle_usage_usd DECIMAL(20,10) NOT NULL DEFAULT 0")
	require.Contains(t, sql, "manual_quota_reset_count BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_subscription_cycles")
	require.Contains(t, sql, "SELECT SUM(ul.actual_cost) FROM usage_logs ul")
	require.Contains(t, sql, "GREATEST( daily_usage_usd, weekly_usage_usd, monthly_usage_usd")
	require.Contains(t, sql, "WHERE status = 'current'")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USAGE_LOGS")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM USAGE_LOGS")
}
