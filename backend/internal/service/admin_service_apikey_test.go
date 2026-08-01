//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// userRepoStubForGroupUpdate implements UserRepository for AdminUpdateAPIKeyGroupID tests.
type userRepoStubForGroupUpdate struct {
	addGroupErr    error
	addGroupCalled bool
	addedUserID    int64
	addedGroupID   int64
}

func (s *userRepoStubForGroupUpdate) AddGroupToAllowedGroups(_ context.Context, userID int64, groupID int64) error {
	s.addGroupCalled = true
	s.addedUserID = userID
	s.addedGroupID = groupID
	return s.addGroupErr
}

func (s *userRepoStubForGroupUpdate) Create(context.Context, *User) error { panic("unexpected") }
func (s *userRepoStubForGroupUpdate) CreateWithEmailAliasGuard(context.Context, *User) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) GetByID(context.Context, int64) (*User, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) GetByEmail(context.Context, string) (*User, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) GetFirstAdmin(context.Context) (*User, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) Update(context.Context, *User, UserUpdateFields) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) Delete(context.Context, int64) error { panic("unexpected") }
func (s *userRepoStubForGroupUpdate) GetUserAvatar(context.Context, int64) (*UserAvatar, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) UpsertUserAvatar(context.Context, int64, UpsertUserAvatarInput) (*UserAvatar, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) DeleteUserAvatar(context.Context, int64) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) List(context.Context, pagination.PaginationParams) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) UpdateBalance(context.Context, int64, float64) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) DeductBalance(context.Context, int64, float64) error {
	panic("unexpected")
}

func (s *userRepoStubForGroupUpdate) AdjustBalance(ctx context.Context, id int64, delta float64) (BalanceChange, error) {
	panic("unexpected AdjustBalance call")
}

func (s *userRepoStubForGroupUpdate) SetBalance(ctx context.Context, id int64, value float64) (BalanceChange, error) {
	panic("unexpected SetBalance call")
}
func (s *userRepoStubForGroupUpdate) UpdateConcurrency(context.Context, int64, int) error {
	panic("unexpected")
}

func (s *userRepoStubForGroupUpdate) BatchSetConcurrency(context.Context, []int64, int) (int, error) {
	return 0, nil
}
func (s *userRepoStubForGroupUpdate) BatchAddConcurrency(context.Context, []int64, int) (int, error) {
	return 0, nil
}
func (s *userRepoStubForGroupUpdate) BatchUpdateLimits(context.Context, []int64, *int, *int) (int, error) {
	return 0, nil
}
func (s *userRepoStubForGroupUpdate) ExistsByEmail(context.Context, string) (bool, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) ExistsByEmailAlias(context.Context, string) (bool, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) RemoveGroupFromAllowedGroups(context.Context, int64) (int64, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) UpdateTotpSecret(context.Context, int64, *string) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) EnableTotp(context.Context, int64) error  { panic("unexpected") }
func (s *userRepoStubForGroupUpdate) DisableTotp(context.Context, int64) error { panic("unexpected") }
func (s *userRepoStubForGroupUpdate) GetByIDIncludeDeleted(ctx context.Context, id int64) (*User, error) {
	panic("unexpected GetByIDIncludeDeleted call")
}
func (s *userRepoStubForGroupUpdate) ListUserAuthIdentities(context.Context, int64) ([]UserAuthIdentityRecord, error) {
	panic("unexpected")
}

func (s *userRepoStubForGroupUpdate) UnbindUserAuthProvider(context.Context, int64, string) error {
	panic("unexpected")
}

func (s *userRepoStubForGroupUpdate) GetLatestUsedAtByUserIDs(context.Context, []int64) (map[int64]*time.Time, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) GetLatestUsedAtByUserID(context.Context, int64) (*time.Time, error) {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) UpdateUserLastActiveAt(context.Context, int64, time.Time) error {
	panic("unexpected")
}
func (s *userRepoStubForGroupUpdate) RemoveGroupFromUserAllowedGroups(context.Context, int64, int64) error {
	panic("unexpected")
}

