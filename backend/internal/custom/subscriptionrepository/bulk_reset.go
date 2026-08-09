package subscriptionrepository

import (
	"context"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/paymentorder"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/ent/usersubscriptioncycle"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ResetQuotaIfBulkEligible serializes eligibility changes, cycle advancement,
// per-item idempotency, and quota reset on the same transaction.
func (r *Repository) ResetQuotaIfBulkEligible(
	ctx context.Context,
	subscriptionID int64,
	operation idempotencyexecution.Execution,
	now time.Time,
) (*service.UserSubscription, bool, error) {
	var result *service.UserSubscription
	eligible := false
	err := r.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		subscription, err := client.UserSubscription.Query().
			Where(usersubscription.IDEQ(subscriptionID)).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return nil
			}
			return err
		}
		if subscription.Status != service.SubscriptionStatusActive ||
			subscription.StartsAt.After(now) || !subscription.ExpiresAt.After(now) {
			return nil
		}

		advanced, err := subscriptionquota.AdvanceCycle(txCtx, client, subscriptionID, now, timezone.StartOfDay(now))
		if err != nil {
			return err
		}
		eligible, err = bulkResetEligibleLocked(txCtx, client, subscription, now)
		if err != nil {
			return err
		}
		if !eligible {
			if advanced {
				result, err = r.GetByID(txCtx, subscriptionID)
			}
			return err
		}

		var resetOperation *idempotencyexecution.Execution
		if operation.OperationKeyHash != "" {
			resetOperation = &operation
		}
		result, err = r.ResetQuota(txCtx, subscriptionID, true, true, true, resetOperation, now)
		return err
	})
	return result, eligible, translateCycleError(err, service.ErrSubscriptionNotFound, nil)
}

func bulkResetEligibleLocked(ctx context.Context, client *dbent.Client, subscription *dbent.UserSubscription, now time.Time) (bool, error) {
	cycle, err := client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDEQ(subscription.ID),
			usersubscriptioncycle.StatusEQ(subscriptionquota.CycleStatusCurrent),
			usersubscriptioncycle.StartsAtLTE(now),
			usersubscriptioncycle.EndsAtGT(now),
		).
		ForUpdate().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	switch cycle.SourceType {
	case subscriptionquota.CycleSourceAssignment, subscriptionquota.CycleSourceLegacy:
		return cycle.ManualBulkQuotaResetEnabled, nil
	case subscriptionquota.CycleSourcePayment:
		return paymentCycleBulkResetEligibleLocked(ctx, client, subscription, cycle)
	default:
		return false, nil
	}
}

func paymentCycleBulkResetEligibleLocked(ctx context.Context, client *dbent.Client, subscription *dbent.UserSubscription, cycle *dbent.UserSubscriptionCycle) (bool, error) {
	if cycle.SourceRef == nil {
		return false, nil
	}
	orderID, err := strconv.ParseInt(strings.TrimSpace(*cycle.SourceRef), 10, 64)
	if err != nil || orderID <= 0 {
		return false, nil
	}
	order, err := client.PaymentOrder.Query().
		Where(
			paymentorder.IDEQ(orderID),
			paymentorder.OrderTypeEQ(payment.OrderTypeSubscription),
			paymentorder.PlanIDNotNil(),
			paymentorder.SubscriptionGroupIDNotNil(),
		).
		ForUpdate().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if order.UserID != subscription.UserID || order.SubscriptionGroupID == nil ||
		*order.SubscriptionGroupID != subscription.GroupID || order.PlanID == nil {
		return false, nil
	}
	_, err = client.SubscriptionPlan.Query().
		Where(
			subscriptionplan.IDEQ(*order.PlanID),
			subscriptionplan.GroupIDEQ(subscription.GroupID),
			subscriptionplan.AllowBulkQuotaResetEQ(true),
		).
		ForUpdate().
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
