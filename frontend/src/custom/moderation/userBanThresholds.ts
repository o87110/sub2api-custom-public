import type { UserBanThresholdOverride } from '@/custom/moderation/api'

export const maxUserBanThreshold = 1000

export function normalizedDefaultBanThreshold(value: number): number {
  if (!Number.isInteger(value) || value < 1) return 10
  return Math.min(value, maxUserBanThreshold)
}

export function isValidUserBanThresholdOverride(override: UserBanThresholdOverride): boolean {
  return Number.isInteger(override.user_id)
    && override.user_id > 0
    && Number.isInteger(override.ban_threshold)
    && override.ban_threshold >= 1
    && override.ban_threshold <= maxUserBanThreshold
}

export function areValidUserBanThresholdOverrides(overrides: UserBanThresholdOverride[]): boolean {
  const seen = new Set<number>()
  for (const override of overrides) {
    if (!isValidUserBanThresholdOverride(override) || seen.has(override.user_id)) return false
    seen.add(override.user_id)
  }
  return true
}

export function cloneUserBanThresholdOverrides(value: unknown): UserBanThresholdOverride[] {
  if (!Array.isArray(value)) return []
  return value.map((item) => {
    const candidate = item as Partial<UserBanThresholdOverride>
    return {
      user_id: Number(candidate.user_id),
      ban_threshold: Number(candidate.ban_threshold),
    }
  })
}
