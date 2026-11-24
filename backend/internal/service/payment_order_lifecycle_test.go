//go:build unit

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/custom/gmpayprovider"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptioninventory"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type paymentOrderLifecycleQueryProvider struct {
	key               string
	lastQueryTradeNo  string
	lastCancelTradeNo string
	queryCalls        int
	cancelCalls       int
	responses         []*payment.QueryOrderResponse
	resp              *payment.QueryOrderResponse
	queryErr          error
	cancelErr         error
}

type paymentOrderLifecycleRedeemRepo struct {
	codesByCode map[string]*RedeemCode
	useCalls    []struct {
		id     int64
		userID int64
	}
}

func (p *paymentOrderLifecycleQueryProvider) Name() string {
	return "payment-order-lifecycle-query-provider"
}

func (p *paymentOrderLifecycleQueryProvider) ProviderKey() string {
	if p.key != "" {
		return p.key
	}
	return payment.TypeAlipay
}

func (p *paymentOrderLifecycleQueryProvider) SupportedTypes() []payment.PaymentType {
	return []payment.PaymentType{p.ProviderKey()}
}

func (p *paymentOrderLifecycleQueryProvider) CreatePayment(context.Context, payment.CreatePaymentRequest) (*payment.CreatePaymentResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) QueryOrder(_ context.Context, tradeNo string) (*payment.QueryOrderResponse, error) {
	p.lastQueryTradeNo = tradeNo
	p.queryCalls++
	if p.queryErr != nil {
		return nil, p.queryErr
	}
	if len(p.responses) > 0 {
		resp := p.responses[0]
		if len(p.responses) > 1 {
			p.responses = p.responses[1:]
		}
		return resp, nil
	}
	return p.resp, nil
}

func (p *paymentOrderLifecycleQueryProvider) VerifyNotification(context.Context, string, map[string]string) (*payment.PaymentNotification, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) Refund(context.Context, payment.RefundRequest) (*payment.RefundResponse, error) {
	panic("unexpected call")
}

func (p *paymentOrderLifecycleQueryProvider) CancelPayment(_ context.Context, tradeNo string) error {
	p.lastCancelTradeNo = tradeNo
	p.cancelCalls++
	return p.cancelErr
}

func (r *paymentOrderLifecycleRedeemRepo) Create(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) CreateBatch(context.Context, []RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) GetByID(_ context.Context, id int64) (*RedeemCode, error) {
	for _, code := range r.codesByCode {
		if code.ID != id {
			continue
		}
		cloned := *code
		return &cloned, nil
	}
	return nil, ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) GetByCode(_ context.Context, code string) (*RedeemCode, error) {
	redeemCode, ok := r.codesByCode[code]
	if !ok {
		return nil, ErrRedeemCodeNotFound
	}
	cloned := *redeemCode
	return &cloned, nil
}

func (r *paymentOrderLifecycleRedeemRepo) Update(context.Context, *RedeemCode) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) BatchUpdate(context.Context, []int64, RedeemCodeBatchUpdateFields) (int64, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Delete(context.Context, int64) error {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) Use(_ context.Context, id, userID int64) error {
	for code, redeemCode := range r.codesByCode {
		if redeemCode.ID != id {
			continue
		}
		now := time.Now().UTC()
		redeemCode.Status = StatusUsed
		redeemCode.UsedBy = &userID
		redeemCode.UsedAt = &now
		r.codesByCode[code] = redeemCode
		r.useCalls = append(r.useCalls, struct {
			id     int64
			userID int64
		}{id: id, userID: userID})
		return nil
	}
	return ErrRedeemCodeNotFound
}

func (r *paymentOrderLifecycleRedeemRepo) List(context.Context, pagination.PaginationParams) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUser(context.Context, int64, int) ([]RedeemCode, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) ListByUserPaginated(context.Context, int64, pagination.PaginationParams, string) ([]RedeemCode, *pagination.PaginationResult, error) {
	panic("unexpected call")
}

func (r *paymentOrderLifecycleRedeemRepo) SumPositiveBalanceByUser(context.Context, int64) (float64, error) {
	panic("unexpected call")
}

func TestVerifyOrderByOutTradeNoBackfillsTradeNoFromPaidQuery(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_trade_no_missing").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-123", got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "upstream-trade-123", reloaded.PaymentTradeNo)

	require.Equal(t, 88.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
	require.Equal(t, int64(1), redeemRepo.useCalls[0].id)
	require.Equal(t, user.ID, redeemRepo.useCalls[0].userID)
}

func TestVerifyOrderByOutTradeNoRetriesZeroAmountPaidQueryOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-retry@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-retry-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-UPSTREAM-RETRY").
		SetOutTradeNo("sub2_checkpaid_retry_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		responses: []*payment.QueryOrderResponse{
			{
				TradeNo: "upstream-trade-zero",
				Status:  payment.ProviderStatusPaid,
				Amount:  0,
			},
			{
				TradeNo: "upstream-trade-retry",
				Status:  payment.ProviderStatusPaid,
				Amount:  88,
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, 2, provider.queryCalls)
	require.Equal(t, OrderStatusCompleted, got.Status)
	require.Equal(t, "upstream-trade-retry", got.PaymentTradeNo)
}

func TestVerifyOrderByOutTradeNoRejectsPaidQueryWithZeroAmount(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-zero-amount@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-zero-amount-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-ZERO-AMOUNT").
		SetOutTradeNo("sub2_checkpaid_zero_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-zero",
			Status:  payment.ProviderStatusPaid,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Empty(t, got.PaymentTradeNo)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Empty(t, reloaded.PaymentTradeNo)

	require.Equal(t, 0.0, userRepo.getByIDUser.Balance)
	require.Empty(t, redeemRepo.useCalls)
}

func TestVerifyOrderByOutTradeNoDoesNotCancelUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-pending-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-PENDING").
		SetOutTradeNo("sub2_checkpaid_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, got.Status)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestCancelOrderStillClosesUnpaidUpstreamOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("cancel-pending@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-pending-user").
		Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("cancel inventory plan").
		SetPrice(88).
		SetRemainingQuantity(0).
		SetForSale(false).
		SetInventoryAutoDelisted(true).
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-PENDING").
		SetOutTradeNo("sub2_cancel_pending").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetPlanID(plan.ID).
		SetPlanInventoryState(subscriptioninventory.StateReserved).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
			Amount:  0,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Equal(t, order.OutTradeNo, provider.lastCancelTradeNo)
	require.Equal(t, 1, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
	plan, err = client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, 1, *plan.RemainingQuantity)
	require.True(t, plan.ForSale)
	require.False(t, plan.InventoryAutoDelisted)
}

func TestCancelOrderReturnsConflictWhenUpstreamAlreadyPaid(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-paid@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-paid-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-PAID").
		SetOutTradeNo("sub2_cancel_paid").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-paid-cancel",
			Status:  payment.ProviderStatusPaid,
			Amount:  10,
		},
	})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	_, err = svc.CancelOrder(ctx, order.ID, user.ID)
	require.Equal(t, "ORDER_ALREADY_PAID", infraerrors.Reason(err))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
}

