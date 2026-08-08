package subscriptionbulkreset

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

type resetterStub struct {
	calls      []int64
	operations []idempotencyexecution.Execution
	failID     int64
}

func (s *resetterStub) AdminResetQuota(_ context.Context, subscriptionID int64, daily, weekly, monthly bool) (*service.UserSubscription, error) {
	if !daily || !weekly || !monthly {
		return nil, errors.New("all quota windows must be reset")
	}
	s.calls = append(s.calls, subscriptionID)
	if subscriptionID == s.failID {
		return nil, errors.New("reset failed")
	}
	return &service.UserSubscription{ID: subscriptionID}, nil
}

func (s *resetterStub) AdminResetQuotaIdempotent(ctx context.Context, subscriptionID int64, daily, weekly, monthly bool, operation idempotencyexecution.Execution) (*service.UserSubscription, error) {
	s.operations = append(s.operations, operation)
	return s.AdminResetQuota(ctx, subscriptionID, daily, weekly, monthly)
}

func TestListCandidatesOnlyReturnsTraceableEnabledCurrentPaymentCycles(t *testing.T) {
	ctx := context.Background()
	client := newBulkResetTestClient(t)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	user := createBulkResetUser(t, client, "shared@example.com")

	eligiblePlan := createBulkResetPlan(t, client, 1, "Eligible", true, false)
	first := createBulkResetSubscription(t, client, user.ID, 1, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, user, first, eligiblePlan, now.Add(-time.Hour), now.Add(24*time.Hour), subscriptionquota.CycleStatusCurrent, nil)

	secondPlan := createBulkResetPlan(t, client, 2, "Sold out but eligible", true, false)
	second := createBulkResetSubscription(t, client, user.ID, 2, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, user, second, secondPlan, now.Add(-time.Hour), now.Add(24*time.Hour), subscriptionquota.CycleStatusCurrent, nil)

	disabledPlan := createBulkResetPlan(t, client, 3, "Disabled", false, true)
	disabled := createBulkResetSubscription(t, client, user.ID, 3, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, user, disabled, disabledPlan, now.Add(-time.Hour), now.Add(24*time.Hour), subscriptionquota.CycleStatusCurrent, nil)

	for index, sourceType := range []string{
		subscriptionquota.CycleSourceLegacy,
		subscriptionquota.CycleSourceRedeem,
		subscriptionquota.CycleSourceAssignment,
	} {
		groupID := int64(10 + index)
		subscription := createBulkResetSubscription(t, client, user.ID, groupID, now, service.SubscriptionStatusActive)
		_, err := client.UserSubscriptionCycle.Create().
			SetSubscriptionID(subscription.ID).
			SetStartsAt(now.Add(-time.Hour)).
			SetEndsAt(now.Add(24 * time.Hour)).
			SetStatus(subscriptionquota.CycleStatusCurrent).
			SetSourceType(sourceType).
			SetSourceRef(fmt.Sprintf("%s-%d", sourceType, index)).
			Save(ctx)
		require.NoError(t, err)
	}

	expired := createBulkResetSubscription(t, client, user.ID, 20, now, service.SubscriptionStatusActive)
	_, err := client.UserSubscription.UpdateOneID(expired.ID).SetExpiresAt(now).Save(ctx)
	require.NoError(t, err)
	suspended := createBulkResetSubscription(t, client, user.ID, 21, now, service.SubscriptionStatusSuspended)
	createBulkResetPaymentCycle(t, client, user, suspended, createBulkResetPlan(t, client, 21, "Suspended", true, true), now.Add(-time.Hour), now.Add(24*time.Hour), subscriptionquota.CycleStatusCurrent, nil)

	svc := &Service{client: client, resetter: &resetterStub{}, now: func() time.Time { return now }}
	got, err := svc.ListCandidates(ctx)

	require.NoError(t, err)
	require.Equal(t, 1, got.UserCount)
	require.Equal(t, 2, got.SubscriptionCount)
	require.ElementsMatch(t, []int64{first.ID, second.ID}, []int64{got.Items[0].SubscriptionID, got.Items[1].SubscriptionID})
}

