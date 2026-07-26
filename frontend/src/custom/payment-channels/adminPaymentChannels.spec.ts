import { describe, expect, it } from 'vitest'
import type { ProviderInstance } from '@/types/payment'
import { aggregateAdminPaymentChannels, stablePaymentChannelID } from './adminPaymentChannels'

function provider(
  id: number,
  providerKey: string,
  supportedTypes: string[],
  overrides: Partial<ProviderInstance> = {},
): ProviderInstance {
  return {
    id,
    provider_key: providerKey,
    name: `${providerKey}-${id}`,
    config: {},
    supported_types: supportedTypes,
    enabled: true,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: id,
    ...overrides,
  }
}

describe('aggregateAdminPaymentChannels', () => {
  it('groups instances by stable user channel and keeps disabled channels visible', () => {
    const channels = aggregateAdminPaymentChannels([
      provider(1, 'alipay', []),
      provider(2, 'easypay', ['alipay', 'wxpay']),
      provider(3, 'easypay', ['alipay'], { enabled: false }),
      provider(4, 'wxpay', [], { enabled: false }),
      provider(5, 'stripe', ['card', 'link']),
      provider(6, 'airwallex', []),
    ])

    expect(channels.map(channel => channel.id)).toEqual([
      'easypay_alipay',
      'official_alipay',
      'easypay_wxpay',
      'official_wxpay',
      'stripe',
      'airwallex',
    ])
    expect(channels.find(channel => channel.id === 'easypay_alipay')).toMatchObject({
      enabled: true,
      instanceCount: 2,
    })
    expect(channels.find(channel => channel.id === 'official_wxpay')).toMatchObject({
      enabled: false,
      instanceCount: 1,
    })
  })

  it('includes EasyPay custom methods and their default display name', () => {
    const channels = aggregateAdminPaymentChannels([
      provider(1, 'easypay', ['ldc'], {
        config: {
          customMethods: '[{"type":"ldc","upstreamType":"epay","displayName":"LDC Pay"}]',
        },
      }),
    ])

    expect(channels).toEqual([{
      id: 'easypay_ldc',
      paymentType: 'ldc',
      providerKey: 'easypay',
      displayName: 'LDC Pay',
      enabled: true,
      instanceCount: 1,
    }])
  })

  it('matches backend stable IDs', () => {
    expect(stablePaymentChannelID('alipay_direct', 'easypay')).toBe('easypay_alipay')
    expect(stablePaymentChannelID('wxpay', 'wxpay')).toBe('official_wxpay')
    expect(stablePaymentChannelID('stripe', 'stripe')).toBe('stripe')
    expect(stablePaymentChannelID('ldc', 'easypay')).toBe('easypay_ldc')
  })
})