// apiKeyRepoStubForGroupUpdate implements APIKeyRepository for AdminUpdateAPIKeyGroupID tests.
type apiKeyRepoStubForGroupUpdate struct {
	key               *APIKey
	getErr            error
	updateErr         error
	replaceErr        error
	updated           *APIKey // captures what was passed to Update
	listKeys          []APIKey
	listFilters       APIKeyListFilters
	listByGroupCalled bool
	listByAnyCalled   bool
	replacedGroupIDs  []int64
	replaceCalled     bool
}

func (s *apiKeyRepoStubForGroupUpdate) ReplaceGroups(_ context.Context, _ int64, groupIDs []int64) error {
	s.replaceCalled = true
	s.replacedGroupIDs = append([]int64(nil), groupIDs...)
	return s.replaceErr
}

func (s *apiKeyRepoStubForGroupUpdate) GetByID(_ context.Context, _ int64) (*APIKey, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	clone := *s.key
	return &clone, nil
}
func (s *apiKeyRepoStubForGroupUpdate) Update(_ context.Context, key *APIKey, _ APIKeyUpdateFields) error {
	if s.updateErr != nil {
		return s.updateErr
	}
	clone := *key
	s.updated = &clone
	return nil
}

// Unused methods – panic on unexpected call.
func (s *apiKeyRepoStubForGroupUpdate) Create(context.Context, *APIKey) error { panic("unexpected") }
func (s *apiKeyRepoStubForGroupUpdate) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) GetByKey(context.Context, string) (*APIKey, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) GetByKeyForAuth(context.Context, string) (*APIKey, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) Delete(context.Context, int64) error { panic("unexpected") }
func (s *apiKeyRepoStubForGroupUpdate) DeleteWithAudit(context.Context, int64) error {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ListByUserID(_ context.Context, _ int64, _ pagination.PaginationParams, filters APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	s.listFilters = filters
	return append([]APIKey(nil), s.listKeys...), &pagination.PaginationResult{Total: int64(len(s.listKeys))}, nil
}
func (s *apiKeyRepoStubForGroupUpdate) VerifyOwnership(context.Context, int64, []int64) ([]int64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) CountByUserID(context.Context, int64) (int64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ExistsByKey(context.Context, string) (bool, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ListByGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	s.listByGroupCalled = true
	return append([]APIKey(nil), s.listKeys...), &pagination.PaginationResult{Total: int64(len(s.listKeys))}, nil
}

func (s *apiKeyRepoStubForGroupUpdate) ListByAnyGroupID(context.Context, int64, pagination.PaginationParams) ([]APIKey, *pagination.PaginationResult, error) {
	s.listByAnyCalled = true
	return append([]APIKey(nil), s.listKeys...), &pagination.PaginationResult{Total: int64(len(s.listKeys))}, nil
}
func (s *apiKeyRepoStubForGroupUpdate) SearchAPIKeys(context.Context, int64, string, int) ([]APIKey, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ClearGroupIDByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) CountByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ListKeysByUserID(context.Context, int64) ([]string, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ListKeysByGroupID(context.Context, int64) ([]string, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) IncrementQuotaUsed(context.Context, int64, float64) (float64, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) UpdateLastUsed(context.Context, int64, time.Time) error {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) IncrementRateLimitUsage(context.Context, int64, float64) error {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) ResetRateLimitWindows(context.Context, int64) error {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) GetRateLimitData(context.Context, int64) (*APIKeyRateLimitData, error) {
	panic("unexpected")
}
func (s *apiKeyRepoStubForGroupUpdate) UpdateGroupIDByUserAndGroup(context.Context, int64, int64, int64) (int64, error) {
	panic("unexpected")
}

// groupRepoStubForGroupUpdate implements GroupRepository for AdminUpdateAPIKeyGroupID tests.
type groupRepoStubForGroupUpdate struct {
	group          *Group
	groups         map[int64]*Group
	getErr         error
	lastGetByIDArg int64
}

func (s *groupRepoStubForGroupUpdate) GetByID(_ context.Context, id int64) (*Group, error) {
	s.lastGetByIDArg = id
	if s.getErr != nil {
		return nil, s.getErr
	}
	if group, ok := s.groups[id]; ok {
		clone := *group
		return &clone, nil
	}
	clone := *s.group
	return &clone, nil
}

// Unused methods – panic on unexpected call.
func (s *groupRepoStubForGroupUpdate) Create(context.Context, *Group) error { panic("unexpected") }
func (s *groupRepoStubForGroupUpdate) GetByIDLite(context.Context, int64) (*Group, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) Update(context.Context, *Group) error { panic("unexpected") }
func (s *groupRepoStubForGroupUpdate) Delete(context.Context, int64) error  { panic("unexpected") }
func (s *groupRepoStubForGroupUpdate) DeleteCascade(context.Context, int64) ([]int64, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) List(context.Context, pagination.PaginationParams) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, *bool) ([]Group, *pagination.PaginationResult, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) ListActive(context.Context) ([]Group, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) ListActiveByPlatform(context.Context, string) ([]Group, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) ExistsByName(context.Context, string) (bool, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) GetAccountCount(context.Context, int64) (int64, int64, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) DeleteAccountGroupsByGroupID(context.Context, int64) (int64, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) GetAccountIDsByGroupIDs(context.Context, []int64) ([]int64, error) {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) BindAccountsToGroup(context.Context, int64, []int64) error {
	panic("unexpected")
}
func (s *groupRepoStubForGroupUpdate) UpdateSortOrders(context.Context, []GroupSortOrderUpdate) error {
	panic("unexpected")
}

type userSubRepoStubForGroupUpdate struct {
	userSubRepoNoop
	getActiveSub  *UserSubscription
	getActiveErr  error
	called        bool
	calledUserID  int64
	calledGroupID int64
}

func (s *userSubRepoStubForGroupUpdate) GetActiveByUserIDAndGroupID(_ context.Context, userID, groupID int64) (*UserSubscription, error) {
	s.called = true
	s.calledUserID = userID
	s.calledGroupID = groupID
	if s.getActiveErr != nil {
		return nil, s.getActiveErr
	}
	if s.getActiveSub == nil {
		return nil, ErrSubscriptionNotFound
	}
	clone := *s.getActiveSub
	return &clone, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAdminService_AdminUpdateAPIKeyGroupID_KeyNotFound(t *testing.T) {
	repo := &apiKeyRepoStubForGroupUpdate{getErr: ErrAPIKeyNotFound}
	svc := &adminServiceImpl{apiKeyRepo: repo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 999, int64Ptr(1))
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_NilGroupID_NoOp(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: int64Ptr(5)}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing}
	svc := &adminServiceImpl{apiKeyRepo: repo}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, nil)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.APIKey.ID)
	// Update should NOT have been called (updated stays nil)
	require.Nil(t, repo.updated)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_Unbind(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: int64Ptr(5), Group: &Group{ID: 5, Name: "Old"}}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: repo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(0))
	require.NoError(t, err)
	require.Nil(t, got.APIKey.GroupID, "group_id should be nil after unbind")
	require.Nil(t, got.APIKey.Group, "group object should be nil after unbind")
	require.True(t, repo.replaceCalled, "ReplaceGroups should have been called")
	require.Empty(t, repo.replacedGroupIDs)
	require.Equal(t, []string{"sk-test"}, cache.keys, "cache should be invalidated")
}

