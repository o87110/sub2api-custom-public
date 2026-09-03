package subscriptioninventory

import (
	"context"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/subscriptionplan"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	subscriptionStatusActive    = "active"
	subscriptionStatusExpired   = "expired"
	subscriptionStatusSuspended = "suspended"
)

const MaxRenewalGraceDays = 30

func ValidateRenewalGraceDays(days int) error {
	if days < 0 || days > MaxRenewalGraceDays {
		return infraerrors.BadRequest("PLAN_RENEWAL_GRACE_INVALID", "renewal grace days must be between 0 and 30")
	}
	return nil
}

// ExistingSubscriptionSnapshot is the small subscription view needed for
// renewal eligibility. Keeping this type independent from the service layer
// avoids a package cycle while making the policy easy to test.
type ExistingSubscriptionSnapshot struct {
	Status    string
	ExpiresAt time.Time
}

type ExistingSubscriptionLoader interface {
	LoadExistingSubscription(context.Context, int64, int64) (*ExistingSubscriptionSnapshot, error)
}

// AuthorizePlanForOrder applies public-purchase and existing-user renewal
// policy without exposing subscription persistence to the payment service.
func AuthorizePlanForOrder(
	ctx context.Context,
	plan *dbent.SubscriptionPlan,
	userID int64,
	loader ExistingSubscriptionLoader,
	now time.Time,
) (bool, error) {
	if plan == nil {
		return false, infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan not found or not for sale")
	}
	var existing *ExistingSubscriptionSnapshot
	var err error
	if loader != nil {
		existing, err = loader.LoadExistingSubscription(ctx, userID, plan.GroupID)
		if err != nil {
			return false, err
		}
	}
	renewal := !IsPlanPubliclyPurchasable(plan) && IsExistingUserRenewalEligible(
		plan.AllowExistingUserRenewal,
		plan.RenewalGraceDays,
		existing,
		now,
	)
	if !IsPlanPubliclyPurchasable(plan) && !renewal {
		return false, soldOutOrUnavailableError(plan)
	}
	return renewal, nil
}

// IsExistingUserRenewalEligible reports whether a user may renew a plan that
// is unavailable to new buyers. Active subscriptions are eligible while their
// term is valid; expired subscriptions are eligible through the configured
// calendar-day grace period. Suspended and missing/invalid subscriptions are
// never eligible.
func IsExistingUserRenewalEligible(
	allow bool,
	graceDays int,
	existing *ExistingSubscriptionSnapshot,
	now time.Time,
) bool {
	return allow && IsExistingSubscriptionTermEligible(graceDays, existing, now)
}

// IsExistingSubscriptionTermEligible determines whether the current term is
// active or still inside its expiry grace period, independent of the plan's
// allow_existing_user_renewal switch. Publicly available plans keep the
// historical renewal behavior; the switch is required only to bypass an
// unavailable plan.
func IsExistingSubscriptionTermEligible(
	graceDays int,
	existing *ExistingSubscriptionSnapshot,
	now time.Time,
) bool {
	if existing == nil || existing.ExpiresAt.IsZero() {
		return false
	}
	if graceDays < 0 {
		return false
	}
	if existing.Status == subscriptionStatusSuspended || existing.Status == "" {
		return false
	}
	if existing.Status == subscriptionStatusActive && existing.ExpiresAt.After(now) {
		return true
	}
	if existing.Status != subscriptionStatusActive && existing.Status != subscriptionStatusExpired {
		return false
	}
	if graceDays == 0 && !existing.ExpiresAt.After(now) {
		return false
	}
	return !now.After(existing.ExpiresAt.AddDate(0, 0, graceDays))
}

// IsPlanPubliclyPurchasable is the single definition of a plan available to
// new buyers. Sold-out plans are excluded even when their display strategy is
// disable_purchase; those plans are only returned to eligible renewals.
func IsPlanPubliclyPurchasable(plan *dbent.SubscriptionPlan) bool {
	return plan != nil && plan.ForSale && !IsSoldOut(plan)
}

