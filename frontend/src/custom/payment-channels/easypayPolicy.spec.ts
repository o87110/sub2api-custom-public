import { describe, expect, it } from 'vitest'
import { classifyEasyPayCustomType } from './easypayPolicy'

describe('classifyEasyPayCustomType', () => {
  it.each([
    'alipay',
    'wxpay',
    'stripe',
    'card',
    'link',
  ])('rejects exact reserved type %s', type => {
    expect(classifyEasyPayCustomType(type)).toBe('exact_reserved')
  })

  it.each(['alipay_hk', 'wxpay_custom'])('rejects reserved prefix type %s', type => {
    expect(classifyEasyPayCustomType(type)).toBe('prefix_reserved')
  })

  it.each(['easypay', 'airwallex', 'usdt_trc20', 'ldc'])('allows non-conflicting type %s', type => {
    expect(classifyEasyPayCustomType(type)).toBe('allowed')
  })

  it('normalizes whitespace and case for policy checks', () => {
    expect(classifyEasyPayCustomType(' STRIPE ')).toBe('exact_reserved')
  })
})
