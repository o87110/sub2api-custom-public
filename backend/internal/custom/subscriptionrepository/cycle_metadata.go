package subscriptionrepository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscriptioncycle"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *Repository) GetByID(ctx context.Context, id int64) (*service.UserSubscription, error) {
	subscription, err := r.BaseUserSubscriptionRepository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return subscription, r.attachCurrentCycleMetadata(ctx, []*service.UserSubscription{subscription}, time.Now())
}

func (r *Repository) GetByIDIncludeDeleted(ctx context.Context, id int64) (*service.UserSubscription, error) {
	subscription, err := r.BaseUserSubscriptionRepository.GetByIDIncludeDeleted(ctx, id)
	if err != nil {
		return nil, err
	}
	return subscription, r.attachCurrentCycleMetadata(ctx, []*service.UserSubscription{subscription}, time.Now())
}

func (r *Repository) GetByUserIDAndGroupID(ctx context.Context, userID, groupID int64) (*service.UserSubscription, error) {
	subscription, err := r.BaseUserSubscriptionRepository.GetByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		return nil, err
	}
	return subscription, r.attachCurrentCycleMetadata(ctx, []*service.UserSubscription{subscription}, time.Now())
}

func (r *Repository) List(ctx context.Context, params pagination.PaginationParams, userID, groupID *int64, status, platform, sortBy, sortOrder string) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	subscriptions, result, err := r.BaseUserSubscriptionRepository.List(ctx, params, userID, groupID, status, platform, sortBy, sortOrder)
	if err != nil {
		return nil, nil, err
	}
	if err := r.attachCurrentCycleMetadataValues(ctx, subscriptions, time.Now()); err != nil {
		return nil, nil, err
	}
	return subscriptions, result, nil
}

func (r *Repository) ListByUserID(ctx context.Context, userID int64) ([]service.UserSubscription, error) {
	subscriptions, err := r.BaseUserSubscriptionRepository.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := r.attachCurrentCycleMetadataValues(ctx, subscriptions, time.Now()); err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *Repository) ListByGroupID(ctx context.Context, groupID int64, params pagination.PaginationParams) ([]service.UserSubscription, *pagination.PaginationResult, error) {
	subscriptions, result, err := r.BaseUserSubscriptionRepository.ListByGroupID(ctx, groupID, params)
	if err != nil {
		return nil, nil, err
	}
	if err := r.attachCurrentCycleMetadataValues(ctx, subscriptions, time.Now()); err != nil {
		return nil, nil, err
	}
	return subscriptions, result, nil
}

func (r *Repository) attachCurrentCycleMetadataValues(ctx context.Context, subscriptions []service.UserSubscription, now time.Time) error {
	pointers := make([]*service.UserSubscription, 0, len(subscriptions))
	for i := range subscriptions {
		pointers = append(pointers, &subscriptions[i])
	}
	return r.attachCurrentCycleMetadata(ctx, pointers, now)
}

func (r *Repository) attachCurrentCycleMetadata(ctx context.Context, subscriptions []*service.UserSubscription, now time.Time) error {
	if len(subscriptions) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(subscriptions))
	subscriptionByID := make(map[int64]*service.UserSubscription, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription == nil {
			continue
		}
		ids = append(ids, subscription.ID)
		subscriptionByID[subscription.ID] = subscription
	}
	if len(ids) == 0 {
		return nil
	}

	client := r.client
	if tx := dbent.TxFromContext(ctx); tx != nil {
		client = tx.Client()
	}
	cycles, err := client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDIn(ids...),
			usersubscriptioncycle.StatusIn(subscriptionquota.CycleStatusCurrent, subscriptionquota.CycleStatusPending),
			usersubscriptioncycle.StartsAtLTE(now),
			usersubscriptioncycle.EndsAtGT(now),
		).
		All(ctx)
	if err != nil {
		return err
	}
	effective := make(map[int64]*dbent.UserSubscriptionCycle, len(cycles))
	for _, cycle := range cycles {
		current := effective[cycle.SubscriptionID]
		if current == nil || (cycle.Status == subscriptionquota.CycleStatusCurrent && current.Status != subscriptionquota.CycleStatusCurrent) {
			effective[cycle.SubscriptionID] = cycle
		}
	}
	for subscriptionID, cycle := range effective {
		subscription := subscriptionByID[subscriptionID]
		if subscription == nil {
			continue
		}
		subscription.CycleSourceType = cycle.SourceType
		subscription.CycleSourceRef = cloneString(cycle.SourceRef)
		subscription.ManualBulkQuotaResetEnabled = subscriptionquota.NormalizeManualBulkQuotaResetEligibility(
			cycle.SourceType,
			cycle.ManualBulkQuotaResetEnabled,
		)
		subscription.ManualBulkQuotaResetEditable = subscription.DeletedAt == nil &&
			subscription.Status == service.SubscriptionStatusActive &&
			!subscription.StartsAt.After(now) && subscription.ExpiresAt.After(now) &&
			(cycle.SourceType == subscriptionquota.CycleSourceAssignment || cycle.SourceType == subscriptionquota.CycleSourceLegacy)
	}
	return nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
