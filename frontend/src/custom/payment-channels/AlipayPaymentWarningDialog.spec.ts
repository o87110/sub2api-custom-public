import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import AlipayPaymentWarningDialog from './AlipayPaymentWarningDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => params
      ? `${key}:${Object.values(params).join('|')}`
      : key,
  }),
}))

function mountDialog() {
  return mount(AlipayPaymentWarningDialog, {
    props: {
      show: true,
      amount: 10,
      currency: 'CNY',
      locale: 'en-US',
    },
    global: {
      stubs: {
        Teleport: true,
        Transition: true,
      },
    },
  })
}

describe('AlipayPaymentWarningDialog', () => {
  it('renders the warning and dynamic tail amount examples', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).toContain('payment.alipayWarning.title')
    expect(wrapper.text()).toContain('payment.alipayWarning.alert')
    expect(wrapper.text()).toContain('10.00')
    expect(wrapper.text()).toContain('10.01')
    expect(wrapper.text()).toContain('10.02')
    expect(wrapper.get('[role="alert"]').exists()).toBe(true)
  })

  it('emits confirm only when the confirmation button is clicked', async () => {
    const wrapper = mountDialog()

    expect(wrapper.emitted('confirm')).toBeUndefined()
    await wrapper.get('button.btn-primary').trigger('click')

    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })

  it('emits cancel from the cancel and close controls', async () => {
    const wrapper = mountDialog()

    await wrapper.get('button.btn-secondary').trigger('click')
    await wrapper.get('button[aria-label="Close modal"]').trigger('click')

    expect(wrapper.emitted('cancel')).toHaveLength(2)
  })
})