// LockAndValidateRenewal serializes renewal authorization with subscription
// revocation and plan-policy updates inside the caller-owned order transaction.
func LockAndValidateRenewal(
	ctx context.Context,
	client *dbent.Client,
	userID int64,
	planID int64,
	now time.Time,
) (*dbent.SubscriptionPlan, bool, error) {
	planQuery := client.SubscriptionPlan.Query().Where(subscriptionplan.IDEQ(planID))
	if client.Driver().Dialect() == dialect.Postgres {
		planQuery.ForUpdate()
	}
	plan, err := planQuery.Only(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("lock renewal plan: %w", err)
	}
	subQuery := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.GroupIDEQ(plan.GroupID),
		)
	if client.Driver().Dialect() == dialect.Postgres {
		subQuery.ForUpdate()
	}
	sub, err := subQuery.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			if IsPlanPubliclyPurchasable(plan) {
				return plan, false, nil
			}
			return nil, false, soldOutOrUnavailableError(plan)
		}
		return nil, false, fmt.Errorf("lock renewal subscription: %w", err)
	}
	eligible := IsExistingUserRenewalEligible(
		plan.AllowExistingUserRenewal,
		plan.RenewalGraceDays,
		&ExistingSubscriptionSnapshot{Status: sub.Status, ExpiresAt: sub.ExpiresAt},
		now,
	)
	if !eligible {
		if IsPlanPubliclyPurchasable(plan) {
			return plan, false, nil
		}
		return nil, false, soldOutOrUnavailableError(plan)
	}
	return plan, true, nil
}

// PrepareOrderInventory revalidates renewal-only access in the order
// transaction and either leaves the order untracked or reserves one unit for
// a normal purchase.
func PrepareOrderInventory(
	ctx context.Context,
	client *dbent.Client,
	userID int64,
	planID int64,
	renewal bool,
	now time.Time,
) (*dbent.SubscriptionPlan, string, error) {
	plan, err := client.SubscriptionPlan.Get(ctx, planID)
	if err != nil {
		return nil, "", infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan not found or not for sale")
	}
	if renewal {
		plan, renewal, err = LockAndValidateRenewal(ctx, client, userID, planID, now)
		if err != nil {
			return nil, "", err
		}
	}
	if renewal {
		return plan, StateUntracked, nil
	}
	state, err := ReserveForOrder(ctx, client, planID, true)
	if err != nil {
		return nil, "", err
	}
	return plan, state, nil
}

func soldOutOrUnavailableError(plan *dbent.SubscriptionPlan) error {
	if IsSoldOut(plan) {
		return soldOutError()
	}
	return infraerrors.NotFound("PLAN_NOT_AVAILABLE", "plan not found or not for sale")
}

// PlanForUser is the personalized public-listing view. RenewalAvailable is a
// derived capability and does not expose the plan's renewal configuration.
type PlanForUser struct {
	Plan             *dbent.SubscriptionPlan
	RenewalAvailable bool
}

// SplitPlansForUser unwraps personalized plans for existing group enrichment
// and returns the derived renewal capability keyed by plan ID.
func SplitPlansForUser(plans []PlanForUser) ([]*dbent.SubscriptionPlan, map[int64]bool) {
	result := make([]*dbent.SubscriptionPlan, 0, len(plans))
	renewalAvailable := make(map[int64]bool, len(plans))
	for _, item := range plans {
		if item.Plan != nil {
			result = append(result, item.Plan)
			renewalAvailable[item.Plan.ID] = item.RenewalAvailable
		}
	}
	return result, renewalAvailable
}

// FilterPlansForUser returns normal sale plans plus unavailable plans from
// groups for which the current user has an eligible existing subscription.
func FilterPlansForUser(
	plans []*dbent.SubscriptionPlan,
	renewalSubscriptions map[int64][]ExistingSubscriptionSnapshot,
	now time.Time,
) []PlanForUser {
	result := make([]PlanForUser, 0, len(plans))
	for _, plan := range plans {
		if plan == nil {
			continue
		}
		public := IsPlanPubliclyPurchasable(plan)
		renewal := false
		for i := range renewalSubscriptions[plan.GroupID] {
			if public {
				break
			}
			if IsExistingSubscriptionTermEligible(
				plan.RenewalGraceDays,
				&renewalSubscriptions[plan.GroupID][i],
				now,
			) {
				renewal = true
				break
			}
		}
		if !public {
			if !renewal || !plan.AllowExistingUserRenewal {
				continue
			}
		}
		result = append(result, PlanForUser{Plan: plan, RenewalAvailable: renewal})
	}
	return result
}