func TestListCandidatesUsesPendingPlanOnlyAfterCurrentCycleBoundary(t *testing.T) {
	ctx := context.Background()
	client := newBulkResetTestClient(t)
	currentTime := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	boundary := currentTime.Add(time.Hour)
	user := createBulkResetUser(t, client, "renewal@example.com")
	currentPlan := createBulkResetPlan(t, client, 30, "Current plan", true, true)
	nextPlan := createBulkResetPlan(t, client, 30, "Next plan", true, true)
	subscription := createBulkResetSubscription(t, client, user.ID, 30, currentTime, service.SubscriptionStatusActive)
	_, err := client.UserSubscription.UpdateOneID(subscription.ID).
		SetExpiresAt(boundary.Add(30 * 24 * time.Hour)).
		SetCurrentCycleEndsAt(boundary).
		Save(ctx)
	require.NoError(t, err)
	createBulkResetPaymentCycle(t, client, user, subscription, currentPlan, currentTime.Add(-time.Hour), boundary, subscriptionquota.CycleStatusCurrent, nil)
	createBulkResetPaymentCycle(t, client, user, subscription, nextPlan, boundary, boundary.Add(30*24*time.Hour), subscriptionquota.CycleStatusPending, nil)

	svc := &Service{client: client, resetter: &resetterStub{}, now: func() time.Time { return currentTime }}
	before, err := svc.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, before.Items, 1)
	require.Equal(t, currentPlan.ID, before.Items[0].PlanID)

	svc.now = func() time.Time { return boundary }
	after, err := svc.ListCandidates(ctx)
	require.NoError(t, err)
	require.Len(t, after.Items, 1)
	require.Equal(t, nextPlan.ID, after.Items[0].PlanID)
	require.Zero(t, after.Items[0].CycleUsageUSD)
	require.Zero(t, after.Items[0].ManualQuotaResetCount)
}

func TestListCandidatesRejectsOrderAndPlanOwnershipMismatch(t *testing.T) {
	ctx := context.Background()
	client := newBulkResetTestClient(t)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	user := createBulkResetUser(t, client, "owner@example.com")
	other := createBulkResetUser(t, client, "other@example.com")

	plan := createBulkResetPlan(t, client, 40, "Mismatched user", true, true)
	subscription := createBulkResetSubscription(t, client, user.ID, 40, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, other, subscription, plan, now.Add(-time.Hour), now.Add(time.Hour), subscriptionquota.CycleStatusCurrent, nil)

	wrongGroupPlan := createBulkResetPlan(t, client, 999, "Mismatched plan group", true, true)
	second := createBulkResetSubscription(t, client, user.ID, 41, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, user, second, wrongGroupPlan, now.Add(-time.Hour), now.Add(time.Hour), subscriptionquota.CycleStatusCurrent, int64Ptr(41))

	svc := &Service{client: client, resetter: &resetterStub{}, now: func() time.Time { return now }}
	got, err := svc.ListCandidates(ctx)
	require.NoError(t, err)
	require.Empty(t, got.Items)
}

func TestResetSelectedDeduplicatesAndContinuesAfterSkipAndFailure(t *testing.T) {
	ctx := context.Background()
	client := newBulkResetTestClient(t)
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	user := createBulkResetUser(t, client, "reset@example.com")
	plan := createBulkResetPlan(t, client, 50, "Resettable", true, true)
	first := createBulkResetSubscription(t, client, user.ID, 50, now, service.SubscriptionStatusActive)
	createBulkResetPaymentCycle(t, client, user, first, plan, now.Add(-time.Hour), now.Add(time.Hour), subscriptionquota.CycleStatusCurrent, nil)
	second := createBulkResetSubscription(t, client, user.ID, 51, now, service.SubscriptionStatusActive)
	secondPlan := createBulkResetPlan(t, client, 51, "Failing", true, true)
	createBulkResetPaymentCycle(t, client, user, second, secondPlan, now.Add(-time.Hour), now.Add(time.Hour), subscriptionquota.CycleStatusCurrent, nil)
	ineligible := createBulkResetSubscription(t, client, user.ID, 52, now, service.SubscriptionStatusActive)

	resetter := &resetterStub{failID: second.ID}
	svc := &Service{client: client, resetter: resetter, now: func() time.Time { return now }}
	operation, err := idempotencyexecution.New("admin.subscriptions.bulk_reset_quota", "admin:7", service.HashIdempotencyKey("bulk-reset-operation"), now, now.Add(24*time.Hour))
	require.NoError(t, err)
	got, err := svc.ResetSelected(ctx, []int64{first.ID, first.ID, ineligible.ID, second.ID}, operation)
	require.NoError(t, err)

	require.Equal(t, 3, got.RequestedCount)
	require.Equal(t, 1, got.SuccessCount)
	require.Equal(t, 1, got.SkippedCount)
	require.Equal(t, 1, got.FailedCount)
	require.Equal(t, []int64{first.ID, second.ID}, resetter.calls)
	require.Len(t, resetter.operations, 2)
	require.NotEmpty(t, resetter.operations[0].OperationKeyHash)
	require.Equal(t, resetter.operations[0], resetter.operations[1])
	require.Equal(t, operation.OperationKeyHash, resetter.operations[0].OperationKeyHash)
	require.Equal(t, ItemStatusSuccess, got.Items[0].Status)
	require.Equal(t, ItemStatusSkipped, got.Items[1].Status)
	require.Equal(t, ReasonNoLongerEligible, got.Items[1].ReasonCode)
	require.Equal(t, ItemStatusFailed, got.Items[2].Status)
}