func TestAdminService_AdminUpdateAPIKeyGroupID_BindActiveGroup(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Pro", Status: StatusActive}}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	require.Equal(t, int64(10), *got.APIKey.GroupID)
	require.Equal(t, []int64{10}, apiKeyRepo.replacedGroupIDs)
	require.Equal(t, []string{"sk-test"}, cache.keys)
	// M3: verify correct group ID was passed to repo
	require.Equal(t, int64(10), groupRepo.lastGetByIDArg)
	// C1 fix: verify Group object is populated
	require.NotNil(t, got.APIKey.Group)
	require.Equal(t, "Pro", got.APIKey.Group.Name)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_SameGroup_Idempotent(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: int64Ptr(10), Group: &Group{ID: 10, Name: "Pro"}}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Pro", Status: StatusActive}}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	require.Equal(t, int64(10), *got.APIKey.GroupID)
	// ReplaceGroups is still called so the compatibility alias and ordered list
	// remain synchronized even for an idempotent legacy request.
	require.True(t, apiKeyRepo.replaceCalled)
	require.Equal(t, []int64{10}, apiKeyRepo.replacedGroupIDs)
	require.Equal(t, []string{"sk-test"}, cache.keys)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_GroupNotFound(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test"}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{getErr: ErrGroupNotFound}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(99))
	require.ErrorIs(t, err, ErrGroupNotFound)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_GroupNotActive(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test"}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 5, Status: StatusDisabled}}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(5))
	require.Error(t, err)
	require.Equal(t, "GROUP_NOT_ACTIVE", infraerrors.Reason(err))
}

func TestAdminService_AdminUpdateAPIKeyGroupID_UpdateFails(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: int64Ptr(3)}
	repo := &apiKeyRepoStubForGroupUpdate{key: existing, replaceErr: errors.New("db write error")}
	svc := &adminServiceImpl{apiKeyRepo: repo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(0))
	require.Error(t, err)
	require.Contains(t, err.Error(), "update api key")
}

func TestAdminService_AdminUpdateAPIKeyGroupID_NegativeGroupID(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test"}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(-5))
	require.Error(t, err)
	require.Equal(t, "INVALID_GROUP_ID", infraerrors.Reason(err))
}

func TestAdminService_AdminUpdateAPIKeyGroupID_PointerIsolation(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Pro", Status: StatusActive}}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, authCacheInvalidator: cache}

	inputGID := int64(10)
	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, &inputGID)
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	// Mutating the input pointer must NOT affect the stored value
	inputGID = 999
	require.Equal(t, int64(10), *got.APIKey.GroupID)
	require.Equal(t, []int64{10}, apiKeyRepo.replacedGroupIDs)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_NilCacheInvalidator(t *testing.T) {
	existing := &APIKey{ID: 1, Key: "sk-test"}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 7, Status: StatusActive}}
	// authCacheInvalidator is nil – should not panic
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(7))
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	require.Equal(t, int64(7), *got.APIKey.GroupID)
}