func TestCancelOrderReturnsConflictWhenAlreadyPaidLocally(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-local-paid@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-local-paid-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-LOCAL-PAID").
		SetOutTradeNo("sub2_cancel_local_paid").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("upstream-paid").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPaid).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	_, err = (&PaymentService{entClient: client}).CancelOrder(ctx, order.ID, user.ID)
	require.Equal(t, "ORDER_ALREADY_PAID", infraerrors.Reason(err))
}

func TestCancelOrderFallsBackWhenUpstreamCancellationFails(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-unavailable@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-unavailable-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-UNAVAILABLE").
		SetOutTradeNo("sub2_cancel_unavailable").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{
		resp:      &payment.QueryOrderResponse{TradeNo: order.OutTradeNo, Status: payment.ProviderStatusPending},
		cancelErr: errors.New("upstream close failed"),
	})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}

func TestCancelOrderFallsBackWhenPaymentProviderUnavailable(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-provider-unavailable@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-provider-unavailable-user").
		Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("cancel provider unavailable inventory plan").
		SetPrice(10).
		SetRemainingQuantity(0).
		SetForSale(false).
		SetInventoryAutoDelisted(true).
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-PROVIDER-UNAVAILABLE").
		SetOutTradeNo("sub2_cancel_provider_unavailable").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(plan.ID).
		SetPlanInventoryState(subscriptioninventory.StateReserved).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{queryErr: errors.New("provider unavailable")}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Zero(t, provider.cancelCalls)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
	plan, err = client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, 1, *plan.RemainingQuantity)
	require.True(t, plan.ForSale)
	require.False(t, plan.InventoryAutoDelisted)
}

func TestCancelOrderDoesNotFallbackWhenProviderAmountIsInvalid(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-invalid-amount@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-invalid-amount-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-INVALID-AMOUNT").
		SetOutTradeNo("sub2_cancel_invalid_amount").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{TradeNo: order.OutTradeNo, Status: payment.ProviderStatusPaid, Amount: 0},
	})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	_, err = svc.CancelOrder(ctx, order.ID, user.ID)
	require.Equal(t, "PAYMENT_STATUS_UNAVAILABLE", infraerrors.Reason(err))
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

func TestCancelOrderClosesLocallyWhenUpstreamAlreadyFailed(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-failed@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-failed-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-FAILED").
		SetOutTradeNo("sub2_cancel_failed").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusFailed,
		},
		cancelErr: errors.New("upstream trade already closed"),
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	outcome, err := svc.CancelOrder(ctx, order.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, checkPaidResultCancelled, outcome)
	require.Zero(t, provider.cancelCalls)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCancelled, reloaded.Status)
}

