package apikeygroups

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveSelection(t *testing.T) {
	legacyID := int64(7)
	groupIDs := []int64{7, 8}

	_, _, err := ResolveSelection(true, &legacyID, &groupIDs)
	require.ErrorIs(t, err, ErrConflictingFields)

	resolved, present, err := ResolveSelection(true, nil, nil)
	require.NoError(t, err)
	require.True(t, present)
	require.Empty(t, resolved)

	resolved, present, err = ResolveSelection(false, nil, &groupIDs)
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, groupIDs, resolved)
	groupIDs[0] = 99
	require.Equal(t, int64(7), resolved[0], "解析结果必须与请求切片隔离")
}

func TestValidateOrderedSelection(t *testing.T) {
	type candidate struct {
		platform string
	}
	candidates := map[int64]candidate{
		1: {platform: "openai"},
		2: {platform: "openai"},
		3: {platform: "anthropic"},
	}
	load := func(groupID int64) (candidate, string, error) {
		value, ok := candidates[groupID]
		if !ok {
			return candidate{}, "", errors.New("missing group")
		}
		return value, value.platform, nil
	}
	var validated []int64
	validateNew := func(groupID int64, _ candidate) error {
		validated = append(validated, groupID)
		return nil
	}

	values, err := ValidateOrderedSelection(
		[]int64{2, 1},
		map[int64]struct{}{1: {}},
		load,
		validateNew,
	)
	require.NoError(t, err)
	require.Len(t, values, 2)
	require.Equal(t, []int64{2}, validated)

	_, err = ValidateOrderedSelection([]int64{1, 1}, nil, load, validateNew)
	require.ErrorIs(t, err, ErrInvalidSelection)
	_, err = ValidateOrderedSelection([]int64{1, 3}, nil, load, validateNew)
	require.ErrorIs(t, err, ErrMixedPlatforms)
	_, err = ValidateOrderedSelection([]int64{0}, nil, load, validateNew)
	require.ErrorIs(t, err, ErrInvalidSelection)
}
