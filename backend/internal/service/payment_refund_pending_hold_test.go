//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentauditlog"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type pendingRefundProviderTestDouble struct {
	refundProviderTestDouble
	queryStatus string
	refundCalls int
}

func (p *pendingRefundProviderTestDouble) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	return &payment.RefundResponse{RefundID: "rf_hold", Status: payment.ProviderStatusPending}, nil
}

func (p *pendingRefundProviderTestDouble) QueryRefund(context.Context, payment.RefundQueryRequest) (*payment.RefundResponse, error) {
	return &payment.RefundResponse{RefundID: "rf_hold", Status: p.queryStatus}, nil
}

// refundHoldWallet 模拟用户余额：DeductAvailableBalance 只能扣到当前余额。
type refundHoldWallet struct {
	balance      float64
	deductCalls  int
	restoreCalls int
}

func (w *refundHoldWallet) repo(userID int64) *mockUserRepo {
	return &mockUserRepo{
		getByIDUser: &User{ID: userID, Balance: w.balance},
		deductAvailableBalanceFn: func(_ context.Context, _ int64, amount float64) (float64, error) {
			w.deductCalls++
			deducted := math.Min(amount, math.Max(w.balance, 0))
			w.balance -= deducted
			return deducted, nil
		},
		updateBalanceFn: func(_ context.Context, _ int64, amount float64) error {
			w.restoreCalls++
			w.balance += amount
			return nil
		},
	}
}

func TestRefundPendingKeepsBalanceDeductedUntilGatewaySettles(t *testing.T) {
	for _, tc := range []struct {
		name        string
		queryStatus string
		wantStatus  string
		wantBalance float64
		wantRestore int
	}{
		{name: "gateway succeeds", queryStatus: payment.ProviderStatusSuccess, wantStatus: OrderStatusRefunded, wantBalance: 0},
		{name: "gateway fails", queryStatus: payment.ProviderStatusFailed, wantStatus: OrderStatusRefundFailed, wantBalance: 100, wantRestore: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "hold-flow")

			wallet := &refundHoldWallet{balance: 100}
			prov := &pendingRefundProviderTestDouble{queryStatus: tc.queryStatus}
			restore := replacePaymentProviderFactoryForTest(t, prov)
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet.repo(order.UserID)}

			plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "user asked", false, true)
			require.NoError(t, err)
			require.Nil(t, early)
			result, err := svc.ExecuteRefund(ctx, plan)
			require.NoError(t, err)
			require.False(t, result.Success)
			require.Contains(t, result.Warning, "pending confirmation")

			// pending 期间余额必须保持扣减，否则用户可先消费再等网关退款成功。
			require.Zero(t, wallet.balance)
			require.Zero(t, wallet.restoreCalls)
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, reloaded.Status)

			// pending 订单不能再次发起退款（会重复扣减并重复调用网关）。
			_, _, err = svc.PrepareRefund(ctx, order.ID, 0, "again", true, true)
			require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))

			result, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.queryStatus == payment.ProviderStatusSuccess, result.Success)
			if result.Success {
				require.Equal(t, 100.0, result.BalanceDeducted)
			}
			require.Equal(t, 1, wallet.deductCalls, "held deduction must not be applied twice")
			require.Equal(t, tc.wantRestore, wallet.restoreCalls)
			require.Equal(t, tc.wantBalance, wallet.balance)
			require.Equal(t, 1, prov.refundCalls)

			reloaded, err = client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, reloaded.Status)

			_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err))
			require.Equal(t, tc.wantRestore, wallet.restoreCalls)
		})
	}
}

