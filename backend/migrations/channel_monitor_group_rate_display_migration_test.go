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