func TestCancelOrderReturnsConflictWhenAnotherTaskAlreadyCancelled(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("cancel-race@example.com").
		SetPasswordHash("hash").
		SetUsername("cancel-race-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("CANCEL-RACE").
		SetOutTradeNo("sub2_cancel_race").
		SetPaymentType("").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	// Simulate a timeout/cancel worker winning the conditional transition before
	// the request's transition is attempted.
	_, err = subscriptioninventory.TransitionPendingOrderAndRelease(ctx, client, order.ID, OrderStatusCancelled)
	require.NoError(t, err)

	_, err = (&PaymentService{entClient: client}).cancelCore(ctx, order, OrderStatusCancelled, "user:1", "user cancelled order")
	require.Equal(t, "ORDER_STATUS_CHANGED", infraerrors.Reason(err))
}

func TestReconcilePendingPaymentOrdersRecoversEasyPayOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("easypay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("easypay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EASYPAY-RECONCILE").
		SetOutTradeNo("sub2_easypay_reconcile").
		SetPaymentType(payment.TypeEasyPay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeEasyPay,
		resp: &payment.QueryOrderResponse{
			TradeNo: "easypay-upstream-trade",
			Status:  payment.ProviderStatusPaid,
			Amount:  10,
		},
	}
	registry.Register(provider)
	userRepo := &mockUserRepo{
		getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username, Balance: 0},
	}
	userRepo.updateBalanceFn = func(context.Context, int64, float64) error {
		userRepo.getByIDUser.Balance += 10
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(redeemRepo, userRepo, nil, nil, nil, client, nil, nil)
	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "easypay-upstream-trade", reloaded.PaymentTradeNo)
	require.Equal(t, 10.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestExpireTimedOutOrderFallsBackOnEmptyOrUnsupportedUpstreamResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		slug string
		resp *payment.QueryOrderResponse
	}{
		{name: "nil response", slug: "nil", resp: nil},
		{name: "empty status", slug: "empty", resp: &payment.QueryOrderResponse{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentOrderLifecycleTestClient(t)
			user, err := client.User.Create().
				SetEmail("empty-upstream-" + tc.slug + "@example.com").
				SetPasswordHash("hash").
				SetUsername("empty-upstream-" + tc.slug).
				Save(ctx)
			require.NoError(t, err)
			order, err := client.PaymentOrder.Create().
				SetUserID(user.ID).
				SetUserEmail(user.Email).
				SetUserName(user.Username).
				SetAmount(10).
				SetPayAmount(10).
				SetFeeRate(0).
				SetRechargeCode("EMPTY-UPSTREAM-" + tc.slug).
				SetOutTradeNo("sub2_empty_upstream_" + tc.slug).
				SetPaymentType(payment.TypeAlipay).
				SetPaymentTradeNo("").
				SetOrderType(payment.OrderTypeBalance).
				SetStatus(OrderStatusPending).
				SetExpiresAt(time.Now().Add(-time.Minute)).
				SetClientIP("127.0.0.1").
				SetSrcHost("api.example.com").
				Save(ctx)
			require.NoError(t, err)

			registry := payment.NewRegistry()
			registry.Register(&paymentOrderLifecycleQueryProvider{resp: tc.resp})
			svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

			expired, err := svc.ExpireTimedOutOrders(ctx)
			require.NoError(t, err)
			require.Equal(t, 1, expired)
			reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
			require.NoError(t, err)
			require.Equal(t, OrderStatusExpired, reloaded.Status)
		})
	}
}

func TestReconcilePendingPaymentOrdersRetriesPaidFulfillment(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("paid-fulfillment-retry@example.com").
		SetPasswordHash("hash").
		SetUsername("paid-fulfillment-retry-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("PAID-FULFILLMENT-RETRY").
		SetOutTradeNo("sub2_paid_fulfillment_retry").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("paid-upstream-trade").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPaid).
		SetPaidAt(time.Now().Add(-time.Minute)).
		SetExpiresAt(time.Now().Add(-time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{ID: user.ID, Email: user.Email, Username: user.Username, Balance: 0},
	}
	userRepo.updateBalanceFn = func(context.Context, int64, float64) error {
		userRepo.getByIDUser.Balance += 10
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(redeemRepo, userRepo, nil, nil, nil, client, nil, nil)
	svc := &PaymentService{entClient: client, redeemService: redeemService, userRepo: userRepo}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, 10.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestExpireTimedOutOrderFallsBackWhenPaymentProviderUnavailable(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("expiry-unavailable@example.com").
		SetPasswordHash("hash").
		SetUsername("expiry-unavailable-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRY-UNAVAILABLE").
		SetOutTradeNo("sub2_expiry_unavailable").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{queryErr: errors.New("provider unavailable")})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
}

func TestExpireTimedOutOrderExpiresWhenUpstreamAlreadyFailed(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("expiry-failed@example.com").
		SetPasswordHash("hash").
		SetUsername("expiry-failed-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRY-FAILED").
		SetOutTradeNo("sub2_expiry_failed").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusFailed,
		},
		cancelErr: errors.New("upstream trade already closed"),
	}
	registry := payment.NewRegistry()
	registry.Register(provider)
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, expired)
	require.Zero(t, provider.cancelCalls)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, reloaded.Status)
}

