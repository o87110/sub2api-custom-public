package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSubscriptionPlanRenewalPolicyMigrationDefaultsOffAndBoundsGraceDays(t *testing.T) {
	content, err := FS.ReadFile("232_subscription_plan_renewal_policy.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "allow_existing_user_renewal BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "renewal_grace_days INTEGER NOT NULL DEFAULT 0")
	require.Contains(t, sql, "renewal_grace_days >= 0 AND renewal_grace_days <= 30")
	require.NotContains(t, sql, "UPDATE subscription_plans")
}
