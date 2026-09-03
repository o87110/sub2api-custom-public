package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptioninventory"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type renewalPolicyUserSubRepo struct {
	userSubRepoNoop
	subs []UserSubscription
}

func (r *renewalPolicyUserSubRepo) ListByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	result := make([]UserSubscription, 0, len(r.subs))
	for i := range r.subs {
		if r.subs[i].UserID == userID {
			result = append(result, r.subs[i])
		}
	}
	return result, nil
}

func (r *renewalPolicyUserSubRepo) GetByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	for i := range r.subs {
		if r.subs[i].UserID == userID && r.subs[i].GroupID == groupID {
			copy := r.subs[i]
			return &copy, nil
		}
	}
	return nil, ErrSubscriptionNotFound
}

func TestListPlansForUserIncludesRenewalOnlyPlansForEligibleUser(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	public, err := client.SubscriptionPlan.Create().
		SetGroupID(1).SetName("public").SetPrice(10).SetForSale(true).Save(ctx)
	require.NoError(t, err)
	zero := 0
	renewalOnly, err := client.SubscriptionPlan.Create().
		SetGroupID(2).SetName("renewal").SetPrice(20).SetForSale(false).
		SetRemainingQuantity(zero).SetAllowExistingUserRenewal(true).SetRenewalGraceDays(5).
		Save(ctx)
	require.NoError(t, err)

	repo := &renewalPolicyUserSubRepo{subs: []UserSubscription{{
		UserID: 7, GroupID: 2, Status: SubscriptionStatusExpired,
		ExpiresAt: time.Now().AddDate(0, 0, -3),
	}}}
	svc := &PaymentService{
		configService:   &PaymentConfigService{entClient: client},
		subscriptionSvc: &SubscriptionService{userSubRepo: repo},
	}

	plans, renewalAvailable, err := svc.ListPlansForUser(ctx, 7)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.False(t, renewalAvailable[public.ID])
	require.True(t, renewalAvailable[renewalOnly.ID])

	plans, renewalAvailable, err = svc.ListPlansForUser(ctx, 8)
	require.NoError(t, err)
	require.Len(t, plans, 1)
	require.Equal(t, public.ID, plans[0].ID)
	require.False(t, renewalAvailable[public.ID])
}

func TestValidateSubOrderAllowsEligibleRenewalButRejectsOutOfGraceUser(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	zero := 0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(2).SetName("renewal").SetPrice(20).SetForSale(false).
		SetRemainingQuantity(zero).SetAllowExistingUserRenewal(true).SetRenewalGraceDays(3).
		Save(ctx)
	require.NoError(t, err)
	repo := &renewalPolicyUserSubRepo{subs: []UserSubscription{{
		UserID: 7, GroupID: 2, Status: SubscriptionStatusExpired,
		ExpiresAt: time.Now().AddDate(0, 0, -2),
	}, {
		UserID: 8, GroupID: 2, Status: SubscriptionStatusExpired,
		ExpiresAt: time.Now().AddDate(0, 0, -4),
	}}}
	groupRepo := &subscriptionGroupRepoStub{group: &Group{
		ID: 2, Status: payment.EntityStatusActive, SubscriptionType: SubscriptionTypeSubscription,
	}}
	svc := &PaymentService{
		configService:   &PaymentConfigService{entClient: client},
		groupRepo:       groupRepo,
		subscriptionSvc: &SubscriptionService{userSubRepo: repo},
	}

	got, renewal, err := svc.validateSubOrder(ctx, CreateOrderRequest{UserID: 7, PlanID: plan.ID})
	require.NoError(t, err)
	require.Equal(t, plan.ID, got.ID)
	require.True(t, renewal)

	_, _, err = svc.validateSubOrder(ctx, CreateOrderRequest{UserID: 8, PlanID: plan.ID})
	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
}

func TestCreateRenewalOrderLeavesLimitedInventoryUntracked(t *testing.T) {
	ctx := context.Background()
	client := newPaymentConfigServiceTestClient(t)
	user, err := client.User.Create().
		SetEmail("renewal-order@example.com").SetPasswordHash("hash").SetUsername("renewal-order").
		Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().
		SetName("renewal group").SetSubscriptionType(SubscriptionTypeSubscription).
		Save(ctx)
	require.NoError(t, err)
	zero := 0
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).SetName("renewal").SetPrice(20).SetForSale(false).
		SetRemainingQuantity(zero).SetAllowExistingUserRenewal(true).SetRenewalGraceDays(3).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserSubscription.Create().
		SetUserID(user.ID).SetGroupID(group.ID).SetStatus(SubscriptionStatusActive).
		SetStartsAt(time.Now().Add(-time.Hour)).
		SetExpiresAt(time.Now().Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	svc := &PaymentService{entClient: client}
	order, err := svc.createOrderInTx(ctx, CreateOrderRequest{
		UserID: user.ID, PaymentType: "test", OrderType: payment.OrderTypeSubscription,
		ClientIP: "127.0.0.1", SrcHost: "api.example.com",
	}, &User{ID: user.ID, Email: user.Email, Username: user.Username}, plan, true, &PaymentConfig{OrderTimeoutMin: 15}, 20, 20, 0, 20, nil)
	require.NoError(t, err)
	require.Equal(t, subscriptioninventory.StateUntracked, order.PlanInventoryState)
	reloaded, err := client.SubscriptionPlan.Get(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, 0, *reloaded.RemainingQuantity)

	_, err = client.SubscriptionPlan.UpdateOneID(plan.ID).SetAllowExistingUserRenewal(false).Save(ctx)
	require.NoError(t, err)
	_, err = svc.createOrderInTx(ctx, CreateOrderRequest{
		UserID: user.ID, PaymentType: "test", OrderType: payment.OrderTypeSubscription,
		ClientIP: "127.0.0.1", SrcHost: "api.example.com",
	}, &User{ID: user.ID, Email: user.Email, Username: user.Username}, plan, true, &PaymentConfig{OrderTimeoutMin: 15}, 20, 20, 0, 20, nil)
	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
}
