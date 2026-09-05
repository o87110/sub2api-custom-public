package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptioninventory"
)

type paymentRenewalSubscriptionLoader struct {
	service *PaymentService
}

func (l paymentRenewalSubscriptionLoader) LoadExistingSubscription(ctx context.Context, userID, groupID int64) (*subscriptioninventory.ExistingSubscriptionSnapshot, error) {
	if l.service == nil || l.service.subscriptionSvc == nil || l.service.subscriptionSvc.userSubRepo == nil {
		return nil, nil
	}
	sub, err := l.service.subscriptionSvc.userSubRepo.GetByUserIDAndGroupID(ctx, userID, groupID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("load existing subscription: %w", err)
	}
	if sub == nil {
		return nil, nil
	}
	return &subscriptioninventory.ExistingSubscriptionSnapshot{Status: sub.Status, ExpiresAt: sub.ExpiresAt}, nil
}

// ListPlansForUser returns public plans plus renewal-only plans personalized
// for the authenticated user. Exact inventory remains admin-only.
func (s *PaymentService) ListPlansForUser(ctx context.Context, userID int64) ([]*dbent.SubscriptionPlan, map[int64]bool, error) {
	if s.configService == nil {
		return nil, nil, fmt.Errorf("payment config service is unavailable")
	}
	plans, err := s.configService.ListPlans(ctx)
	if err != nil {
		return nil, nil, err
	}

	snapshots := make(map[int64][]subscriptioninventory.ExistingSubscriptionSnapshot)
	if s.subscriptionSvc != nil {
		subs, err := s.subscriptionSvc.ListUserSubscriptions(ctx, userID)
		if err != nil {
			return nil, nil, fmt.Errorf("list user subscriptions for renewal: %w", err)
		}
		for i := range subs {
			snapshots[subs[i].GroupID] = append(snapshots[subs[i].GroupID], subscriptioninventory.ExistingSubscriptionSnapshot{
				Status:    subs[i].Status,
				ExpiresAt: subs[i].ExpiresAt,
			})
		}
	}
	personalized := subscriptioninventory.FilterPlansForUser(plans, snapshots, time.Now())
	entities, renewalAvailable := subscriptioninventory.SplitPlansForUser(personalized)
	return entities, renewalAvailable, nil
}