func TestReconcilePendingPaymentOrdersRecoversRecentlyExpiredOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("expired-paid@example.com").
		SetPasswordHash("hash").
		SetUsername("expired-paid-user").
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetFeeRate(0).
		SetRechargeCode("EXPIRED-PAID").
		SetOutTradeNo("sub2_expired_paid").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetStatus(OrderStatusExpired).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	registry.Register(&paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{TradeNo: "expired-upstream-trade", Status: payment.ProviderStatusPaid, Amount: 10},
	})
	svc := &PaymentService{entClient: client, registry: registry, providersLoaded: true}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPaid, reloaded.Status)
}

func TestExpireTimedOutOrderReleasesReservedInventory(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	user, err := client.User.Create().
		SetEmail("expired-inventory@example.com").
		SetPasswordHash("hash").
		SetUsername("expired-inventory-user").
		Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("expired inventory plan").
		SetPrice(20).
		SetRemainingQuantity(0).
		SetForSale(false).
		SetInventoryAutoDelisted(true).
		Save(ctx)
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(20).
		SetPayAmount(20).
		SetRechargeCode("EXPIRED-INVENTORY").
		SetPaymentType("").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(plan.ID).
		SetPlanInventoryState(subscriptioninventory.StateReserved).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	count, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	order, err = client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusExpired, order.Status)
	plan, err = client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, 1, *plan.RemainingQuantity)
	require.True(t, plan.ForSale)
}

func TestReconcilePendingPaymentOrdersBackfillsPaidOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("wxpay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("wxpay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(50).
		SetFeeRate(0).
		SetRechargeCode("WXPAY-RECONCILE").
		SetOutTradeNo("sub2_wxpay_reconcile").
		SetPaymentType(payment.TypeWxpay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeWxpay,
		resp: &payment.QueryOrderResponse{
			TradeNo: "wxpay-upstream-trade-123",
			Status:  payment.ProviderStatusPaid,
			Amount:  50,
			Metadata: map[string]string{
				"trade_state": "SUCCESS",
			},
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Zero(t, provider.cancelCalls)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusCompleted, reloaded.Status)
	require.Equal(t, "wxpay-upstream-trade-123", reloaded.PaymentTradeNo)
	require.Equal(t, 50.0, userRepo.getByIDUser.Balance)
	require.Len(t, redeemRepo.useCalls, 1)
}

func TestReconcilePendingPaymentOrdersQueriesAlipayOrder(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("alipay-reconcile@example.com").
		SetPasswordHash("hash").
		SetUsername("alipay-reconcile-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(50).
		SetPayAmount(50).
		SetFeeRate(0).
		SetRechargeCode("ALIPAY-RECONCILE").
		SetOutTradeNo("sub2_alipay_reconcile").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeAlipay,
		resp: &payment.QueryOrderResponse{
			TradeNo: order.OutTradeNo,
			Status:  payment.ProviderStatusPending,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		providersLoaded: true,
	}

	recovered, err := svc.ReconcilePendingPaymentOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, recovered)
	require.Equal(t, 1, provider.queryCalls)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
}

func TestVerifyOrderByOutTradeNoUsesOutTradeNoWhenPaymentTradeNoAlreadyExistsForAlipay(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	user, err := client.User.Create().
		SetEmail("checkpaid-existing-trade@example.com").
		SetPasswordHash("hash").
		SetUsername("checkpaid-existing-trade-user").
		Save(ctx)
	require.NoError(t, err)

	order, err := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("CHECKPAID-EXISTING-TRADE-NO").
		SetOutTradeNo("sub2_checkpaid_use_out_trade_no").
		SetPaymentType(payment.TypeAlipay).
		SetPaymentTradeNo("upstream-trade-existing").
		SetOrderType(payment.OrderTypeBalance).
		SetStatus(OrderStatusPending).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		Save(ctx)
	require.NoError(t, err)

	userRepo := &mockUserRepo{
		getByIDUser: &User{
			ID:       user.ID,
			Email:    user.Email,
			Username: user.Username,
			Balance:  0,
		},
	}
	userRepo.updateBalanceFn = func(ctx context.Context, id int64, amount float64) error {
		require.Equal(t, user.ID, id)
		if userRepo.getByIDUser != nil {
			userRepo.getByIDUser.Balance += amount
		}
		return nil
	}
	redeemRepo := &paymentOrderLifecycleRedeemRepo{
		codesByCode: map[string]*RedeemCode{
			order.RechargeCode: {
				ID:     1,
				Code:   order.RechargeCode,
				Type:   RedeemTypeBalance,
				Value:  order.Amount,
				Status: StatusUnused,
			},
		},
	}
	redeemService := NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		client,
		nil,
		nil,
	)
	registry := payment.NewRegistry()
	provider := &paymentOrderLifecycleQueryProvider{
		resp: &payment.QueryOrderResponse{
			TradeNo: "upstream-trade-existing",
			Status:  payment.ProviderStatusPaid,
			Amount:  88,
		},
	}
	registry.Register(provider)

	svc := &PaymentService{
		entClient:       client,
		registry:        registry,
		redeemService:   redeemService,
		userRepo:        userRepo,
		providersLoaded: true,
	}

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, user.ID)
	require.NoError(t, err)
	require.Equal(t, order.OutTradeNo, provider.lastQueryTradeNo)
	require.Equal(t, "upstream-trade-existing", got.PaymentTradeNo)
}