// ---------------------------------------------------------------------------
// Tests: AllowedGroup auto-sync
// ---------------------------------------------------------------------------

func TestAdminService_AdminUpdateAPIKeyGroupID_ExclusiveGroup_AddsAllowedGroup(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Exclusive", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard}}
	userRepo := &userRepoStubForGroupUpdate{}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	require.Equal(t, int64(10), *got.APIKey.GroupID)
	// 验证 AddGroupToAllowedGroups 被调用，且参数正确
	require.True(t, userRepo.addGroupCalled)
	require.Equal(t, int64(42), userRepo.addedUserID)
	require.Equal(t, int64(10), userRepo.addedGroupID)
	// 验证 result 标记了自动授权
	require.True(t, got.AutoGrantedGroupAccess)
	require.NotNil(t, got.GrantedGroupID)
	require.Equal(t, int64(10), *got.GrantedGroupID)
	require.Equal(t, "Exclusive", got.GrantedGroupName)
	require.Equal(t, []int64{42}, cache.userIDs, "专属分组授权会影响该用户的全部 API Key 快照")
	require.Empty(t, cache.keys)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_NonExclusiveGroup_NoAllowedGroupUpdate(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Public", Status: StatusActive, IsExclusive: false, SubscriptionType: SubscriptionTypeStandard}}
	userRepo := &userRepoStubForGroupUpdate{}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.NotNil(t, got.APIKey.GroupID)
	// 非专属分组不触发 AddGroupToAllowedGroups
	require.False(t, userRepo.addGroupCalled)
	require.False(t, got.AutoGrantedGroupAccess)
}

func TestAdminService_AdminUpdateAPIKeyGroups_ExclusiveGrantInvalidatesWholeUser(t *testing.T) {
	settings := &SettingService{}
	settings.refreshCachedSettings(&SystemSettings{APIKeyMultiGroupEnabled: true})
	existing := &APIKey{
		ID:       1,
		UserID:   42,
		Key:      "sk-test",
		GroupID:  int64Ptr(1),
		GroupIDs: []int64{1},
		User:     &User{ID: 42, Balance: 100},
	}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{groups: map[int64]*Group{
		1:  {ID: 1, Name: "Public", Platform: PlatformOpenAI, Status: StatusActive},
		10: {ID: 10, Name: "Exclusive", Platform: PlatformOpenAI, Status: StatusActive, IsExclusive: true},
	}}
	userRepo := &userRepoStubForGroupUpdate{}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		apiKeyRepo:           apiKeyRepo,
		groupRepo:            groupRepo,
		userRepo:             userRepo,
		authCacheInvalidator: cache,
		settingService:       settings,
	}

	got, err := svc.AdminUpdateAPIKeyGroups(context.Background(), 1, []int64{1, 10})
	require.NoError(t, err)
	require.True(t, got.AutoGrantedGroupAccess)
	require.Equal(t, []int64{1, 10}, apiKeyRepo.replacedGroupIDs)
	require.True(t, userRepo.addGroupCalled)
	require.Equal(t, []int64{42}, cache.userIDs)
	require.Empty(t, cache.keys)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_SubscriptionGroup_Blocked(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Sub", Status: StatusActive, IsExclusive: false, SubscriptionType: SubscriptionTypeSubscription}}
	userRepo := &userRepoStubForGroupUpdate{}
	userSubRepo := &userSubRepoStubForGroupUpdate{getActiveErr: ErrSubscriptionNotFound}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo, userSubRepo: userSubRepo}

	// 无有效订阅时应拒绝绑定
	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_REQUIRED", infraerrors.Reason(err))
	require.True(t, userSubRepo.called)
	require.Equal(t, int64(42), userSubRepo.calledUserID)
	require.Equal(t, int64(10), userSubRepo.calledGroupID)
	require.False(t, userRepo.addGroupCalled)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_ExistingSecondarySkipsSubscriptionRevalidation(t *testing.T) {
	existing := &APIKey{
		ID: 1, UserID: 42, Key: "sk-test", GroupID: int64Ptr(1),
		GroupIDs: []int64{1, 10},
	}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{
		ID: 10, Name: "Expired subscription", Status: StatusDisabled,
		SubscriptionType: SubscriptionTypeSubscription,
	}}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.Equal(t, []int64{10}, apiKeyRepo.replacedGroupIDs)
	require.Equal(t, []int64{10}, got.APIKey.GroupIDs)
	require.Equal(t, int64(10), *got.APIKey.GroupID)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_SubscriptionGroup_RequiresRepo(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Sub", Status: StatusActive, IsExclusive: false, SubscriptionType: SubscriptionTypeSubscription}}
	userRepo := &userRepoStubForGroupUpdate{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo}

	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.Error(t, err)
	require.Equal(t, "SUBSCRIPTION_REPOSITORY_UNAVAILABLE", infraerrors.Reason(err))
	require.False(t, userRepo.addGroupCalled)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_SubscriptionGroup_AllowsActiveSubscription(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Sub", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeSubscription}}
	userRepo := &userRepoStubForGroupUpdate{}
	userSubRepo := &userSubRepoStubForGroupUpdate{
		getActiveSub: &UserSubscription{ID: 99, UserID: 42, GroupID: 10},
	}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo, userSubRepo: userSubRepo}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.NoError(t, err)
	require.True(t, userSubRepo.called)
	require.NotNil(t, got.APIKey.GroupID)
	require.Equal(t, int64(10), *got.APIKey.GroupID)
	require.False(t, userRepo.addGroupCalled)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_ExclusiveGroup_AllowedGroupAddFails_ReturnsError(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: nil}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	groupRepo := &groupRepoStubForGroupUpdate{group: &Group{ID: 10, Name: "Exclusive", Status: StatusActive, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard}}
	userRepo := &userRepoStubForGroupUpdate{addGroupErr: errors.New("db error")}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, groupRepo: groupRepo, userRepo: userRepo}

	// 严格模式：AddGroupToAllowedGroups 失败时，整体操作报错
	_, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(10))
	require.Error(t, err)
	require.Contains(t, err.Error(), "add group to user allowed groups")
	require.True(t, userRepo.addGroupCalled)
	// apiKey 不应被更新
	require.False(t, apiKeyRepo.replaceCalled)
}

