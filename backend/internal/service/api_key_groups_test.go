//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/groupaccess"
	pkgerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type orderedAPIKeyGroupRepoStub struct {
	*apiKeyRepoStub
	updatedGroupIDs []int64
	updateFields    APIKeyUpdateFields
}

func (s *orderedAPIKeyGroupRepoStub) UpdateWithGroups(
	_ context.Context,
	_ *APIKey,
	fields APIKeyUpdateFields,
	groupIDs []int64,
) error {
	s.updatedGroupIDs = append([]int64(nil), groupIDs...)
	s.updateFields = fields
	return nil
}

type orderedAPIKeyGroupLookupStub struct {
	*groupRepoStub
	groups map[int64]*Group
}

func (s *orderedAPIKeyGroupLookupStub) GetByID(_ context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	clone := *group
	return &clone, nil
}

func TestRequestedAPIKeyGroupIDsCompatibilitySemantics(t *testing.T) {
	groupID := int64(7)
	groupIDs := []int64{7, 8}

	_, _, err := requestedAPIKeyGroupIDs(true, &groupID, &groupIDs)
	require.ErrorIs(t, err, ErrAPIKeyGroupFieldsConflict)
	require.Equal(t, "group_ids", pkgerrors.FromError(err).Metadata["field"])

	got, set, err := requestedAPIKeyGroupIDs(true, nil, nil)
	require.NoError(t, err)
	require.True(t, set)
	require.Empty(t, got)

	got, set, err = requestedAPIKeyGroupIDs(false, nil, &groupIDs)
	require.NoError(t, err)
	require.True(t, set)
	require.Equal(t, groupIDs, got)
	groupIDs[0] = 99
	require.Equal(t, int64(7), got[0], "服务层必须复制请求切片")
}

