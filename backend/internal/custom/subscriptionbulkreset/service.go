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
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
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
	SourceType            string  `json:"source_type"`
	SourceName            string  `json:"source_name"`
	GroupID               int64   `json:"group_id"`
	GroupName             string  `json:"group_name"`
	PlanID                *int64  `json:"plan_id"`
	PlanName              *string `json:"plan_name"`
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
	AdminResetQuotaIfBulkEligible(context.Context, int64, idempotencyexecution.Execution) (*service.UserSubscription, bool, error)
}

type cycleMutationCacheInvalidator interface {
	InvalidateSubscriptionCachesAfterCycleMutation(userID, groupID int64)
}

type Service struct {
	client           *dbent.Client
	resetter         quotaResetter
	cacheInvalidator cycleMutationCacheInvalidator
	now              func() time.Time
}

func NewService(client *dbent.Client, subscriptionService *service.SubscriptionService) *Service {
	return &Service{
		client:           client,
		resetter:         subscriptionService,
		cacheInvalidator: subscriptionService,
		now:              time.Now,
	}
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

func (s *Service) UpdateCurrentCycleManualEligibility(ctx context.Context, subscriptionID int64, enabled bool) error {
	now := s.now()
	return s.withTx(ctx, func(txCtx context.Context, client *dbent.Client) error {
		subscription, err := client.UserSubscription.Query().
			Where(usersubscription.IDEQ(subscriptionID)).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return service.ErrSubscriptionNotFound.WithCause(err)
			}
			return err
		}
		if subscription.Status != service.SubscriptionStatusActive ||
			subscription.StartsAt.After(now) || !subscription.ExpiresAt.After(now) {
			return infraerrors.Conflict("SUBSCRIPTION_BULK_RESET_ELIGIBILITY_NOT_EDITABLE", "only active manual subscription cycles can change bulk reset eligibility")
		}
		advanced, err := subscriptionquota.AdvanceCycle(txCtx, client, subscriptionID, now, timezone.StartOfDay(now))
		if err != nil {
			return err
		}
		cycle, err := client.UserSubscriptionCycle.Query().
			Where(
				usersubscriptioncycle.SubscriptionIDEQ(subscriptionID),
				usersubscriptioncycle.StatusEQ(subscriptionquota.CycleStatusCurrent),
				usersubscriptioncycle.StartsAtLTE(now),
				usersubscriptioncycle.EndsAtGT(now),
			).
			ForUpdate().
			Only(txCtx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return infraerrors.Conflict("SUBSCRIPTION_BULK_RESET_ELIGIBILITY_NOT_EDITABLE", "the active subscription cycle is unavailable")
			}
			return err
		}
		if cycle.SourceType != subscriptionquota.CycleSourceAssignment && cycle.SourceType != subscriptionquota.CycleSourceLegacy {
			return infraerrors.Conflict("SUBSCRIPTION_BULK_RESET_ELIGIBILITY_NOT_EDITABLE", "payment and redeem subscription cycles cannot use manual bulk reset eligibility")
		}
		if cycle.ManualBulkQuotaResetEnabled == enabled {
			s.invalidateCycleMutationCachesAfterCommit(txCtx, advanced, subscription.UserID, subscription.GroupID)
			return nil
		}
		if _, err = cycle.Update().SetManualBulkQuotaResetEnabled(enabled).Save(txCtx); err != nil {
			return err
		}
		s.invalidateCycleMutationCachesAfterCommit(txCtx, advanced, subscription.UserID, subscription.GroupID)
		return nil
	})
}

func (s *Service) invalidateCycleMutationCachesAfterCommit(ctx context.Context, advanced bool, userID, groupID int64) {
	if !advanced || s.cacheInvalidator == nil {
		return
	}
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		s.cacheInvalidator.InvalidateSubscriptionCachesAfterCycleMutation(userID, groupID)
		return
	}
	tx.OnCommit(func(next dbent.Committer) dbent.Committer {
		return dbent.CommitFunc(func(ctx context.Context, tx *dbent.Tx) error {
			if err := next.Commit(ctx, tx); err != nil {
				return err
			}
			s.cacheInvalidator.InvalidateSubscriptionCachesAfterCycleMutation(userID, groupID)
			return nil
		})
	})
}