func TestPaymentOrderAllowsRegistryFallbackOnlyForLegacyOrdersWithoutPinnedProviderState(t *testing.T) {
	t.Parallel()

	require.True(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
	}))

	instanceID := "12"
	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType:        payment.TypeAlipay,
		ProviderInstanceID: &instanceID,
	}))

	require.False(t, paymentOrderAllowsRegistryFallback(&dbent.PaymentOrder{
		PaymentType: payment.TypeAlipay,
		ProviderSnapshot: map[string]any{
			"schema_version":       2,
			"provider_instance_id": "12",
		},
	}))
}

func TestPaymentOrderQueryReferenceUsesOutTradeNoForOfficialProviders(t *testing.T) {
	t.Parallel()

	order := &dbent.PaymentOrder{
		PaymentType:    payment.TypeWxpay,
		OutTradeNo:     "sub2_out_trade_no",
		PaymentTradeNo: "wx-transaction-id",
	}

	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, &paymentOrderLifecycleQueryProvider{}))
	require.Equal(t, "sub2_out_trade_no", paymentOrderQueryReference(order, paymentFulfillmentTestProvider{
		key: payment.TypeWxpay,
	}))
}

func newPaymentOrderLifecycleTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:payment_order_lifecycle?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// createGmpayLifecycleOrder creates a pending GMPay order with the snapshot
// and trade binding a real creation would persist.
func createGmpayLifecycleOrder(t *testing.T, client *dbent.Client, outTradeNo, tradeNo string) *dbent.PaymentOrder {
	return createGmpayLifecycleInventoryOrder(t, client, outTradeNo, tradeNo, nil, time.Now().Add(time.Hour))
}

// createGmpayLifecycleInventoryOrder builds a GMPay order, optionally bound
// to a plan with a live reserved inventory unit and an arbitrary expiry.
func createGmpayLifecycleInventoryOrder(t *testing.T, client *dbent.Client, outTradeNo, tradeNo string, plan *dbent.SubscriptionPlan, expiresAt time.Time) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	user, err := client.User.Create().
		SetEmail("gmpay-" + outTradeNo + "@example.com").
		SetPasswordHash("hash").
		SetUsername("gmpay-" + outTradeNo).
		Save(ctx)
	require.NoError(t, err)
	create := client.PaymentOrder.Create().
		SetUserID(user.ID).
		SetUserEmail(user.Email).
		SetUserName(user.Username).
		SetAmount(88).
		SetPayAmount(88).
		SetFeeRate(0).
		SetRechargeCode("GMPAY-" + outTradeNo).
		SetOutTradeNo(outTradeNo).
		SetPaymentType("usdt").
		SetProviderKey(payment.TypeEasyPay).
		SetPaymentTradeNo(tradeNo).
		SetStatus(OrderStatusPending).
		SetExpiresAt(expiresAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("api.example.com").
		SetProviderSnapshot(map[string]any{
			"schema_version":   2,
			"provider_key":     payment.TypeEasyPay,
			"protocol":         "epusdt_gmpay",
			"payment_network":  "trc20",
			"upstream_network": "tron",
		})
	if plan != nil {
		create = create.
			SetOrderType(payment.OrderTypeSubscription).
			SetPlanID(plan.ID).
			SetPlanInventoryState(subscriptioninventory.StateReserved)
	} else {
		create = create.SetOrderType(payment.OrderTypeBalance)
	}
	order, err := create.Save(ctx)
	require.NoError(t, err)
	return order
}

// gmpayLifecycleInventoryFixture creates a limited plan (one unit already
// reserved by the order) plus an already-expired GMPay subscription order,
// wired to the stub provider through the real instance path.
func gmpayLifecycleInventoryFixture(t *testing.T, client *dbent.Client, outTradeNo, tradeNo string) (*dbent.PaymentOrder, *dbent.SubscriptionPlan) {
	t.Helper()
	ctx := context.Background()
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("GMPay protective plan " + outTradeNo).
		SetPrice(88).
		SetRemainingQuantity(4).
		SetForSale(true).
		SetInventoryAutoDelisted(false).
		Save(ctx)
	require.NoError(t, err)
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeEasyPay).
		SetName("EPUSDT GMPay " + outTradeNo).
		SetConfig("{}").
		SetSupportedTypes("usdt").
		SetEnabled(true).
		SetPaymentMode("qrcode").
		Save(ctx)
	require.NoError(t, err)
	order := createGmpayLifecycleInventoryOrder(t, client, outTradeNo, tradeNo, plan, time.Now().Add(-time.Minute))
	_, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)
	return order, plan
}

// gmpayLifecycleFixture wires a GMPay order to a stub provider through the
// real instance path (snapshot orders never fall back to the registry).
func gmpayLifecycleFixture(t *testing.T, client *dbent.Client, outTradeNo, tradeNo string) *dbent.PaymentOrder {
	t.Helper()
	ctx := context.Background()
	inst, err := client.PaymentProviderInstance.Create().
		SetProviderKey(payment.TypeEasyPay).
		SetName("EPUSDT GMPay " + outTradeNo).
		SetConfig("{}").
		SetSupportedTypes("usdt").
		SetEnabled(true).
		SetPaymentMode("qrcode").
		Save(ctx)
	require.NoError(t, err)
	order := createGmpayLifecycleOrder(t, client, outTradeNo, tradeNo)
	_, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetProviderInstanceID(strconv.FormatInt(inst.ID, 10)).
		Save(ctx)
	require.NoError(t, err)
	return order
}

