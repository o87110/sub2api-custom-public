import { nextTick, ref } from 'vue'
import { describe, expect, it } from 'vitest'

import type { CheckoutInfoResponse, SubscriptionPlan } from '@/types/payment'
import { usePaymentChannelPricing } from './usePaymentChannelPricing'

function checkout(): CheckoutInfoResponse {
  return {
    methods: {},
    method_options: [
      {
        id: 'limited',
        payment_type: 'alipay',
        provider_key: 'easypay',
        fee_rate: 0,
        daily_limit: 0,
        single_min: 0,
        single_max: 50,
        available: true,
      },
      {
        id: 'fee-channel',
        payment_type: 'alipay',
        provider_key: 'alipay',
        currency: 'CNY',
        fee_rate: 2,
        daily_limit: 0,
        single_min: 0,
        single_max: 102,
        available: true,
      },
    ],
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
  }
}

describe('usePaymentChannelPricing', () => {
  it('reselects an available channel after the recharge amount changes', async () => {
    const checkoutState = ref(checkout())
    const activeTab = ref<'recharge' | 'subscription'>('recharge')
    const amount = ref<number | null>(0)
    const selectedPlan = ref<SubscriptionPlan | null>(null)
    const selectedChannelId = ref('limited')
    const pricing = usePaymentChannelPricing({
      checkout: checkoutState,
      activeTab,
      amount,
      selectedPlan,
      selectedChannelId,
      locale: () => 'zh-CN',
      t: key => key,
    })

    amount.value = 100
    await nextTick()

    expect(selectedChannelId.value).toBe('fee-channel')
    expect(pricing.feeAmount.value).toBe(2)
    expect(pricing.totalAmount.value).toBe(102)
    expect(pricing.globalMaxAmount.value).toBe(100)
  })

  it('keeps recharge and subscription reselection isolated by active tab', async () => {
    const checkoutState = ref(checkout())
    const activeTab = ref<'recharge' | 'subscription'>('subscription')
    const amount = ref<number | null>(100)
    const selectedPlan = ref<SubscriptionPlan | null>({ id: 7, price: 20 } as SubscriptionPlan)
    const selectedChannelId = ref('limited')
    const pricing = usePaymentChannelPricing({
      checkout: checkoutState,
      activeTab,
      amount,
      selectedPlan,
      selectedChannelId,
      locale: () => 'zh-CN',
      t: key => key,
    })

    await nextTick()
    expect(selectedChannelId.value).toBe('limited')
    expect(pricing.canSubmitSubscription.value).toBe(true)
  })
})
