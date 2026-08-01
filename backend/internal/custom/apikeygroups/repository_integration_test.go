package apikeygroups_test

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/apikeygroup"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	customapikeygroups "github.com/Wei-Shaw/sub2api/internal/custom/apikeygroups"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type groupReplacer interface {
	ReplaceGroups(ctx context.Context, keyID int64, groupIDs []int64) error
}

type groupUpdater interface {
	UpdateWithGroups(
		ctx context.Context,
		key *service.APIKey,
		fields service.APIKeyUpdateFields,
		groupIDs []int64,
	) error
}

func newRepository(t *testing.T) (service.APIKeyRepository, *dbent.Client) {
	t.Helper()

	db, err := sql.Open(
		"sqlite",
		"file:custom_api_key_groups?mode=memory&cache=shared&_pragma=foreign_keys(1)",
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return repository.NewAPIKeyRepository(client, db), client
}

func mustCreateUser(t *testing.T, ctx context.Context, client *dbent.Client, email string) int64 {
	t.Helper()
	entity, err := client.User.Create().
		SetEmail(email).
		SetPasswordHash("test-password-hash").
		SetRole(service.RoleUser).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	return entity.ID
}

func mustCreateGroup(
	t *testing.T,
	ctx context.Context,
	client *dbent.Client,
	name, platform string,
) *dbent.Group {
	t.Helper()
	entity, err := client.Group.Create().
		SetName(name).
		SetPlatform(platform).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)
	return entity
}

func TestAPIKeyRepositoryOrderedGroupsCreateAndFilter(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-create@test.com")
	groupA := mustCreateGroup(t, ctx, client, "ordered-a", service.PlatformOpenAI)
	groupB := mustCreateGroup(t, ctx, client, "ordered-b", service.PlatformOpenAI)

	key := &service.APIKey{
		UserID:   userID,
		Key:      "sk-ordered-groups-create",
		Name:     "Ordered groups",
		GroupIDs: []int64{groupA.ID, groupB.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	stored, err := client.APIKey.Get(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.GroupID)
	require.Equal(t, groupA.ID, *stored.GroupID)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{groupA.ID, groupB.ID}, got.GroupIDs)
	require.Equal(t, groupA.ID, *got.GroupID)
	require.Equal(t, []string{"ordered-a", "ordered-b"}, []string{got.Groups[0].Name, got.Groups[1].Name})

	links, err := client.APIKeyGroup.Query().
		Where(apikeygroup.APIKeyIDEQ(key.ID)).
		Order(apikeygroup.ByPriority()).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, links, 2)
	require.Equal(t, []int{0, 1}, []int{links[0].Priority, links[1].Priority})

	filtered, page, err := repo.ListByUserID(
		ctx,
		userID,
		pagination.PaginationParams{Page: 1, PageSize: 10},
		service.APIKeyListFilters{GroupID: &groupB.ID},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, filtered, 1)
	require.Equal(t, key.ID, filtered[0].ID)
}

func TestAPIKeyRepositoryReplaceGroupsCompactsAndSynchronizesLegacyGroup(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-replace@test.com")
	groupA := mustCreateGroup(t, ctx, client, "replace-a", service.PlatformAnthropic)
	groupB := mustCreateGroup(t, ctx, client, "replace-b", service.PlatformAnthropic)
	groupC := mustCreateGroup(t, ctx, client, "replace-c", service.PlatformAnthropic)

	key := &service.APIKey{
		UserID:   userID,
		Key:      "sk-ordered-groups-replace",
		Name:     "Replace groups",
		GroupIDs: []int64{groupA.ID, groupB.ID, groupC.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	replacer, ok := repo.(groupReplacer)
	require.True(t, ok)
	require.NoError(t, replacer.ReplaceGroups(ctx, key.ID, []int64{groupC.ID, groupA.ID}))
	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{groupC.ID, groupA.ID}, got.GroupIDs)
	require.Equal(t, groupC.ID, *got.GroupID)

	links, err := client.APIKeyGroup.Query().
		Where(apikeygroup.APIKeyIDEQ(key.ID)).
		Order(apikeygroup.ByPriority()).
		All(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1}, []int{links[0].Priority, links[1].Priority})

	require.NoError(t, replacer.ReplaceGroups(ctx, key.ID, nil))
	got, err = repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Empty(t, got.GroupIDs)
	require.Empty(t, got.Groups)
	require.Nil(t, got.GroupID)

	emptyGroupID := int64(0)
	filtered, page, err := repo.ListByUserID(
		ctx,
		userID,
		pagination.PaginationParams{Page: 1, PageSize: 10},
		service.APIKeyListFilters{GroupID: &emptyGroupID},
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, key.ID, filtered[0].ID)
}

