package subscriptioninventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestQuantityPatchPreservesOmittedNullAndPositiveInteger(t *testing.T) {
	t.Parallel()

	var request struct {
		Quantity QuantityPatch `json:"remaining_quantity"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{}`), &request))
	require.False(t, request.Quantity.Present)

	require.NoError(t, json.Unmarshal([]byte(`{"remaining_quantity":null}`), &request))
	require.True(t, request.Quantity.Present)
	require.Nil(t, request.Quantity.Value)

	require.NoError(t, json.Unmarshal([]byte(`{"remaining_quantity":7}`), &request))
	require.True(t, request.Quantity.Present)
	require.Equal(t, 7, *request.Quantity.Value)

	require.Error(t, json.Unmarshal([]byte(`{"remaining_quantity":1.5}`), &request))
}

func TestResolveAdminAvailabilityRestoresOnlyAutomaticDelisting(t *testing.T) {
	t.Parallel()

	zero := 0
	five := 5
	patch := QuantityPatch{Present: true, Value: &five}
	automatic, err := ResolveAdminAvailability(AdminAvailability{
		RemainingQuantity:     &zero,
		ForSale:               false,
		InventoryAutoDelisted: true,
	}, patch, nil)
	require.NoError(t, err)
	require.True(t, automatic.ForSale)
	require.False(t, automatic.InventoryAutoDelisted)

	manual, err := ResolveAdminAvailability(AdminAvailability{
		RemainingQuantity: &zero,
		ForSale:           false,
	}, patch, nil)
	require.NoError(t, err)
	require.False(t, manual.ForSale)

	list := true
	_, err = ResolveAdminAvailability(AdminAvailability{
		RemainingQuantity: &zero,
	}, QuantityPatch{}, &list)
	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
}

func TestReserveReleaseAndConsumeInventoryStates(t *testing.T) {
	ctx := context.Background()
	client := newInventoryTestClient(t, 1)

	t.Run("unlimited remains untracked", func(t *testing.T) {
		plan := createInventoryTestPlan(t, client, nil)
		state, err := ReserveForOrder(ctx, client, plan.ID, true)
		require.NoError(t, err)
		require.Equal(t, StateUntracked, state)
	})

	t.Run("last unit delists and release restores once", func(t *testing.T) {
		one := 1
		plan := createInventoryTestPlan(t, client, &one)
		state, err := ReserveForOrder(ctx, client, plan.ID, true)
		require.NoError(t, err)
		require.Equal(t, StateReserved, state)
		order := createInventoryTestOrder(t, client, plan.ID, StateReserved, nil)

		plan = requireInventoryPlan(t, client, plan.ID)
		require.Equal(t, 0, *plan.RemainingQuantity)
		require.False(t, plan.ForSale)
		require.True(t, plan.InventoryAutoDelisted)

		require.NoError(t, ReleaseReservation(ctx, client, order.ID))
		require.NoError(t, ReleaseReservation(ctx, client, order.ID))
		plan = requireInventoryPlan(t, client, plan.ID)
		require.Equal(t, 1, *plan.RemainingQuantity)
		require.True(t, plan.ForSale)
		require.False(t, plan.InventoryAutoDelisted)
		require.Equal(t, StateReleased, requireInventoryOrder(t, client, order.ID).PlanInventoryState)
	})

	t.Run("manual delisting is preserved on release", func(t *testing.T) {
		one := 1
		plan := createInventoryTestPlan(t, client, &one)
		_, err := ReserveForOrder(ctx, client, plan.ID, true)
		require.NoError(t, err)
		order := createInventoryTestOrder(t, client, plan.ID, StateReserved, nil)
		_, err = client.SubscriptionPlan.UpdateOneID(plan.ID).
			SetForSale(false).
			SetInventoryAutoDelisted(false).
			Save(ctx)
		require.NoError(t, err)

		require.NoError(t, ReleaseReservation(ctx, client, order.ID))
		plan = requireInventoryPlan(t, client, plan.ID)
		require.Equal(t, 1, *plan.RemainingQuantity)
		require.False(t, plan.ForSale)
		require.False(t, plan.InventoryAutoDelisted)
	})

	t.Run("consumed reservations are idempotent and never returned", func(t *testing.T) {
		one := 1
		plan := createInventoryTestPlan(t, client, &one)
		_, err := ReserveForOrder(ctx, client, plan.ID, true)
		require.NoError(t, err)
		paidAt := time.Now()
		order := createInventoryTestOrder(t, client, plan.ID, StateReserved, &paidAt)

		require.NoError(t, ConsumeForFulfillment(ctx, client, order.ID))
		require.NoError(t, ConsumeForFulfillment(ctx, client, order.ID))
		require.NoError(t, ReleaseReservation(ctx, client, order.ID))
		require.Equal(t, StateConsumed, requireInventoryOrder(t, client, order.ID).PlanInventoryState)
		require.Equal(t, 0, *requireInventoryPlan(t, client, plan.ID).RemainingQuantity)
	})
}