func TestMaximumBatchResultFitsDefaultIdempotencyResponseLimit(t *testing.T) {
	result := Result{
		RequestedCount: MaxBatchSize,
		SkippedCount:   MaxBatchSize,
		Items:          make([]ItemResult, 0, MaxBatchSize),
	}
	for i := 1; i <= MaxBatchSize; i++ {
		result.Items = append(result.Items, ItemResult{
			SubscriptionID: int64(i),
			Status:         ItemStatusSkipped,
			ReasonCode:     ReasonNoLongerEligible,
			Message:        "subscription is no longer eligible for bulk quota reset",
		})
	}

	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.Less(t, len(raw), service.DefaultIdempotencyConfig().MaxStoredResponseLen)
}

func newBulkResetTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", dbName))
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func createBulkResetUser(t *testing.T, client *dbent.Client, email string) *dbent.User {
	t.Helper()
	user, err := client.User.Create().SetEmail(email).SetPasswordHash("hash").SetUsername(strings.Split(email, "@")[0]).Save(context.Background())
	require.NoError(t, err)
	return user
}

func createBulkResetPlan(t *testing.T, client *dbent.Client, groupID int64, name string, enabled, forSale bool) *dbent.SubscriptionPlan {
	t.Helper()
	group := ensureBulkResetGroup(t, client, groupID)
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(group.ID).
		SetName(name).
		SetPrice(10).
		SetAllowBulkQuotaReset(enabled).
		SetForSale(forSale).
		Save(context.Background())
	require.NoError(t, err)
	return plan
}

func createBulkResetSubscription(t *testing.T, client *dbent.Client, userID, groupID int64, now time.Time, status string) *dbent.UserSubscription {
	t.Helper()
	group := ensureBulkResetGroup(t, client, groupID)
	subscription, err := client.UserSubscription.Create().
		SetUserID(userID).
		SetGroupID(group.ID).
		SetStartsAt(now.Add(-time.Hour)).
		SetExpiresAt(now.Add(48 * time.Hour)).
		SetStatus(status).
		SetCurrentCycleStartsAt(now.Add(-time.Hour)).
		SetCurrentCycleEndsAt(now.Add(24 * time.Hour)).
		SetCycleUsageUsd(12.5).
		SetManualQuotaResetCount(2).
		Save(context.Background())
	require.NoError(t, err)
	return subscription
}

func ensureBulkResetGroup(t *testing.T, client *dbent.Client, logicalID int64) *dbent.Group {
	t.Helper()
	name := fmt.Sprintf("Group %d", logicalID)
	group, err := client.Group.Query().Where(dbgroup.NameEQ(name)).Only(context.Background())
	if err == nil {
		return group
	}
	require.True(t, dbent.IsNotFound(err), "query group: %v", err)
	group, err = client.Group.Create().
		SetName(name).
		SetPlatform("openai").
		SetRateMultiplier(1).
		Save(context.Background())
	require.NoError(t, err)
	return group
}

func createBulkResetPaymentCycle(t *testing.T, client *dbent.Client, orderUser *dbent.User, subscription *dbent.UserSubscription, plan *dbent.SubscriptionPlan, startsAt, endsAt time.Time, status string, orderGroupOverride *int64) {
	t.Helper()
	groupID := subscription.GroupID
	if orderGroupOverride != nil {
		groupID = *orderGroupOverride
	}
	order, err := client.PaymentOrder.Create().
		SetUserID(orderUser.ID).
		SetUserEmail(orderUser.Email).
		SetUserName(orderUser.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode(fmt.Sprintf("bulk-%d-%d", subscription.ID, plan.ID)).
		SetPaymentType("test").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(plan.ID).
		SetSubscriptionGroupID(groupID).
		SetExpiresAt(endsAt).
		SetClientIP("127.0.0.1").
		SetSrcHost("test").
		Save(context.Background())
	require.NoError(t, err)
	_, err = client.UserSubscriptionCycle.Create().
		SetSubscriptionID(subscription.ID).
		SetStartsAt(startsAt).
		SetEndsAt(endsAt).
		SetStatus(status).
		SetSourceType(subscriptionquota.CycleSourcePayment).
		SetSourceRef(strconv.FormatInt(order.ID, 10)).
		Save(context.Background())
	require.NoError(t, err)
}

func int64Ptr(value int64) *int64 { return &value }