func (s *Service) ResetSelected(ctx context.Context, subscriptionIDs []int64, operation idempotencyexecution.Execution) (*Result, error) {
	ids := uniquePositiveIDs(subscriptionIDs)
	result := &Result{
		RequestedCount: len(ids),
		Items:          make([]ItemResult, 0, len(ids)),
	}
	for _, subscriptionID := range ids {
		_, eligible, resetErr := s.resetter.AdminResetQuotaIfBulkEligible(ctx, subscriptionID, operation)
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
		if !eligible {
			result.SkippedCount++
			result.Items = append(result.Items, ItemResult{
				SubscriptionID: subscriptionID,
				Status:         ItemStatusSkipped,
				ReasonCode:     ReasonNoLongerEligible,
				Message:        "subscription is no longer eligible for bulk quota reset",
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
		WithUser().
		WithGroup()
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
		current := effectiveCycleBySubscription[cycle.SubscriptionID]
		if current != nil && current.Status == subscriptionquota.CycleStatusCurrent {
			continue
		}
		effectiveCycleBySubscription[cycle.SubscriptionID] = cycle
		if cycle.SourceType == subscriptionquota.CycleSourcePayment {
			if orderID, ok := parseOrderID(cycle.SourceRef); ok {
				orderIDs = append(orderIDs, orderID)
			}
		}
	}
	orderIDs = uniquePositiveIDs(orderIDs)
	orderByID := make(map[int64]*dbent.PaymentOrder, len(orderIDs))
	planIDs := make([]int64, 0, len(orderIDs))
	if len(orderIDs) > 0 {
		orders, err := s.client.PaymentOrder.Query().
			Where(
				paymentorder.IDIn(orderIDs...),
				paymentorder.OrderTypeEQ(payment.OrderTypeSubscription),
				paymentorder.PlanIDNotNil(),
				paymentorder.SubscriptionGroupIDNotNil(),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, order := range orders {
			orderByID[order.ID] = order
			if order.PlanID != nil {
				planIDs = append(planIDs, *order.PlanID)
			}
		}
	}
	planIDs = uniquePositiveIDs(planIDs)
	planByID := make(map[int64]*dbent.SubscriptionPlan, len(planIDs))
	if len(planIDs) > 0 {
		plans, err := s.client.SubscriptionPlan.Query().
			Where(
				subscriptionplan.IDIn(planIDs...),
				subscriptionplan.AllowBulkQuotaResetEQ(true),
			).
			All(ctx)
		if err != nil {
			return nil, err
		}
		for _, plan := range plans {
			planByID[plan.ID] = plan
		}
	}

	items := make([]Candidate, 0, len(effectiveCycleBySubscription))
	for subscriptionID, cycle := range effectiveCycleBySubscription {
		subscription := subscriptionByID[subscriptionID]
		if subscription == nil {
			continue
		}
		user, err := subscription.Edges.UserOrErr()
		if err != nil {
			return nil, fmt.Errorf("load subscription user: %w", err)
		}
		group, err := subscription.Edges.GroupOrErr()
		if err != nil {
			return nil, fmt.Errorf("load subscription group: %w", err)
		}
		cycleUsageUSD := subscription.CycleUsageUsd
		manualResetCount := subscription.ManualQuotaResetCount
		if cycle.Status == subscriptionquota.CycleStatusPending {
			cycleUsageUSD = 0
			manualResetCount = 0
		}
		candidate := Candidate{
			SubscriptionID:        subscription.ID,
			UserID:                user.ID,
			UserEmail:             user.Email,
			Username:              user.Username,
			SourceType:            cycle.SourceType,
			GroupID:               group.ID,
			GroupName:             group.Name,
			CycleUsageUSD:         cycleUsageUSD,
			ManualQuotaResetCount: manualResetCount,
		}
		switch cycle.SourceType {
		case subscriptionquota.CycleSourcePayment:
			orderID, ok := parseOrderID(cycle.SourceRef)
			if !ok {
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
			planID, planName := plan.ID, plan.Name
			candidate.PlanID = &planID
			candidate.PlanName = &planName
			candidate.SourceName = plan.Name
		case subscriptionquota.CycleSourceAssignment, subscriptionquota.CycleSourceLegacy:
			if !cycle.ManualBulkQuotaResetEnabled {
				continue
			}
			candidate.SourceName = group.Name
		default:
			continue
		}
		items = append(items, candidate)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].SourceName != items[j].SourceName {
			return items[i].SourceName < items[j].SourceName
		}
		if items[i].UserEmail != items[j].UserEmail {
			return items[i].UserEmail < items[j].UserEmail
		}
		return items[i].SubscriptionID < items[j].SubscriptionID
	})
	return items, nil
}

func (s *Service) withTx(ctx context.Context, fn func(context.Context, *dbent.Client) error) error {
	if tx := dbent.TxFromContext(ctx); tx != nil {
		return fn(ctx, tx.Client())
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	txCtx := dbent.NewTxContext(ctx, tx)
	if err := fn(txCtx, tx.Client()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
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
