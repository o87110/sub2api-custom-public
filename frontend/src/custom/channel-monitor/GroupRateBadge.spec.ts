import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import GroupRateBadge from './GroupRateBadge.vue'
import {
  formatGroupRateMultiplier,
  isValidGroupRateDisplayTemplate,
  renderGroupRateDisplay,
} from './groupRate'

const locale = ref('zh-CN')

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale }),
}))

describe('GroupRateBadge', () => {
  it.each([
    [0, '0'],
    [0.18, '0.18'],
    [1.23456, '1.2346'],
    [2.5, '2.5'],
    [-0, '0'],
  ])('formats %s without unnecessary trailing zeroes', (rate, expected) => {
    expect(formatGroupRateMultiplier(rate)).toBe(expected)
  })

  it('renders an accessible compact badge in Chinese and English', async () => {
    const wrapper = mount(GroupRateBadge, { props: { rate: 0.18 } })
    const badge = wrapper.get('[data-testid="channel-monitor-group-rate"]')

    expect(badge.text()).toBe('0.18x')
    expect(badge.attributes('title')).toBe('0.18x')
    expect(badge.attributes('aria-label')).toBe('分组倍率：0.18x')
    expect(badge.classes()).toEqual(expect.arrayContaining([
      'max-w-32',
      'truncate',
      'text-xs',
      'tabular-nums',
      'dark:text-gray-200',
    ]))

    locale.value = 'en'
    await wrapper.vm.$nextTick()
    expect(badge.attributes('aria-label')).toBe('Group rate: 0.18x')
  })

  it.each([
    ['', '0.1x'],
    ['{rate}优先用', '0.1优先用'],
    ['约{rate}x', '约0.1x'],
    ['{rate}', '0.1'],
  ])('renders template %s as %s', (template, expected) => {
    expect(renderGroupRateDisplay(0.1, template)).toBe(expected)
    const wrapper = mount(GroupRateBadge, { props: { rate: 0.1, template } })
    expect(wrapper.get('[data-testid="channel-monitor-group-rate"]').text()).toBe(expected)
    expect(wrapper.get('[data-testid="channel-monitor-group-rate"]').attributes('title')).toBe(expected)
  })

  it('validates placeholder count and template length', () => {
    expect(isValidGroupRateDisplayTemplate('')).toBe(true)
    expect(isValidGroupRateDisplayTemplate('{rate}优先用')).toBe(true)
    expect(isValidGroupRateDisplayTemplate('0.1x')).toBe(false)
    expect(isValidGroupRateDisplayTemplate('{rate}/{rate}')).toBe(false)
    expect(isValidGroupRateDisplayTemplate(`${'界'.repeat(64)}{rate}`)).toBe(false)
  })

  it('does not render an invalid numeric value', () => {
    const wrapper = mount(GroupRateBadge, { props: { rate: Number.NaN } })
    expect(wrapper.find('[data-testid="channel-monitor-group-rate"]').exists()).toBe(false)
  })
})