// TestGmpayQueryPaidDoesNotAutoFulfill: a switched-network child payment
// settles the parent upstream without touching its network projection, so a
// paid checkout query is not trustworthy fulfillment evidence. The order must
// stay pending (no balance credit, no trade_no rewrite) and wait for the
// signed callback.
func TestGmpayQueryPaidDoesNotAutoFulfill(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeEasyPay,
		resp: &payment.QueryOrderResponse{
			TradeNo:  "gmpay-trade-original",
			Status:   payment.ProviderStatusPaid,
			Amount:   88,
			Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"},
		},
	}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order := gmpayLifecycleFixture(t, client, "sub2_gmpay_query_paid", "gmpay-trade-original")

	got, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	// Upstream reports paid: the caller learns the upstream state, but the
	// local order must not be fulfilled on query-only evidence.
	require.Equal(t, OrderStatusPending, got.Status)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Equal(t, "gmpay-trade-original", reloaded.PaymentTradeNo)

	audits, err := svc.GetOrderAuditLogs(ctx, order.ID)
	require.NoError(t, err)
	require.NotEmpty(t, audits)
	actions := make([]string, 0, len(audits))
	for _, entry := range audits {
		actions = append(actions, entry.Action)
	}
	require.Contains(t, actions, "GMPAY_QUERY_PAID_UNVERIFIED")
	held, _, holdErr := svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.True(t, held, "query-paid hold must be persisted")
}

// TestGmpayQueryPaidDifferentTradeNoKeepsBinding: even when the query answers
// with a different trade id, the locally bound payment_trade_no must never be
// rewritten and the order must not fulfill.
func TestGmpayQueryPaidDifferentTradeNoKeepsBinding(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeEasyPay,
		resp: &payment.QueryOrderResponse{
			TradeNo:  "gmpay-trade-foreign",
			Status:   payment.ProviderStatusPaid,
			Amount:   88,
			Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"},
		},
	}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order := gmpayLifecycleFixture(t, client, "sub2_gmpay_trade_rewrite", "gmpay-trade-original")

	_, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)

	reloaded, err := client.PaymentOrder.Get(ctx, order.ID)
	require.NoError(t, err)
	require.Equal(t, "gmpay-trade-original", reloaded.PaymentTradeNo)
	require.Equal(t, OrderStatusPending, reloaded.Status)
}

// TestGmpayManualReviewHoldSurvivesQueryOutage: once status 6 has been seen,
// the hold is persisted on the audit trail — a later query outage must not
// fall back to the provider-unavailable local cancel path and release the
// reserved inventory.
// assertGmpayOrderAndInventoryUntouched verifies both the order status and
// the reserved inventory unit stayed unchanged.
func assertGmpayOrderAndInventoryUntouched(t *testing.T, client *dbent.Client, svc *PaymentService, order *dbent.PaymentOrder, plan *dbent.SubscriptionPlan) {
	t.Helper()
	reloaded, err := client.PaymentOrder.Get(context.Background(), order.ID)
	require.NoError(t, err)
	require.Equal(t, OrderStatusPending, reloaded.Status)
	require.Equal(t, subscriptioninventory.StateReserved, reloaded.PlanInventoryState)
	reloadedPlan, err := client.SubscriptionPlan.Get(context.Background(), plan.ID)
	require.NoError(t, err)
	require.Equal(t, plan.RemainingQuantity, reloadedPlan.RemainingQuantity)
}

// TestGmpayManualReviewHoldSurvivesQueryOutage: once status 6 has been seen
// on an already-expired order with a live reserved inventory unit, the hold
// is persisted on the audit trail — a later query outage must not fall back
// to the provider-unavailable local cancel path and release the inventory.
func TestGmpayManualReviewHoldSurvivesQueryOutage(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeEasyPay,
		resp: &payment.QueryOrderResponse{
			TradeNo:  "gmpay-trade-hold",
			Status:   payment.ProviderStatusPending,
			Amount:   88,
			Metadata: map[string]string{gmpayprovider.GmpayManualReviewMetadataKey: "true"},
		},
	}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_hold_outage", "gmpay-trade-hold")

	// The order is already expired; the expiry sweep contacts upstream, sees
	// status 6 and must skip it (status and inventory untouched).
	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)

	// Next query fails entirely (outage): the persisted hold must keep the
	// local transition held instead of expiring/cancelling the order.
	provider.resp = nil
	provider.queryErr = fmt.Errorf("upstream unreachable")
	expired, err = svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)

	_, err = svc.CancelOrder(ctx, order.ID, order.UserID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "manual review")
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

