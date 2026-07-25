import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import PaymentChannelSelector from './PaymentChannelSelector.vue'
import type { PaymentChannelOption } from './paymentChannels'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string) => key,
  }),
}))

const channels: PaymentChannelOption[] = [
  {
    id: 'easypay_alipay',
    payment_type: 'alipay',
    provider_key: 'easypay',
    currency: 'CNY',
    fee_rate: 0,
    daily_limit: 0,
    single_min: 0,
    single_max: 0,
    available: true,
  },
  {
    id: 'official_alipay',
    payment_type: 'alipay',
    provider_key: 'alipay',
    currency: 'CNY',
    fee_rate: 0,
    daily_limit: 0,
    single_min: 100,
    single_max: 0,
    available: false,
  },
]

describe('PaymentChannelSelector', () => {
  it('exposes pressed and disabled state and only selects available channels', async () => {
    const wrapper = mount(PaymentChannelSelector, {
      props: {
        methods: channels,
        selected: 'easypay_alipay',
      },
    })
    const buttons = wrapper.findAll('button')
    const group = wrapper.get('[role="group"]')

    expect(buttons[0].attributes('aria-pressed')).toBe('true')
    expect(buttons[0].attributes('aria-label')).toBe('payment.channels.easypayAlipay')
    expect(buttons[1].attributes()).toHaveProperty('disabled')
    expect(group.classes()).toEqual(expect.arrayContaining([
      'grid-cols-1',
      'min-[375px]:grid-cols-2',
      'sm:flex',
    ]))
    expect(buttons[0].classes()).toEqual(expect.arrayContaining([
      'min-h-[64px]',
      'focus-visible:ring-2',
      'dark:focus-visible:ring-offset-dark-900',
    ]))
    expect(buttons[1].classes()).toEqual(expect.arrayContaining([
      'cursor-not-allowed',
      'dark:bg-dark-800/50',
    ]))

    await buttons[0].trigger('click')
    await buttons[1].trigger('click')
    expect(wrapper.emitted('select')).toEqual([['easypay_alipay']])
  })
})
