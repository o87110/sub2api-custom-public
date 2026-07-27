package channelmonitor

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func newGroupRateLookupTestClient(t *testing.T) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:group_rate_lookup?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestEntGroupRateLookupListByAPIKeysReturnsOnlyActiveKeys(t *testing.T) {
	ctx := context.Background()
	client := newGroupRateLookupTestClient(t)

	user, err := client.User.Create().
		SetEmail("group-rate-lookup@example.com").
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	currentGroup, err := client.Group.Create().
		SetName("OpenAI 18%").
		SetPlatform("openai").
		SetRateMultiplier(0.18).
		Save(ctx)
	require.NoError(t, err)

	createKey := func(key, status string, groupID *int64, deletedAt *time.Time) {
		t.Helper()
		builder := client.APIKey.Create().
			SetUserID(user.ID).
			SetName(key).
			SetKey(key).
			SetStatus(status)
		if groupID != nil {
			builder.SetGroupID(*groupID)
		}
		if deletedAt != nil {
			builder.SetDeletedAt(*deletedAt)
		}
		_, createErr := builder.Save(ctx)
		require.NoError(t, createErr)
	}

	groupID := currentGroup.ID
	deletedAt := time.Now()
	createKey("sk-active", service.StatusAPIKeyActive, &groupID, nil)
	createKey("sk-disabled", service.StatusAPIKeyDisabled, &groupID, nil)
	createKey("sk-quota-exhausted", service.StatusAPIKeyQuotaExhausted, &groupID, nil)
	createKey("sk-expired", service.StatusAPIKeyExpired, &groupID, nil)
	createKey("sk-deleted", service.StatusAPIKeyActive, &groupID, &deletedAt)
	createKey("sk-no-group", service.StatusAPIKeyActive, nil, nil)

	rates, err := NewEntGroupRateLookup(client).ListByAPIKeys(ctx, []string{
		"sk-active",
		"sk-disabled",
		"sk-quota-exhausted",
		"sk-expired",
		"sk-deleted",
		"sk-no-group",
		"sk-unknown",
	})
	require.NoError(t, err)
	require.Equal(t, map[string]GroupRate{
		"sk-active": {
			Platform:       "openai",
			RateMultiplier: 0.18,
		},
	}, rates)
}
