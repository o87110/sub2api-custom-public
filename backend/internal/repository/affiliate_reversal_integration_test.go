//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/custom/affiliatereversal"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAffiliateReversalService_StrictRecoveryIsAtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	inviter := mustCreateUser(t, integrationEntClient, &service.User{
		Email:          fmt.Sprintf("affiliate-reversal-inviter-%d@example.com", time.Now().UnixNano()),
		PasswordHash:   "hash",
		Role:           service.RoleUser,
		Status:         service.StatusActive,
		Balance:        1,
		TotalRecharged: 10,
		Concurrency:    5,
	})
	invitee := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        fmt.Sprintf("affiliate-reversal-invitee-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Role:         service.RoleUser,
		Status:       service.StatusActive,
		Concurrency:  5,
	})

	now := time.Now().UTC()
	_, err := integrationEntClient.ExecContext(ctx, `
INSERT INTO user_affiliates (
    user_id, aff_code, inviter_id, aff_quota, aff_frozen_quota,
    aff_history_quota, created_at, updated_at
) VALUES
    ($1, $2, NULL, 2, 3, 8, NOW(), NOW()),
    ($3, $4, $1, 0, 0, 0, NOW(), NOW())`,
		inviter.ID,
		fmt.Sprintf("AR%010d", inviter.ID),
		invitee.ID,
		fmt.Sprintf("AE%010d", invitee.ID),
	)
	require.NoError(t, err)

	createOrder := func(sequence int, amount float64) *dbent.PaymentOrder {
		order, createErr := integrationEntClient.PaymentOrder.Create().
			SetUserID(invitee.ID).
			SetUserEmail(invitee.Email).
			SetUserName("invitee").
			SetAmount(amount).
			SetPayAmount(amount).
			SetRechargeCode("affiliate-reversal").
			SetOutTradeNo(fmt.Sprintf("affiliate-reversal-%d-%d", now.UnixNano(), sequence)).
			SetPaymentType("alipay").
			SetPaymentTradeNo(fmt.Sprintf("trade-%d-%d", now.UnixNano(), sequence)).
			SetStatus("COMPLETED").
			SetExpiresAt(now.Add(time.Hour)).
			SetClientIP("127.0.0.1").
			SetSrcHost("example.com").
			Save(ctx)
		require.NoError(t, createErr)
		return order
	}
	frozenOrder := createOrder(1, 3)
	availableOrder := createOrder(2, 5)
	frozenUntil := now.Add(time.Hour)
	for _, item := range []struct {
		orderID int64
		amount  float64
		frozen  *time.Time
	}{
		{orderID: frozenOrder.ID, amount: 3, frozen: &frozenUntil},
		{orderID: availableOrder.ID, amount: 5},
	} {
		_, err = integrationEntClient.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger (
    user_id, action, amount, source_user_id, source_order_id,
    frozen_until, created_at, updated_at
) VALUES ($1, 'accrue', $2, $3, $4, $5, NOW(), NOW())`,
			inviter.ID, item.amount, invitee.ID, item.orderID, item.frozen)
		require.NoError(t, err)
		_, err = integrationEntClient.ExecContext(ctx, `
INSERT INTO payment_audit_logs (order_id, action, detail, operator, created_at)
VALUES ($1, 'AFFILIATE_REBATE_APPLIED', $2, 'system', NOW())`,
			fmt.Sprint(item.orderID), fmt.Sprintf(`{"rebateAmount":%v}`, item.amount))
		require.NoError(t, err)
	}

	svc := affiliatereversal.NewService(integrationEntClient, nil, nil)
	orderIDs := []int64{availableOrder.ID, frozenOrder.ID}
	preview, err := svc.Preview(ctx, orderIDs)
	require.NoError(t, err)
	require.InDelta(t, 3, preview.TotalBalanceDeducted, 1e-8)
	require.True(t, preview.HasNegativeBalance)

	execution, err := idempotencyexecution.New(
		"admin.affiliates.rebates.reverse",
		fmt.Sprintf("admin:%d", inviter.ID),
		"test-key-hash",
		now,
		now.Add(time.Hour),
	)
	require.NoError(t, err)
	input := affiliatereversal.ReverseInput{
		OrderIDs:               orderIDs,
		PreviewToken:           preview.PreviewToken,
		Reason:                 "integration correction",
		ConfirmNegativeBalance: true,
	}
	result, err := svc.Reverse(ctx, input, inviter.ID, execution)
	require.NoError(t, err)
	require.Equal(t, 2, result.ReversedCount)
	require.InDelta(t, 3, result.TotalBalanceDeducted, 1e-8)

	accountRows, err := integrationEntClient.QueryContext(ctx, `
SELECT ua.aff_quota::double precision,
       ua.aff_frozen_quota::double precision,
       ua.aff_history_quota::double precision,
       u.balance::double precision,
       u.total_recharged::double precision
FROM user_affiliates ua
JOIN users u ON u.id = ua.user_id
WHERE ua.user_id = $1`, inviter.ID)
	require.NoError(t, err)
	defer func() { _ = accountRows.Close() }()
	require.True(t, accountRows.Next())
	var quota, frozen, history, balance, totalRecharged float64
	require.NoError(t, accountRows.Scan(&quota, &frozen, &history, &balance, &totalRecharged))
	require.InDelta(t, 0, quota, 1e-8)
	require.InDelta(t, 0, frozen, 1e-8)
	require.InDelta(t, 0, history, 1e-8)
	require.InDelta(t, -2, balance, 1e-8)
	require.InDelta(t, 7, totalRecharged, 1e-8)

	require.Equal(t, 2, querySingleInt(t, ctx, integrationEntClient,
		"SELECT COUNT(*) FROM user_affiliate_ledger WHERE source_order_id IN ($1, $2) AND action = 'reversed' AND frozen_until IS NULL",
		frozenOrder.ID, availableOrder.ID))
	require.Equal(t, 2, querySingleInt(t, ctx, integrationEntClient,
		"SELECT COUNT(*) FROM user_affiliate_reversals WHERE source_order_id IN ($1, $2)",
		frozenOrder.ID, availableOrder.ID))

	repo := NewAffiliateRepository(integrationEntClient, integrationDB)
	accrued, err := repo.GetAccruedRebateFromInvitee(ctx, inviter.ID, invitee.ID)
	require.NoError(t, err)
	require.Zero(t, accrued)
	thawed, err := repo.ThawFrozenQuota(ctx, inviter.ID)
	require.NoError(t, err)
	require.Zero(t, thawed)

	replayed, err := svc.Reverse(ctx, input, inviter.ID, execution)
	require.NoError(t, err)
	require.Equal(t, result.ReversedCount, replayed.ReversedCount)
	require.Equal(t, 2, querySingleInt(t, ctx, integrationEntClient,
		"SELECT COUNT(*) FROM user_affiliate_reversals WHERE source_order_id IN ($1, $2)",
		frozenOrder.ID, availableOrder.ID))
}
