package moderation

import (
	"errors"
	"fmt"
	"sort"
)

var ErrAPIAuditScopeEmpty = errors.New("API 审计指定范围至少需要一个分组")

// APIAuditScope narrows API-based moderation within the existing audit scope.
type APIAuditScope struct {
	AllInScope bool    `json:"all_in_scope"`
	GroupIDs   []int64 `json:"group_ids"`
}

// DefaultAPIAuditScope preserves legacy behavior for configurations created
// before the nested API audit scope was introduced.
func DefaultAPIAuditScope() *APIAuditScope {
	return &APIAuditScope{AllInScope: true, GroupIDs: []int64{}}
}

// NormalizeAPIAuditScope returns an isolated, deterministic scope value.
func NormalizeAPIAuditScope(scope *APIAuditScope) *APIAuditScope {
	if scope == nil {
		return DefaultAPIAuditScope()
	}
	result := &APIAuditScope{AllInScope: scope.AllInScope}
	if scope.AllInScope {
		result.GroupIDs = []int64{}
		return result
	}
	seen := make(map[int64]struct{}, len(scope.GroupIDs))
	for _, id := range scope.GroupIDs {
		if id <= 0 {
			continue
		}
		seen[id] = struct{}{}
	}
	result.GroupIDs = make([]int64, 0, len(seen))
	for id := range seen {
		result.GroupIDs = append(result.GroupIDs, id)
	}
	sort.Slice(result.GroupIDs, func(i, j int) bool { return result.GroupIDs[i] < result.GroupIDs[j] })
	return result
}

// Includes reports whether a group selected by the parent audit scope remains
// eligible for API moderation.
func (scope *APIAuditScope) Includes(groupID *int64) bool {
	normalized := NormalizeAPIAuditScope(scope)
	if normalized.AllInScope {
		return true
	}
	if groupID == nil {
		return false
	}
	index := sort.Search(len(normalized.GroupIDs), func(i int) bool {
		return normalized.GroupIDs[i] >= *groupID
	})
	return index < len(normalized.GroupIDs) && normalized.GroupIDs[index] == *groupID
}

// ValidateAPIAuditScope enforces that an explicit API scope is a non-empty
// subset of the existing audit scope whenever API moderation is active.
func ValidateAPIAuditScope(
	parentAllGroups bool,
	parentGroupIDs []int64,
	scope *APIAuditScope,
	requireNonEmpty bool,
) error {
	normalized := NormalizeAPIAuditScope(scope)
	parentIDs := make(map[int64]struct{}, len(parentGroupIDs))
	for _, id := range parentGroupIDs {
		if id > 0 {
			parentIDs[id] = struct{}{}
		}
	}
	if normalized.AllInScope {
		return nil
	}
	if requireNonEmpty && len(normalized.GroupIDs) == 0 {
		return ErrAPIAuditScopeEmpty
	}
	if parentAllGroups {
		return nil
	}
	for _, id := range normalized.GroupIDs {
		if _, ok := parentIDs[id]; !ok {
			return fmt.Errorf("API 审计分组 %d 不在总审计范围内", id)
		}
	}
	return nil
}
