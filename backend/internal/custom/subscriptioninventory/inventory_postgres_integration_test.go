//go:build integration

package subscriptioninventory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const inventoryConcurrencyWorkers = 12

type inventoryReservationResult struct {
	state string
	err   error
}

func TestSubscriptionPlanInventoryPostgresConcurrentReservations(t *testing.T) {
	client, db := newPostgresInventoryTestClient(t)

	for _, tc := range []struct {
		name             string
		action           string
		wantForSale      bool
		wantAutoDelisted bool
	}{
		{name: "delist", action: SoldOutActionDelist, wantForSale: false, wantAutoDelisted: true},
		{name: "disable purchase", action: SoldOutActionDisablePurchase, wantForSale: true, wantAutoDelisted: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			quantity := 3
			plan, err := client.SubscriptionPlan.Create().
				SetGroupID(1).
				SetName(fmt.Sprintf("inventory-%s-%d", tc.action, time.Now().UnixNano())).
				SetPrice(10).
				SetRemainingQuantity(quantity).
				SetSoldOutAction(tc.action).
				Save(ctx)
			require.NoError(t, err)

			ready := make(chan error, inventoryConcurrencyWorkers)
			start := make(chan struct{})
			results := make(chan inventoryReservationResult, inventoryConcurrencyWorkers)
			var wg sync.WaitGroup
			for range inventoryConcurrencyWorkers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					tx, beginErr := client.Tx(ctx)
					ready <- beginErr
					if beginErr != nil {
						results <- inventoryReservationResult{err: beginErr}
						return
					}
					<-start

					txCtx := dbent.NewTxContext(ctx, tx)
					state, reserveErr := ReserveForOrder(txCtx, tx.Client(), plan.ID, true)
					if reserveErr != nil {
						_ = tx.Rollback()
						results <- inventoryReservationResult{err: reserveErr}
						return
					}
					if commitErr := tx.Commit(); commitErr != nil {
						results <- inventoryReservationResult{err: commitErr}
						return
					}
					results <- inventoryReservationResult{state: state}
				}()
			}

			beginErrors := make([]error, 0, inventoryConcurrencyWorkers)
			for range inventoryConcurrencyWorkers {
				beginErrors = append(beginErrors, <-ready)
			}
			activeConnections := db.Stats().InUse
			close(start)
			wg.Wait()
			close(results)

			for _, beginErr := range beginErrors {
				require.NoError(t, beginErr)
			}
			require.GreaterOrEqual(t, activeConnections, inventoryConcurrencyWorkers)

			successes := 0
			for result := range results {
				if result.err == nil {
					successes++
					require.Equal(t, StateReserved, result.state)
					continue
				}
				require.Equal(t, "PLAN_SOLD_OUT", infraerrors.Reason(result.err), "unexpected reservation error: %v", result.err)
			}
			require.Equal(t, quantity, successes)

			stored, err := client.SubscriptionPlan.Get(ctx, plan.ID)
			require.NoError(t, err)
			require.NotNil(t, stored.RemainingQuantity)
			require.Equal(t, 0, *stored.RemainingQuantity)
			require.Equal(t, tc.wantForSale, stored.ForSale)
			require.Equal(t, tc.wantAutoDelisted, stored.InventoryAutoDelisted)
		})
	}
}

func newPostgresInventoryTestClient(t *testing.T) (*dbent.Client, *sql.DB) {
	t.Helper()
	probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer probeCancel()
	if err := exec.CommandContext(probeCtx, "docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Docker is required for PostgreSQL inventory integration tests: %v", err)
		}
		t.Skipf("Docker is not available; skipping PostgreSQL inventory integration test: %v", err)
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
		tcpostgres.WithDatabase("sub2api_inventory_test"),
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
	db.SetMaxOpenConns(inventoryConcurrencyWorkers + 4)
	db.SetMaxIdleConns(inventoryConcurrencyWorkers + 4)
	require.NoError(t, db.PingContext(ctx))
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	require.NoError(t, client.Schema.Create(ctx))
	t.Cleanup(func() { _ = client.Close() })
	return client, db
}
