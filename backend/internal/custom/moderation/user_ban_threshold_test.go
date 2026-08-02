package moderation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateUserBanThresholdOverrides(t *testing.T) {
	require.NoError(t, ValidateUserBanThresholdOverrides(nil))
	require.NoError(t, ValidateUserBanThresholdOverrides([]UserBanThresholdOverride{
		{UserID: 11, BanThreshold: 20},
		{UserID: 12, BanThreshold: 1},
	}))
	require.EqualError(t, ValidateUserBanThresholdOverrides([]UserBanThresholdOverride{
		{UserID: 0, BanThreshold: 20},
	}), "user_ban_thresholds[0].user_id 必须为正整数")
	require.EqualError(t, ValidateUserBanThresholdOverrides([]UserBanThresholdOverride{
		{UserID: 11, BanThreshold: MaxUserBanThreshold + 1},
	}), "user_ban_thresholds[0].ban_threshold 必须在 1-1000 之间")
	require.EqualError(t, ValidateUserBanThresholdOverrides([]UserBanThresholdOverride{
		{UserID: 11, BanThreshold: 20},
		{UserID: 11, BanThreshold: 30},
	}), "user_ban_thresholds[1].user_id 与用户 11 重复")
}

func TestEffectiveBanThresholdUsesExactUserOverride(t *testing.T) {
	overrides := []UserBanThresholdOverride{
		{UserID: 11, BanThreshold: 20},
		{UserID: 12, BanThreshold: 30},
	}
	require.Equal(t, 20, EffectiveBanThreshold(10, overrides, 11))
	require.Equal(t, 30, EffectiveBanThreshold(50, overrides, 12), "用户值应完全覆盖后续变化的全局值")
	require.Equal(t, 10, EffectiveBanThreshold(10, overrides, 13))
	require.Equal(t, 10, EffectiveBanThreshold(10, overrides, 0))
	require.Equal(t, 10, EffectiveBanThreshold(10, []UserBanThresholdOverride{
		{UserID: 11, BanThreshold: MaxUserBanThreshold + 1},
	}, 11), "绕过 API 写入的非法配置必须回退到全局阈值")
}

func TestCloneUserBanThresholdOverridesIsIsolatedAndNonNil(t *testing.T) {
	require.NotNil(t, CloneUserBanThresholdOverrides(nil))

	original := []UserBanThresholdOverride{{UserID: 11, BanThreshold: 20}}
	clone := CloneUserBanThresholdOverrides(original)
	clone[0].BanThreshold = 99
	require.Equal(t, 20, original[0].BanThreshold)
}
