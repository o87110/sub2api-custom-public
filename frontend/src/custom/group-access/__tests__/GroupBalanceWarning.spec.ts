import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import GroupBalanceWarning from '../GroupBalanceWarning.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale: { value: 'zh-CN' } })
}))

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('GroupBalanceWarning', () => {
  it('opens from the question button and closes with Escape', async () => {
    wrapper = mount(GroupBalanceWarning, {
      props: {
        requirement: {
          groupName: '分组 A',
          minimumBalance: 100,
          currentBalance: 80,
          usableBalance: 0,
          balanceGap: 20,
          eligible: false
        }
      },
      attachTo: document.body
    })

    expect(wrapper.get('[data-test="group-balance-warning"]').text()).toContain('余额不足')
    expect(document.body.querySelector('[data-test="group-balance-warning-panel"]')).toBeNull()

    await wrapper.get('[data-test="group-balance-warning-help"]').trigger('click')
    await nextTick()

    const panel = document.body.querySelector('[data-test="group-balance-warning-panel"]')
    expect(panel?.textContent).toContain('当前余额')
    expect(panel?.textContent).toContain('$80.00')
    expect(panel?.textContent).toContain('超过 $20.00')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(document.body.querySelector('[data-test="group-balance-warning-panel"]')).toBeNull()
  })

  it('does not render a misleading zero gap for an equal balance', async () => {
    wrapper = mount(GroupBalanceWarning, {
      props: {
        requirement: {
          groupName: '分组 A',
          minimumBalance: 100,
          currentBalance: 100,
          usableBalance: 0,
          balanceGap: 0,
          eligible: false
        }
      },
      attachTo: document.body
    })

    await wrapper.get('[data-test="group-balance-warning-help"]').trigger('click')
    await nextTick()

    const text = document.body.querySelector(
      '[data-test="group-balance-warning-panel"]'
    )?.textContent
    expect(text).toContain('当前余额刚好等于门槛')
    expect(text).not.toContain('还需增加')
  })
})