func TestAdminService_AdminUpdateAPIKeyGroupID_Unbind_NoAllowedGroupUpdate(t *testing.T) {
	existing := &APIKey{ID: 1, UserID: 42, Key: "sk-test", GroupID: int64Ptr(10), Group: &Group{ID: 10, Name: "Exclusive"}}
	apiKeyRepo := &apiKeyRepoStubForGroupUpdate{key: existing}
	userRepo := &userRepoStubForGroupUpdate{}
	cache := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{apiKeyRepo: apiKeyRepo, userRepo: userRepo, authCacheInvalidator: cache}

	got, err := svc.AdminUpdateAPIKeyGroupID(context.Background(), 1, int64Ptr(0))
	require.NoError(t, err)
	require.Nil(t, got.APIKey.GroupID)
	// 解绑时不修改 allowed_groups
	require.False(t, userRepo.addGroupCalled)
	require.False(t, got.AutoGrantedGroupAccess)
}

func TestAdminServiceGetUserAPIKeysFeatureDisabledUsesPrimaryOnly(t *testing.T) {
	primaryID := int64(1)
	secondaryID := int64(2)
	repo := &apiKeyRepoStubForGroupUpdate{listKeys: []APIKey{{
		ID:       7,
		GroupID:  &primaryID,
		GroupIDs: []int64{primaryID, secondaryID},
		Groups:   []Group{{ID: primaryID}, {ID: secondaryID}},
	}}}
	svc := &adminServiceImpl{apiKeyRepo: repo}

	keys, total, err := svc.GetUserAPIKeys(context.Background(), 9, 1, 20, "", "")
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.True(t, repo.listFilters.PrimaryGroupOnly)
	require.Equal(t, []int64{primaryID}, keys[0].GroupIDs)
	require.Len(t, keys[0].Groups, 1)
}

