//go:build integration

package subscriptionrepository_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscriptioncycle"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/custom/idempotencyexecution"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionbulkreset"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	customrepository "github.com/Wei-Shaw/sub2api/internal/custom/subscriptionrepository"
	"github.com/Wei-Shaw/sub2api/internal/payment"
	baserepository "github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

type bulkResetFixture struct {
	user         *dbent.User
	group        *dbent.Group
	subscription *dbent.UserSubscription
	cycle        *dbent.UserSubscriptionCycle
	plan         *dbent.SubscriptionPlan
}

type bulkResetOutcome struct {
	result   *service.UserSubscription
	eligible bool
	err      error
}

var postgresBulkResetNameSequence atomic.Uint64

func TestBulkResetEligibilityAndQuotaMutationAreAtomicOnPostgres(t *testing.T) {
	client := newPostgresBulkResetTestClient(t)
	repository := customrepository.New(client, baserepository.NewUserSubscriptionRepository(client))
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("manual eligibility closed after preview skips without mutation", func(t *testing.T) {
		fixture := createPostgresManualBulkResetFixture(t, client, now, true)
		_, err := fixture.cycle.Update().SetManualBulkQuotaResetEnabled(false).Save(context.Background())
		require.NoError(t, err)

		result, eligible, err := repository.ResetQuotaIfBulkEligible(context.Background(), fixture.subscription.ID, idempotencyexecution.Execution{}, now)
		require.NoError(t, err)
		require.False(t, eligible)
		require.Nil(t, result)
		assertPostgresSubscriptionUsage(t, client, fixture.subscription.ID, 11, 12, 13, 14, 2)
	})

	t.Run("payment plan closure is rechecked under its row lock", func(t *testing.T) {
		fixture := createPostgresPaymentBulkResetFixture(t, client, now, true)
		tx, err := client.Tx(context.Background())
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		_, err = tx.SubscriptionPlan.UpdateOneID(fixture.plan.ID).SetAllowBulkQuotaReset(false).Save(context.Background())
		require.NoError(t, err)

		outcomes := make(chan bulkResetOutcome, 1)
		go func() {
			result, eligible, resetErr := repository.ResetQuotaIfBulkEligible(context.Background(), fixture.subscription.ID, idempotencyexecution.Execution{}, now)
			outcomes <- bulkResetOutcome{result: result, eligible: eligible, err: resetErr}
		}()

		select {
		case outcome := <-outcomes:
			t.Fatalf("bulk reset returned before the plan eligibility transaction committed: %+v", outcome)
		case <-time.After(100 * time.Millisecond):
		}
		require.NoError(t, tx.Commit())

		select {
		case outcome := <-outcomes:
			require.NoError(t, outcome.err)
			require.False(t, outcome.eligible)
			require.Nil(t, outcome.result)
		case <-time.After(10 * time.Second):
			t.Fatal("bulk reset did not resume after the plan eligibility transaction committed")
		}
		assertPostgresSubscriptionUsage(t, client, fixture.subscription.ID, 11, 12, 13, 14, 2)
	})

	t.Run("boundary advance returns refreshed state when the new cycle is ineligible", func(t *testing.T) {
		fixture := createPostgresBoundaryBulkResetFixture(t, client, now, false)

		result, eligible, err := repository.ResetQuotaIfBulkEligible(context.Background(), fixture.subscription.ID, idempotencyexecution.Execution{}, now)
		require.NoError(t, err)
		require.False(t, eligible)
		require.NotNil(t, result, "the caller needs the refreshed subscription to invalidate caches")
		require.Equal(t, now, result.CurrentCycleStartsAt)
		assertPostgresSubscriptionUsage(t, client, fixture.subscription.ID, 0, 0, 0, 0, 0)
	})

	t.Run("manual eligibility boundary advance invalidates the prior cycle cache after commit", func(t *testing.T) {
		fixture := createPostgresBoundaryBulkResetFixture(t, client, now, false)
		subscriptionService := service.NewSubscriptionService(nil, repository, nil, client, &config.Config{
			SubscriptionCache: config.SubscriptionCacheConfig{L1Size: 100, L1TTLSeconds: 60},
		})
		t.Cleanup(subscriptionService.Stop)
		before, err := subscriptionService.GetActiveSubscription(context.Background(), fixture.user.ID, fixture.group.ID)
		require.NoError(t, err)
		require.Equal(t, float64(11), before.DailyUsageUSD)

		eligibilityService := subscriptionbulkreset.NewService(client, subscriptionService)
		require.NoError(t, eligibilityService.UpdateCurrentCycleManualEligibility(context.Background(), fixture.subscription.ID, true))

		after, err := subscriptionService.GetActiveSubscription(context.Background(), fixture.user.ID, fixture.group.ID)
		require.NoError(t, err)
		require.Equal(t, now, after.CurrentCycleStartsAt)
		require.Zero(t, after.DailyUsageUSD)
		currentCycle, err := client.UserSubscriptionCycle.Query().
			Where(
				usersubscriptioncycle.SubscriptionIDEQ(fixture.subscription.ID),
				usersubscriptioncycle.StatusEQ(subscriptionquota.CycleStatusCurrent),
			).
			Only(context.Background())
		require.NoError(t, err)
		require.True(t, currentCycle.ManualBulkQuotaResetEnabled)
	})

	t.Run("boundary reset counts once in the new cycle and replay is idempotent", func(t *testing.T) {
		fixture := createPostgresBoundaryBulkResetFixture(t, client, now, true)
		operation, err := idempotencyexecution.New(
			"admin.subscriptions.bulk_reset_quota",
			"admin:7",
			service.HashIdempotencyKey("postgres-boundary-reset"),
			now,
			now.Add(24*time.Hour),
		)
		require.NoError(t, err)

		first, eligible, err := repository.ResetQuotaIfBulkEligible(context.Background(), fixture.subscription.ID, operation, now)
		require.NoError(t, err)
		require.True(t, eligible)
		require.NotNil(t, first)
		require.Equal(t, now, first.CurrentCycleStartsAt)
		require.Equal(t, int64(1), first.ManualQuotaResetCount)
		assertPostgresSubscriptionUsage(t, client, fixture.subscription.ID, 0, 0, 0, 0, 1)

		second, eligible, err := repository.ResetQuotaIfBulkEligible(context.Background(), fixture.subscription.ID, operation, now)
		require.NoError(t, err)
		require.True(t, eligible)
		require.NotNil(t, second)
		require.Equal(t, int64(1), second.ManualQuotaResetCount)
		assertPostgresSubscriptionUsage(t, client, fixture.subscription.ID, 0, 0, 0, 0, 1)
	})
}

