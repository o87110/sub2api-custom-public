import { describe, expect, it } from 'vitest'
import {
  areValidUserBanThresholdOverrides,
  cloneUserBanThresholdOverrides,
  normalizedDefaultBanThreshold,
} from '@/custom/moderation/userBanThresholds'

describe('user ban threshold helpers', () => {
  it('validates positive unique users and integer thresholds', () => {
    expect(areValidUserBanThresholdOverrides([
      { user_id: 1, ban_threshold: 20 },
      { user_id: 2, ban_threshold: 1 },
    ])).toBe(true)
    expect(areValidUserBanThresholdOverrides([{ user_id: 0, ban_threshold: 20 }])).toBe(false)
    expect(areValidUserBanThresholdOverrides([{ user_id: 1, ban_threshold: 1001 }])).toBe(false)
    expect(areValidUserBanThresholdOverrides([
      { user_id: 1, ban_threshold: 20 },
      { user_id: 1, ban_threshold: 30 },
    ])).toBe(false)
  })

  it('clones API values and supplies a bounded row default', () => {
    const source = [{ user_id: 1, ban_threshold: 20 }]
    const clone = cloneUserBanThresholdOverrides(source)
    clone[0].ban_threshold = 99
    expect(source[0].ban_threshold).toBe(20)
    expect(cloneUserBanThresholdOverrides(undefined)).toEqual([])
    expect(normalizedDefaultBanThreshold(30)).toBe(30)
    expect(normalizedDefaultBanThreshold(2000)).toBe(1000)
    expect(normalizedDefaultBanThreshold(Number.NaN)).toBe(10)
  })
})
