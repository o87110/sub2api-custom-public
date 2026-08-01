package moderation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeAPIAuditScopeDefaultsAndCanonicalizes(t *testing.T) {
	require.Equal(t, &APIAuditScope{AllInScope: true, GroupIDs: []int64{}}, NormalizeAPIAuditScope(nil))
	require.Equal(t, &APIAuditScope{AllInScope: true, GroupIDs: []int64{}}, NormalizeAPIAuditScope(&APIAuditScope{
		AllInScope: true,
		GroupIDs:   []int64{3, 2},
	}))
	require.Equal(t, &APIAuditScope{GroupIDs: []int64{2, 3}}, NormalizeAPIAuditScope(&APIAuditScope{
		GroupIDs: []int64{3, 0, 2, 3, -1},
	}))
}

func TestAPIAuditScopeIncludes(t *testing.T) {
	groupID := int64(3)
	require.True(t, DefaultAPIAuditScope().Includes(nil))
	require.True(t, (&APIAuditScope{GroupIDs: []int64{3, 5}}).Includes(&groupID))
	require.False(t, (&APIAuditScope{GroupIDs: []int64{5}}).Includes(&groupID))
	require.False(t, (&APIAuditScope{GroupIDs: []int64{3}}).Includes(nil))
}

func TestValidateAPIAuditScopeRequiresNonEmptySubset(t *testing.T) {
	require.NoError(t, ValidateAPIAuditScope(false, nil, DefaultAPIAuditScope(), true))
	require.ErrorIs(t, ValidateAPIAuditScope(true, nil, &APIAuditScope{}, true), ErrAPIAuditScopeEmpty)
	require.EqualError(t, ValidateAPIAuditScope(false, []int64{2}, &APIAuditScope{GroupIDs: []int64{3}}, true), "API 审计分组 3 不在总审计范围内")
	require.NoError(t, ValidateAPIAuditScope(false, []int64{2, 3}, &APIAuditScope{GroupIDs: []int64{3}}, true))
	require.NoError(t, ValidateAPIAuditScope(false, []int64{2}, &APIAuditScope{}, false))
}