func createPostgresManualBulkResetFixture(t *testing.T, client *dbent.Client, now time.Time, enabled bool) *bulkResetFixture {
	t.Helper()
	fixture := createPostgresBulkResetBaseFixture(t, client, now, now.Add(24*time.Hour), now.Add(24*time.Hour))
	cycle, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(fixture.subscription.ID).
		SetStartsAt(now.Add(-24 * time.Hour)).
		SetEndsAt(now.Add(24 * time.Hour)).
		SetStatus(subscriptionquota.CycleStatusCurrent).
		SetSourceType(subscriptionquota.CycleSourceAssignment).
		SetManualBulkQuotaResetEnabled(enabled).
		Save(context.Background())
	require.NoError(t, err)
	fixture.cycle = cycle
	return fixture
}

func createPostgresPaymentBulkResetFixture(t *testing.T, client *dbent.Client, now time.Time, enabled bool) *bulkResetFixture {
	t.Helper()
	fixture := createPostgresBulkResetBaseFixture(t, client, now, now.Add(24*time.Hour), now.Add(24*time.Hour))
	plan, err := client.SubscriptionPlan.Create().
		SetGroupID(fixture.group.ID).
		SetName(uniquePostgresBulkResetName(t, "plan")).
		SetPrice(10).
		SetAllowBulkQuotaReset(enabled).
		Save(context.Background())
	require.NoError(t, err)
	order, err := client.PaymentOrder.Create().
		SetUserID(fixture.user.ID).
		SetUserEmail(fixture.user.Email).
		SetUserName(fixture.user.Username).
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode(uniquePostgresBulkResetName(t, "order")).
		SetPaymentType("test").
		SetPaymentTradeNo("").
		SetOrderType(payment.OrderTypeSubscription).
		SetPlanID(plan.ID).
		SetSubscriptionGroupID(fixture.group.ID).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetClientIP("127.0.0.1").
		SetSrcHost("test").
		Save(context.Background())
	require.NoError(t, err)
	cycle, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(fixture.subscription.ID).
		SetStartsAt(now.Add(-24 * time.Hour)).
		SetEndsAt(now.Add(24 * time.Hour)).
		SetStatus(subscriptionquota.CycleStatusCurrent).
		SetSourceType(subscriptionquota.CycleSourcePayment).
		SetSourceRef(strconv.FormatInt(order.ID, 10)).
		Save(context.Background())
	require.NoError(t, err)
	fixture.plan = plan
	fixture.cycle = cycle
	return fixture
}

