package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionCycleManualBulkQuotaResetMigrationDefaultsExistingCyclesOff(t *testing.T) {
	content, err := FS.ReadFile("198_subscription_cycle_manual_bulk_quota_reset.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS manual_bulk_quota_reset_enabled BOOLEAN NOT NULL DEFAULT FALSE")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE USER_SUBSCRIPTION_CYCLES")
}
