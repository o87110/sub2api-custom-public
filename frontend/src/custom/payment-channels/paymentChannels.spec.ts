import { describe, expect, it } from 'vitest'
import type { CheckoutInfoResponse } from '@/types/payment'
import {
  ALIPAY_MOBILE_PRECREATE_DEEP_LINK,
  findPaymentChannel,
  isGatewayChannelFailureCode,
  normalizePaymentChannelOptions,
  paymentChannelLabel,
} from './paymentChannels'

function checkout(overrides: Partial<CheckoutInfoResponse> = {}): CheckoutInfoResponse {
  return {
    methods: {},
    method_options: [],
    global_min: 0,
    global_max: 0,
    plans: [],
    balance_disabled: false,
    balance_recharge_multiplier: 1,
    subscription_usd_to_cny_rate: 0,
    recharge_fee_rate: 0,
    help_text: '',
    help_image_url: '',
    stripe_publishable_key: '',
    ...overrides,
  }
}

describe('normalizePaymentChannelOptions', () => {
  it('keeps provider channels separate and prioritizes EasyPay', () => {
    const options = normalizePaymentChannelOptions(checkout({
      method_options: [
        {
          id: 'official_wxpay',
          payment_type: 'wxpay',
          provider_key: 'wxpay',
          currency: 'CNY',
          fee_rate: 0,
          daily_limit: 0,
          single_min: 0,
          single_max: 0,
          available: true,
        },
        {
          id: 'easypay_alipay',
          payment_type: 'alipay',
          provider_key: 'easypay',
          currency: 'CNY',
          fee_rate: 0,
          daily_limit: 0,
          single_min: 10,
          single_max: 100,
          available: true,
        },
        {
          id: 'official_alipay',
          payment_type: 'alipay',
          provider_key: 'alipay',
          currency: 'CNY',
          fee_rate: 0,
          daily_limit: 0,
          single_min: 20,
          single_max: 200,
          available: true,
          capabilities: [ALIPAY_MOBILE_PRECREATE_DEEP_LINK],
        },
      ],
    }))

    expect(options.map(option => option.id)).toEqual([
      'easypay_alipay',
      'official_alipay',
      'official_wxpay',
    ])
    expect(findPaymentChannel(options, '', 'alipay', 'alipay')?.id).toBe('official_alipay')
  })

  it('falls back to the legacy single-method interface', () => {
    const options = normalizePaymentChannelOptions(checkout({
      recharge_fee_rate: 2.5,
      methods: {
        alipay: {
          currency: 'CNY',
          daily_limit: 0,
          daily_used: 0,
          daily_remaining: 0,
          single_min: 10,
          single_max: 100,
          fee_rate: 0,
          available: true,
        },
      },
      alipay_mobile_precreate_deep_link: true,
    }))

    expect(options).toHaveLength(1)
    expect(options[0]).toMatchObject({
      id: 'alipay',
      payment_type: 'alipay',
      provider_key: '',
      fee_rate: 2.5,
      legacy: true,
      capabilities: [ALIPAY_MOBILE_PRECREATE_DEEP_LINK],
    })
  })

  it('keeps built-in payment methods ahead of Stripe and Airwallex on legacy backends', () => {
    const method = {
      currency: 'CNY',
      daily_limit: 0,
      daily_used: 0,
      daily_remaining: 0,
      single_min: 0,
      single_max: 0,
      fee_rate: 0,
      available: true,
    }
    const options = normalizePaymentChannelOptions(checkout({
      methods: {
        airwallex: method,
        stripe: method,
        wxpay: method,
        alipay: method,
      },
    }))

    expect(options.map(option => option.id)).toEqual([
      'alipay',
      'wxpay',
      'stripe',
      'airwallex',
    ])
  })

  it('sorts Stripe, Airwallex, and custom methods after the four built-in channels', () => {
    const base = {
      currency: 'CNY',
      fee_rate: 0,
      daily_limit: 0,
      single_min: 0,
      single_max: 0,
      available: true,
    }
    const options = normalizePaymentChannelOptions(checkout({
      method_options: [
        { ...base, id: 'easypay_ldc', payment_type: 'ldc', provider_key: 'easypay' },
        { ...base, id: 'airwallex', payment_type: 'airwallex', provider_key: 'airwallex' },
        { ...base, id: 'official_wxpay', payment_type: 'wxpay', provider_key: 'wxpay' },
        { ...base, id: 'stripe', payment_type: 'stripe', provider_key: 'stripe' },
        { ...base, id: 'easypay_wxpay', payment_type: 'wxpay', provider_key: 'easypay' },
        { ...base, id: 'official_alipay', payment_type: 'alipay', provider_key: 'alipay' },
        { ...base, id: 'easypay_alipay', payment_type: 'alipay', provider_key: 'easypay' },
      ],
    }))

    expect(options.map(option => option.id)).toEqual([
      'easypay_alipay',
      'official_alipay',
      'easypay_wxpay',
      'official_wxpay',
      'stripe',
      'airwallex',
      'easypay_ldc',
    ])
  })

  it('preserves disjoint amount ranges from the provider group', () => {
    const options = normalizePaymentChannelOptions(checkout({
      method_options: [{
        id: 'easypay_alipay',
        payment_type: 'alipay',
        provider_key: 'easypay',
        currency: 'CNY',
        fee_rate: 0,
        daily_limit: 0,
        single_min: 1,
        single_max: 100,
        amount_ranges: [
          { single_min: 1, single_max: 10 },
          { single_min: 20, single_max: 100 },
        ],
        available: true,
      }],
    }))

    expect(options[0].amount_ranges).toEqual([
      { single_min: 1, single_max: 10 },
      { single_min: 20, single_max: 100 },
    ])
  })
})

describe('isGatewayChannelFailureCode', () => {
  it.each([
    'PAYMENT_METHOD_CURRENCY_CONFLICT',
    'WXPAY_CONFIG_INVALID_KEY',
    'WECHAT_PAYMENT_MP_NOT_CONFIGURED',
    'WECHAT_H5_NOT_AUTHORIZED',
  ])('treats %s as a switchable channel failure', (code) => {
    expect(isGatewayChannelFailureCode(code)).toBe(true)
  })

  it('does not classify an invalid provider selection as a transient channel failure', () => {
    expect(isGatewayChannelFailureCode('INVALID_PAYMENT_PROVIDER_SELECTION')).toBe(false)
  })
})

describe('paymentChannelLabel', () => {
  it('prefers the backend custom display name over fixed localized labels', () => {
    expect(paymentChannelLabel({
      id: 'official_alipay',
      payment_type: 'alipay',
      provider_key: 'alipay',
      display_name: '支付宝备用通道',
      fee_rate: 0,
      daily_limit: 0,
      single_min: 0,
      single_max: 0,
      available: true,
    }, key => key)).toBe('支付宝备用通道')
  })
})