// TestGmpayManualReviewHoldReentryPersists: status may oscillate 6 → waiting
// → 6 while the order is still live. The audit table has a unique
// (order_id, action) index, so the second hold entry must still persist —
// after the re-entry, a query outage on the (now expired) order must be
// protected again.
func TestGmpayManualReviewHoldReentryPersists(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	manualReview := &payment.QueryOrderResponse{
		TradeNo:  "gmpay-trade-reentry",
		Status:   payment.ProviderStatusPending,
		Amount:   88,
		Metadata: map[string]string{gmpayprovider.GmpayManualReviewMetadataKey: "true"},
	}
	waiting := &payment.QueryOrderResponse{
		TradeNo: "gmpay-trade-reentry",
		Status:  payment.ProviderStatusPending,
		Amount:  88,
	}
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeEasyPay, resp: manualReview}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_hold_reentry", "gmpay-trade-reentry")
	// Keep the order live (not yet expired) for the reconciliation-driven
	// status oscillation, then flip it to expired for the outage check.
	_, err := client.PaymentOrder.UpdateOneID(order.ID).
		SetExpiresAt(time.Now().Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	// 6 → hold recorded.
	_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	held, _, holdErr := svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.True(t, held)

	// Waiting → hold released, order stays pending (reconcile never cancels).
	provider.resp = waiting
	_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	held, _, holdErr = svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.False(t, held)

	// 6 again → the second hold entry must survive the unique action index.
	provider.resp = manualReview
	_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	held, _, holdErr = svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.True(t, held)

	// Now expire the order and simulate a query outage: protection holds.
	_, err = client.PaymentOrder.UpdateOneID(order.ID).
		SetExpiresAt(time.Now().Add(-time.Minute)).
		Save(ctx)
	require.NoError(t, err)
	provider.resp = nil
	provider.queryErr = fmt.Errorf("upstream unreachable")

	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

// TestGmpayQueryPaidHoldSurvivesOutage: an order whose paid state was only
// observed via query (waiting for the signed callback) must not be expired —
// inventory included — when a later query fails.
func TestGmpayQueryPaidHoldSurvivesOutage(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	provider := &paymentOrderLifecycleQueryProvider{
		key: payment.TypeEasyPay,
		resp: &payment.QueryOrderResponse{
			// Matches the locally bound trade and amount exactly; without the
			// protective hold this is precisely the response that used to let
			// the order expire on the next outage.
			TradeNo:  "gmpay-trade-paidwait",
			Status:   payment.ProviderStatusPaid,
			Amount:   88,
			Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"},
		},
	}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_paid_outage", "gmpay-trade-paidwait")

	// User tries to cancel; upstream already reports paid (query evidence),
	// so the cancel is refused and the order waits for the signed callback.
	_, err := svc.CancelOrder(ctx, order.ID, order.UserID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already been paid")
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)

	// Query outage + expiry sweep: the persisted query-paid hold protects the
	// order and its reserved inventory from the local expiry path.
	provider.resp = nil
	provider.queryErr = fmt.Errorf("upstream unreachable")

	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

// TestGmpayQueryPaidHoldNotReleasedByPendingStatus: after a paid was observed
// via query, non-terminal upstream answers (status 5, on-chain confirming)
// must NOT release the protective hold — the payment may already be settling
// on-chain. The expiry sweep must keep the order and its reserved inventory.
func TestGmpayQueryPaidHoldNotReleasedByPendingStatus(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	paidResp := &payment.QueryOrderResponse{
		TradeNo:  "gmpay-trade-pending5",
		Status:   payment.ProviderStatusPaid,
		Amount:   88,
		Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"},
	}
	confirmingResp := &payment.QueryOrderResponse{
		// status 5 (on-chain confirming) maps to a plain pending with no
		// markers — precisely the response that used to release the hold.
		TradeNo: "gmpay-trade-pending5",
		Status:  payment.ProviderStatusPending,
		Amount:  88,
	}
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeEasyPay, resp: paidResp}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_pending5", "gmpay-trade-pending5")

	// Paid observed via query: hold persisted, order kept.
	_, err := svc.CancelOrder(ctx, order.ID, order.UserID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "already been paid")
	held, reason, holdErr := svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.True(t, held)
	require.Equal(t, gmpayProtectReasonQueryPaid, reason)

	// Upstream now answers status 5 (non-terminal): the hold must survive and
	// the already-expired order must not be swept with its inventory.
	provider.resp = confirmingResp
	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

// TestGmpayHoldReadFailureKeepsProtection: when the protective-hold audit
// read itself fails while the upstream query is also failing, the local
// cancel/expiry path must hold conservatively — an unknown state is not "no
// protection".
func TestGmpayHoldReadFailureKeepsProtection(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)

	paidResp := &payment.QueryOrderResponse{
		TradeNo:  "gmpay-trade-readfail",
		Status:   payment.ProviderStatusPaid,
		Amount:   88,
		Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"},
	}
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeEasyPay, resp: paidResp}
	restore := replacePaymentProviderFactoryForTest(t, provider)
	defer restore()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_readfail", "gmpay-trade-readfail")

	// Paid observed via query: hold persisted.
	_, err := svc.CancelOrder(ctx, order.ID, order.UserID)
	require.Error(t, err)
	held, _, holdErr := svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, holdErr)
	require.True(t, held)

	// Both the upstream query and the local audit read now fail.
	provider.resp = nil
	provider.queryErr = fmt.Errorf("upstream unreachable")
	originalRead := gmpayReadProtectiveHold
	gmpayReadProtectiveHold = func(context.Context, *dbent.Client, int64) (bool, string, error) {
		return false, "", fmt.Errorf("audit storage unavailable")
	}
	defer func() { gmpayReadProtectiveHold = originalRead }()

	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 0, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)

	_, err = svc.CancelOrder(ctx, order.ID, order.UserID)
	require.Error(t, err)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

