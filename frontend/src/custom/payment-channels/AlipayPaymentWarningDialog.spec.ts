import { enableAutoUnmount, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { createI18n } from 'vue-i18n'
import { baseCompile } from '@intlify/message-compiler'
import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'
import AlipayPaymentWarningDialog from './AlipayPaymentWarningDialog.vue'

enableAutoUnmount(afterEach)

// 测试沿用运行时版 vue-i18n，预编译真实文案以验证命名插槽。
function warningMessages(messages: typeof zh) {
  return {
    payment: {
      alipayWarning: Object.fromEntries(Object.entries(messages.payment.alipayWarning)
        .map(([key, message]) => [key, new Function(`return ${baseCompile(message, { mode: 'arrow' }).code}`)()])),
    },
  }
}

function mountDialog(locale: 'zh' | 'en' = 'zh') {
  return mount(AlipayPaymentWarningDialog, {
    attachTo: document.body,
    props: {
      show: true,
      amount: 10,
      currency: 'CNY',
      locale: 'en-US',
    },
    global: {
      plugins: [createI18n({ legacy: false, locale, messages: { en: warningMessages(en), zh: warningMessages(zh) } })],
      stubs: {
        Teleport: true,
        Transition: true,
      },
    },
  })
}

describe('AlipayPaymentWarningDialog', () => {
  it('renders the Chinese warning with individually emphasized payment instructions', () => {
    const wrapper = mountDialog()

    expect(wrapper.text()).toContain('支付宝付款必读')
    expect(wrapper.get('[role="alert"]').text()).toContain('请严格按照付款页面显示的金额支付')
    expect(wrapper.get('[role="alert"] span.block').text()).toBe('一分钱都不能差！')
    expect(wrapper.findAll('strong').map(emphasis => emphasis.text())).toEqual([
      '尾数', '¥10.00', '¥10.01 / ¥10.02', '请以付款页面显示的金额为准', '不是手续费',
    ])
    expect(wrapper.get('ol li:last-child p').text()).toBe(zh.payment.alipayWarning.mismatchWillFail)
    expect(wrapper.text()).not.toMatch(/\{\w+\}/)
  })

  it('renders the English warning using the same emphasis slots', () => {
    const wrapper = mountDialog('en')

    expect(wrapper.text()).toContain('Read Before Alipay Payment')
    expect(wrapper.get('[role="alert"] span.block').text()).toBe('The amount must match to the cent!')
    expect(wrapper.findAll('strong').map(emphasis => emphasis.text())).toEqual([
      'trailing adjustment', '¥10.00', '¥10.01 / ¥10.02',
      'Always pay the amount shown on the payment page', 'not a service fee',
    ])
    expect(wrapper.get('ol li:nth-child(2)').text()).toContain('The trailing amount is not a service fee.')
    expect(wrapper.text()).not.toMatch(/\{\w+\}/)
  })

  it('updates the emphasized amount and examples when the payable amount changes', async () => {
    const wrapper = mountDialog()

    await wrapper.setProps({ amount: 25.6 })

    expect(wrapper.findAll('strong').map(emphasis => emphasis.text())).toContain('¥25.60')
    expect(wrapper.text()).toContain('¥25.61 / ¥25.62')
    expect(wrapper.text()).not.toContain('¥10.01')
  })

  it('emits confirm only when the confirmation button is clicked', async () => {
    const wrapper = mountDialog()

    expect(wrapper.emitted('confirm')).toBeUndefined()
    await wrapper.get('button.alipay-confirm-button').trigger('click')

    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })

  it('emits cancel from the cancel and close controls', async () => {
    const wrapper = mountDialog()

    await wrapper.get('button.btn-secondary').trigger('click')
    await wrapper.get('button[aria-label="Close modal"]').trigger('click')

    expect(wrapper.emitted('cancel')).toHaveLength(2)
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('cancels on Escape and releases the scroll lock when closed', async () => {
    const wrapper = mountDialog()

    expect(document.body.classList.contains('modal-open')).toBe(true)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.emitted('confirm')).toBeUndefined()

    await wrapper.setProps({ show: false })
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })
})
