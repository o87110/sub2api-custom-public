package moderation

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type groupExistenceReaderStub struct {
	existing map[int64]bool
	err      error
	calls    map[int64]int
}

func (s *groupExistenceReaderStub) Exists(_ context.Context, groupID int64) (bool, error) {
	if s.calls == nil {
		s.calls = make(map[int64]int)
	}
	s.calls[groupID]++
	if s.err != nil {
		return false, s.err
	}
	return s.existing[groupID], nil
}

func TestReconcileDeletedAuditGroupsDropsOnlyPersistedMissingIDs(t *testing.T) {
	reader := &groupExistenceReaderStub{existing: map[int64]bool{7: true}}

	result, err := ReconcileDeletedAuditGroups(
		context.Background(),
		AuditGroupSelection{OverallGroupIDs: []int64{7, 71}, APIGroupIDs: []int64{7, 71}},
		AuditGroupSelection{OverallGroupIDs: []int64{7, 71}, APIGroupIDs: []int64{7, 71}},
		reader,
	)

	require.NoError(t, err)
	require.Equal(t, []int64{7}, result.OverallGroupIDs)
	require.Equal(t, []int64{7}, result.APIGroupIDs)
	require.Equal(t, 1, reader.calls[7])
	require.Equal(t, 1, reader.calls[71])
}

func TestReconcileDeletedAuditGroupsRejectsNewMissingID(t *testing.T) {
	reader := &groupExistenceReaderStub{existing: map[int64]bool{7: true}}

	_, err := ReconcileDeletedAuditGroups(
		context.Background(),
		AuditGroupSelection{OverallGroupIDs: []int64{7}},
		AuditGroupSelection{OverallGroupIDs: []int64{7, 99}},
		reader,
	)

	var unknown *UnknownAuditGroupError
	require.ErrorAs(t, err, &unknown)
	require.Equal(t, AuditGroupScopeOverall, unknown.Scope)
	require.EqualValues(t, 99, unknown.GroupID)
}

func TestReconcileDeletedAuditGroupsPreservesLookupFailure(t *testing.T) {
	lookupErr := errors.New("database unavailable")
	reader := &groupExistenceReaderStub{err: lookupErr}

	_, err := ReconcileDeletedAuditGroups(
		context.Background(),
		AuditGroupSelection{OverallGroupIDs: []int64{7}},
		AuditGroupSelection{OverallGroupIDs: []int64{7}},
		reader,
	)

	require.ErrorIs(t, err, lookupErr)
}