func TestGmpayPromotesManualReviewHoldToQueryPaid(t *testing.T) {
	ctx := context.Background()
	client := newPaymentOrderLifecycleTestClient(t)
	const trade = "gmpay-trade-hold-promotion"
	manual := &payment.QueryOrderResponse{TradeNo: trade, Status: payment.ProviderStatusPending, Amount: 88,
		Metadata: map[string]string{gmpayprovider.GmpayManualReviewMetadataKey: "true"}}
	provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeEasyPay, resp: manual}
	defer replacePaymentProviderFactoryForTest(t, provider)()
	svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
	order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_hold_promotion", trade)
	_, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	provider.resp = &payment.QueryOrderResponse{TradeNo: trade, Status: payment.ProviderStatusPaid, Amount: 88,
		Metadata: map[string]string{gmpayprovider.GmpayQueryPaidMetadataKey: "true"}}
	_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	provider.resp = manual
	_, err = svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
	require.NoError(t, err)
	held, reason, err := svc.gmpayProtectiveHoldState(ctx, order.ID)
	require.NoError(t, err)
	require.True(t, held)
	require.Equal(t, gmpayProtectReasonQueryPaid, reason)
	provider.resp = &payment.QueryOrderResponse{TradeNo: trade, Status: payment.ProviderStatusPending, Amount: 88}
	expired, err := svc.ExpireTimedOutOrders(ctx)
	require.NoError(t, err)
	require.Zero(t, expired)
	assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
}

func TestGmpayProtectiveReleaseRequiresCurrentEvidence(t *testing.T) {
	const trade = "gmpay-trade-release-evidence"
	for _, tc := range []struct {
		name, status, tradeNo, failure string
		wantExpired                    bool
	}{
		{name: "release recheck unavailable", status: payment.ProviderStatusPending, tradeNo: trade, failure: "read"},
		{name: "release audit write unavailable", status: payment.ProviderStatusPending, tradeNo: trade, failure: "write"},
		{name: "terminal status for another trade", status: payment.ProviderStatusFailed, tradeNo: "gmpay-other-trade"},
		{name: "terminal status for the bound trade", status: payment.ProviderStatusFailed, tradeNo: trade, wantExpired: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			client := newPaymentOrderLifecycleTestClient(t)
			provider := &paymentOrderLifecycleQueryProvider{key: payment.TypeEasyPay, resp: &payment.QueryOrderResponse{
				TradeNo: trade, Status: payment.ProviderStatusPending, Amount: 88,
				Metadata: map[string]string{gmpayprovider.GmpayManualReviewMetadataKey: "true"},
			}}
			defer replacePaymentProviderFactoryForTest(t, provider)()
			svc := &PaymentService{entClient: client, loadBalancer: &captureLoadBalancer{}}
			order, plan := gmpayLifecycleInventoryFixture(t, client, "sub2_gmpay_release_evidence", trade)
			_, err := svc.VerifyOrderByOutTradeNo(ctx, order.OutTradeNo, order.UserID)
			require.NoError(t, err)
			provider.resp = &payment.QueryOrderResponse{TradeNo: tc.tradeNo, Status: tc.status, Amount: 88}
			injected := false
			switch tc.failure {
			case "read":
				reads := 0
				original := gmpayReadProtectiveHold
				defer func() { gmpayReadProtectiveHold = original }()
				gmpayReadProtectiveHold = func(ctx context.Context, c *dbent.Client, id int64) (bool, string, error) {
					reads++
					if reads > 1 {
						injected = true
						return false, "", errors.New("release recheck unavailable")
					}
					return original(ctx, c, id)
				}
			case "write":
				client.PaymentAuditLog.Use(func(next dbent.Mutator) dbent.Mutator {
					return dbent.MutateFunc(func(context.Context, dbent.Mutation) (dbent.Value, error) {
						injected = true
						return nil, errors.New("release audit write unavailable")
					})
				})
			}
			expired, err := svc.ExpireTimedOutOrders(ctx)
			require.NoError(t, err)
			if tc.failure != "" {
				require.True(t, injected)
			}
			if tc.wantExpired {
				require.Equal(t, 1, expired)
				got, err := client.PaymentOrder.Get(ctx, order.ID)
				require.NoError(t, err)
				require.Equal(t, OrderStatusExpired, got.Status)
				require.Equal(t, subscriptioninventory.StateReleased, got.PlanInventoryState)
				return
			}
			require.Zero(t, expired)
			assertGmpayOrderAndInventoryUntouched(t, client, svc, order, plan)
		})
	}
}