func createPostgresBoundaryBulkResetFixture(t *testing.T, client *dbent.Client, now time.Time, nextEnabled bool) *bulkResetFixture {
	t.Helper()
	fixture := createPostgresBulkResetBaseFixture(t, client, now, now, now.Add(24*time.Hour))
	current, err := client.UserSubscriptionCycle.Create().
		SetSubscriptionID(fixture.subscription.ID).
		SetStartsAt(now.Add(-24 * time.Hour)).
		SetEndsAt(now).
		SetStatus(subscriptionquota.CycleStatusCurrent).
		SetSourceType(subscriptionquota.CycleSourceAssignment).
		SetManualBulkQuotaResetEnabled(true).
		Save(context.Background())
	require.NoError(t, err)
	_, err = client.UserSubscriptionCycle.Create().
		SetSubscriptionID(fixture.subscription.ID).
		SetStartsAt(now).
		SetEndsAt(now.Add(24 * time.Hour)).
		SetStatus(subscriptionquota.CycleStatusPending).
		SetSourceType(subscriptionquota.CycleSourceAssignment).
		SetManualBulkQuotaResetEnabled(nextEnabled).
		Save(context.Background())
	require.NoError(t, err)
	fixture.cycle = current
	return fixture
}

func createPostgresBulkResetBaseFixture(t *testing.T, client *dbent.Client, now, currentCycleEndsAt, expiresAt time.Time) *bulkResetFixture {
	t.Helper()
	ctx := context.Background()
	group, err := client.Group.Create().
		SetName(uniquePostgresBulkResetName(t, "group")).
		SetPlatform("openai").
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)
	user, err := client.User.Create().
		SetEmail(uniquePostgresBulkResetName(t, "user") + "@example.com").
		SetPasswordHash("hash").
		SetUsername(uniquePostgresBulkResetName(t, "user")).
		Save(ctx)
	require.NoError(t, err)
	subscription, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(now.Add(-24 * time.Hour)).
		SetExpiresAt(expiresAt).
		SetStatus(service.SubscriptionStatusActive).
		SetDailyWindowStart(now.Add(-time.Hour)).
		SetWeeklyWindowStart(now.Add(-time.Hour)).
		SetMonthlyWindowStart(now.Add(-time.Hour)).
		SetDailyUsageUsd(11).
		SetWeeklyUsageUsd(12).
		SetMonthlyUsageUsd(13).
		SetCycleUsageUsd(14).
		SetManualQuotaResetCount(2).
		SetCurrentCycleStartsAt(now.Add(-24 * time.Hour)).
		SetCurrentCycleEndsAt(currentCycleEndsAt).
		Save(ctx)
	require.NoError(t, err)
	return &bulkResetFixture{user: user, group: group, subscription: subscription}
}

func assertPostgresSubscriptionUsage(t *testing.T, client *dbent.Client, subscriptionID int64, daily, weekly, monthly, cycle float64, resets int64) {
	t.Helper()
	stored, err := client.UserSubscription.Get(context.Background(), subscriptionID)
	require.NoError(t, err)
	require.Equal(t, daily, stored.DailyUsageUsd)
	require.Equal(t, weekly, stored.WeeklyUsageUsd)
	require.Equal(t, monthly, stored.MonthlyUsageUsd)
	require.Equal(t, cycle, stored.CycleUsageUsd)
	require.Equal(t, resets, stored.ManualQuotaResetCount)
}

func uniquePostgresBulkResetName(t *testing.T, prefix string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), postgresBulkResetNameSequence.Add(1))
}

func newPostgresBulkResetTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer probeCancel()
	if err := exec.CommandContext(probeCtx, "docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Docker is required for PostgreSQL subscription repository integration tests: %v", err)
		}
		t.Skipf("Docker is not available; skipping PostgreSQL subscription repository integration test: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	image := strings.TrimSpace(os.Getenv("SUB2API_TEST_POSTGRES_IMAGE"))
	if image == "" {
		image = "postgres:18.1-alpine3.23"
	}
	container, err := tcpostgres.Run(
		ctx,
		image,
		tcpostgres.WithDatabase("sub2api_subscription_repository_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		tcpostgres.BasicWaitStrategies(),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(12)
	require.NoError(t, db.PingContext(ctx))
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	require.NoError(t, client.Schema.Create(ctx))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
