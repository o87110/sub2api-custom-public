package subscriptionbulkreset

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
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
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	MaxBatchSize = 300

	ItemStatusSuccess = "success"
	ItemStatusSkipped = "skipped"
	ItemStatusFailed  = "failed"

	ReasonNoLongerEligible = "SUBSCRIPTION_NO_LONGER_ELIGIBLE"
	ReasonQuotaResetFailed = "QUOTA_RESET_FAILED"
)

type Candidate struct {
	SubscriptionID        int64   `json:"subscription_id"`
	UserID                int64   `json:"user_id"`
	UserEmail             string  `json:"user_email"`
	Username              string  `json:"username"`
	PlanID                int64   `json:"plan_id"`
	PlanName              string  `json:"plan_name"`
	CycleUsageUSD         float64 `json:"cycle_usage_usd"`
	ManualQuotaResetCount int64   `json:"manual_quota_reset_count"`
}

type CandidateList struct {
	UserCount         int         `json:"user_count"`
	SubscriptionCount int         `json:"subscription_count"`
	Items             []Candidate `json:"items"`
}

type ItemResult struct {
	SubscriptionID int64  `json:"subscription_id"`
	Status         string `json:"status"`
	ReasonCode     string `json:"reason_code,omitempty"`
	Message        string `json:"message,omitempty"`
}

type Result struct {
	RequestedCount int          `json:"requested_count"`
	SuccessCount   int          `json:"success_count"`
	SkippedCount   int          `json:"skipped_count"`
	FailedCount    int          `json:"failed_count"`
	Items          []ItemResult `json:"items"`
}

type quotaResetter interface {
	AdminResetQuota(context.Context, int64, bool, bool, bool) (*service.UserSubscription, error)
	AdminResetQuotaIdempotent(context.Context, int64, bool, bool, bool, idempotencyexecution.Execution) (*service.UserSubscription, error)
}

type Service struct {
	client   *dbent.Client
	resetter quotaResetter
	now      func() time.Time
}

func NewService(client *dbent.Client, subscriptionService *service.SubscriptionService) *Service {
	return &Service{client: client, resetter: subscriptionService, now: time.Now}
}

func (s *Service) ListCandidates(ctx context.Context) (*CandidateList, error) {
	items, err := s.resolveCandidates(ctx, nil, s.now())
	if err != nil {
		return nil, err
	}
	users := make(map[int64]struct{}, len(items))
	for _, item := range items {
		users[item.UserID] = struct{}{}
	}
	return &CandidateList{
		UserCount:         len(users),
		SubscriptionCount: len(items),
		Items:             items,
	}, nil
}

func (s *Service) ResetSelected(ctx context.Context, subscriptionIDs []int64, operation idempotencyexecution.Execution) (*Result, error) {
	ids := uniquePositiveIDs(subscriptionIDs)
	result := &Result{
		RequestedCount: len(ids),
		Items:          make([]ItemResult, 0, len(ids)),
	}
	for _, subscriptionID := range ids {
		items, err := s.resolveCandidates(ctx, []int64{subscriptionID}, s.now())
		if err != nil {
			slog.Error("bulk subscription quota reset eligibility check failed", "subscription_id", subscriptionID, "error", err)
			result.FailedCount++
			result.Items = append(result.Items, ItemResult{
				SubscriptionID: subscriptionID,
				Status:         ItemStatusFailed,
				ReasonCode:     ReasonQuotaResetFailed,
				Message:        "failed to verify subscription eligibility",
			})
			continue
		}
		if len(items) != 1 {
			result.SkippedCount++
			result.Items = append(result.Items, ItemResult{
				SubscriptionID: subscriptionID,
				Status:         ItemStatusSkipped,
				ReasonCode:     ReasonNoLongerEligible,
				Message:        "subscription is no longer eligible for bulk quota reset",
			})
			continue
		}
		var resetErr error
		if operation.OperationKeyHash != "" {
			_, resetErr = s.resetter.AdminResetQuotaIdempotent(ctx, subscriptionID, true, true, true, operation)
		} else {
			_, resetErr = s.resetter.AdminResetQuota(ctx, subscriptionID, true, true, true)
		}
		if resetErr != nil {
			slog.Error("bulk subscription quota reset failed", "subscription_id", subscriptionID, "error", resetErr)
			result.FailedCount++
			result.Items = append(result.Items, ItemResult{
				SubscriptionID: subscriptionID,
				Status:         ItemStatusFailed,
				ReasonCode:     ReasonQuotaResetFailed,
				Message:        "quota reset failed",
			})
			continue
		}
		result.SuccessCount++
		result.Items = append(result.Items, ItemResult{SubscriptionID: subscriptionID, Status: ItemStatusSuccess})
	}
	return result, nil
}