func TestFinalizeRefundFailedRestoresHeldDeductionOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "hold-failed-once",
		`{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`)

	wallet := &refundHoldWallet{}
	svc := &PaymentService{entClient: client, userRepo: wallet.repo(order.UserID)}
	plan := svc.refundFinalizePlan(order)
	detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
	require.NoError(t, err)
	detail.applyTo(plan)

	first, err := svc.finalizeRefundFailed(ctx, plan, context.DeadlineExceeded)
	require.NoError(t, err)
	require.False(t, first.Success)
	second, err := svc.finalizeRefundFailed(ctx, plan, context.DeadlineExceeded)
	require.Nil(t, second)
	require.Equal(t, "CONFLICT", infraerrors.Reason(err))

	require.Equal(t, 1, wallet.restoreCalls)
	require.Equal(t, 60.0, wallet.balance)
}

func TestQueryAndFinalizeRefundHeldDeductionWithoutBalanceDeduction(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	// 管理员选择不扣余额时，终态收敛也不能按旧逻辑补扣。
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "hold-no-deduct",
		`{"refundID":"rf_test","deductionHeld":true,"deductionType":"none","balanceDeducted":0}`)

	wallet := &refundHoldWallet{balance: 100}
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusSuccess})
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet.repo(order.UserID)}

	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, result.Success)
	require.Zero(t, wallet.deductCalls)
	require.Equal(t, 100.0, wallet.balance)
}

type successRefundProviderTestDouble struct {
	refundProviderTestDouble
	refundCalls int
}

func (p *successRefundProviderTestDouble) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	p.refundCalls++
	return &payment.RefundResponse{RefundID: "rf_retry", Status: payment.ProviderStatusSuccess}, nil
}

func countRefundAuditsForTest(t *testing.T, ctx context.Context, client *dbent.Client, orderID int64, action string) int {
	t.Helper()
	n, err := client.PaymentAuditLog.Query().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(orderID, 10)), paymentauditlog.ActionEQ(action)).
		Count(ctx)
	require.NoError(t, err)
	return n
}

func TestFinalizeRefundFailedRecordsRollbackFailureWhenHeldDeductionCannotBeReturned(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "hold-return-fails",
		`{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`)

	wallet := &refundHoldWallet{balance: 40}
	repo := wallet.repo(order.UserID)
	repo.updateBalanceFn = func(context.Context, int64, float64) error {
		wallet.restoreCalls++
		return errors.New("db unavailable")
	}
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: repo}

	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "refund deduction rollback failed")
	require.Equal(t, 1, wallet.restoreCalls)
	require.Equal(t, 40.0, wallet.balance)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, reloaded.Status)
	require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_ROLLBACK_FAILED"))
	require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_FAILED"))

	// 扣减仍被保留：重试退款必须跳过扣减，否则同一笔退款会扣两次。
	retryProvider := &successRefundProviderTestDouble{}
	restoreRetry := replacePaymentProviderFactoryForTest(t, retryProvider)
	defer restoreRetry()
	plan, early, err := svc.PrepareRefund(ctx, order.ID, 0, "retry", true, true)
	require.NoError(t, err)
	require.Nil(t, early)
	retried, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, retried.Success)
	require.Zero(t, wallet.deductCalls)
	require.Equal(t, 40.0, wallet.balance)
	require.Equal(t, 1, retryProvider.refundCalls)
}

func TestFinalizeRefundFailedReturnsHeldDeductionAtomically(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "hold-return-atomic",
		`{"refundID":"rf_test","deductionHeld":true,"deductionType":"balance","balanceDeducted":60}`)

	wallet := &refundHoldWallet{}
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet.repo(order.UserID)}

	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.NotContains(t, result.Warning, "rollback failed")
	require.Equal(t, 1, wallet.restoreCalls)
	require.Equal(t, 60.0, wallet.balance)
	require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_FAILED"))
	require.Zero(t, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_ROLLBACK_FAILED"))
}

