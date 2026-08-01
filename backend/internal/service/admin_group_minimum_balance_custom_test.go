//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type adminMinimumBalanceUserRepoStub struct {
	*userRepoStubForGroupUpdate
	user              *User
	getByIDCalled     bool
	removeGroupCalled bool
}

func newAdminMinimumBalanceUserRepoStub(user *User) *adminMinimumBalanceUserRepoStub {
	return &adminMinimumBalanceUserRepoStub{
		userRepoStubForGroupUpdate: &userRepoStubForGroupUpdate{},
		user:                       user,
	}
}

func (s *adminMinimumBalanceUserRepoStub) GetByID(_ context.Context, userID int64) (*User, error) {
	s.getByIDCalled = true
	clone := *s.user
	if clone.ID == 0 {
		clone.ID = userID
	}
	return &clone, nil
}

func (s *adminMinimumBalanceUserRepoStub) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	s.removeGroupCalled = true
	return nil
}

type adminMinimumBalanceAPIKeyRepoStub struct {
	*apiKeyRepoStubForGroupUpdate
	migratedKeys  int64
	migrateCalled bool
}

func newAdminMinimumBalanceAPIKeyRepoStub(key *APIKey) *adminMinimumBalanceAPIKeyRepoStub {
	return &adminMinimumBalanceAPIKeyRepoStub{
		apiKeyRepoStubForGroupUpdate: &apiKeyRepoStubForGroupUpdate{key: key},
	}
}

func (s *adminMinimumBalanceAPIKeyRepoStub) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	s.migrateCalled = true
	return s.migratedKeys, nil
}

func newAdminMinimumBalanceTestClient(t *testing.T) *dbent.Client {
	t.Helper()
	db, err := sql.Open("sqlite", "file:admin_minimum_balance?mode=memory&cache=shared&_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(driver)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestAdminUpdateAPIKeyGroupIDMinimumBalanceUsesLatestUser(t *testing.T) {
	apiKeyRepo := newAdminMinimumBalanceAPIKeyRepoStub(&APIKey{ID: 1, UserID: 42, Key: "sk-test"})
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{
		ID:             10,
		Name:           "Pro",
		Status:         StatusActive,
		MinimumBalance: 100,
	}}
	userRepo := newAdminMinimumBalanceUserRepoStub(&User{ID: 42, Balance: 100})
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.Error(t, err)
	require.Equal(t, "GROUP_MINIMUM_BALANCE_NOT_MET", infraerrors.Reason(err))
	require.True(t, userRepo.getByIDCalled)
	require.Nil(t, apiKeyRepo.updated)
}

func TestAdminUpdateAPIKeyGroupIDSameGroupSkipsMinimumBalance(t *testing.T) {
	apiKeyRepo := newAdminMinimumBalanceAPIKeyRepoStub(&APIKey{
		ID:      1,
		UserID:  42,
		Key:     "sk-test",
		GroupID: int64Ptr(10),
	})
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{
		ID:             10,
		Name:           "Pro",
		Status:         StatusActive,
		MinimumBalance: 100,
	}}
	userRepo := newAdminMinimumBalanceUserRepoStub(&User{ID: 42, Balance: 0})
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.False(t, userRepo.getByIDCalled)
	require.Zero(t, groupRepo.lastGetByIDArg)
	require.NotNil(t, apiKeyRepo.updated)
}

func TestReplaceUserGroupMinimumBalanceRejectsActualMigration(t *testing.T) {
	apiKeyRepo := newAdminMinimumBalanceAPIKeyRepoStub(nil)
	apiKeyRepo.migratedKeys = 2
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{
		ID:               20,
		Name:             "New Exclusive",
		Status:           StatusActive,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
		MinimumBalance:   100,
	}}
	userRepo := newAdminMinimumBalanceUserRepoStub(&User{ID: 42, Balance: 100})
	svc := &adminServiceImpl{
		apiKeyRepo: apiKeyRepo,
		groupRepo:  groupRepo,
		userRepo:   userRepo,
		entClient:  newAdminMinimumBalanceTestClient(t),
	}

	_, err := svc.ReplaceUserGroup(context.Background(), 42, 10, 20)
	require.Error(t, err)
	require.Equal(t, "GROUP_MINIMUM_BALANCE_NOT_MET", infraerrors.Reason(err))
	require.True(t, apiKeyRepo.migrateCalled)
	require.True(t, userRepo.getByIDCalled)
	require.False(t, userRepo.addGroupCalled)
	require.False(t, userRepo.removeGroupCalled)
}

func TestReplaceUserGroupZeroMigrationSkipsMinimumBalance(t *testing.T) {
	apiKeyRepo := newAdminMinimumBalanceAPIKeyRepoStub(nil)
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{
		ID:               20,
		Name:             "New Exclusive",
		Status:           StatusActive,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
		MinimumBalance:   100,
	}}
	userRepo := newAdminMinimumBalanceUserRepoStub(nil)
	svc := &adminServiceImpl{
		apiKeyRepo: apiKeyRepo,
		groupRepo:  groupRepo,
		userRepo:   userRepo,
		entClient:  newAdminMinimumBalanceTestClient(t),
	}

	result, err := svc.ReplaceUserGroup(context.Background(), 42, 10, 20)
	require.NoError(t, err)
	require.Zero(t, result.MigratedKeys)
	require.True(t, apiKeyRepo.migrateCalled)
	require.False(t, userRepo.getByIDCalled)
	require.True(t, userRepo.addGroupCalled)
	require.True(t, userRepo.removeGroupCalled)
}
