package affiliatereversal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculatePlanUsesFrozenThenAvailableThenBalance(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	frozenUntil := now.Add(time.Hour)
	targets := []targetSnapshot{
		{LedgerID: 1, OrderID: 101, InviterID: 7, InviteeID: 11, Amount: 3, FrozenUntil: &frozenUntil, CreatedAt: now},
		{LedgerID: 2, OrderID: 102, InviterID: 7, InviteeID: 12, Amount: 8, CreatedAt: now.Add(time.Minute)},
	}
	accounts := map[int64]accountSnapshot{
		7: {
			InviterID:       7,
			Email:           "inviter@example.com",
			AffQuota:        5,
			AffFrozenQuota:  3,
			AffHistoryQuota: 20,
			Balance:         1,
			TotalRecharged:  10,
		},
	}

	plan, err := calculatePlan(targets, accounts)
	require.NoError(t, err)
	require.Equal(t, 2, plan.Preview.OrderCount)
	require.InDelta(t, 11, plan.Preview.TotalRebateAmount, moneyEpsilon)
	require.InDelta(t, 3, plan.Preview.TotalBalanceDeducted, moneyEpsilon)
	require.True(t, plan.Preview.HasNegativeBalance)
	require.Equal(t, "frozen", plan.Preview.Orders[0].QuotaBucket)
	require.InDelta(t, 3, plan.Preview.Orders[0].FrozenDeducted, moneyEpsilon)
	require.Equal(t, "available", plan.Preview.Orders[1].QuotaBucket)
	require.InDelta(t, 5, plan.Preview.Orders[1].QuotaDeducted, moneyEpsilon)
	require.InDelta(t, 3, plan.Preview.Orders[1].BalanceDeducted, moneyEpsilon)

	final := plan.FinalAccounts[7]
	require.InDelta(t, 0, final.AffQuota, moneyEpsilon)
	require.InDelta(t, 0, final.AffFrozenQuota, moneyEpsilon)
	require.InDelta(t, 9, final.AffHistoryQuota, moneyEpsilon)
	require.InDelta(t, -2, final.Balance, moneyEpsilon)
	require.InDelta(t, 7, final.TotalRecharged, moneyEpsilon)
}

func TestCalculatePlanRejectsInconsistentHistoryOrRechargeTotals(t *testing.T) {
	t.Parallel()
	target := targetSnapshot{LedgerID: 1, OrderID: 101, InviterID: 7, InviteeID: 11, Amount: 5, CreatedAt: time.Now()}

	_, err := calculatePlan([]targetSnapshot{target}, map[int64]accountSnapshot{
		7: {InviterID: 7, AffHistoryQuota: 4, AffQuota: 5, TotalRecharged: 100},
	})
	require.ErrorIs(t, err, ErrInvariant)

	_, err = calculatePlan([]targetSnapshot{target}, map[int64]accountSnapshot{
		7: {InviterID: 7, AffHistoryQuota: 5, AffQuota: 0, TotalRecharged: 4},
	})
	require.ErrorIs(t, err, ErrInvariant)
}

func TestPreviewTokenChangesWithFinancialSnapshot(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 30, 10, 0, 0, 0, time.UTC)
	targets := []targetSnapshot{{LedgerID: 1, OrderID: 101, InviterID: 7, InviteeID: 11, Amount: 5, CreatedAt: now, LedgerUpdatedAt: now}}
	accounts := map[int64]accountSnapshot{7: {InviterID: 7, AffQuota: 5, AffHistoryQuota: 5, Balance: 1, TotalRecharged: 5, AffiliateUpdatedAt: now, UserUpdatedAt: now}}

	first, err := previewToken(targets, accounts)
	require.NoError(t, err)
	second, err := previewToken(targets, accounts)
	require.NoError(t, err)
	require.Equal(t, first, second)

	changed := accounts[7]
	changed.Balance = 0.5
	accounts[7] = changed
	third, err := previewToken(targets, accounts)
	require.NoError(t, err)
	require.NotEqual(t, first, third)
}

func TestNormalizeOrderIDsDeduplicatesAndSorts(t *testing.T) {
	t.Parallel()
	ids, err := NormalizeOrderIDs([]int64{5, 3, 5, 4})
	require.NoError(t, err)
	require.Equal(t, []int64{3, 4, 5}, ids)

	_, err = NormalizeOrderIDs([]int64{0})
	require.ErrorIs(t, err, ErrInvalidOrders)
}