func TestAdminServiceGetUserAPIKeysFeatureEnabledPreservesPriorityList(t *testing.T) {
	primaryID := int64(1)
	secondaryID := int64(2)
	repo := &apiKeyRepoStubForGroupUpdate{listKeys: []APIKey{{
		ID:       7,
		GroupID:  &primaryID,
		GroupIDs: []int64{primaryID, secondaryID},
		Groups:   []Group{{ID: primaryID}, {ID: secondaryID}},
	}}}
	settings := &SettingService{}
	settings.apiKeyMultiGroupEnabled.Store(true)
	svc := &adminServiceImpl{apiKeyRepo: repo, settingService: settings}

	keys, _, err := svc.GetUserAPIKeys(context.Background(), 9, 1, 20, "", "")
	require.NoError(t, err)
	require.False(t, repo.listFilters.PrimaryGroupOnly)
	require.Equal(t, []int64{primaryID, secondaryID}, keys[0].GroupIDs)
	require.Len(t, keys[0].Groups, 2)
}

func TestAdminServiceGetGroupAPIKeysFeatureDisabledUsesPrimaryQuery(t *testing.T) {
	primaryID := int64(1)
	secondaryID := int64(2)
	repo := &apiKeyRepoStubForGroupUpdate{listKeys: []APIKey{{
		ID:       7,
		GroupID:  &primaryID,
		GroupIDs: []int64{primaryID, secondaryID},
		Groups:   []Group{{ID: primaryID}, {ID: secondaryID}},
	}}}
	svc := &adminServiceImpl{apiKeyRepo: repo}

	keys, total, err := svc.GetGroupAPIKeys(context.Background(), primaryID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.True(t, repo.listByGroupCalled)
	require.False(t, repo.listByAnyCalled)
	require.Equal(t, []int64{primaryID}, keys[0].GroupIDs)
	require.Len(t, keys[0].Groups, 1)
}

func TestAdminServiceGetGroupAPIKeysFeatureEnabledUsesAnyPriorityQuery(t *testing.T) {
	primaryID := int64(1)
	secondaryID := int64(2)
	repo := &apiKeyRepoStubForGroupUpdate{listKeys: []APIKey{{
		ID:       7,
		GroupID:  &primaryID,
		GroupIDs: []int64{primaryID, secondaryID},
		Groups:   []Group{{ID: primaryID}, {ID: secondaryID}},
	}}}
	settings := &SettingService{}
	settings.apiKeyMultiGroupEnabled.Store(true)
	svc := &adminServiceImpl{apiKeyRepo: repo, settingService: settings}

	keys, total, err := svc.GetGroupAPIKeys(context.Background(), secondaryID, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.False(t, repo.listByGroupCalled)
	require.True(t, repo.listByAnyCalled)
	require.Equal(t, []int64{primaryID, secondaryID}, keys[0].GroupIDs)
	require.Len(t, keys[0].Groups, 2)
}