func TestQueryAndFinalizeRefundRejectsUnreadablePendingAudit(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	// 审计无法解析时不能退化为旧逻辑：成功会二次扣减，失败会吞掉保留的扣减。
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "hold-bad-audit", `not-json`)

	for _, queryStatus := range []string{payment.ProviderStatusSuccess, payment.ProviderStatusFailed} {
		wallet := &refundHoldWallet{balance: 100}
		restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: queryStatus})
		svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}, userRepo: wallet.repo(order.UserID)}

		result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
		restore()
		require.Error(t, err, queryStatus)
		require.Nil(t, result)
		require.Zero(t, wallet.deductCalls)
		require.Zero(t, wallet.restoreCalls)
		require.Equal(t, 100.0, wallet.balance)
	}

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundPending, reloaded.Status)
}

func TestRefundPendingKeepsExactSubscriptionSnapshotUntilGatewaySettles(t *testing.T) {
	for _, tc := range []struct {
		name        string
		queryStatus string
		wantRestore int
		wantStatus  string
	}{
		{name: "gateway succeeds", queryStatus: payment.ProviderStatusSuccess, wantStatus: OrderStatusRefunded},
		{name: "gateway fails", queryStatus: payment.ProviderStatusFailed, wantRestore: 1, wantStatus: OrderStatusRefundFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "term-hold-"+strconv.Itoa(tc.wantRestore))
			var err error
			order, err = order.Update().SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(30).SetSubscriptionDays(30).Save(ctx)
			require.NoError(t, err)

			now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
			cycleSourceRef := "payment-order-1002"
			snapshot := &subscriptionquota.TermSnapshot{
				SubscriptionID: 10, UserID: order.UserID, GroupID: 30,
				ExpiresAt: now.Add(30 * 24 * time.Hour), Status: SubscriptionStatusActive,
				CurrentCycleStartsAt: now, CurrentCycleEndsAt: now.Add(5 * 24 * time.Hour),
				Cycles: []subscriptionquota.TermCycleSnapshot{{
					ID: 101, StartsAt: now.Add(5 * 24 * time.Hour), EndsAt: now.Add(30 * 24 * time.Hour),
					Status: subscriptionquota.CycleStatusPending, SourceType: subscriptionquota.CycleSourcePayment, SourceRef: &cycleSourceRef,
				}},
				Expected: &subscriptionquota.TermExpectedState{
					ExpiresAt: now.Add(5 * 24 * time.Hour), Status: SubscriptionStatusActive,
					CurrentCycleStartsAt: now, CurrentCycleEndsAt: now.Add(5 * 24 * time.Hour),
					Cycles: []subscriptionquota.TermCycleSnapshot{{
						ID: 101, StartsAt: now.Add(5 * 24 * time.Hour), EndsAt: now.Add(5 * 24 * time.Hour),
						Status: subscriptionquota.CycleStatusCancelled, SourceType: subscriptionquota.CycleSourcePayment, SourceRef: &cycleSourceRef,
					}},
				},
			}
			repo := &refundTermSnapshotRepo{sub: UserSubscription{ID: 10, UserID: order.UserID, GroupID: 30}}
			provider := &pendingRefundProviderTestDouble{queryStatus: tc.queryStatus}
			restore := replacePaymentProviderFactoryForTest(t, provider)
			defer restore()
			svc := &PaymentService{
				entClient: client, loadBalancer: &captureLoadBalancer{},
				subscriptionSvc: NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil),
			}
			plan := &RefundPlan{
				OrderID: order.ID, Order: order, RefundAmount: order.Amount, GatewayAmount: order.PayAmount,
				DeductionType: payment.DeductionTypeSubscription, SubDaysToDeduct: 30,
				SubscriptionID: snapshot.SubscriptionID, SubscriptionTermSnapshot: snapshot,
			}
			_, err = svc.markRefundPending(ctx, plan, &payment.RefundResponse{RefundID: "rf_term", Status: payment.ProviderStatusPending})
			require.NoError(t, err)
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, snapshot, detail.SubscriptionTermSnapshot, "the exact cycle and CAS state must survive the audit round trip")

			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.queryStatus == payment.ProviderStatusSuccess, result.Success)
			require.Equal(t, tc.wantRestore, repo.restoreCalls)
			if tc.wantRestore > 0 {
				require.True(t, repo.restoreInTx, "subscription restoration must share the payment finalization transaction")
				require.Equal(t, snapshot, repo.restoredState)
				require.Equal(t, cycleSourceRef, *repo.restoredState.Cycles[0].SourceRef)
			}
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, reloaded.Status)
			_, err = svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.Equal(t, "INVALID_STATUS", infraerrors.Reason(err), "refund must settle once")
			require.Equal(t, tc.wantRestore, repo.restoreCalls)
		})
	}
}