func (s *Service) resolveCandidates(ctx context.Context, subscriptionIDs []int64, now time.Time) ([]Candidate, error) {
	query := s.client.UserSubscription.Query().
		Where(
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.StartsAtLTE(now),
			usersubscription.ExpiresAtGT(now),
		).
		WithUser()
	if len(subscriptionIDs) > 0 {
		query = query.Where(usersubscription.IDIn(subscriptionIDs...))
	}
	subscriptions, err := query.All(ctx)
	if err != nil || len(subscriptions) == 0 {
		return []Candidate{}, err
	}

	subscriptionByID := make(map[int64]*dbent.UserSubscription, len(subscriptions))
	ids := make([]int64, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		subscriptionByID[subscription.ID] = subscription
		ids = append(ids, subscription.ID)
	}

	cycles, err := s.client.UserSubscriptionCycle.Query().
		Where(
			usersubscriptioncycle.SubscriptionIDIn(ids...),
			usersubscriptioncycle.StatusIn(subscriptionquota.CycleStatusCurrent, subscriptionquota.CycleStatusPending),
			usersubscriptioncycle.SourceTypeEQ(subscriptionquota.CycleSourcePayment),
			usersubscriptioncycle.StartsAtLTE(now),
			usersubscriptioncycle.EndsAtGT(now),
		).
		All(ctx)
	if err != nil || len(cycles) == 0 {
		return []Candidate{}, err
	}

	effectiveCycleBySubscription := make(map[int64]*dbent.UserSubscriptionCycle, len(cycles))
	orderIDs := make([]int64, 0, len(cycles))
	for _, cycle := range cycles {
		orderID, ok := parseOrderID(cycle.SourceRef)
		if !ok {
			continue
		}
		current := effectiveCycleBySubscription[cycle.SubscriptionID]
		if current != nil && current.Status == subscriptionquota.CycleStatusCurrent {
			continue
		}
		effectiveCycleBySubscription[cycle.SubscriptionID] = cycle
		orderIDs = append(orderIDs, orderID)
	}
	orderIDs = uniquePositiveIDs(orderIDs)
	if len(orderIDs) == 0 {
		return []Candidate{}, nil
	}

	orders, err := s.client.PaymentOrder.Query().
		Where(
			paymentorder.IDIn(orderIDs...),
			paymentorder.OrderTypeEQ(payment.OrderTypeSubscription),
			paymentorder.PlanIDNotNil(),
			paymentorder.SubscriptionGroupIDNotNil(),
		).
		All(ctx)
	if err != nil || len(orders) == 0 {
		return []Candidate{}, err
	}
	orderByID := make(map[int64]*dbent.PaymentOrder, len(orders))
	planIDs := make([]int64, 0, len(orders))
	for _, order := range orders {
		orderByID[order.ID] = order
		if order.PlanID != nil {
			planIDs = append(planIDs, *order.PlanID)
		}
	}
	planIDs = uniquePositiveIDs(planIDs)

	plans, err := s.client.SubscriptionPlan.Query().
		Where(
			subscriptionplan.IDIn(planIDs...),
			subscriptionplan.AllowBulkQuotaResetEQ(true),
		).
		All(ctx)
	if err != nil || len(plans) == 0 {
		return []Candidate{}, err
	}
	planByID := make(map[int64]*dbent.SubscriptionPlan, len(plans))
	for _, plan := range plans {
		planByID[plan.ID] = plan
	}

	items := make([]Candidate, 0, len(effectiveCycleBySubscription))
	for subscriptionID, cycle := range effectiveCycleBySubscription {
		subscription := subscriptionByID[subscriptionID]
		orderID, ok := parseOrderID(cycle.SourceRef)
		if subscription == nil || !ok {
			continue
		}
		order := orderByID[orderID]
		if order == nil || order.PlanID == nil || order.SubscriptionGroupID == nil ||
			order.UserID != subscription.UserID || *order.SubscriptionGroupID != subscription.GroupID {
			continue
		}
		plan := planByID[*order.PlanID]
		if plan == nil || plan.GroupID != subscription.GroupID {
			continue
		}
		user, err := subscription.Edges.UserOrErr()
		if err != nil {
			return nil, fmt.Errorf("load subscription user: %w", err)
		}
		cycleUsageUSD := subscription.CycleUsageUsd
		manualResetCount := subscription.ManualQuotaResetCount
		if cycle.Status == subscriptionquota.CycleStatusPending {
			cycleUsageUSD = 0
			manualResetCount = 0
		}
		items = append(items, Candidate{
			SubscriptionID:        subscription.ID,
			UserID:                user.ID,
			UserEmail:             user.Email,
			Username:              user.Username,
			PlanID:                plan.ID,
			PlanName:              plan.Name,
			CycleUsageUSD:         cycleUsageUSD,
			ManualQuotaResetCount: manualResetCount,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].PlanName != items[j].PlanName {
			return items[i].PlanName < items[j].PlanName
		}
		if items[i].UserEmail != items[j].UserEmail {
			return items[i].UserEmail < items[j].UserEmail
		}
		return items[i].SubscriptionID < items[j].SubscriptionID
	})
	return items, nil
}

func parseOrderID(sourceRef *string) (int64, bool) {
	if sourceRef == nil {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimSpace(*sourceRef), 10, 64)
	return id, err == nil && id > 0
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
