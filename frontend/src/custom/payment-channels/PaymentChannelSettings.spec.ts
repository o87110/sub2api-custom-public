import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import type { ProviderInstance } from '@/types/payment'
import PaymentChannelSettings from './PaymentChannelSettings.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('zh-CN'),
    t: (key: string) => ({
      'payment.channels.easypayAlipay': '易支付渠道支付宝',
      'payment.channels.officialAlipay': '官方渠道支付宝',
    }[key] || key),
  }),
}))

function provider(
  id: number,
  providerKey: string,
  enabled: boolean,
): ProviderInstance {
  return {
    id,
    provider_key: providerKey,
    name: `${providerKey}-${id}`,
    config: {},
    supported_types: ['alipay'],
    enabled,
    payment_mode: '',
    refund_enabled: false,
    allow_user_refund: false,
    limits: '',
    sort_order: id,
  }
}

describe('PaymentChannelSettings', () => {
  it('shows enabled and disabled channels and preserves explicit zero fees', async () => {
    const wrapper = mount(PaymentChannelSettings, {
      props: {
        modelValue: {
          easypay_alipay: { fee_rate: 0 },
        },
        providers: [
          provider(1, 'easypay', true),
          provider(2, 'alipay', false),
        ],
        defaultFeeRate: 2.5,
      },
    })

    expect(wrapper.get('[data-test="channel-setting-easypay_alipay"]').text()).toContain('当前可用')
    expect(wrapper.get('[data-test="channel-setting-official_alipay"]').text()).toContain('已禁用')
    expect(wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]').element).toHaveProperty('value', '0')
    expect(wrapper.get('[data-test="channel-setting-easypay_alipay"]').text()).toContain('免手续费')
  })

  it('emits trimmed display names and inherited or custom fee settings', async () => {
    const wrapper = mount(PaymentChannelSettings, {
      props: {
        modelValue: {
          future_channel: { display_name: '保留配置' },
        },
        providers: [provider(1, 'easypay', true)],
        defaultFeeRate: 2.5,
      },
    })

    await wrapper.get('[data-test="channel-display-name-easypay_alipay"]').setValue(' 支付宝优惠通道 ')
    await wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]').setValue('1.5')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      future_channel: { display_name: '保留配置' },
      easypay_alipay: {
        display_name: '支付宝优惠通道',
        fee_rate: 1.5,
      },
    })

    await wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]').setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      future_channel: { display_name: '保留配置' },
      easypay_alipay: {
        display_name: '支付宝优惠通道',
      },
    })
  })

  it('does not collapse an unfinished decimal fee during parent model updates', async () => {
    const wrapper = mount(PaymentChannelSettings, {
      props: {
        modelValue: {},
        providers: [provider(1, 'easypay', true)],
        defaultFeeRate: 2.5,
      },
    })
    const input = wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]')

    await input.setValue('1')
    const firstUpdate = wrapper.emitted('update:modelValue')?.at(-1)?.[0]
    await wrapper.setProps({ modelValue: firstUpdate })

    await input.setValue('1.')
    expect(input.element).toHaveProperty('value', '1.')
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)

    await input.setValue('1.5')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      easypay_alipay: { fee_rate: 1.5 },
    })
  })

  it('reports field errors and exposes validation for the settings save bridge', async () => {
    const wrapper = mount(PaymentChannelSettings, {
      attachTo: document.body,
      props: {
        modelValue: {},
        providers: [provider(1, 'easypay', true)],
        defaultFeeRate: 2.5,
      },
    })

    await wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]').setValue('1.001')
    expect(wrapper.text()).toContain('请输入 0–100，最多两位小数')
    expect((wrapper.vm as unknown as { validate: () => boolean }).validate()).toBe(false)

    await wrapper.get('[data-test="channel-display-name-easypay_alipay"]').setValue('支付宝\u007f')
    expect(wrapper.text()).toContain('名称最多 100 个字符，且不能包含控制字符')
    expect((wrapper.vm as unknown as { validate: () => boolean }).validate()).toBe(false)

    await wrapper.get('[data-test="channel-display-name-easypay_alipay"]').setValue('')
    await wrapper.get('[data-test="channel-fee-rate-easypay_alipay"]').setValue('100')
    expect((wrapper.vm as unknown as { validate: () => boolean }).validate()).toBe(true)
    wrapper.unmount()
  })
})