func TestRefundPendingRejectsMissingSubscriptionSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name           string
		subscriptionID int64
	}{
		{name: "missing snapshot", subscriptionID: 10},
		{name: "missing subscription ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			detail, err := json.Marshal(refundPendingAuditDetail{
				RefundID: "rf_term", DeductionHeld: true, DeductionType: payment.DeductionTypeSubscription,
				SubDaysDeducted: 30, SubscriptionID: tc.subscriptionID,
			})
			require.NoError(t, err)
			order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "term-missing-snapshot", string(detail))
			order, err = order.Update().SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(30).SetSubscriptionDays(30).Save(ctx)
			require.NoError(t, err)
			restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
			defer restore()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
			require.Nil(t, result)
			require.ErrorContains(t, err, "snapshot is missing or mismatched")
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusRefundPending, reloaded.Status)
		})
	}
}

func TestRefundPendingSubscriptionWithoutNewDeductionTracksRetryState(t *testing.T) {
	for _, tc := range []struct {
		name              string
		subscriptionID    int64
		subDaysToDeduct   int
		wantDeductionHeld bool
	}{
		{name: "force without active subscription", subDaysToDeduct: 30},
		{name: "retry after failed rollback", subscriptionID: 10, wantDeductionHeld: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentConfigServiceTestClient(t)
			order := createCompletedRefundableOrderForTest(t, ctx, client, "term-no-deduction")
			var err error
			order, err = order.Update().SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(30).SetSubscriptionDays(30).Save(ctx)
			require.NoError(t, err)
			svc := &PaymentService{entClient: client}
			plan := &RefundPlan{
				OrderID: order.ID, Order: order, RefundAmount: order.Amount,
				DeductionType: payment.DeductionTypeSubscription, SubDaysToDeduct: tc.subDaysToDeduct,
				SubscriptionID: tc.subscriptionID,
			}
			_, err = svc.markRefundPending(ctx, plan, &payment.RefundResponse{RefundID: "rf_term", Status: payment.ProviderStatusPending})
			require.NoError(t, err)
			detail, err := svc.latestRefundPendingDetail(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, tc.wantDeductionHeld, detail.DeductionHeld)
			require.Equal(t, !tc.wantDeductionHeld, detail.DeductionRollbackOK)
		})
	}
}

func TestValidHeldSubscriptionSnapshotRequiresMatchingOrder(t *testing.T) {
	groupID := int64(30)
	order := &dbent.PaymentOrder{UserID: 20, SubscriptionGroupID: &groupID}
	snapshot := &subscriptionquota.TermSnapshot{
		SubscriptionID: 10, UserID: 20, GroupID: 30,
		Expected: &subscriptionquota.TermExpectedState{},
	}
	require.True(t, validHeldSubscriptionSnapshot(snapshot, 10, order))
	require.False(t, validHeldSubscriptionSnapshot(snapshot, 0, order))
	require.False(t, validHeldSubscriptionSnapshot(snapshot, 11, order))
	require.False(t, validHeldSubscriptionSnapshot(snapshot, 10, &dbent.PaymentOrder{UserID: 21, SubscriptionGroupID: &groupID}))
	require.False(t, validHeldSubscriptionSnapshot(snapshot, 10, &dbent.PaymentOrder{UserID: 20}))
	otherGroupID := int64(31)
	require.False(t, validHeldSubscriptionSnapshot(snapshot, 10, &dbent.PaymentOrder{UserID: 20, SubscriptionGroupID: &otherGroupID}))
}