func TestRemoveGroupAssignmentsCompactsAndPromotesLegacyPrimary(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-delete@test.com")
	groupA := mustCreateGroup(t, ctx, client, "delete-a", service.PlatformOpenAI)
	groupB := mustCreateGroup(t, ctx, client, "delete-b", service.PlatformOpenAI)
	groupC := mustCreateGroup(t, ctx, client, "delete-c", service.PlatformOpenAI)

	key := &service.APIKey{
		UserID:   userID,
		Key:      "sk-ordered-groups-delete",
		Name:     "Delete group",
		GroupIDs: []int64{groupA.ID, groupB.ID, groupC.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	affected, err := customapikeygroups.RemoveGroupAssignments(ctx, client, groupA.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{key.ID}, affected)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{groupB.ID, groupC.ID}, got.GroupIDs)
	require.NotNil(t, got.GroupID)
	require.Equal(t, groupB.ID, *got.GroupID)

	links, err := client.APIKeyGroup.Query().
		Where(apikeygroup.APIKeyIDEQ(key.ID)).
		Order(apikeygroup.ByPriority()).
		All(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{0, 1}, []int{links[0].Priority, links[1].Priority})
}

func TestReplaceUserGroupRejectsMixedPlatformsAndRollsBack(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-user-replace@test.com")
	groupA := mustCreateGroup(t, ctx, client, "user-replace-a", service.PlatformAnthropic)
	groupB := mustCreateGroup(t, ctx, client, "user-replace-b", service.PlatformAnthropic)
	groupC := mustCreateGroup(t, ctx, client, "user-replace-c", service.PlatformOpenAI)

	key := &service.APIKey{
		UserID:   userID,
		Key:      "sk-ordered-groups-user-replace",
		Name:     "Replace user group",
		GroupIDs: []int64{groupA.ID, groupB.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	migrated, err := customapikeygroups.ReplaceUserGroup(ctx, client, userID, groupA.ID, groupC.ID)
	require.ErrorIs(t, err, customapikeygroups.ErrMixedPlatforms)
	require.Zero(t, migrated)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{groupA.ID, groupB.ID}, got.GroupIDs)
	require.Equal(t, groupA.ID, *got.GroupID)
}

func TestAPIKeyRepositoryUpdateWithGroupsRollsBackAllFields(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-rollback@test.com")
	groupA := mustCreateGroup(t, ctx, client, "rollback-a", service.PlatformGemini)
	groupB := mustCreateGroup(t, ctx, client, "rollback-b", service.PlatformGemini)

	key := &service.APIKey{
		UserID:   userID,
		Key:      "sk-ordered-groups-rollback",
		Name:     "Before rollback",
		GroupIDs: []int64{groupA.ID},
		Status:   service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	updater, ok := repo.(groupUpdater)
	require.True(t, ok)
	key.Name = "Must roll back"
	err := updater.UpdateWithGroups(
		ctx,
		key,
		service.APIKeyUpdateFields{Name: true},
		[]int64{groupB.ID, 999999999},
	)
	require.Error(t, err)

	got, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.Equal(t, "Before rollback", got.Name)
	require.Equal(t, []int64{groupA.ID}, got.GroupIDs)
	require.Equal(t, groupA.ID, *got.GroupID)
}

func TestAPIKeyGroupUniqueConstraints(t *testing.T) {
	repo, client := newRepository(t)
	ctx := context.Background()
	userID := mustCreateUser(t, ctx, client, "ordered-groups-constraints@test.com")
	groupA := mustCreateGroup(t, ctx, client, "constraints-a", service.PlatformOpenAI)
	groupB := mustCreateGroup(t, ctx, client, "constraints-b", service.PlatformOpenAI)

	key := &service.APIKey{
		UserID: userID,
		Key:    "sk-ordered-groups-constraints",
		Name:   "Constraint groups",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))
	_, err := client.APIKeyGroup.Create().
		SetAPIKeyID(key.ID).
		SetGroupID(groupA.ID).
		SetPriority(0).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.APIKeyGroup.Create().
		SetAPIKeyID(key.ID).
		SetGroupID(groupB.ID).
		SetPriority(0).
		Save(ctx)
	require.Error(t, err, "同一 API Key 不允许重复 priority")

	_, err = client.APIKeyGroup.Create().
		SetAPIKeyID(key.ID).
		SetGroupID(groupA.ID).
		SetPriority(1).
		Save(ctx)
	require.Error(t, err, "同一 API Key 不允许重复 group_id")
}
