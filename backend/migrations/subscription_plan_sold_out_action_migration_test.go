package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanSoldOutActionMigrationDefaultsToHistoricalDelisting(t *testing.T) {
	content, err := FS.ReadFile("195_subscription_plan_sold_out_action.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "sold_out_action VARCHAR(32) NOT NULL DEFAULT 'delist'")
	require.Contains(t, sql, "sold_out_action IN ('delist', 'disable_purchase')")
	require.NotContains(t, sql, "UPDATE subscription_plans")
}
