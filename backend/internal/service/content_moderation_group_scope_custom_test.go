//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

type contentModerationGroupRepositoryStub struct {
	GroupRepository
	existing map[int64]bool
}

func (s *contentModerationGroupRepositoryStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	if !s.existing[id] {
		return nil, ErrGroupNotFound
	}
	return &Group{ID: id, Status: StatusActive}, nil
}

func TestCustomContentModerationUpdatePrunesDeletedPersistedGroups(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AllGroups = false
	cfg.GroupIDs = []int64{7, 71}
	cfg.APIAuditScope = &APIAuditScope{AllInScope: false, GroupIDs: []int64{7, 71}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw),
	}}
	groups := &contentModerationGroupRepositoryStub{existing: map[int64]bool{7: true}}
	svc := NewContentModerationService(settings, nil, nil, groups, nil, nil, nil, nil)
	message := "updated"

	view, err := svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{BlockMessage: &message})

	require.NoError(t, err)
	require.Equal(t, []int64{7}, view.GroupIDs)
	require.Equal(t, []int64{7}, view.APIAuditScope.GroupIDs)
	var saved ContentModerationConfig
	require.NoError(t, json.Unmarshal([]byte(settings.values[SettingKeyContentModerationConfig]), &saved))
	require.Equal(t, []int64{7}, saved.GroupIDs)
	require.Equal(t, []int64{7}, saved.APIAuditScope.GroupIDs)
}

func TestCustomContentModerationUpdateRejectsNewMissingGroup(t *testing.T) {
	cfg := defaultContentModerationConfig()
	cfg.AllGroups = false
	cfg.GroupIDs = []int64{7}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings := &contentModerationTestSettingRepo{values: map[string]string{
		SettingKeyContentModerationConfig: string(raw),
	}}
	groups := &contentModerationGroupRepositoryStub{existing: map[int64]bool{7: true}}
	svc := NewContentModerationService(settings, nil, nil, groups, nil, nil, nil, nil)
	groupIDs := []int64{7, 99}

	_, err = svc.UpdateConfig(context.Background(), UpdateContentModerationConfigInput{GroupIDs: &groupIDs})

	require.ErrorContains(t, err, "审计分组不存在: 99")
}
