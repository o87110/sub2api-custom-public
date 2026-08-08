package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanBulkQuotaResetMigrationDefaultsExistingPlansOff(t *testing.T) {
	content, err := FS.ReadFile("197_subscription_plan_bulk_quota_reset.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS allow_bulk_quota_reset BOOLEAN NOT NULL DEFAULT FALSE")
	require.NotContains(t, strings.ToUpper(sql), "UPDATE SUBSCRIPTION_PLANS")
}
