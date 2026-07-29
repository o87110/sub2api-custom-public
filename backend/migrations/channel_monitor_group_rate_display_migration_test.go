package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorGroupRateDisplayMigration(t *testing.T) {
	content, err := FS.ReadFile("191_channel_monitor_group_rate_display.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "group_rate_override DECIMAL(10,4) NULL")
	require.Contains(t, sql, "group_rate_display_template VARCHAR(64) NOT NULL DEFAULT ''")
	require.Contains(t, sql, "channel_monitors_group_rate_override_check")
	require.Contains(t, sql, "table_schema = current_schema()")
	require.Contains(t, sql, "CHECK (group_rate_override IS NULL OR group_rate_override > 0)")
}

func TestGroupMinimumBalanceMigration(t *testing.T) {
	content, err := FS.ReadFile("192_add_group_minimum_balance.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "minimum_balance DECIMAL(20,8) NOT NULL DEFAULT 0")
	require.Contains(t, sql, "groups_minimum_balance_nonnegative")
	require.Contains(t, sql, "table_schema = current_schema()")
	require.Contains(t, sql, "CHECK (minimum_balance >= 0)")
}
