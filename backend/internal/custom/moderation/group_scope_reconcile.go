package moderation

import (
	"context"
	"fmt"
)

const (
	AuditGroupScopeOverall = "overall"
	AuditGroupScopeAPI     = "api"
)

// AuditGroupSelection contains the persisted group IDs for both moderation scopes.
type AuditGroupSelection struct {
	OverallGroupIDs []int64
	APIGroupIDs     []int64
}

// GroupExistenceReader isolates reconciliation from the concrete group repository.
type GroupExistenceReader interface {
	Exists(ctx context.Context, groupID int64) (bool, error)
}

// UnknownAuditGroupError identifies a newly submitted group that never existed
// in the previously persisted scope.
type UnknownAuditGroupError struct {
	Scope   string
	GroupID int64
}

func (e *UnknownAuditGroupError) Error() string {
	return fmt.Sprintf("unknown %s audit group: %d", e.Scope, e.GroupID)
}

// ReconcileDeletedAuditGroups removes only missing IDs inherited from the
// persisted configuration. Newly submitted missing IDs remain validation errors.
func ReconcileDeletedAuditGroups(
	ctx context.Context,
	persisted AuditGroupSelection,
	candidate AuditGroupSelection,
	reader GroupExistenceReader,
) (AuditGroupSelection, error) {
	if reader == nil {
		return cloneAuditGroupSelection(candidate), nil
	}

	existence := make(map[int64]bool)
	checked := make(map[int64]struct{})
	lookup := func(groupID int64) (bool, error) {
		if _, ok := checked[groupID]; ok {
			return existence[groupID], nil
		}
		exists, err := reader.Exists(ctx, groupID)
		if err != nil {
			return false, fmt.Errorf("lookup audit group %d: %w", groupID, err)
		}
		checked[groupID] = struct{}{}
		existence[groupID] = exists
		return exists, nil
	}

	overall, err := reconcileAuditGroupIDs(
		candidate.OverallGroupIDs,
		persisted.OverallGroupIDs,
		AuditGroupScopeOverall,
		lookup,
	)
	if err != nil {
		return AuditGroupSelection{}, err
	}
	api, err := reconcileAuditGroupIDs(
		candidate.APIGroupIDs,
		persisted.APIGroupIDs,
		AuditGroupScopeAPI,
		lookup,
	)
	if err != nil {
		return AuditGroupSelection{}, err
	}
	return AuditGroupSelection{OverallGroupIDs: overall, APIGroupIDs: api}, nil
}

func reconcileAuditGroupIDs(
	candidate []int64,
	persisted []int64,
	scope string,
	lookup func(groupID int64) (bool, error),
) ([]int64, error) {
	persistedIDs := make(map[int64]struct{}, len(persisted))
	for _, groupID := range persisted {
		persistedIDs[groupID] = struct{}{}
	}

	result := make([]int64, 0, len(candidate))
	for _, groupID := range candidate {
		exists, err := lookup(groupID)
		if err != nil {
			return nil, err
		}
		if exists {
			result = append(result, groupID)
			continue
		}
		if _, inherited := persistedIDs[groupID]; inherited {
			continue
		}
		return nil, &UnknownAuditGroupError{Scope: scope, GroupID: groupID}
	}
	return result, nil
}

func cloneAuditGroupSelection(selection AuditGroupSelection) AuditGroupSelection {
	return AuditGroupSelection{
		OverallGroupIDs: append([]int64(nil), selection.OverallGroupIDs...),
		APIGroupIDs:     append([]int64(nil), selection.APIGroupIDs...),
	}
}