func TestAPIKeyMultiGroupFeatureDefaultsDisabledAndUsesPrimaryOnly(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{
		GroupID:  int64Ptr(1),
		Group:    &Group{ID: 1, Name: "A"},
		GroupIDs: []int64{1, 2},
		Groups:   []Group{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
	}

	require.False(t, svc.APIKeyMultiGroupEnabled())
	svc.applyMultiGroupFeature(key)
	require.Equal(t, []int64{1}, key.GroupIDs)
	require.Len(t, key.Groups, 1)
	require.Equal(t, int64(1), *key.GroupID)
	require.Equal(t, int64(1), key.Group.ID)
}

func TestAPIKeyMultiGroupFeatureCanBeEnabledAndRefreshesImmediately(t *testing.T) {
	settingService := &SettingService{}
	svc := &APIKeyService{settingService: settingService}
	key := &APIKey{
		GroupID:  int64Ptr(1),
		Group:    &Group{ID: 1, Name: "A"},
		GroupIDs: []int64{1, 2},
		Groups:   []Group{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
	}

	settingService.refreshCachedSettings(&SystemSettings{APIKeyMultiGroupEnabled: true})
	require.True(t, svc.APIKeyMultiGroupEnabled())
	svc.applyMultiGroupFeature(key)
	require.Equal(t, []int64{1, 2}, key.GroupIDs)

	settingService.refreshCachedSettings(&SystemSettings{APIKeyMultiGroupEnabled: false})
	require.False(t, svc.APIKeyMultiGroupEnabled())
}

func TestAPIKeyGroupsRequestRejectedWhileFeatureDisabled(t *testing.T) {
	groupIDs := []int64{1, 2}
	svc := NewAPIKeyService(
		&apiKeyRepoStub{},
		&userRepoStub{user: &User{ID: 42}},
		&groupRepoStub{},
		nil,
		nil,
		nil,
		nil,
	)

	_, err := svc.Create(context.Background(), 42, CreateAPIKeyRequest{
		Name:     "disabled",
		GroupIDs: &groupIDs,
	})
	require.ErrorIs(t, err, ErrAPIKeyMultiGroupDisabled)
}

func TestAPIKeyGroupFilterAndResponseFollowGlobalSwitch(t *testing.T) {
	repo := &apiKeyRepoStub{
		allowListByUserID: true,
		listByUserIDKeys: []APIKey{{
			ID:       1,
			GroupID:  int64Ptr(1),
			Group:    &Group{ID: 1},
			GroupIDs: []int64{1, 2},
			Groups:   []Group{{ID: 1}, {ID: 2}},
		}},
	}
	settingService := &SettingService{}
	svc := &APIKeyService{apiKeyRepo: repo, settingService: settingService}
	params := pagination.PaginationParams{Page: 1, PageSize: 20}
	groupID := int64(2)

	keys, _, err := svc.List(
		context.Background(), 42, params, APIKeyListFilters{GroupID: &groupID})
	require.NoError(t, err)
	require.True(t, repo.listByUserIDFilters[0].PrimaryGroupOnly)
	require.Equal(t, []int64{1}, keys[0].GroupIDs)

	settingService.refreshCachedSettings(&SystemSettings{APIKeyMultiGroupEnabled: true})
	keys, _, err = svc.List(
		context.Background(), 42, params, APIKeyListFilters{GroupID: &groupID})
	require.NoError(t, err)
	require.False(t, repo.listByUserIDFilters[1].PrimaryGroupOnly)
	require.Equal(t, []int64{1, 2}, keys[0].GroupIDs)
}

func TestAPIKeyGroupsRejectDuplicateOverLimitAndMixedPlatforms(t *testing.T) {
	groupRepo := &orderedAPIKeyGroupLookupStub{
		groupRepoStub: &groupRepoStub{},
		groups: map[int64]*Group{
			1: {ID: 1, Name: "openai-a", Platform: PlatformOpenAI, Status: StatusActive},
			2: {ID: 2, Name: "anthropic-a", Platform: PlatformAnthropic, Status: StatusActive},
		},
	}
	svc := &APIKeyService{groupRepo: groupRepo}
	user := &User{ID: 9}

	_, err := svc.validateAPIKeyGroups(context.Background(), user, []int64{1, 1}, nil)
	require.ErrorIs(t, err, ErrAPIKeyGroupsInvalid)
	require.Equal(t, "group_ids", pkgerrors.FromError(err).Metadata["field"])

	tooMany := make([]int64, maxAPIKeyGroups+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, err = svc.validateAPIKeyGroups(context.Background(), user, tooMany, nil)
	require.ErrorIs(t, err, ErrAPIKeyGroupsInvalid)

	_, err = svc.validateAPIKeyGroups(context.Background(), user, []int64{1, 2}, nil)
	require.ErrorIs(t, err, ErrAPIKeyGroupsInvalid)

	_, err = svc.validateAPIKeyGroups(context.Background(), user, []int64{99}, nil)
	require.ErrorIs(t, err, ErrGroupNotFound)
	require.Equal(t, "group_ids", pkgerrors.FromError(err).Metadata["field"])
}

func TestAPIKeyGroupsPureReorderSkipsNewBalanceGate(t *testing.T) {
	key := &APIKey{
		ID:       1,
		UserID:   42,
		Key:      "sk-ordered-reorder",
		Name:     "before",
		Status:   StatusActive,
		GroupIDs: []int64{1, 2},
	}
	apiKeyRepo := &orderedAPIKeyGroupRepoStub{apiKeyRepoStub: &apiKeyRepoStub{apiKey: key}}
	groupRepo := &orderedAPIKeyGroupLookupStub{
		groupRepoStub: &groupRepoStub{},
		groups: map[int64]*Group{
			1: {
				ID: 1, Name: "A", Platform: PlatformOpenAI, Status: StatusActive,
				MinimumBalance: 100,
			},
			2: {
				ID: 2, Name: "B", Platform: PlatformOpenAI, Status: StatusActive,
				MinimumBalance: 100,
			},
			3: {
				ID: 3, Name: "C", Platform: PlatformOpenAI, Status: StatusActive,
				MinimumBalance: 100,
			},
		},
	}
	userRepo := &userRepoStub{user: &User{ID: 42, Balance: 1}}
	svc := NewAPIKeyService(apiKeyRepo, userRepo, groupRepo, nil, nil, nil, nil)
	settingService := &SettingService{}
	settingService.apiKeyMultiGroupEnabled.Store(true)
	svc.SetSettingService(settingService)

	reordered := []int64{2, 1}
	updated, err := svc.Update(context.Background(), key.ID, key.UserID, UpdateAPIKeyRequest{
		GroupIDs: &reordered,
	})
	require.NoError(t, err)
	require.Equal(t, reordered, apiKeyRepo.updatedGroupIDs)
	require.Equal(t, reordered, updated.GroupIDs)
	require.Equal(t, int64(2), *updated.GroupID)

	withNewGroup := []int64{2, 1, 3}
	_, err = svc.Update(context.Background(), key.ID, key.UserID, UpdateAPIKeyRequest{
		GroupIDs: &withNewGroup,
	})
	require.Equal(t, groupaccess.MinimumBalanceNotMetReason, pkgerrors.Reason(err))
	require.Equal(t, "group_ids", pkgerrors.FromError(err).Metadata["field"])
}

func TestLegacyGroupIDReplacesMultiGroupListWhenFeatureEnabled(t *testing.T) {
	groupID := int64(1)
	key := &APIKey{
		ID:       1,
		UserID:   42,
		Key:      "sk-legacy-replace",
		Name:     "before",
		Status:   StatusActive,
		GroupID:  &groupID,
		GroupIDs: []int64{1, 2},
	}
	apiKeyRepo := &orderedAPIKeyGroupRepoStub{
		apiKeyRepoStub: &apiKeyRepoStub{apiKey: key},
	}
	groupRepo := &orderedAPIKeyGroupLookupStub{
		groupRepoStub: &groupRepoStub{},
		groups: map[int64]*Group{
			1: {
				ID:             1,
				Name:           "A",
				Platform:       PlatformOpenAI,
				Status:         StatusActive,
				MinimumBalance: 100,
			},
		},
	}
	settingService := &SettingService{}
	settingService.apiKeyMultiGroupEnabled.Store(true)
	svc := NewAPIKeyService(
		apiKeyRepo,
		&userRepoStub{user: &User{ID: 42, Balance: 1}},
		groupRepo,
		nil,
		nil,
		nil,
		nil,
	)
	svc.SetSettingService(settingService)

	updated, err := svc.Update(context.Background(), key.ID, key.UserID, UpdateAPIKeyRequest{
		GroupID:    &groupID,
		GroupIDSet: true,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{groupID}, apiKeyRepo.updatedGroupIDs)
	require.Equal(t, []int64{groupID}, updated.GroupIDs)
}

func TestAPIKeyUpdateResponseUsesPrimaryOnlyWhenFeatureDisabled(t *testing.T) {
	repo := &apiKeyRepoStub{apiKey: &APIKey{
		ID:       1,
		UserID:   42,
		Key:      "sk-test",
		GroupID:  int64Ptr(1),
		Group:    &Group{ID: 1, Name: "A"},
		GroupIDs: []int64{1, 2},
		Groups:   []Group{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
	}}
	svc := &APIKeyService{apiKeyRepo: repo, settingService: &SettingService{}}
	name := "renamed"

	updated, err := svc.Update(
		context.Background(), 1, 42, UpdateAPIKeyRequest{Name: &name})
	require.NoError(t, err)
	require.Equal(t, []int64{1}, updated.GroupIDs)
	require.Len(t, updated.Groups, 1)
	require.Len(t, repo.updatedKeys, 1)
	require.Equal(t, []int64{1, 2}, repo.updatedKeys[0].GroupIDs)
}
