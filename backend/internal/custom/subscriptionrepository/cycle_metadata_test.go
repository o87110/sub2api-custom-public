package subscriptionrepository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/custom/subscriptionquota"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestAttachCurrentCycleMetadataUsesTransactionClient(t *testing.T) {
	ctx := context.Background()
	client := newCycleMetadataTestClient(t)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	user, err := client.User.Create().SetEmail("tx-cycle@example.com").SetPasswordHash("hash").SetUsername("tx-cycle").Save(ctx)
	require.NoError(t, err)
	group, err := client.Group.Create().SetName("Tx cycle").SetPlatform("openai").SetRateMultiplier(1).Save(ctx)
	require.NoError(t, err)
	subscription, err := client.UserSubscription.Create().
		SetUserID(user.ID).
		SetGroupID(group.ID).
		SetStartsAt(now.Add(-time.Hour)).
		SetExpiresAt(now.Add(24 * time.Hour)).
		SetStatus(service.SubscriptionStatusActive).
		SetCurrentCycleStartsAt(now.Add(-time.Hour)).
		SetCurrentCycleEndsAt(now.Add(24 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	tx, err := client.Tx(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	txCtx := dbent.NewTxContext(ctx, tx)
	_, err = tx.Client().UserSubscriptionCycle.Create().
		SetSubscriptionID(subscription.ID).
		SetStartsAt(now.Add(-time.Hour)).
		SetEndsAt(now.Add(24 * time.Hour)).
		SetStatus(subscriptionquota.CycleStatusCurrent).
		SetSourceType(subscriptionquota.CycleSourceAssignment).
		SetManualBulkQuotaResetEnabled(true).
		Save(txCtx)
	require.NoError(t, err)

	value := &service.UserSubscription{
		ID:        subscription.ID,
		StartsAt:  subscription.StartsAt,
		ExpiresAt: subscription.ExpiresAt,
		Status:    subscription.Status,
	}
	repo := &Repository{client: client}
	require.NoError(t, repo.attachCurrentCycleMetadata(txCtx, []*service.UserSubscription{value}, now))
	require.Equal(t, subscriptionquota.CycleSourceAssignment, value.CycleSourceType)
	require.True(t, value.ManualBulkQuotaResetEnabled)
	require.True(t, value.ManualBulkQuotaResetEditable)
}

func newCycleMetadataTestClient(t *testing.T) *dbent.Client {
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
