package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAffiliateReversalsMigrationPreservesOriginalFinancialRecords(t *testing.T) {
	content, err := FS.ReadFile("231_affiliate_reversals.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_affiliate_reversals")
	require.Contains(t, sql, "source_ledger_id BIGINT NULL REFERENCES user_affiliate_ledger(id) ON DELETE RESTRICT")
	require.Contains(t, sql, "CONSTRAINT user_affiliate_reversals_source_order_unique UNIQUE (source_order_id)")
	require.Contains(t, sql, "rebate_amount = frozen_quota_deducted + available_quota_deducted + balance_deducted")
	require.Contains(t, sql, "WHERE pal.action = 'AFFILIATE_REBATE_REVERSED'")
	require.Contains(t, sql, "FALSE, 'legacy manual affiliate reversal'")
	require.Contains(t, sql, "SET action = 'reversed', frozen_until = NULL")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM USER_AFFILIATE_LEDGER")
	require.NotContains(t, strings.ToUpper(sql), "DELETE FROM PAYMENT_ORDERS")
}
