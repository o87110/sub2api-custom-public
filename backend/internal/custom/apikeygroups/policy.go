package apikeygroups

import "errors"

const MaxGroups = 10

var (
	ErrConflictingFields = errors.New("group_id and group_ids cannot be supplied together")
	ErrInvalidSelection  = errors.New("invalid API key group selection")
	ErrMixedPlatforms    = errors.New("API key groups must use one platform")
)

// ResolveSelection applies the public replacement semantics shared by create
// and update requests. A non-nil groupIDs pointer represents the new field;
// groupIDSet preserves an explicitly supplied legacy null.
func ResolveSelection(
	groupIDSet bool,
	groupID *int64,
	groupIDs *[]int64,
) ([]int64, bool, error) {
	legacySet := groupIDSet || groupID != nil
	if groupIDs != nil && legacySet {
		return nil, false, ErrConflictingFields
	}
	if groupIDs != nil {
		return append([]int64(nil), (*groupIDs)...), true, nil
	}
	if legacySet {
		if groupID == nil {
			return []int64{}, true, nil
		}
		return []int64{*groupID}, true, nil
	}
	return nil, false, nil
}

// ValidateOrderedSelection validates list shape and platform consistency while
// allowing the caller to supply domain-specific lookup and eligibility gates.
// Eligibility is evaluated only for newly-added assignments, so reordering or
// removing an existing assignment cannot be blocked by later balance changes.
func ValidateOrderedSelection[T any](
	groupIDs []int64,
	existing map[int64]struct{},
	load func(groupID int64) (value T, platform string, err error),
	validateNew func(groupID int64, value T) error,
) ([]T, error) {
	if len(groupIDs) > MaxGroups {
		return nil, ErrInvalidSelection
	}
	seen := make(map[int64]struct{}, len(groupIDs))
	values := make([]T, 0, len(groupIDs))
	platform := ""
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return nil, ErrInvalidSelection
		}
		if _, duplicate := seen[groupID]; duplicate {
			return nil, ErrInvalidSelection
		}
		seen[groupID] = struct{}{}

		value, candidatePlatform, err := load(groupID)
		if err != nil {
			return nil, err
		}
		if platform == "" {
			platform = candidatePlatform
		} else if platform != candidatePlatform {
			return nil, ErrMixedPlatforms
		}
		if _, alreadyAssigned := existing[groupID]; !alreadyAssigned && validateNew != nil {
			if err := validateNew(groupID, value); err != nil {
				return nil, err
			}
		}
		values = append(values, value)
	}
	return values, nil
}
