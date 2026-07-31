import { computed, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import type { CreateOrderResult } from '@/types/payment'
import type { PaymentRecoverySnapshot } from '@/components/payment/paymentFlow'
import type { PaymentChannelOption } from './paymentChannels'
import {
  buildWechatOAuthAuthorizeUrl,
  shouldFallbackToDesktopQr,
  usePaymentChannelRecovery,
} from './usePaymentChannelRecovery'

function emptyRecovery(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    paymentChannelId: '',
    providerKey: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

function channel(id: string, providerKey: string): PaymentChannelOption {
  return {
    id,
    payment_type: 'wxpay',
    provider_key: providerKey,
    fee_rate: 0,
    daily_limit: 0,
    single_min: 0,
    single_max: 0,
    available: true,
  }
}

describe('payment channel recovery policy', () => {
  it('allows one same-channel mobile-to-QR fallback but no repeated or cross-channel retry', () => {
    expect(shouldFallbackToDesktopQr(
      { reason: 'WECHAT_H5_NOT_AUTHORIZED' }, 'wxpay', false, true,
    )).toBe(true)
    expect(shouldFallbackToDesktopQr(
      { reason: 'WECHAT_H5_NOT_AUTHORIZED' }, 'wxpay', true, true,
    )).toBe(false)
    expect(shouldFallbackToDesktopQr(
      { reason: 'PAYMENT_GATEWAY_ERROR' }, 'stripe', false, true,
    )).toBe(false)
  })

  it('keeps provider_key when creating the desktop QR order and only suggests a backup', async () => {
    const channels = computed(() => [channel('official', 'wxpay'), channel('backup', 'easypay')])
    const createPaymentOrder = vi.fn(async (): Promise<CreateOrderResult> => ({
      order_id: 778,
      amount: 88,
      pay_amount: 88,
      fee_rate: 0,
      expires_at: '2099-01-01T00:10:00.000Z',
      payment_type: 'wxpay',
      provider_key: 'wxpay',
      qr_code: 'weixin://wxpay/fallback',
      out_trade_no: 'sub2_qr_778',
    }))
    const paymentState = ref(emptyRecovery())
    const paymentPhase = ref<'select' | 'paying'>('select')
    const recovery = usePaymentChannelRecovery({
      channelOptions: channels,
      selectedChannelId: ref('official'),
      amount: ref(88),
      selectedPlan: ref(null),
      paymentState,
      paymentPhase,
      errorMessage: ref(''),
      errorHintMessage: ref(''),
      balanceAmountFitsChannel: () => true,
      subscriptionAmountFitsChannel: () => true,
      createPaymentOrder,
      submitOrder: vi.fn(),
      resolveRoute: () => '/payment/stripe',
      replaceRoute: vi.fn(),
      persistRecoverySnapshot: vi.fn(),
      isMobile: () => true,
      origin: () => 'https://example.com',
      showWarning: vi.fn(),
      t: (key, params) => params && typeof params === 'object'
        ? `${key}:${String(params.channel || '')}`
        : key,
    })

    expect(await recovery.attemptMobileQrFallback(
      { reason: 'WECHAT_H5_NOT_AUTHORIZED' },
      {
        orderAmount: 88,
        orderType: 'balance',
        paymentType: 'wxpay',
        providerKey: 'wxpay',
        paymentChannelId: 'official',
        attempted: false,
      },
    )).toBe(true)
    expect(createPaymentOrder).toHaveBeenCalledTimes(1)
    expect(createPaymentOrder).toHaveBeenCalledWith(expect.objectContaining({
      payment_type: 'wxpay',
      provider_key: 'wxpay',
      is_mobile: false,
    }))
    expect(paymentPhase.value).toBe('paying')

    expect(recovery.appendBackupChannelHint(
      { reason: 'PAYMENT_GATEWAY_ERROR' },
      '',
      'wxpay',
      { attemptedChannelId: 'official', orderType: 'balance', orderAmount: 88 },
    )).toContain('payment.errors.switchChannelHint')
    expect(createPaymentOrder).toHaveBeenCalledTimes(1)
  })

  it('preserves payment context in the WeChat OAuth redirect', () => {
    const url = buildWechatOAuthAuthorizeUrl(
      'https://pay.example.com/oauth?redirect=/purchase',
      {
        paymentType: 'wxpay_direct',
        providerKey: 'WXPAY',
        orderType: 'subscription',
        planId: 7,
        orderAmount: 12.5,
      },
      'https://app.example.com',
    )
    const parsed = new URL(url)
    expect(parsed.searchParams.get('provider_key')).toBe('wxpay')
    expect(parsed.searchParams.get('redirect')).toContain('payment_type=wxpay')
    expect(parsed.searchParams.get('redirect')).toContain('provider_key=wxpay')
  })
})
