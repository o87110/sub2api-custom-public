package affiliatereversal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRebateRecordsListProjectsBalanceSnapshots(t *testing.T) {
	require.Contains(t, rebateRecordsListSQL, "reversal.balance_before::double precision AS balance_before")
	require.Contains(t, rebateRecordsListSQL, "reversal.balance_after::double precision AS balance_after")
	require.Contains(t, rebateRecordsListSQL, "balance_before,\n       balance_after")
}
