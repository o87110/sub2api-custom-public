package moderation

import "fmt"

const MaxUserBanThreshold = 1000

// UserBanThresholdOverride assigns an absolute automatic-ban threshold to one user.
type UserBanThresholdOverride struct {
	UserID       int64 `json:"user_id"`
	BanThreshold int   `json:"ban_threshold"`
}

// CloneUserBanThresholdOverrides returns an isolated copy that is safe to retain in a runtime snapshot.
func CloneUserBanThresholdOverrides(overrides []UserBanThresholdOverride) []UserBanThresholdOverride {
	if len(overrides) == 0 {
		return []UserBanThresholdOverride{}
	}
	return append([]UserBanThresholdOverride(nil), overrides...)
}

// ValidateUserBanThresholdOverrides validates the persisted API shape without depending on user lifecycle state.
func ValidateUserBanThresholdOverrides(overrides []UserBanThresholdOverride) error {
	seen := make(map[int64]struct{}, len(overrides))
	for index, override := range overrides {
		if override.UserID <= 0 {
			return fmt.Errorf("user_ban_thresholds[%d].user_id 必须为正整数", index)
		}
		if override.BanThreshold < 1 || override.BanThreshold > MaxUserBanThreshold {
			return fmt.Errorf("user_ban_thresholds[%d].ban_threshold 必须在 1-%d 之间", index, MaxUserBanThreshold)
		}
		if _, exists := seen[override.UserID]; exists {
			return fmt.Errorf("user_ban_thresholds[%d].user_id 与用户 %d 重复", index, override.UserID)
		}
		seen[override.UserID] = struct{}{}
	}
	return nil
}

// EffectiveBanThreshold resolves an exact per-user override and otherwise returns the global threshold.
func EffectiveBanThreshold(globalThreshold int, overrides []UserBanThresholdOverride, userID int64) int {
	if userID <= 0 || ValidateUserBanThresholdOverrides(overrides) != nil {
		return globalThreshold
	}
	for _, override := range overrides {
		if override.UserID == userID && override.BanThreshold > 0 {
			return override.BanThreshold
		}
	}
	return globalThreshold
}
