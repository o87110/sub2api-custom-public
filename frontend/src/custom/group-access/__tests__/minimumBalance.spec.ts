import { describe, expect, it } from 'vitest'
import type { Group } from '@/types'
import {
  formatGroupBalance,
  groupBalanceRequirement,
  minimumBalanceErrorToast
} from '../minimumBalance'

function group(overrides: Partial<Group> = {}): Group {
  return {
    id: 7,
    name: '分组 A',
    description: null,
    platform: 'anthropic',
    rate_multiplier: 1,
    minimum_balance: 0,
    peak_rate_enabled: false,
    peak_start: '',
    peak_end: '',
    peak_rate_multiplier: 1,
    is_exclusive: false,
    status: 'active',
    subscription_type: 'standard',
    ...overrides
  } as Group
}

describe('minimum balance helpers', () => {
  it('does not expose a warning when the gate is disabled or eligible', () => {
    expect(groupBalanceRequirement(group())).toBeNull()
    expect(
      groupBalanceRequirement(
        group({
          minimum_balance: 100,
          current_balance: 120,
          balance_eligible: true
        })
      )
    ).toBeNull()
  })

  it('builds the unavailable requirement returned by the server', () => {
    expect(
      groupBalanceRequirement(
        group({
          minimum_balance: 100,
          current_balance: 80,
          usable_balance: 0,
          balance_gap: 20,
          balance_eligible: false
        })
      )
    ).toEqual({
      groupName: '分组 A',
      minimumBalance: 100,
      currentBalance: 80,
      usableBalance: 0,
      balanceGap: 20,
      eligible: false
    })
  })

  it('uses two decimals normally and preserves sub-cent differences', () => {
    expect(formatGroupBalance(100.126)).toBe('$100.13')
    expect(formatGroupBalance(0.00000001)).toBe('$0.00000001')
  })

  it('maps below-threshold and equal-threshold API failures to Chinese toast text', () => {
    const below = {
      reason: 'GROUP_MINIMUM_BALANCE_NOT_MET',
      metadata: {
        group_name: '分组 A',
        current_balance: '80',
        minimum_balance: '100',
        balance_gap: '20'
      }
    }
    expect(minimumBalanceErrorToast(below, 'zh-CN')).toBe(
      '无法使用“分组 A”：当前余额 $80.00，余额需高于 $100.00，还需增加超过 $20.00。'
    )

    const rawAxiosBelow = {
      response: {
        data: {
          reason: 'GROUP_MINIMUM_BALANCE_NOT_MET',
          metadata: {
            group_name: '分组 A',
            current_balance: '80',
            minimum_balance: '100',
            balance_gap: '20'
          }
        }
      }
    }
    expect(minimumBalanceErrorToast(rawAxiosBelow, 'zh-CN')).toBe(
      '无法使用“分组 A”：当前余额 $80.00，余额需高于 $100.00，还需增加超过 $20.00。'
    )

    const equal = {
      response: {
        data: {
          code: 'GROUP_MINIMUM_BALANCE_NOT_MET',
          metadata: {
            group_name: '分组 A',
            current_balance: '100',
            minimum_balance: '100',
            balance_gap: '0'
          }
        }
      }
    }
    expect(minimumBalanceErrorToast(equal, 'zh-CN')).toBe(
      '无法使用“分组 A”：当前余额刚好为 $100.00，必须高于 $100.00；余额增加后将自动恢复。'
    )
  })
})
