package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptioninventory"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCreateOrderInTxReservesLimitedInventoryAndLeavesUnlimitedUntracked(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	dbUser, err := client.User.Create().
		SetEmail("inventory-order@example.com").
		SetPasswordHash("hash").
		SetUsername("inventory-order-user").
		Save(ctx)
	require.NoError(t, err)
	user := &User{ID: dbUser.ID, Email: dbUser.Email, Username: dbUser.Username}
	svc := &PaymentService{entClient: client}
	cfg := &PaymentConfig{OrderTimeoutMin: 15}
	req := CreateOrderRequest{
		UserID:      dbUser.ID,
		PaymentType: "test",
		OrderType:   payment.OrderTypeSubscription,
		ClientIP:    "127.0.0.1",
		SrcHost:     "api.example.com",
	}

	one := 1
	limited, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("limited").
		SetPrice(10).
		SetRemainingQuantity(one).
		SetForSale(true).
		Save(ctx)
	require.NoError(t, err)
	order, err := svc.createOrderInTx(ctx, req, user, limited, false, cfg, 10, 10, 0, 10, nil)
	require.NoError(t, err)
	require.Equal(t, subscriptioninventory.StateReserved, order.PlanInventoryState)
	limited, err = client.SubscriptionPlan.Get(ctx, limited.ID)
	require.NoError(t, err)
	require.Equal(t, 0, *limited.RemainingQuantity)
	require.False(t, limited.ForSale)

	_, err = svc.createOrderInTx(ctx, req, user, limited, false, cfg, 10, 10, 0, 10, nil)
	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))

	unlimited, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("unlimited").
		SetPrice(10).
		SetForSale(true).
		Save(ctx)
	require.NoError(t, err)
	order, err = svc.createOrderInTx(ctx, req, user, unlimited, false, cfg, 10, 10, 0, 10, nil)
	require.NoError(t, err)
	require.Equal(t, subscriptioninventory.StateUntracked, order.PlanInventoryState)
}

func TestPaymentCreationFailureReleasesReservationExactlyOnce(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("inventory-failure@example.com").
		SetPasswordHash("hash").
		SetUsername("inventory-failure-user").
		Save(ctx)
	require.NoError(t, err)
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("failed payment plan").
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
		SetRechargeCode("PAYMENT-CREATION-FAILURE").
		SetPaymentType("test").
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

	updated, err := subscriptioninventory.TransitionPendingOrderAndRelease(ctx, client, order.ID, OrderStatusFailed)
	require.NoError(t, err)
	require.Equal(t, 1, updated)
	updated, err = subscriptioninventory.TransitionPendingOrderAndRelease(ctx, client, order.ID, OrderStatusFailed)
	require.NoError(t, err)
	require.Equal(t, 0, updated)

	order, err = client.PaymentOrder.Query().Where(paymentorder.IDEQ(order.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, OrderStatusFailed, order.Status)
	require.Equal(t, subscriptioninventory.StateReleased, order.PlanInventoryState)
	plan, err = client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, 1, *plan.RemainingQuantity)
	require.True(t, plan.ForSale)
	require.False(t, plan.InventoryAutoDelisted)
}