func TestRefundPendingStaleSubscriptionSnapshotRequiresManualReconciliation(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	order := createPendingRefundOrderWithDetailForTest(t, ctx, client, "term-stale-snapshot", `{}`)
	var err error
	order, err = order.Update().SetOrderType(payment.OrderTypeSubscription).SetSubscriptionGroupID(30).SetSubscriptionDays(30).Save(ctx)
	require.NoError(t, err)
	snapshot := &subscriptionquota.TermSnapshot{
		SubscriptionID: 10, UserID: order.UserID, GroupID: 30,
		Expected: &subscriptionquota.TermExpectedState{},
	}
	detail, err := json.Marshal(refundPendingAuditDetail{
		RefundID: "rf_term", DeductionHeld: true, DeductionType: payment.DeductionTypeSubscription,
		SubDaysDeducted: 30, SubscriptionID: 10, SubscriptionTermSnapshot: snapshot,
	})
	require.NoError(t, err)
	_, err = client.PaymentAuditLog.Update().
		Where(paymentauditlog.OrderIDEQ(strconv.FormatInt(order.ID, 10)), paymentauditlog.ActionEQ("REFUND_PENDING")).
		SetDetail(string(detail)).Save(ctx)
	require.NoError(t, err)

	repo := &refundTermSnapshotRepo{restoreErr: subscriptionquota.ErrTermSnapshotStale}
	restore := replacePaymentProviderFactoryForTest(t, &pendingRefundProviderTestDouble{queryStatus: payment.ProviderStatusFailed})
	defer restore()
	svc := &PaymentService{
		entClient: client, loadBalancer: &captureLoadBalancer{},
		subscriptionSvc: NewSubscriptionService(groupRepoNoop{}, repo, nil, nil, nil),
	}
	result, err := svc.QueryAndFinalizeRefund(ctx, order.ID)
	require.NoError(t, err)
	require.False(t, result.Success)
	require.Contains(t, result.Warning, "rollback failed")
	require.Equal(t, 1, repo.restoreCalls)
	require.True(t, repo.restoreInTx)
	require.Nil(t, repo.restoredState)
	require.Equal(t, 1, countRefundAuditsForTest(t, ctx, client, order.ID, "REFUND_ROLLBACK_FAILED"))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusRefundFailed, reloaded.Status)

	// 失败补偿已经留下人工处理标记，重新向网关退款时不能再次扣减周期。
	retryProvider := &successRefundProviderTestDouble{}
	restoreRetry := replacePaymentProviderFactoryForTest(t, retryProvider)
	defer restoreRetry()
	plan := &RefundPlan{OrderID: order.ID, Order: reloaded, RefundAmount: order.Amount,
		DeductionType: payment.DeductionTypeSubscription, SubDaysToDeduct: 30, SubscriptionID: 10}
	retried, err := svc.ExecuteRefund(ctx, plan)
	require.NoError(t, err)
	require.True(t, retried.Success)
	require.Equal(t, 1, repo.restoreCalls)
	require.Equal(t, 1, retryProvider.refundCalls)
}

func createCompletedRefundableOrderForTest(t *testing.T, ctx context.Context, client *dbent.Client, suffix string) *dbent.PaymentOrder {
	t.Helper()
	user, err := client.User.Create().
		SetEmail(suffix + "@example.com").
		SetPasswordHash("hash").
		SetUsername(suffix).
		Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeStripe).
		SetName(suffix + "-provider").
		SetConfig("{}").
		SetSupportedTypes("stripe").
		SetEnabled(true).
		SetRefundEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(100).
		SetPayAmount(100).
		SetFeeRate(0).
		SetRechargeCode("REFUND-" + suffix).
		SetOutTradeNo("sub2_" + suffix).
		SetPaymentType(payment.TypeStripe).
		SetPaymentTradeNo("pi_" + suffix).
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusCompleted).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetPaidAt(time.Now()).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)
	return order
}
