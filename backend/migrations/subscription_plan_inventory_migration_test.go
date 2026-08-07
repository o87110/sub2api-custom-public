package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanInventoryMigrationKeepsHistoricalRowsUnlimited(t *testing.T) {
	content, err := FS.ReadFile("194_subscription_plan_inventory.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS remaining_quantity INTEGER")
	require.NotContains(t, sql, "remaining_quantity INTEGER NOT NULL")
	require.Contains(t, sql, "remaining_quantity IS NULL OR remaining_quantity >= 0")
	require.Contains(t, sql, "inventory_auto_delisted BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "plan_inventory_state VARCHAR(16) NOT NULL DEFAULT 'untracked'")
	require.Contains(t, sql, "plan_inventory_state IN ('untracked', 'reserved', 'consumed', 'released')")
}