func TestReleasedPaidOrderReacquiresWithoutOverselling(t *testing.T) {
	ctx := context.Background()
	client := newInventoryTestClient(t, 1)
	one := 1
	plan := createInventoryTestPlan(t, client, &one)
	paidAt := time.Now()
	first := createInventoryTestOrder(t, client, plan.ID, StateReleased, &paidAt)
	second := createInventoryTestOrder(t, client, plan.ID, StateReleased, &paidAt)

	require.NoError(t, ConsumeForFulfillment(ctx, client, first.ID))
	err := ConsumeForFulfillment(ctx, client, second.ID)
	require.Error(t, err)
	require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
	require.Equal(t, StateReleased, requireInventoryOrder(t, client, second.ID).PlanInventoryState)
	require.Equal(t, 0, *requireInventoryPlan(t, client, plan.ID).RemainingQuantity)
}

func TestConcurrentReservationNeverExceedsRemainingQuantity(t *testing.T) {
	ctx := context.Background()
	client := newInventoryTestClient(t, 1)
	quantity := 3
	plan := createInventoryTestPlan(t, client, &quantity)

	var wg sync.WaitGroup
	results := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ReserveForOrder(ctx, client, plan.ID, true)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	successes := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(err))
	}
	require.Equal(t, quantity, successes)
	plan = requireInventoryPlan(t, client, plan.ID)
	require.Equal(t, 0, *plan.RemainingQuantity)
	require.False(t, plan.ForSale)
	require.True(t, plan.InventoryAutoDelisted)
}

func newInventoryTestClient(t *testing.T, maxOpenConnections int) *dbent.Client {
	t.Helper()
	dbName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=memory&cache=shared&_pragma=foreign_keys(1)", dbName))
	require.NoError(t, err)
	db.SetMaxOpenConns(maxOpenConnections)
	t.Cleanup(func() { _ = db.Close() })
	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	_, err = client.User.Create().
		SetEmail("inventory@example.com").
		SetPasswordHash("test-password-hash").
		SetUsername("inventory").
		Save(context.Background())
	require.NoError(t, err)
	return client
}

func createInventoryTestPlan(t *testing.T, client *dbent.Client, quantity *int) *dbent.SubscriptionPlan {
	t.Helper()
	builder := client.SubscriptionPlan.Create().
		SetGroupID(1).
		SetName("Inventory plan").
		SetPrice(10).
		SetForSale(true)
	if quantity != nil {
		builder.SetRemainingQuantity(*quantity)
	}
	plan, err := builder.Save(context.Background())
	require.NoError(t, err)
	return plan
}

func createInventoryTestOrder(t *testing.T, client *dbent.Client, planID int64, state string, paidAt *time.Time) *dbent.PaymentOrder {
	t.Helper()
	builder := client.PaymentOrder.Create().
		SetUserID(1).
		SetUserEmail("inventory@example.com").
		SetUserName("inventory").
		SetAmount(10).
		SetPayAmount(10).
		SetRechargeCode("").
		SetPaymentType("test").
		SetPaymentTradeNo("").
		SetPlanID(planID).
		SetPlanInventoryState(state).
		SetExpiresAt(time.Now().Add(time.Hour)).
		SetClientIP("").
		SetSrcHost("")
	if paidAt != nil {
		builder.SetPaidAt(*paidAt)
	}
	order, err := builder.Save(context.Background())
	require.NoError(t, err)
	return order
}

func requireInventoryPlan(t *testing.T, client *dbent.Client, id int64) *dbent.SubscriptionPlan {
	t.Helper()
	plan, err := client.SubscriptionPlan.Get(context.Background(), id)
	require.NoError(t, err)
	return plan
}

func requireInventoryOrder(t *testing.T, client *dbent.Client, id int64) *dbent.PaymentOrder {
	t.Helper()
	order, err := client.PaymentOrder.Get(context.Background(), id)
	require.NoError(t, err)
	return order
}
